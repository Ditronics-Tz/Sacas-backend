package services

import (
	"errors"
	"fmt"
	"time"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

// Invitation issuance errors.
var (
	// ErrInviteRoleNotDelegable is returned when an institution tries to invite
	// someone into a role it may not grant.
	ErrInviteRoleNotDelegable = errors.New("an institution cannot invite a user into this role")
	// ErrTooManyPendingInvitations is returned when an institution exceeds its
	// outstanding-invitation cap.
	ErrTooManyPendingInvitations = errors.New("too many outstanding invitations for this institution")
	// ErrInvitationAlreadyPending is returned when an address already has a
	// live invitation. The caller should resend rather than create a duplicate.
	ErrInvitationAlreadyPending = errors.New("an invitation is already outstanding for this address")
)

// InvitationLimits bound how many invitations an institution may hold open.
//
// A cap matters because an invitation is an unauthenticated path into creating
// an account: without a limit, a compromised institution admin could issue
// thousands and the abuse would only surface at scale.
type InvitationLimits struct {
	// MaxPendingPerInstitution is the cap on outstanding invitations.
	MaxPendingPerInstitution int64
	// TTL is how long a new invitation is valid.
	TTL time.Duration
	// ResendCooldown is the minimum gap between resends of the same invitation.
	ResendCooldown time.Duration
}

// DefaultInvitationLimits is the starting configuration.
func DefaultInvitationLimits() InvitationLimits {
	return InvitationLimits{
		MaxPendingPerInstitution: 200,
		TTL:                      models.DefaultInvitationTTL,
		ResendCooldown:           60 * time.Second,
	}
}

// InvitationService issues and revokes invitations.
type InvitationService struct {
	repo   repositories.InvitationRepository
	audit  *AuditRecorder
	limits InvitationLimits
	// clock is injectable so TTL and cooldown are testable without sleeping.
	clock func() time.Time
}

func NewInvitationService(repo repositories.InvitationRepository, audit *AuditRecorder, limits InvitationLimits) *InvitationService {
	if limits.TTL <= 0 {
		limits.TTL = models.DefaultInvitationTTL
	}
	return &InvitationService{
		repo:   repo,
		audit:  audit,
		limits: limits,
		clock:  func() time.Time { return time.Now().UTC() },
	}
}

// IssuedInvitation carries the plaintext token back to the inviter. The token is
// never retrievable again — only its hash is stored.
type IssuedInvitation struct {
	Invitation *models.InstitutionInvitation
	Token      string
	// Resent is true when an existing outstanding invitation was reissued rather
	// than a new row created, so the caller can word the response accordingly.
	Resent bool
}

// Issue creates an invitation, or reissues an existing live one for the same
// address.
//
// A repeat invite reuses the row instead of creating a second one, so an
// institution cannot accumulate unlimited live invitations for one address.
func (s *InvitationService) Issue(
	institutionID uint,
	inviterID uint,
	email string,
	role models.UserRole,
) (*IssuedInvitation, error) {
	// Reuse the RBAC rule rather than restating it: an invitation must not be a
	// path around the role restrictions that direct assignment enforces.
	if !CanInvite(models.RoleAdmin, role) {
		return nil, ErrInviteRoleNotDelegable
	}

	now := s.clock()

	pending, err := s.repo.CountActiveForInstitution(institutionID, now)
	if err != nil {
		return nil, fmt.Errorf("count pending invitations: %w", err)
	}
	if s.limits.MaxPendingPerInstitution > 0 && pending >= s.limits.MaxPendingPerInstitution {
		return nil, ErrTooManyPendingInvitations
	}

	// Reissue an existing live invitation rather than duplicating it.
	existing, err := s.repo.GetPendingForEmail(institutionID, email, now)
	if err != nil {
		return nil, fmt.Errorf("look up existing invitation: %w", err)
	}
	if len(existing) > 0 {
		// A resend inside the cooldown is refused, so the endpoint cannot be
		// used to spam one mailbox.
		if s.limits.ResendCooldown > 0 && existing[0].LastSentAt != nil &&
			now.Sub(*existing[0].LastSentAt) < s.limits.ResendCooldown {
			return nil, fmt.Errorf("%w: wait %s before resending",
				ErrInvitationAlreadyPending, s.limits.ResendCooldown)
		}
		token, hash, err := GenerateInvitationToken()
		if err != nil {
			return nil, err
		}
		if err := s.repo.Create(&models.InstitutionInvitation{
			InstitutionID: institutionID,
			Email:         email,
			Role:          role,
			TokenHash:     hash,
			InvitedByID:   inviterID,
			ExpiresAt:     now.Add(s.limits.TTL),
			LastSentAt:    &now,
		}); err != nil {
			return nil, fmt.Errorf("reissue invitation: %w", err)
		}
		// Revoke the superseded row so only one live invitation exists for the
		// address; a token from the old copy must not keep working.
		if _, err := s.repo.Revoke(institutionID, existing[0].ID, now); err != nil {
			logger.Warn("Failed to revoke superseded invitation %d: %v", existing[0].ID, err)
		}
		logger.Info("Invitation reissued for %s into institution %d", email, institutionID)
		return &IssuedInvitation{Token: token, Resent: true}, nil
	}

	token, hash, err := GenerateInvitationToken()
	if err != nil {
		return nil, err
	}
	invitation := &models.InstitutionInvitation{
		InstitutionID: institutionID,
		Email:         email,
		Role:          role,
		TokenHash:     hash,
		InvitedByID:   inviterID,
		ExpiresAt:     now.Add(s.limits.TTL),
		LastSentAt:    &now,
	}
	if err := s.repo.Create(invitation); err != nil {
		return nil, fmt.Errorf("create invitation: %w", err)
	}

	logger.Info("Invitation issued for %s into institution %d (id %d)", email, institutionID, invitation.ID)
	return &IssuedInvitation{Invitation: invitation, Token: token}, nil
}

// Revoke withdraws an outstanding invitation. An already-accepted invitation
// cannot be revoked, which is reported as false rather than an error.
func (s *InvitationService) Revoke(institutionID, id uint) (bool, error) {
	ok, err := s.repo.Revoke(institutionID, id, s.clock())
	if err != nil {
		return false, fmt.Errorf("revoke invitation: %w", err)
	}
	return ok, nil
}
