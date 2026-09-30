package controllers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"go_boilerplate/internal/auth"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
	"go_boilerplate/pkg/security"
)

// InstitutionWorkspaceController serves an institution's own profile, its
// members, and its invitations.
//
// Every route here is scoped to the caller's institution by TenantMiddleware, so
// the institution ID is read from the context and never from the request. A
// platform super_admin is refused by RequireInstitutionWorkspace, and a support
// session is refused by the route's permission gate, which keeps both out of
// every write path.
type InstitutionWorkspaceController struct {
	institutionRepo repositories.InstitutionRepository
	userRepo        repositories.UserRepository
	staffRepo       repositories.StaffRepository
	invitationRepo  repositories.InvitationRepository
	invitations     *services.InvitationService
	audit           *services.AuditRecorder
}

func NewInstitutionWorkspaceController(
	institutionRepo repositories.InstitutionRepository,
	userRepo repositories.UserRepository,
	staffRepo repositories.StaffRepository,
	invitationRepo repositories.InvitationRepository,
	invitations *services.InvitationService,
	audit *services.AuditRecorder,
) *InstitutionWorkspaceController {
	return &InstitutionWorkspaceController{
		institutionRepo: institutionRepo,
		userRepo:        userRepo,
		staffRepo:       staffRepo,
		invitationRepo:  invitationRepo,
		invitations:     invitations,
		audit:           audit,
	}
}

// --- Own profile ------------------------------------------------------------

// Get handles GET /api/protected/institution — the caller's own institution.
func (c *InstitutionWorkspaceController) Get(ctx *gin.Context) {
	inst := tenantID(ctx)

	institution, err := c.institutionRepo.GetByID(inst)
	if err != nil {
		respondRepoError(ctx, "Institution not found", err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"institution": institution,
		"scope":       "institution",
	})
}

type UpdateInstitutionProfileRequest struct {
	Name    *string `json:"name,omitempty" binding:"omitempty,min=2,max=150"`
	Region  *string `json:"region,omitempty"`
	Phone   *string `json:"phone,omitempty"`
	Address *string `json:"address,omitempty"`
	LogoURL *string `json:"logo_url,omitempty"`
	// Email is deliberately not editable here: the contact address doubles as
	// the institution's identity in transactional mail, so changing it is a
	// platform operation.
	Email *string `json:"email,omitempty" binding:"omitempty,email"`
}

// Update handles PUT /api/protected/institution.
//
// An institution may change its presentation — name, region, address, phone,
// logo — but NOT its own status or plan. Those are platform decisions: an
// institution suspending itself would let it vanish from the platform's view,
// and an institution upgrading itself would let it grant itself paid features.
// A request naming either is rejected with an explanation rather than silently
// ignored, so a client that expects it to work learns why it does not.
func (c *InstitutionWorkspaceController) Update(ctx *gin.Context) {
	inst := tenantID(ctx)

	institution, err := c.institutionRepo.GetByID(inst)
	if err != nil {
		respondRepoError(ctx, "Institution not found", err)
		return
	}

	var raw map[string]any
	if err := ctx.ShouldBindJSON(&raw); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}
	if _, ok := raw["status"]; ok {
		ctx.JSON(http.StatusForbidden, gin.H{
			"error":   "Institution status is managed by the platform",
			"details": "Ask your platform administrator to activate or suspend the institution.",
		})
		return
	}
	if _, ok := raw["plan"]; ok {
		ctx.JSON(http.StatusForbidden, gin.H{"error": "Institution plan is managed by the platform"})
		return
	}

	var req UpdateInstitutionProfileRequest
	if err := decodeFrom(raw, &req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	changed := map[string]any{}
	if req.Name != nil {
		institution.Name = *req.Name
		changed["name"] = *req.Name
	}
	if req.Region != nil {
		institution.Region = *req.Region
		changed["region"] = *req.Region
	}
	if req.Phone != nil {
		institution.Phone = *req.Phone
		changed["phone"] = *req.Phone
	}
	if req.Address != nil {
		institution.Address = *req.Address
		changed["address"] = *req.Address
	}
	if req.LogoURL != nil {
		institution.LogoURL = *req.LogoURL
		changed["logo_url"] = *req.LogoURL
	}
	if req.Email != nil {
		institution.Email = *req.Email
		changed["email"] = *req.Email
	}

	if len(changed) == 0 {
		ctx.JSON(http.StatusOK, gin.H{"message": "No changes supplied", "institution": institution})
		return
	}

	if err := c.institutionRepo.Update(institution); err != nil {
		logger.Error("Failed to update institution %d: %v", inst, err)
		respondRepoError(ctx, "Institution not found", err)
		return
	}

	c.audit.Record(ctx, models.AuditInstitutionUpdate, "institution",
		uintString(institution.ID), institution.Name, map[string]any{
			"self_service": true,
			"changed":      changed,
		})

	ctx.JSON(http.StatusOK, gin.H{"message": "Institution updated", "institution": institution})
}

