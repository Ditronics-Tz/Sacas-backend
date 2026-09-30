package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"go_boilerplate/internal/auth"
	"go_boilerplate/internal/middlewares"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

// Onboarding errors, kept distinct so the handler can map each to a status code
// without string matching.
var (
	// ErrInstitutionNameTaken is returned when the derived or supplied slug is
	// already in use.
	ErrInstitutionNameTaken = errors.New("an institution with this name or slug already exists")
	// ErrEmailInUse is returned when the administrator's address is taken.
	ErrEmailInUse = errors.New("an account with this email already exists")
	// ErrOnboardingNotAllowed is returned for a payload the caller may not
	// create, such as asking for a role it cannot grant.
	ErrOnboardingNotAllowed = errors.New("this account may not be created with those details")
	// ErrInvitationInvalid is returned for any unusable invitation. One error
	// for not-found, expired, revoked, and already-accepted, so the response
	// cannot be used to probe which it was.
	ErrInvitationInvalid = errors.New("this invitation is no longer valid")
)

// RegisterInstitutionRequest is the public signup payload.
type RegisterInstitutionRequest struct {
	InstitutionName string `json:"institution_name" binding:"required,min=2,max=150"`
	InstitutionType string `json:"institution_type" binding:"required"`
	Region          string `json:"region,omitempty"`
	Phone           string `json:"phone,omitempty"`
	Address         string `json:"address,omitempty"`
	Country         string `json:"country,omitempty"`

	AdminEmail     string `json:"admin_email" binding:"required,email"`
	AdminPassword  string `json:"admin_password" binding:"required,min=8,strongpassword"`
	AdminFirstName string `json:"admin_first_name" binding:"required,min=2,max=50"`
	AdminLastName  string `json:"admin_last_name" binding:"required,min=2,max=50"`
	AdminPhone     string `json:"admin_phone,omitempty"`
}

// OnboardingResult is what a successful signup returns.
type OnboardingResult struct {
	Institution *models.Institution
	Admin       *models.User
}

// OnboardingService creates institutions and the accounts that belong to them.
//
// Everything that must not half-happen lives here rather than in the handler,
// so the transaction boundary is in one place.
type OnboardingService struct {
	db              *gorm.DB
	institutionRepo repositories.InstitutionRepository
	userRepo        repositories.UserRepository
	audit           *AuditRecorder
}

func NewOnboardingService(
	db *gorm.DB,
	institutionRepo repositories.InstitutionRepository,
	userRepo repositories.UserRepository,
	audit *AuditRecorder,
) *OnboardingService {
	return &OnboardingService{
		db:              db,
		institutionRepo: institutionRepo,
		userRepo:        userRepo,
		audit:           audit,
	}
}

