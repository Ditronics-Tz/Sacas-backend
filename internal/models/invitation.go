package models

import "time"

// InvitationStatus is the lifecycle of an invitation.
type InvitationStatus string

const (
	// InvitationPending is the only status that can be accepted.
	InvitationPending InvitationStatus = "pending"
	// InvitationAccepted means the invitee created their account.
	InvitationAccepted InvitationStatus = "accepted"
	// InvitationRevoked means an administrator withdrew the invitation.
	InvitationRevoked InvitationStatus = "revoked"
	// InvitationExpired is derived from ExpiresAt rather than stored, so an
	// invitation cannot be revived by clearing a flag.
	InvitationExpired InvitationStatus = "expired"
)

// InstitutionInvitation is a single-use, expiring offer to join an institution.
//
// Security properties, all of which are load-bearing:
//
//   - Only a hash of the token is stored. A database leak therefore does not
//     hand out working invitations.
//   - The token is single-use: AcceptedAt is set on acceptance, and the accept
//     path refuses an already-accepted invitation.
//   - The token expires, and expiry is derived from ExpiresAt rather than a
//     stored flag, so it cannot be cleared.
//   - An invitation is scoped to one institution AND one email. Accepting
//     invites the account into that institution only.
type InstitutionInvitation struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	InstitutionID uint   `gorm:"not null;index" json:"institution_id"`
	Email         string `gorm:"not null;index" json:"email"`
	// Role is the role the invitee will hold. It cannot be super_admin or
	// institution_support: those are not delegable by an institution, and
	// allowing either would let an institution mint a platform account or a
	// read-only account that outlives the invitation.
	Role UserRole `gorm:"not null;default:'user'" json:"role"`

	// TokenHash is a SHA-256 hex digest of the token. The plaintext token is
	// returned once, to the inviter, and never stored.
	TokenHash string `gorm:"not null;uniqueIndex" json:"-"`

	// InvitedByID is the administrator who issued the invitation, so "who
	// invited this person" is answerable.
	InvitedByID uint `json:"invited_by_id"`

	ExpiresAt  time.Time  `gorm:"not null;index" json:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	// RevokedAt is set when withdrawn. NULL means not revoked.
	RevokedAt *time.Time `json:"revoked_at,omitempty"`

	// AcceptedUserID links the account created on acceptance, so a duplicate
	// accept is detectable and the invitation's outcome is visible.
	AcceptedUserID *uint `json:"accepted_user_id,omitempty"`

	// LastSentAt records the most recent resend, so the endpoint can rate-limit
	// by invitation rather than creating unlimited copies.
	LastSentAt *time.Time `json:"last_sent_at,omitempty"`
}

// IsUsable reports whether the invitation can still be accepted at time now.
func (i *InstitutionInvitation) IsUsable(now time.Time) bool {
	if i == nil {
		return false
	}
	if i.AcceptedAt != nil || i.RevokedAt != nil {
		return false
	}
	return i.ExpiresAt.After(now)
}

// Status returns the invitation's current state, deriving expiry from the clock.
func (i *InstitutionInvitation) Status(now time.Time) InvitationStatus {
	switch {
	case i == nil:
		return InvitationExpired
	case i.AcceptedAt != nil:
		return InvitationAccepted
	case i.RevokedAt != nil:
		return InvitationRevoked
	case !i.ExpiresAt.After(now):
		return InvitationExpired
	default:
		return InvitationPending
	}
}

// IsDelegableRole reports whether an institution may invite a user into this
// role.
//
// The rule is the same one `auth.AssignableRoles` imposes on direct role
// assignment: an institution may only create plain users. An invitation must not
// become a bypass for that, or the anti-escalation work in the RBAC pass would be
// trivially circumvented by inviting someone as a coordinator instead.
func IsDelegableRole(role UserRole) bool {
	return role == RoleUser
}

// DefaultInvitationTTL is how long a new invitation is valid.
const DefaultInvitationTTL = 7 * 24 * time.Hour
