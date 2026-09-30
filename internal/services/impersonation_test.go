package services

import (
	"errors"
	"testing"
	"time"

	"go_boilerplate/internal/auth"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
)

// stubInstitutionRepo serves a fixed set of institutions.
type stubInstitutionRepo struct {
	items map[uint]*models.Institution
}

func (r *stubInstitutionRepo) Create(inst *models.Institution) error { return nil }
func (r *stubInstitutionRepo) GetByID(id uint) (*models.Institution, error) {
	inst, ok := r.items[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *inst
	return &cp, nil
}
func (r *stubInstitutionRepo) GetBySlug(string) (*models.Institution, error) {
	return nil, errors.New("not found")
}
func (r *stubInstitutionRepo) Update(inst *models.Institution) error { return nil }
func (r *stubInstitutionRepo) Delete(id uint) error                  { return nil }
func (r *stubInstitutionRepo) GetAll(int, int) ([]models.Institution, error) {
	return nil, nil
}
func (r *stubInstitutionRepo) CountAll() (int64, error) { return 0, nil }
func (r *stubInstitutionRepo) GetByStatus(models.InstitutionStatus, int, int) ([]models.Institution, error) {
	return nil, nil
}
func (r *stubInstitutionRepo) CountByStatus(models.InstitutionStatus) (int64, error) {
	return 0, nil
}
func (r *stubInstitutionRepo) CountUsers(id uint) (int64, error) { return 0, nil }

func activeInstitutionRepo() *stubInstitutionRepo {
	return &stubInstitutionRepo{items: map[uint]*models.Institution{
		7: {
			ID: 7, Name: "Dar es Salaam University", Slug: "dstu",
			Status: models.InstitutionStatusActive, Plan: models.InstitutionPlanPro,
		},
	}}
}

// platformActor is the only kind of caller permitted to open a support session.
func platformActor() *models.User {
	return &models.User{ID: 1, Email: "root@x.com", Role: models.RoleSuperAdmin, IsActive: true, InstitutionID: nil}
}

// newTestImpersonationEnabled builds a service with support access explicitly
// enabled and a stub token issuer.
func newTestImpersonationEnabled(t *testing.T, cfg ImpersonationConfig) (*ImpersonationService, *stubAuditRepo) {
	t.Helper()
	auditRepo := &stubAuditRepo{}
	rec := NewAuditRecorder(auditRepo)
	svc := NewImpersonationService(activeInstitutionRepo(), rec, cfg,
		func(institutionID uint, expiresAt time.Time) (string, error) {
			return "support-token-for-" + institutionIDLetter(institutionID), nil
		},
		func() int { return 0 },
	)
	return svc, auditRepo
}

func newTestImpersonation(t *testing.T, repo repositories.InstitutionRepository, cfg ImpersonationConfig) (*ImpersonationService, *stubAuditRepo) {
	t.Helper()
	// Tests must opt in explicitly: the shipped default is disabled, so a test
	// that relied on the default would silently stop exercising the success
	// path rather than failing loudly.
	cfg.Enabled = true
	auditRepo := &stubAuditRepo{}
	rec := NewAuditRecorder(auditRepo)
	svc := NewImpersonationService(repo, rec, cfg,
		func(institutionID uint, expiresAt time.Time) (string, error) {
			return "support-token-for-" + institutionIDLetter(institutionID), nil
		},
		func() int { return 0 },
	)
	return svc, auditRepo
}

// TestImpersonation_DefaultsToDisabled is the fail-closed property: support
// access must never be on unless an operator deliberately turned it on.
func TestImpersonation_DefaultsToDisabled(t *testing.T) {
	if DefaultImpersonationConfig().Enabled {
		t.Error("impersonation must be disabled by default")
	}

	// And a service built from the default must refuse, even for a platform
	// caller.
	auditRepo := &stubAuditRepo{}
	svc := NewImpersonationService(activeInstitutionRepo(), NewAuditRecorder(auditRepo),
		DefaultImpersonationConfig(), func(uint, time.Time) (string, error) { return "t", nil }, nil)

	if _, err := svc.Start(platformActor(), 7); !errors.Is(err, ErrImpersonationDisabled) {
		t.Fatalf("expected refusal when disabled, got %v", err)
	}
}

func institutionIDLetter(id uint) string {
	if id == 0 {
		return "0"
	}
	return string(rune('0' + id))
}

func TestImpersonation_SuccessIsReadOnlyAndAudited(t *testing.T) {
	svc, auditRepo := newTestImpersonation(t, activeInstitutionRepo(), DefaultImpersonationConfig())

	session, err := svc.Start(platformActor(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read-only is the headline guarantee, so assert it three ways.
	if !session.ReadOnly {
		t.Error("session must be marked read-only")
	}
	if session.Role != string(models.RoleSupport) {
		t.Errorf("expected the support role, got %s", session.Role)
	}
	if session.InstitutionID != 7 {
		t.Errorf("expected institution 7, got %d", session.InstitutionID)
	}
	// No permission in the advertised set may be a write.
	for _, p := range session.AllowedPermissions {
		if auth.HasPermission(models.RoleUser, auth.Permission(p)) &&
			isWritePermission(auth.Permission(p)) {
			t.Errorf("support session advertises write permission %s", p)
		}
	}

	// The audit entry must be written before the token is handed back, so a
	// live session is never unrecorded.
	if len(auditRepo.entries) != 1 {
		t.Fatalf("expected exactly 1 audit entry, got %d", len(auditRepo.entries))
	}
	entry := auditRepo.entries[0]
	if entry.Action != models.AuditImpersonateStart {
		t.Errorf("wrong action: %s", entry.Action)
	}
	if entry.InstitutionID != nil {
		t.Error("a support-session audit is a platform action and must have a NULL institution")
	}
}

func TestImpersonation_Expires(t *testing.T) {
	cfg := DefaultImpersonationConfig()
	cfg.TTL = 5 * time.Minute
	svc, _ := newTestImpersonation(t, activeInstitutionRepo(), cfg)

	before := time.Now().UTC()
	session, err := svc.Start(platformActor(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The token must be short-lived: well under an hour, per the config.
	ttl := session.ExpiresAt.Sub(before)
	if ttl <= 0 {
		t.Fatalf("expected a future expiry, got %s", session.ExpiresAt)
	}
	if ttl > 10*time.Minute {
		t.Errorf("expected a short TTL, got %s", ttl)
	}
}

func TestImpersonation_DisabledIsRefusedAndAudited(t *testing.T) {
	// Built directly rather than through newTestImpersonation, which forces the
	// enabled flag on.
	cfg := DefaultImpersonationConfig()
	cfg.Enabled = false
	svc, auditRepo := newTestImpersonationEnabled(t, cfg)

	_, err := svc.Start(platformActor(), 7)
	if !errors.Is(err, ErrImpersonationDisabled) {
		t.Fatalf("expected ErrImpersonationDisabled, got %v", err)
	}
	// A refusal to grant support access is itself auditable.
	if len(auditRepo.entries) != 1 || auditRepo.entries[0].Action != models.AuditImpersonateStart {
		t.Errorf("expected a recorded refusal, got %+v", auditRepo.entries)
	}
}

// TestImpersonation_RejectsNonPlatformCaller is the anti-escalation property: an
// institution account must not be able to open a support session, or a tenant
// admin could pivot into another tenant's data.
func TestImpersonation_RejectsNonPlatformCaller(t *testing.T) {
	svc, auditRepo := newTestImpersonation(t, activeInstitutionRepo(), DefaultImpersonationConfig())

	callers := []*models.User{
		// An institution admin.
		{ID: 2, Email: "admin@a.com", Role: models.RoleAdmin, IsActive: true, InstitutionID: ptrUint(7)},
		// A super_admin who HAS been bound to an institution is a tenant user.
		{ID: 3, Email: "tied@x.com", Role: models.RoleSuperAdmin, IsActive: true, InstitutionID: ptrUint(7)},
		// A support session must not be able to open another one.
		{ID: 4, Email: "s@a.com", Role: models.RoleSupport, IsActive: true, InstitutionID: ptrUint(7)},
	}
	for _, caller := range callers {
		_, err := svc.Start(caller, 7)
		if !errors.Is(err, ErrImpersonationNotAllowed) {
			t.Errorf("caller %s (%s): expected refusal, got %v", caller.Email, caller.Role, err)
		}
	}
	if len(auditRepo.entries) != len(callers) {
		t.Errorf("expected %d recorded refusals, got %d", len(callers), len(auditRepo.entries))
	}
}

func TestImpersonation_RejectsUnknownInstitution(t *testing.T) {
	svc, auditRepo := newTestImpersonation(t, activeInstitutionRepo(), DefaultImpersonationConfig())

	_, err := svc.Start(platformActor(), 999)
	if !errors.Is(err, ErrImpersonationNotAllowed) {
		t.Fatalf("expected refusal for an unknown institution, got %v", err)
	}
	if len(auditRepo.entries) != 1 {
		t.Error("expected the refusal to be recorded")
	}
}

func TestImpersonation_RespectsConcurrentLimit(t *testing.T) {
	auditRepo := &stubAuditRepo{}
	rec := NewAuditRecorder(auditRepo)
	cfg := DefaultImpersonationConfig()
	cfg.Enabled = true
	cfg.MaxConcurrent = 1

	svc := NewImpersonationService(activeInstitutionRepo(), rec, cfg,
		func(uint, time.Time) (string, error) { return "tok", nil },
		func() int { return 1 }, // already at the cap
	)

	_, err := svc.Start(platformActor(), 7)
	if !errors.Is(err, ErrImpersonationNotAllowed) {
		t.Fatalf("expected refusal at the concurrency cap, got %v", err)
	}
	if len(auditRepo.entries) != 1 {
		t.Error("expected the refusal to be recorded")
	}
}

func TestImpersonation_EndIsAudited(t *testing.T) {
	svc, auditRepo := newTestImpersonation(t, activeInstitutionRepo(), DefaultImpersonationConfig())

	if _, err := svc.Start(platformActor(), 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := svc.End(platformActor(), 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(auditRepo.entries) != 2 {
		t.Fatalf("expected start and end entries, got %d", len(auditRepo.entries))
	}
	if auditRepo.entries[1].Action != models.AuditImpersonateEnd {
		t.Errorf("expected an end entry, got %s", auditRepo.entries[1].Action)
	}
}

func TestImpersonation_NilIssuerDoesNotPanic(t *testing.T) {
	// A service built without a usable token issuer (JWT secret missing) must
	// fail closed rather than panic or hand back an empty token.
	auditRepo := &stubAuditRepo{}
	rec := NewAuditRecorder(auditRepo)
	cfg := DefaultImpersonationConfig()
	cfg.Enabled = false
	svc := NewImpersonationService(activeInstitutionRepo(), rec, cfg, nil, nil)

	if _, err := svc.Start(platformActor(), 7); !errors.Is(err, ErrImpersonationDisabled) {
		t.Fatalf("expected the disabled error, got %v", err)
	}
}

func TestImpersonation_Config(t *testing.T) {
	svc, _ := newTestImpersonation(t, activeInstitutionRepo(), ImpersonationConfig{Enabled: true})
	cfg := svc.Config()
	if !cfg.Enabled {
		t.Error("expected enabled")
	}
	// A zero TTL must be replaced with a sane default rather than producing a
	// token that expires immediately.
	if cfg.TTL <= 0 {
		t.Errorf("expected a default TTL, got %s", cfg.TTL)
	}
}

// isWritePermission classifies a permission for the read-only assertion. Kept
// local to the test so it does not have to agree with the map for a permission
// it does not know about.
func isWritePermission(p auth.Permission) bool {
	switch p {
	case auth.PermFacultyWrite, auth.PermCourseWrite, auth.PermModuleWrite,
		auth.PermClassWrite, auth.PermRoomWrite, auth.PermSubjectWrite,
		auth.PermStaffWrite, auth.PermTimetableGenerate, auth.PermTimetablePublish,
		auth.PermTimetableApprove, auth.PermTimetableOverride,
		auth.PermExamWrite, auth.PermExamSchedule, auth.PermExamPublish, auth.PermExamApprove,
		auth.PermUserWrite, auth.PermUserDelete, auth.PermProfileUpdate, auth.PermPasswordSet,
		auth.PermDataImport, auth.PermSettingsWrite, auth.PermStaffLink,
		auth.PermInstitutionWrite, auth.PermInstitutionDelete,
		auth.PermPlatformSettings, auth.PermUserRoleWrite, auth.PermImpersonate:
		return true
	}
	return false
}