// RegisterInstitution creates a pending institution and its first
// administrator in one transaction.
//
// Transactional because a half-created signup is the worst outcome available: an
// institution with no administrator is unreachable, and an administrator with no
// institution cannot sign in at all. Either row alone is useless, so both land or
// neither does.
//
// The institution is created PENDING, not active. Approval is a platform
// decision (see ApprovalPolicy below).
func (s *OnboardingService) RegisterInstitution(req RegisterInstitutionRequest) (*OnboardingResult, error) {
	institutionType := models.InstitutionType(req.InstitutionType)
	if !institutionType.IsValid() {
		return nil, fmt.Errorf("%w: unknown institution type %q", ErrOnboardingNotAllowed, req.InstitutionType)
	}

	slug := middlewares.Slugify(req.InstitutionName)
	if slug == "" {
		return nil, fmt.Errorf("%w: could not derive a slug from the institution name", ErrOnboardingNotAllowed)
	}

	// Reject a taken slug up front so the caller gets a clear conflict rather
	// than a raw unique-constraint error.
	if existing, err := s.institutionRepo.GetBySlug(slug); err == nil && existing != nil {
		return nil, ErrInstitutionNameTaken
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	country := req.Country
	if country == "" {
		country = "Tanzania"
	}

	institution := &models.Institution{
		Name:    req.InstitutionName,
		Slug:    slug,
		Type:    institutionType,
		Region:  req.Region,
		Country: country,
		Phone:   req.Phone,
		Address: req.Address,
		// Pending: the platform activates it. A free public signup that
		// activated immediately would let anyone mint a usable tenant.
		Status: models.InstitutionStatusPending,
		Plan:   models.InstitutionPlanFree,
	}
	admin := &models.User{
		Email:       strings.ToLower(strings.TrimSpace(req.AdminEmail)),
		Password:    string(hashedPassword),
		FirstName:   req.AdminFirstName,
		LastName:    req.AdminLastName,
		PhoneNumber: req.AdminPhone,
		// The first account of a new institution administers it. This is the
		// one place an administrator is created without a platform decision,
		// and it is safe because the institution cannot be used until approved.
		Role:       models.RoleAdmin,
		IsActive:   false, // Activated on email verification.
		IsVerified: false,
	}

	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(institution).Error; err != nil {
			return fmt.Errorf("create institution: %w", err)
		}
		// The administrator is bound to the institution just created. Setting
		// this inside the transaction is what guarantees the two are consistent.
		admin.InstitutionID = &institution.ID
		if err := tx.Create(admin).Error; err != nil {
			return fmt.Errorf("create administrator: %w", err)
		}
		return nil
	})
	if err != nil {
		// A duplicate email surfaces here as a constraint violation; translate
		// it so the caller gets a useful message.
		if isUniqueViolation(err) {
			if _, lookupErr := s.userRepo.GetByEmail(admin.Email); lookupErr == nil {
				return nil, ErrEmailInUse
			}
		}
		return nil, err
	}

	// The audit row is written after the commit, so a failed audit can never
	// roll back a completed signup.
	if s.audit != nil {
		s.audit.RecordSystem(
			models.AuditInstitutionCreate, &institution.ID,
			"institution", fmt.Sprint(institution.ID), institution.Name,
			map[string]any{
				"slug":          institution.Slug,
				"type":          string(institution.Type),
				"status":        string(institution.Status),
				"admin_email":   admin.Email,
				"admin_user_id": admin.ID,
				"source":        "public_registration",
			},
		)
	}

	logger.Info("Institution registered: %s (ID %d, status %s, admin %s)",
		institution.Name, institution.ID, institution.Status, admin.Email)

	return &OnboardingResult{Institution: institution, Admin: admin}, nil
}

// ApprovalPolicy documents the decision this service implements.
//
//	pending → active, by a platform super_admin.
//
// The alternative — instant activation — was rejected because a public signup
// endpoint that mints a usable tenant for free is trivially automated, and
// because approval is where a platform verifies an institution is real before it
// gets staff, rooms, and curriculum data. The cost is that a genuine institution
// waits for a human, which is the right trade at this stage: onboarding friction
// is recoverable, an unvetted tenant holding data is not.
const ApprovalPolicy = "pending -> active, approved by a platform super_admin"