// --- Members ----------------------------------------------------------------

// memberView is a member plus what the UI needs to render the row and decide
// which actions to offer.
type memberView struct {
	*models.User
	// StaffID is the linked staff record, or nil when the member has none.
	StaffID *uint `json:"staff_id,omitempty"`
	// StaffName is denormalised so the list needs no second call per row.
	StaffName string `json:"staff_name,omitempty"`
	// CanManage reflects whether the CALLER may modify this member, so the UI
	// does not reimplement the role rules.
	CanManage bool `json:"can_manage"`
}

// ListMembers handles GET /api/protected/institution/members.
func (c *InstitutionWorkspaceController) ListMembers(ctx *gin.Context) {
	inst := tenantID(ctx)
	limit, offset := parsePagination(ctx)

	users, err := c.userRepo.GetAll(inst, limit, offset)
	if err != nil {
		logger.Error("Failed to list institution members: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list members"})
		return
	}
	total, err := c.userRepo.CountByInstitution(inst)
	if err != nil {
		logger.Warn("Failed to count institution members: %v", err)
	}

	members := make([]memberView, 0, len(users))
	for i := range users {
		members = append(members, c.memberView(ctx, inst, &users[i]))
	}

	ctx.JSON(http.StatusOK, gin.H{
		"members": members,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// GetMember handles GET /api/protected/institution/members/:id.
func (c *InstitutionWorkspaceController) GetMember(ctx *gin.Context) {
	inst := tenantID(ctx)

	member, ok := c.resolveMember(ctx, inst)
	if !ok {
		return
	}

	ctx.JSON(http.StatusOK, gin.H{"member": c.memberView(ctx, inst, member)})
}

type CreateMemberRequest struct {
	Email     string `json:"email" binding:"required,email"`
	Password  string `json:"password" binding:"required,min=8,strongpassword"`
	FirstName string `json:"first_name" binding:"required,min=2,max=50"`
	LastName  string `json:"last_name" binding:"required,min=2,max=50"`
	Phone     string `json:"phone,omitempty"`
	// Role is optional and defaults to `user`. It is checked against the same
	// rule the RBAC layer applies, so this endpoint cannot be used to mint an
	// administrator or a coordinator.
	Role string `json:"role,omitempty"`
	// StaffID links the new member to an existing staff record in this
	// institution, so they can be scheduled immediately.
	StaffID *uint `json:"staff_id,omitempty"`
}

// CreateMember handles POST /api/protected/institution/members.
//
// Two ways to add someone, because both are real workflows:
//
//	SendInvitation=true  → issue an invitation; the invitee sets their own
//	                       password via the public accept endpoint. Preferred,
//	                       because the password never crosses the network twice
//	                       and the institution never holds a password it did not
//	                       choose.
//	password supplied     → direct creation. Still permitted for bulk seeding
//	                       and for an institution that manages passwords itself.
func (c *InstitutionWorkspaceController) CreateMember(ctx *gin.Context) {
	inst := tenantID(ctx)

	var raw map[string]any
	if err := ctx.ShouldBindJSON(&raw); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}
	if sendInvite, _ := raw["send_invitation"].(bool); sendInvite {
		var req CreateInvitationRequest
		if err := decodeFrom(raw, &req); err != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
			return
		}
		c.issueInvitation(ctx, inst, req, "member_signup")
		return
	}

	var req CreateMemberRequest
	if err := decodeFrom(raw, &req); err != nil {
		if strings.Contains(err.Error(), "assword") {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": security.PasswordPolicyMessage})
			return
		}
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}
	if !security.ValidPassword(req.Password) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": security.PasswordPolicyMessage})
		return
	}

	role := models.RoleUser
	if req.Role != "" {
		role = models.UserRole(req.Role)
	}
	// The RBAC rule, not a local copy: an institution admin may only create
	// plain users.
	if !auth.CanAssign(models.UserRole(ctx.GetString("role")), role) {
		c.audit.RecordDenied(ctx, models.AuditUserCreate, "user", "", req.Email,
			"caller may not assign role "+string(role))
		ctx.JSON(http.StatusForbidden, gin.H{
			"error":          "You may not assign this role",
			"assignable":     roleStrings(auth.AssignableRoles(models.UserRole(ctx.GetString("role")))),
			"requested_role": string(role),
		})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if existing, err := c.userRepo.GetByEmail(email); err == nil && existing != nil {
		ctx.JSON(http.StatusConflict, gin.H{"error": "An account with this email already exists"})
		return
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("Failed to hash member password: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create member"})
		return
	}

	member := &models.User{
		Email:         email,
		Password:      string(hashed),
		FirstName:     req.FirstName,
		LastName:      req.LastName,
		PhoneNumber:   req.Phone,
		Role:          role,
		IsActive:      true, // Created by an administrator inside a verified institution.
		IsVerified:    true,
		InstitutionID: &inst,
	}
	if err := c.userRepo.Create(member); err != nil {
		logger.Error("Failed to create member: %v", err)
		ctx.JSON(http.StatusConflict, gin.H{"error": "Email already exists or creation failed"})
		return
	}

	if req.StaffID != nil && c.staffRepo != nil {
		if err := c.staffRepo.SetUserLink(inst, *req.StaffID, &member.ID); err != nil {
			// The member exists; the link is the part that failed. Report it
			// rather than pretending the whole thing worked, but do not delete a
			// valid account over a failed convenience link.
			logger.Error("Created member %d but could not link staff %d: %v", member.ID, *req.StaffID, err)
			ctx.JSON(http.StatusCreated, gin.H{
				"message": "Member created, but the staff link could not be applied",
				"member":  c.memberView(ctx, inst, member),
				"warning": "The specified staff record may belong to another institution or may already be linked.",
			})
			return
		}
	}

	c.audit.Record(ctx, models.AuditUserCreate, "user", uintString(member.ID), member.Email, map[string]any{
		"assigned_role":  string(member.Role),
		"source":         "institution_member",
		"institution_id": inst,
	})

	ctx.JSON(http.StatusCreated, gin.H{"message": "Member created", "member": c.memberView(ctx, inst, member)})
}

