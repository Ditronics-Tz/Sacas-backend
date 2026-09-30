package services

import (
	"errors"
	"fmt"
	"time"

	"go_boilerplate/internal/auth"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
)

// Impersonation support-access controls.
//
// Design constraints, in priority order:
//
//  1. Read-only. An impersonation token never carries write permissions, so a
//     support session cannot change an institution's data even if the UI
//     offers the button.
//  2. Short-lived and single-institution. A token names exactly one institution
//     and expires on the order of minutes.
//  3. Always audited. Starting and ending an impersonation are both recorded,
//     and the audit row is written before the token is returned, so there is no
//     window in which a live support session is unrecorded.
//  4. Not escalatable. The token's role is a synthetic support role that holds
//     read permissions only, so it can never be used to grant a role, create a
//     user, or impersonate again.
var (
	// ErrImpersonationDisabled is returned when support access is switched off
	// in configuration. It defaults to off: support access must be an explicit
	// operational decision, not a default.
	ErrImpersonationDisabled = errors.New("institution impersonation is disabled")

	// ErrImpersonationNotAllowed is returned when the target institution is
	// not in a state that can be inspected.
	ErrImpersonationNotAllowed = errors.New("cannot impersonate this institution")
)

// ImpersonationConfig holds the operational limits, injected so the policy is
// visible in one place and testable without a clock.
type ImpersonationConfig struct {
	// Enabled gates the feature entirely.
	Enabled bool
	// TTL bounds how long a support token stays valid.
	TTL time.Duration
	// MaxConcurrent bounds simultaneous support sessions, so one stuck
	// session cannot quietly occupy the operator slot forever.
	MaxConcurrent int
}

// DefaultImpersonationConfig is a conservative starting point.
//
// Enabled defaults to FALSE. Support access must be an explicit operational
// decision, not a default: a read-only session inside a real institution is
// still a sensitive capability, and shipping it on by default would mean every
// deployment has it until someone notices. When enabled, the limits are fifteen
// minutes and one session at a time.
func DefaultImpersonationConfig() ImpersonationConfig {
	return ImpersonationConfig{
		Enabled:       false,
		TTL:           15 * time.Minute,
		MaxConcurrent: 1,
	}
}

// ImpersonationSession is what a successful start returns to the operator.
type ImpersonationSession struct {
	Token           string    `json:"token"`
	InstitutionID   uint      `json:"institution_id"`
	InstitutionName string    `json:"institution_name"`
	Role            string    `json:"role"`
	ExpiresAt       time.Time `json:"expires_at"`
	// ReadOnly is always true. It is stated in the response so the client can
	// disable editing affordances without hard-coding the assumption.
	ReadOnly bool `json:"read_only"`
	// AllowedPermissions is the exact set the token grants, so the operator
	// sees what the session can do before using it.
	AllowedPermissions []string `json:"allowed_permissions"`
}

// ImpersonationService issues and validates support-access tokens.
type ImpersonationService struct {
	repo   repositories.InstitutionRepository
	audit  *AuditRecorder
	config ImpersonationConfig
	now    func() time.Time
	// issue signs a token for a support session. Injected so the JWT signing
	// details stay out of this file and tests can supply a fake.
	issue func(institutionID uint, expiresAt time.Time) (string, error)
	// active counts live sessions; injected so the concurrency cap is testable
	// without shared global state in tests.
	active func() int
}

// NewImpersonationService builds the service. issue must be supplied by the
// caller (see services.NewSupportTokenIssuer).
func NewImpersonationService(
	repo repositories.InstitutionRepository,
	audit *AuditRecorder,
	cfg ImpersonationConfig,
	issue func(institutionID uint, expiresAt time.Time) (string, error),
	active func() int,
) *ImpersonationService {
	if cfg.TTL <= 0 {
		cfg.TTL = 15 * time.Minute
	}
	return &ImpersonationService{
		repo:   repo,
		audit:  audit,
		config: cfg,
		now:    func() time.Time { return time.Now().UTC() },
		issue:  issue,
		active: active,
	}
}