// AcceptInvitation turns a valid invitation into a user account, in one
// transaction.
//
// The invitation is marked accepted with a conditional UPDATE inside the same
// transaction that creates the account, so a replayed token cannot create a
// second account even under concurrent requests.
func (s *OnboardingService) AcceptInvitation(
	invitationRepo repositories.InvitationRepository,
	token string,
	password, firstName, lastName, phone string,
) (*models.User, error) {
	hash := HashInvitationToken(token)

	invitation, err := invitationRepo.GetByTokenHash(hash)
	if err != nil {
		return nil, ErrInvitationInvalid
	}

	now := time.Now().UTC()
	if !invitation.IsUsable(now) {
		// One error for every unusable state, so the response cannot be used to
		// discover whether a token once existed.
		return nil, ErrInvitationInvalid
	}

	// An invitation binds one address. Reject if that address already has an
	// account, rather than silently attaching the invitation to someone else.
	if existing, err := s.userRepo.GetByEmail(invitation.Email); err == nil && existing != nil {
		return nil, ErrEmailInUse
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &models.User{
		Email:       invitation.Email,
		Password:    string(hashedPassword),
		FirstName:   firstName,
		LastName:    lastName,
		PhoneNumber: phone,
		// The invited role, which the invitation path restricts to `user`.
		Role:          invitation.Role,
		IsActive:      true, // No email gate: the invitation already proved control of the address.
		IsVerified:    true,
		InstitutionID: &invitation.InstitutionID,
	}

	err = s.db.Transaction(func(tx *gorm.DB) error {
		// Conditional update first: if this affects no rows, another request
		// already accepted it, and creating the account here would leave two
		// accounts for one invitation.
		accepted, err := markAcceptedTx(tx, invitation.ID, user, now)
		if err != nil {
			return err
		}
		if !accepted {
			return ErrInvitationInvalid
		}
		// Link the accepted user to the invitation row in the same transaction.
		if err := tx.Model(&models.InstitutionInvitation{}).
			Where("id = ?", invitation.ID).
			Update("accepted_user_id", user.ID).Error; err != nil {
			return fmt.Errorf("link invitation: %w", err)
		}
		if err := tx.Create(user).Error; err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrInvitationInvalid) {
			return nil, ErrInvitationInvalid
		}
		if isUniqueViolation(err) {
			return nil, ErrEmailInUse
		}
		return nil, err
	}

	if s.audit != nil {
		s.audit.RecordSystem(
			models.AuditUserCreate, &invitation.InstitutionID,
			"user", fmt.Sprint(user.ID), user.Email,
			map[string]any{
				"role":          string(user.Role),
				"source":        "invitation",
				"invitation_id": invitation.ID,
			},
		)
	}

	logger.Info("Invitation accepted by %s into institution %d", user.Email, invitation.InstitutionID)
	return user, nil
}

// markAcceptedTx performs the conditional acceptance inside a transaction,
// using the same predicate as the repository method but on the transaction's
// handle.
func markAcceptedTx(tx *gorm.DB, invitationID uint, user *models.User, at time.Time) (bool, error) {
	var existing models.InstitutionInvitation
	if err := tx.Where("id = ?", invitationID).First(&existing).Error; err != nil {
		return false, ErrInvitationInvalid
	}
	res := tx.Model(&models.InstitutionInvitation{}).
		Where("id = ?", invitationID).
		Where("accepted_at IS NULL AND revoked_at IS NULL AND expires_at > ?", at).
		Update("accepted_at", at)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// --- Invitation token handling -----------------------------------------------

// GenerateInvitationToken returns a fresh plaintext token and its stored hash.
//
// The plaintext is returned to the caller exactly once. 32 bytes from crypto/rand
// base64url-encoded: a 192-bit token is not brute-forceable within the
// invitation's lifetime.
func GenerateInvitationToken() (plaintext string, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate invitation token: %w", err)
	}
	plaintext = base64.RawURLEncoding.EncodeToString(raw)
	return plaintext, HashInvitationToken(plaintext), nil
}

// HashInvitationToken digests a token for storage and lookup.
//
// SHA-256 is appropriate here and not a password-hashing miss: the token has 192
// bits of entropy, so there is no dictionary to attack and the lookup must be a
// plain index match. A slow KDF would only add latency to a high-entropy secret.
func HashInvitationToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// isUniqueViolation reports whether an error is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505). GORM wraps the driver error, so the message is
// checked as well as the typed errors.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "23505") ||
		strings.Contains(msg, "duplicate key value") ||
		strings.Contains(msg, "unique constraint")
}

// CanInvite reports whether a caller may issue an invitation for a role.
//
// Two independent conditions, and both are required:
//
//   - The caller must be able to administer users (PermUserWrite). Role
//     assignability alone is NOT enough: auth.AssignableRoles returns `user` for
//     every non-platform caller, so a lecturer technically "may assign" a user
//     and would pass an assignability-only check. Issuing an invitation is an
//     unauthenticated path into account creation, so it is gated on the same
//     permission the direct member-creation route uses.
//   - The target role must be delegable, per models.IsDelegableRole. This is the
//     same rule direct assignment enforces, so an invitation cannot become a path
//     around it.
func CanInvite(callerRole models.UserRole, targetRole models.UserRole) bool {
	if !auth.CanAdminister(callerRole) {
		return false
	}
	if !models.IsDelegableRole(targetRole) {
		return false
	}
	return auth.CanAssign(callerRole, targetRole)
}