type PatchMemberRequest struct {
	FirstName *string `json:"first_name,omitempty" binding:"omitempty,min=2,max=50"`
	LastName  *string `json:"last_name,omitempty" binding:"omitempty,min=2,max=50"`
	Phone     *string `json:"phone,omitempty"`
	Email     *string `json:"email,omitempty" binding:"omitempty,email"`
	IsActive  *bool   `json:"is_active,omitempty"`
	Role      *string `json:"role,omitempty"`
	// StaffID links this member to a staff record in the same institution.
	StaffID *uint `json:"staff_id,omitempty"`
	// ClearStaffID unlinks the member from its staff record.
	ClearStaffID bool `json:"clear_staff_id,omitempty"`
}

// PatchMember handles PATCH /api/protected/institution/members/:id.
func (c *InstitutionWorkspaceController) PatchMember(ctx *gin.Context) {
	inst := tenantID(ctx)

	member, ok := c.resolveMember(ctx, inst)
	if !ok {
		return
	}
	callerID, _ := currentUserID(ctx)
	callerRole := models.UserRole(ctx.GetString("role"))

	var req PatchMemberRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	// Self-demotion guard. An administrator downgrading themselves could leave
	// the institution with nobody able to manage it, and the request would then
	// be unfixable from inside the institution.
	if member.ID == callerID && req.Role != nil && models.UserRole(*req.Role) != member.Role {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":   "You cannot change your own role",
			"details": "Ask another administrator or the platform to do it.",
		})
		return
	}

	previousRole := member.Role
	previousActive := member.IsActive

	if req.Role != nil {
		newRole := models.UserRole(*req.Role)
		if !newRole.IsValid() {
			ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role"})
			return
		}
		// An institution admin may not promote anyone, including themselves.
		if !auth.CanAssign(callerRole, newRole) {
			c.audit.RecordDenied(ctx, models.AuditRoleChange, "user", uintString(member.ID), member.Email,
				"caller "+string(callerRole)+" may not assign role "+string(newRole))
			ctx.JSON(http.StatusForbidden, gin.H{
				"error":          "You may not assign this role",
				"assignable":     roleStrings(auth.AssignableRoles(callerRole)),
				"requested_role": string(newRole),
			})
			return
		}
		// A platform account is never manageable from an institution.
		if newRole == models.RoleSuperAdmin {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":   "A super_admin must not be bound to an institution",
				"details": "Platform accounts are created by the platform, not by an institution.",
			})
			return
		}
		if newRole != previousRole {
			// Last-admin guard: do not let the institution lose its only
			// administrator, which would leave it unable to manage itself.
			if previousRole == models.RoleAdmin && newRole != models.RoleAdmin {
				if remaining, err := c.countOtherAdmins(inst, member.ID); err == nil && remaining == 0 {
					ctx.JSON(http.StatusBadRequest, gin.H{
						"error":   "This is the institution's only administrator",
						"details": "Promote another member to administrator first.",
					})
					return
				}
			}
			member.Role = newRole
		}
	}

	if req.FirstName != nil {
		member.FirstName = *req.FirstName
	}
	if req.LastName != nil {
		member.LastName = *req.LastName
	}
	if req.Phone != nil {
		member.PhoneNumber = *req.Phone
	}
	if req.Email != nil {
		newEmail := strings.ToLower(strings.TrimSpace(*req.Email))
		if existing, err := c.userRepo.GetByEmail(newEmail); err == nil && existing != nil && existing.ID != member.ID {
			ctx.JSON(http.StatusConflict, gin.H{"error": "An account with this email already exists"})
			return
		}
		member.Email = newEmail
	}

	// Deactivating yourself would end your session with no way back in.
	if req.IsActive != nil && !*req.IsActive && member.ID == callerID {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "You cannot deactivate your own account"})
		return
	}
	if req.IsActive != nil {
		member.IsActive = *req.IsActive
	}

	if err := c.userRepo.Update(inst, member); err != nil {
		logger.Error("Failed to update member %d: %v", member.ID, err)
		respondRepoError(ctx, "Member not found", err)
		return
	}

	if err := c.applyStaffLink(ctx, inst, member, req); err != nil {
		// The member update already committed; report the link failure without
		// pretending the whole request succeeded.
		ctx.JSON(http.StatusOK, gin.H{
			"message": "Member updated, but the staff link could not be applied",
			"member":  c.memberView(ctx, inst, member),
			"warning": err.Error(),
		})
		return
	}

	if member.Role != previousRole {
		c.audit.Record(ctx, models.AuditRoleChange, "user", uintString(member.ID), member.Email, map[string]any{
			"from": string(previousRole),
			"to":   string(member.Role),
		})
	}
	if member.IsActive != previousActive {
		action := models.AuditUserActivate
		if !member.IsActive {
			action = models.AuditUserSuspend
		}
		c.audit.Record(ctx, action, "user", uintString(member.ID), member.Email, map[string]any{
			"is_active": member.IsActive,
		})
	}

	ctx.JSON(http.StatusOK, gin.H{"message": "Member updated", "member": c.memberView(ctx, inst, member)})
}