// Start opens a read-only support session against one institution.
//
// The audit row is written before the token is returned, so a support session
// cannot exist without a corresponding audit entry.
func (s *ImpersonationService) Start(actor *models.User, institutionID uint) (*ImpersonationSession, error) {
	if !s.config.Enabled {
		// A refusal to grant support access is itself auditable.
		s.audit.RecordSystem(
			models.AuditImpersonateStart, nil, "institution", fmt.Sprint(institutionID),
			"denied: impersonation disabled",
			map[string]any{"reason": "disabled", "institution_id": institutionID},
		)
		return nil, ErrImpersonationDisabled
	}

	// A platform account with no institution cannot be impersonated, and
	// neither can an institution that does not exist or is not active.
	if actor == nil || !actor.IsPlatform() {
		s.deny(actor, institutionID, "caller is not a platform account")
		return nil, ErrImpersonationNotAllowed
	}

	institution, err := s.repo.GetByID(institutionID)
	if err != nil {
		s.deny(actor, institutionID, "institution not found")
		return nil, fmt.Errorf("%w: institution %d", ErrImpersonationNotAllowed, institutionID)
	}
	if institution == nil {
		s.deny(actor, institutionID, "institution not found")
		return nil, fmt.Errorf("%w: institution %d", ErrImpersonationNotAllowed, institutionID)
	}

	if s.active != nil && s.config.MaxConcurrent > 0 && s.active() >= s.config.MaxConcurrent {
		s.deny(actor, institutionID, "concurrent session limit reached")
		return nil, fmt.Errorf("%w: concurrent support session limit reached", ErrImpersonationNotAllowed)
	}

	expiresAt := s.now().Add(s.config.TTL)
	token, err := s.issue(institutionID, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("issue support token: %w", err)
	}

	// Write the audit entry before returning the token, so a live session is
	// never unrecorded.
	s.audit.RecordSystem(
		models.AuditImpersonateStart, nil, "institution", fmt.Sprint(institutionID),
		fmt.Sprintf("read-only support session for %s", institution.Name),
		map[string]any{
			"institution_id":   institutionID,
			"institution_name": institution.Name,
			"actor_id":         actor.ID,
			"actor_email":      actor.Email,
			"expires_at":       expiresAt.Format(time.RFC3339),
			"read_only":        true,
		},
	)

	return &ImpersonationSession{
		Token:              token,
		InstitutionID:      institutionID,
		InstitutionName:    institution.Name,
		Role:               string(models.RoleSupport),
		ExpiresAt:          expiresAt,
		ReadOnly:           true,
		AllowedPermissions: auth.SupportPermissions(),
	}, nil
}

// End closes a support session and records it. Ending is audited even though
// the session is already over, because a missing end row is how an abandoned
// session is detected.
func (s *ImpersonationService) End(actor *models.User, institutionID uint) error {
	s.audit.RecordSystem(
		models.AuditImpersonateEnd, nil, "institution", fmt.Sprint(institutionID),
		"support session ended",
		map[string]any{
			"institution_id": institutionID,
			"actor_id":       actorID(actor),
			"actor_email":    actorEmail(actor),
		},
	)
	return nil
}

func (s *ImpersonationService) deny(actor *models.User, institutionID uint, reason string) {
	s.audit.RecordSystem(
		models.AuditImpersonateStart, nil, "institution", fmt.Sprint(institutionID),
		"denied: "+reason,
		map[string]any{
			"institution_id": institutionID,
			"actor_id":       actorID(actor),
			"actor_email":    actorEmail(actor),
			"reason":         reason,
		},
	)
}

// Config exposes the effective limits so the operator UI can show them.
func (s *ImpersonationService) Config() ImpersonationConfig { return s.config }

func actorID(u *models.User) uint {
	if u == nil {
		return 0
	}
	return u.ID
}

func actorEmail(u *models.User) string {
	if u == nil {
		return ""
	}
	return u.Email
}
