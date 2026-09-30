package services

import (
	"errors"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
)

// Tenant used by the service tests. buildSolverRequest is always called with
// the caller's institution, and the class must belong to it.
const testInstitution uint = 1

// --- Minimal tenant-aware stubs covering only what buildSolverRequest calls ---

type stubClassRepo struct {
	class *models.Class
	// lastScope records the institution the last lookup used, so a test can
	// assert the engine never widened its scope.
	lastScope uint
}

func (s *stubClassRepo) Create(institutionID uint, c *models.Class) error { return nil }
func (s *stubClassRepo) GetByID(institutionID, id uint) (*models.Class, error) {
	s.lastScope = institutionID
	if s.class == nil {
		return nil, gorm.ErrRecordNotFound
	}
	// A class owned by another institution is a miss, exactly as the real
	// repository behaves.
	if !repositories.InTenant(s.class.InstitutionID, institutionID) {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *s.class
	return &cp, nil
}
func (s *stubClassRepo) Update(institutionID uint, c *models.Class) error { return nil }
func (s *stubClassRepo) Delete(institutionID, id uint) error              { return nil }
func (s *stubClassRepo) GetAll(institutionID uint, limit, offset int) ([]models.Class, error) {
	return nil, nil
}
func (s *stubClassRepo) GetByCourse(institutionID, courseID uint, limit, offset int) ([]models.Class, error) {
	return nil, nil
}
func (s *stubClassRepo) GetByYear(institutionID uint, year int, limit, offset int) ([]models.Class, error) {
	return nil, nil
}

// CountTimetableEntries is not exercised by the solver engine; the value is
// fixed so the publish-related methods are inert here.
func (s *stubClassRepo) CountTimetableEntries(institutionID, classID uint) (int64, error) {
	if s.class == nil || !repositories.InTenant(s.class.InstitutionID, institutionID) {
		return 0, gorm.ErrRecordNotFound
	}
	return 0, nil
}

func (s *stubClassRepo) MarkPublished(institutionID, classID, userID uint, at time.Time) (bool, error) {
	return false, nil
}

func (s *stubClassRepo) MarkApproved(institutionID, classID, userID uint, at time.Time) (bool, error) {
	return false, nil
}

func (s *stubClassRepo) ClearPublication(institutionID, classID uint) error { return nil }

type stubModuleRepo struct {
	// byCourse is keyed by course ID and holds modules belonging to that course.
	byCourse map[uint][]models.Module
	// general holds course-less (general_subject) modules.
	general []models.Module
	// scopes records every institution the repo was queried with.
	scopes []uint
}

func (s *stubModuleRepo) record(institutionID uint) { s.scopes = append(s.scopes, institutionID) }

func (s *stubModuleRepo) Create(institutionID uint, m *models.Module) error { return nil }
func (s *stubModuleRepo) GetByID(institutionID, id uint) (*models.Module, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *stubModuleRepo) Update(institutionID uint, m *models.Module) error { return nil }
func (s *stubModuleRepo) Delete(institutionID, id uint) error               { return nil }
func (s *stubModuleRepo) GetAll(institutionID uint, limit, offset int) ([]models.Module, error) {
	return nil, nil
}
func (s *stubModuleRepo) GetByCourse(institutionID, courseID uint, limit, offset int) ([]models.Module, error) {
	s.record(institutionID)
	var out []models.Module
	for _, m := range s.byCourse[courseID] {
		if repositories.InTenant(m.InstitutionID, institutionID) {
			out = append(out, m)
		}
	}
	return out, nil
}
func (s *stubModuleRepo) GetByType(institutionID uint, t models.ModuleType, limit, offset int) ([]models.Module, error) {
	return nil, nil
}
func (s *stubModuleRepo) GetGeneralModules(institutionID uint, limit, offset int) ([]models.Module, error) {
	s.record(institutionID)
	var out []models.Module
	for _, m := range s.general {
		if repositories.InTenant(m.InstitutionID, institutionID) {
			out = append(out, m)
		}
	}
	return out, nil
}
func (s *stubModuleRepo) GetWithStaff(institutionID, id uint) (*models.Module, error) {
	return nil, gorm.ErrRecordNotFound
}

type stubSubjectRepo struct {
	all    []models.Subject
	scopes []uint
}

func (s *stubSubjectRepo) Create(institutionID uint, x *models.Subject) error { return nil }
func (s *stubSubjectRepo) GetByID(institutionID, id uint) (*models.Subject, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *stubSubjectRepo) Update(institutionID uint, x *models.Subject) error { return nil }
func (s *stubSubjectRepo) Delete(institutionID, id uint) error                { return nil }
func (s *stubSubjectRepo) GetAll(institutionID uint, limit, offset int) ([]models.Subject, error) {
	s.scopes = append(s.scopes, institutionID)
	var out []models.Subject
	for _, sub := range s.all {
		if repositories.InTenant(sub.InstitutionID, institutionID) {
			out = append(out, sub)
		}
	}
	return out, nil
}
func (s *stubSubjectRepo) GetByCreditHours(institutionID uint, h int) ([]models.Subject, error) {
	return nil, nil
}

type stubStaffRepo struct {
	all    []models.Staff
	scopes []uint
}

func (s *stubStaffRepo) Create(institutionID uint, x *models.Staff) error { return nil }
func (s *stubStaffRepo) GetByID(institutionID, id uint) (*models.Staff, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *stubStaffRepo) GetByEmail(institutionID uint, email string) (*models.Staff, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *stubStaffRepo) GetByUserID(institutionID, userID uint) (*models.Staff, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *stubStaffRepo) Update(institutionID uint, x *models.Staff) error { return nil }
func (s *stubStaffRepo) Delete(institutionID, id uint) error              { return nil }
func (s *stubStaffRepo) GetAll(institutionID uint, limit, offset int) ([]models.Staff, error) {
	s.scopes = append(s.scopes, institutionID)
	var out []models.Staff
	for _, st := range s.all {
		if repositories.InTenant(st.InstitutionID, institutionID) {
			out = append(out, st)
		}
	}
	return out, nil
}
func (s *stubStaffRepo) GetByFaculty(institutionID, facultyID uint, limit, offset int) ([]models.Staff, error) {
	return nil, nil
}
func (s *stubStaffRepo) GetWithModules(institutionID, id uint) (*models.Staff, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *stubStaffRepo) UpdatePreferences(institutionID, id uint, p string) error {
	return nil
}
func (s *stubStaffRepo) SetUserLink(institutionID, staffID uint, userID *uint) error {
	return nil
}
func (s *stubStaffRepo) AssignModule(institutionID, staffID, moduleID uint) error { return nil }
func (s *stubStaffRepo) UnassignModule(institutionID, staffID, moduleID uint) error {
	return nil
}
func (s *stubStaffRepo) ListModules(institutionID, staffID uint) ([]models.Module, error) {
	return nil, nil
}
func (s *stubStaffRepo) ListStaffForModule(institutionID, moduleID uint) ([]models.Staff, error) {
	return nil, nil
}

type stubRoomRepo struct {
	all    []models.Room
	scopes []uint
}

func (s *stubRoomRepo) Create(institutionID uint, x *models.Room) error { return nil }
func (s *stubRoomRepo) GetByID(institutionID, id uint) (*models.Room, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *stubRoomRepo) Update(institutionID uint, x *models.Room) error { return nil }
func (s *stubRoomRepo) Delete(institutionID, id uint) error             { return nil }
func (s *stubRoomRepo) GetAll(institutionID uint, limit, offset int) ([]models.Room, error) {
	s.scopes = append(s.scopes, institutionID)
	var out []models.Room
	for _, room := range s.all {
		if repositories.InTenant(room.InstitutionID, institutionID) {
			out = append(out, room)
		}
	}
	return out, nil
}
func (s *stubRoomRepo) GetByCapacity(institutionID uint, min int) ([]models.Room, error) {
	return nil, nil
}
func (s *stubRoomRepo) GetLabRooms(institutionID uint) ([]models.Room, error) { return nil, nil }
func (s *stubRoomRepo) GetStickyRooms(institutionID uint) ([]models.Room, error) {
	return nil, nil
}
func (s *stubRoomRepo) GetAvailableRooms(institutionID uint, d models.Weekday, st, et string) ([]models.Room, error) {
	return nil, nil
}

type stubSettingsRepo struct {
	settings *models.GenerationSettings
	err      error
	// scoped records which institution the settings lookup was made for.
	scoped uint
}

func (s *stubSettingsRepo) Get() (*models.GenerationSettings, error) {
	if s.err != nil {
		if errors.Is(s.err, repositories.ErrNotConfigured) {
			return models.DefaultGenerationSettings(), s.err
		}
		return nil, s.err
	}
	cp := *s.settings
	return &cp, nil
}

func (s *stubSettingsRepo) GetOverride(institutionID uint) (*models.GenerationSettings, error) {
	return nil, nil
}

func (s *stubSettingsRepo) GetForInstitution(institutionID uint) (*models.GenerationSettings, error) {
	s.scoped = institutionID
	return s.Get()
}

func (s *stubSettingsRepo) Upsert(x *models.GenerationSettings) error { return nil }
func (s *stubSettingsRepo) UpsertForInstitution(institutionID uint, x *models.GenerationSettings) error {
	return nil
}
func (s *stubSettingsRepo) DeleteOverride(institutionID uint) error { return nil }

func newServiceForBuildTest(settingsRepo repositories.GenerationSettingsRepository) *TimetableService {
	classRepo := &stubClassRepo{class: &models.Class{
		ID: 1, CourseID: 1, NumberOfStudents: 30, InstitutionID: testInstitution,
	}}
	courseID := uint(1)
	moduleRepo := &stubModuleRepo{byCourse: map[uint][]models.Module{
		1: {{ID: 2, CreditHours: 2, CourseID: &courseID, InstitutionID: testInstitution}},
	}}
	return NewTimetableService(
		nil, &stubStaffRepo{}, classRepo, moduleRepo,
		&stubRoomRepo{}, &stubSubjectRepo{}, nil, settingsRepo,
	)
}

// TestBuildSolverRequest_SettingsFlowThrough: configured settings reach the
// SolverRequest.
func TestBuildSolverRequest_SettingsFlowThrough(t *testing.T) {
	repo := &stubSettingsRepo{settings: &models.GenerationSettings{
		ID:            models.SingletonID,
		TimeBudgetSec: 90,
		SoftWeights:   datatypes.JSON(`{"preferred_start_weight":2.5,"session_spread_weight":1}`),
	}}
	svc := newServiceForBuildTest(repo)

	req, err := svc.buildSolverRequest(testInstitution, 1, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.TimeBudgetSec != 90 {
		t.Fatalf("expected time_budget_sec 90, got %v", req.TimeBudgetSec)
	}
	if req.SoftWeights["preferred_start_weight"] != 2.5 || req.SoftWeights["session_spread_weight"] != 1 {
		t.Fatalf("soft weights did not flow through: %v", req.SoftWeights)
	}
}

// TestBuildSolverRequest_SettingsScopedToClassInstitution verifies the settings
// lookup uses the CLASS's institution rather than the caller's raw scope, so an
// institution override is applied to its own runs.
func TestBuildSolverRequest_SettingsScopedToClassInstitution(t *testing.T) {
	repo := &stubSettingsRepo{settings: &models.GenerationSettings{
		ID: models.SingletonID, TimeBudgetSec: 45, SoftWeights: datatypes.JSON(`{}`),
	}}
	svc := newServiceForBuildTest(repo)

	if _, err := svc.buildSolverRequest(testInstitution, 1, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.scoped != testInstitution {
		t.Fatalf("expected settings scoped to %d, got %d", testInstitution, repo.scoped)
	}
}

// TestBuildSolverRequest_NotConfiguredFallsBackToDefaults: fresh deployment
// (no settings row) must NOT fail generation — defaults are used.
func TestBuildSolverRequest_NotConfiguredFallsBackToDefaults(t *testing.T) {
	repo := &stubSettingsRepo{err: repositories.ErrNotConfigured}
	svc := newServiceForBuildTest(repo)

	req, err := svc.buildSolverRequest(testInstitution, 1, false)
	if err != nil {
		t.Fatalf("not-configured settings must fall back, got error: %v", err)
	}
	if req.TimeBudgetSec != 30 {
		t.Fatalf("expected default time_budget_sec 30, got %v", req.TimeBudgetSec)
	}
	if len(req.SoftWeights) != 0 {
		t.Fatalf("expected empty soft weights, got %v", req.SoftWeights)
	}
}

// TestBuildSolverRequest_NilRepoFallsBackToDefaults: legacy call sites that
// construct the service without a settings repo keep working.
func TestBuildSolverRequest_NilRepoFallsBackToDefaults(t *testing.T) {
	svc := newServiceForBuildTest(nil)

	req, err := svc.buildSolverRequest(testInstitution, 1, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.TimeBudgetSec != 30 {
		t.Fatalf("expected default time_budget_sec 30, got %v", req.TimeBudgetSec)
	}
}

// TestBuildSolverRequest_DBErrorFailsLoudly: a real repository error must
// abort request construction — silently using defaults would mask a
// production issue.
func TestBuildSolverRequest_DBErrorFailsLoudly(t *testing.T) {
	repo := &stubSettingsRepo{err: errors.New("db down")}
	svc := newServiceForBuildTest(repo)

	if _, err := svc.buildSolverRequest(testInstitution, 1, false); err == nil {
		t.Fatalf("expected error on settings repo failure, got nil")
	}
}

// TestBuildSolverRequest_RejectsCrossTenantClass is the core isolation test for
// the engine: asking to build a solver request for another institution's class
// must fail with ErrClassNotInInstitution rather than silently generating a
// timetable from foreign data.
func TestBuildSolverRequest_RejectsCrossTenantClass(t *testing.T) {
	svc := NewTimetableService(
		nil, &stubStaffRepo{},
		&stubClassRepo{class: &models.Class{
			ID: 1, CourseID: 1, NumberOfStudents: 30, InstitutionID: 2, // belongs to tenant 2
		}},
		&stubModuleRepo{}, &stubRoomRepo{}, &stubSubjectRepo{}, nil, nil,
	)

	_, err := svc.buildSolverRequest(1 /* caller is tenant 1 */, 1, false)
	if err == nil {
		t.Fatal("expected an error for a cross-tenant class, got nil")
	}
	if !errors.Is(err, ErrClassNotInInstitution) {
		t.Fatalf("expected ErrClassNotInInstitution, got %v", err)
	}
}

// TestBuildSolverRequest_ExcludesOtherTenantsData verifies the engine reads
// only the class's own institution. Institution B's staff, rooms, and general
// subjects must not reach institution A's solver payload.
func TestBuildSolverRequest_ExcludesOtherTenantsData(t *testing.T) {
	classRepo := &stubClassRepo{class: &models.Class{
		ID: 1, CourseID: 7, NumberOfStudents: 30, InstitutionID: 1,
	}}
	moduleRepo := &stubModuleRepo{
		byCourse: map[uint][]models.Module{
			7: {
				{ID: 10, CreditHours: 1, InstitutionID: 1},
				{ID: 90, CreditHours: 1, InstitutionID: 2}, // tenant B
			},
		},
		general: []models.Module{
			{ID: 11, CreditHours: 1, InstitutionID: 1},
			{ID: 91, CreditHours: 1, InstitutionID: 2}, // tenant B general subject
		},
	}
	staffRepo := &stubStaffRepo{all: []models.Staff{
		{ID: 1, MaxHours: 40, InstitutionID: 1},
		{ID: 9, MaxHours: 40, InstitutionID: 2}, // tenant B
	}}
	roomRepo := &stubRoomRepo{all: []models.Room{
		{ID: 1, Capacity: 50, InstitutionID: 1},
		{ID: 9, Capacity: 50, InstitutionID: 2}, // tenant B
	}}

	svc := NewTimetableService(
		nil, staffRepo, classRepo, moduleRepo, roomRepo, &stubSubjectRepo{}, nil, nil,
	)

	req, err := svc.buildSolverRequest(1, 1, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, m := range req.Modules {
		if m.ID == 90 || m.ID == 91 {
			t.Fatalf("solver request leaked another tenant's module: %+v", req.Modules)
		}
	}
	for _, st := range req.Staff {
		if st.ID == 9 {
			t.Fatalf("solver request leaked another tenant's staff: %+v", req.Staff)
		}
	}
	for _, room := range req.Rooms {
		if room.ID == 9 {
			t.Fatalf("solver request leaked another tenant's room: %+v", req.Rooms)
		}
	}
	// Sanity check: own-tenant data is present, so the test is not vacuous.
	if len(req.Modules) != 2 {
		t.Fatalf("expected 2 of the institution's modules, got %d: %+v", len(req.Modules), req.Modules)
	}
	if len(req.Staff) != 1 || req.Staff[0].ID != 1 {
		t.Fatalf("expected only own staff, got %+v", req.Staff)
	}
	if len(req.Rooms) != 1 || req.Rooms[0].ID != 1 {
		t.Fatalf("expected only own rooms, got %+v", req.Rooms)
	}
}

// TestBuildSolverRequest_AllQueriesScoped asserts the institution was threaded
// into every repository read the engine performs, rather than only the class
// lookup. This is the regression guard against a future unscoped call.
func TestBuildSolverRequest_AllQueriesScoped(t *testing.T) {
	classRepo := &stubClassRepo{class: &models.Class{
		ID: 1, CourseID: 7, NumberOfStudents: 30, InstitutionID: 42,
	}}
	moduleRepo := &stubModuleRepo{byCourse: map[uint][]models.Module{7: {}}}
	staffRepo := &stubStaffRepo{}
	roomRepo := &stubRoomRepo{}

	svc := NewTimetableService(
		nil, staffRepo, classRepo, moduleRepo, roomRepo, &stubSubjectRepo{}, nil, nil,
	)

	const scope uint = 42
	if _, err := svc.buildSolverRequest(scope, 1, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if classRepo.lastScope != scope {
		t.Errorf("class repo queried with %d, want %d", classRepo.lastScope, scope)
	}
	for _, s := range moduleRepo.scopes {
		if s != scope {
			t.Errorf("module repo queried with %d, want %d", s, scope)
		}
	}
	for _, s := range staffRepo.scopes {
		if s != scope {
			t.Errorf("staff repo queried with %d, want %d", s, scope)
		}
	}
	for _, s := range roomRepo.scopes {
		if s != scope {
			t.Errorf("room repo queried with %d, want %d", s, scope)
		}
	}
}