// DeleteMember handles DELETE /api/protected/institution/members/:id.
//
// The removal is recorded in the audit log, and the member's Staff record is
// left in place: unlinking is automatic (the user_id column becomes NULL), and
// the teaching history attached to that staff record is worth keeping.
func (c *InstitutionWorkspaceController) DeleteMember(ctx *gin.Context) {
	inst := tenantID(ctx)

	member, ok := c.resolveMember(ctx, inst)
	if !ok {
		return
	}
	callerID, _ := currentUserID(ctx)
	if member.ID == callerID {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "You cannot remove your own account"})
		return
	}

	// Do not leave the institution with no administrator.
	if member.Role == models.RoleAdmin {
		if remaining, err := c.countOtherAdmins(inst, member.ID); err == nil && remaining == 0 {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":   "This is the institution's only administrator",
				"details": "Promote another member to administrator before removing this account.",
			})
			return
		}
	}

	deletedEmail := member.Email
	deletedRole := member.Role

	if err := c.userRepo.Delete(inst, member.ID); err != nil {
		logger.Error("Failed to delete member %d: %v", member.ID, err)
		respondRepoError(ctx, "Member not found", err)
		return
	}

	c.audit.Record(ctx, models.AuditUserDelete, "user", uintString(member.ID), deletedEmail, map[string]any{
		"role":     string(deletedRole),
		"severity": "high",
		"source":   "institution_member",
	})

	ctx.JSON(http.StatusOK, gin.H{"message": "Member removed"})
}

// --- Invitations ------------------------------------------------------------

type CreateInvitationRequest struct {
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role,omitempty"`
	// Note is stored for the operator's own records. It is not sent to the
	// invitee, so it is safe to leave internal context in it.
	Note string `json:"note,omitempty"`
}

// CreateInvitation handles POST /api/protected/institution/invitations.
func (c *InstitutionWorkspaceController) CreateInvitation(ctx *gin.Context) {
	inst := tenantID(ctx)

	var req CreateInvitationRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}
	c.issueInvitation(ctx, inst, req, "invitation")
}

// ListInvitations handles GET /api/protected/institution/invitations.
func (c *InstitutionWorkspaceController) ListInvitations(ctx *gin.Context) {
	inst := tenantID(ctx)
	limit, offset := parsePagination(ctx)

	invitations, err := c.invitationRepo.GetAll(inst, limit, offset)
	if err != nil {
		logger.Error("Failed to list invitations: %v", err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list invitations"})
		return
	}

	now := time.Now().UTC()
	views := make([]invitationView, 0, len(invitations))
	for i := range invitations {
		views = append(views, invitationViewOf(&invitations[i], now))
	}
	ctx.JSON(http.StatusOK, gin.H{"invitations": views, "limit": limit, "offset": offset})
}

// RevokeInvitation handles DELETE /api/protected/institution/invitations/:id.
func (c *InstitutionWorkspaceController) RevokeInvitation(ctx *gin.Context) {
	inst := tenantID(ctx)

	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid invitation ID"})
		return
	}

	ok, err := c.invitations.Revoke(inst, id)
	if err != nil {
		logger.Error("Failed to revoke invitation %d: %v", id, err)
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to revoke invitation"})
		return
	}
	// An already-accepted invitation cannot be revoked; that is a state
	// conflict, not a missing record.
	if !ok {
		ctx.JSON(http.StatusConflict, gin.H{
			"error":   "This invitation cannot be revoked",
			"details": "It may have already been accepted.",
		})
		return
	}

	c.audit.Record(ctx, models.AuditUserDelete, "invitation", uintString(id), "", map[string]any{
		"operation": "revoke_invitation",
	})

	ctx.JSON(http.StatusOK, gin.H{"message": "Invitation revoked"})
}

// invitationView is an invitation plus its derived status, so the UI does not
// have to compute expiry itself and cannot disagree with the server.
type invitationView struct {
	*models.InstitutionInvitation
	Status string `json:"status"`
	// AcceptURL is a relative path the inviter can share. The token itself is
	// NOT included: only the issuer sees it, in the create response.
	AcceptPath string `json:"accept_path"`
}

func invitationViewOf(inv *models.InstitutionInvitation, now time.Time) invitationView {
	return invitationView{
		InstitutionInvitation: inv,
		Status:                string(inv.Status(now)),
		AcceptPath:            "/api/auth/accept-invitation",
	}
}

// --- Helpers ----------------------------------------------------------------

// issueInvitation is the shared body for the two entry points that create an
// invitation (POST /invitations and a member create with send_invitation).
func (c *InstitutionWorkspaceController) issueInvitation(
	ctx *gin.Context,
	institutionID uint,
	req CreateInvitationRequest,
	source string,
) {
	role := models.RoleUser
	if req.Role != "" {
		role = models.UserRole(req.Role)
	}
	callerRole := models.UserRole(ctx.GetString("role"))
	callerID, _ := currentUserID(ctx)

	email := strings.ToLower(strings.TrimSpace(req.Email))

	// CanInvite, not CanAssign: issuing an invitation is an unauthenticated path
	// into account creation, so it needs the same capability the direct
	// member-creation route uses (user:write), not merely role assignability —
	// which a lecturer technically has for the `user` role.
	if !services.CanInvite(callerRole, role) {
		c.audit.RecordDenied(ctx, models.AuditUserCreate, "invitation", "", req.Email,
			"caller "+string(callerRole)+" may not invite role "+string(role))
		ctx.JSON(http.StatusForbidden, gin.H{
			"error":          "You may not invite a user into this role",
			"your_role":      string(callerRole),
			"assignable":     roleStrings(auth.AssignableRoles(callerRole)),
			"requested_role": string(role),
		})
		return
	}

	if existing, err := c.userRepo.GetByEmail(email); err == nil && existing != nil {
		ctx.JSON(http.StatusConflict, gin.H{
			"error":   "This person already has an account",
			"details": "Add them as a member instead of inviting them.",
		})
		return
	}

	issued, err := c.invitations.Issue(institutionID, callerID, email, role)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInviteRoleNotDelegable):
			ctx.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		case errors.Is(err, services.ErrTooManyPendingInvitations):
			ctx.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		case errors.Is(err, services.ErrInvitationAlreadyPending):
			ctx.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
		default:
			logger.Error("Failed to issue invitation for %s: %v", email, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create invitation"})
		}
		return
	}

	view := invitationViewOf(issued.Invitation, time.Now().UTC())
	// The plaintext token is returned exactly once, here. Only its hash is
	// stored, so it cannot be retrieved again.
	response := gin.H{
		"message":     "Invitation created. Share this link with the invitee — it is shown only once.",
		"invitation":  view,
		"accept_path": "/api/auth/accept-invitation",
		"source":      source,
	}
	if issued.Token != "" {
		response["token"] = issued.Token
	}
	if issued.Resent {
		response["resent"] = true
	}

	c.audit.Record(ctx, models.AuditUserCreate, "invitation",
		uintString(issued.Invitation.ID), email, map[string]any{
			"invited_role": string(role),
			"source":       source,
			"resent":       issued.Resent,
		})

	ctx.JSON(http.StatusCreated, response)
}

// resolveMember loads a member and confirms it belongs to the caller's
// institution, writing the error response on failure.
func (c *InstitutionWorkspaceController) resolveMember(ctx *gin.Context, institutionID uint) (*models.User, bool) {
	id, err := parseIDParam(ctx, "id")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Invalid member ID"})
		return nil, false
	}

	member, err := c.userRepo.GetByID(id)
	if err != nil {
		respondRepoError(ctx, "Member not found", err)
		return nil, false
	}
	// A member of another institution is a miss, not a 403: the API should not
	// confirm that another tenant's account exists.
	if member.InstitutionID == nil || !repositories.InTenant(*member.InstitutionID, institutionID) {
		respondNotFound(ctx, "Member not found")
		return nil, false
	}
	return member, true
}

// memberView assembles a member row, resolving the staff link within the
// institution so a link cannot point at another tenant's staff.
func (c *InstitutionWorkspaceController) memberView(ctx *gin.Context, institutionID uint, user *models.User) memberView {
	view := memberView{User: user, CanManage: c.canManage(ctx, user)}

	if c.staffRepo != nil && user.ID != 0 {
		if staff, err := c.staffRepo.GetByUserID(institutionID, user.ID); err == nil && staff != nil {
			view.StaffID = &staff.ID
			view.StaffName = staff.Name
		}
	}
	return view
}

// canManage reports whether the CALLER may modify this member.
//
// An institution administrator may manage ordinary users. Nobody may manage
// themselves through this path (the handlers guard that separately, with a
// clearer message), and nobody may manage a platform account.
func (c *InstitutionWorkspaceController) canManage(ctx *gin.Context, target *models.User) bool {
	callerRole := models.UserRole(ctx.GetString("role"))
	if !auth.CanAdminister(callerRole) {
		return false
	}
	if target.Role == models.RoleSuperAdmin || target.Role == models.RoleSupport {
		return false
	}
	callerID, _ := currentUserID(ctx)
	return callerID != target.ID
}

// applyStaffLink applies a staff link change, refusing a link into another
// institution.
func (c *InstitutionWorkspaceController) applyStaffLink(
	ctx *gin.Context, institutionID uint, member *models.User, req PatchMemberRequest,
) error {
	if c.staffRepo == nil {
		return nil
	}
	switch {
	case req.ClearStaffID:
		if err := c.staffRepo.SetUserLink(institutionID, member.ID, nil); err != nil {
			return errors.New("the staff link could not be cleared")
		}
	case req.StaffID != nil:
		// The staff record must exist in this institution, or the link would
		// cross a tenant boundary.
		if _, err := c.staffRepo.GetByID(institutionID, *req.StaffID); err != nil {
			return errors.New("the staff record was not found in this institution")
		}
		if err := c.staffRepo.SetUserLink(institutionID, *req.StaffID, &member.ID); err != nil {
			return errors.New("that staff record may already be linked to another member")
		}
	}
	return nil
}

// countOtherAdmins counts administrators in the institution other than the given
// user, which is how the last-admin guard decides whether a change would leave
// the institution unmanaged.
func (c *InstitutionWorkspaceController) countOtherAdmins(institutionID uint, excludeUserID uint) (int, error) {
	users, err := c.userRepo.GetAll(institutionID, maxPageSize, 0)
	if err != nil {
		return 0, err
	}
	count := 0
	for i := range users {
		u := &users[i]
		if u.ID == excludeUserID {
			continue
		}
		if u.Role == models.RoleAdmin && u.IsActive {
			count++
		}
	}
	return count, nil
}

// decodeFrom re-decodes a raw JSON map into a typed struct, so a request can be
// inspected for forbidden fields and then validated normally.
func decodeFrom(raw map[string]any, target any) error {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("re-encode payload: %w", err)
	}
	return json.Unmarshal(encoded, target)
}
