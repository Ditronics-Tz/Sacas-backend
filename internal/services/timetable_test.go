package services

import (
	"errors"
	"testing"

	"gorm.io/datatypes"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
)

// --- Minimal stubs covering only the repo methods buildSolverRequest calls ---

type stubClassRepo struct{}

func (s *stubClassRepo) Create(c *models.Class) error { return nil }
func (s *stubClassRepo) GetByID(id uint) (*models.Class, error) {
	return &models.Class{CourseID: 1, NumberOfStudents: 30}, nil
}
func (s *stubClassRepo) Update(c *models.Class) error                     { return nil }
func (s *stubClassRepo) Delete(id uint) error                             { return nil }
func (s *stubClassRepo) GetAll(limit, offset int) ([]models.Class, error) { return nil, nil }
func (s *stubClassRepo) GetByCourse(courseID uint, limit, offset int) ([]models.Class, error) {
	return nil, nil
}
func (s *stubClassRepo) GetByYear(year int, limit, offset int) ([]models.Class, error) {
	return nil, nil
}

type stubModuleRepo struct{}

func (s *stubModuleRepo) Create(m *models.Module) error { return nil }
func (s *stubModuleRepo) GetByID(id uint) (*models.Module, error) {
	return nil, errors.New("not found")
}
func (s *stubModuleRepo) Update(m *models.Module) error                     { return nil }
func (s *stubModuleRepo) Delete(id uint) error                              { return nil }
func (s *stubModuleRepo) GetAll(limit, offset int) ([]models.Module, error) { return nil, nil }
func (s *stubModuleRepo) GetByCourse(courseID uint, limit, offset int) ([]models.Module, error) {
	return []models.Module{{ID: 2, CreditHours: 2, CourseID: &courseID}}, nil
}
func (s *stubModuleRepo) GetByType(t models.ModuleType, limit, offset int) ([]models.Module, error) {
	return nil, nil
}
func (s *stubModuleRepo) GetGeneralModules(limit, offset int) ([]models.Module, error) {
	return nil, nil
}
func (s *stubModuleRepo) GetWithStaff(id uint) (*models.Module, error) {
	return nil, errors.New("not found")
}

type stubSubjectRepo struct{}

func (s *stubSubjectRepo) Create(x *models.Subject) error { return nil }
func (s *stubSubjectRepo) GetByID(id uint) (*models.Subject, error) {
	return nil, errors.New("not found")
}
func (s *stubSubjectRepo) Update(x *models.Subject) error                     { return nil }
func (s *stubSubjectRepo) Delete(id uint) error                               { return nil }
func (s *stubSubjectRepo) GetAll(limit, offset int) ([]models.Subject, error) { return nil, nil }
func (s *stubSubjectRepo) GetByCreditHours(h int) ([]models.Subject, error)   { return nil, nil }

type stubStaffRepo struct{}

func (s *stubStaffRepo) Create(x *models.Staff) error { return nil }
func (s *stubStaffRepo) GetByID(id uint) (*models.Staff, error) {
	return nil, errors.New("not found")
}
func (s *stubStaffRepo) GetByEmail(email string) (*models.Staff, error) {
	return nil, errors.New("not found")
}
func (s *stubStaffRepo) GetByUserID(userID uint) (*models.Staff, error) {
	return nil, errors.New("not found")
}
func (s *stubStaffRepo) Update(x *models.Staff) error                     { return nil }
func (s *stubStaffRepo) Delete(id uint) error                             { return nil }
func (s *stubStaffRepo) GetAll(limit, offset int) ([]models.Staff, error) { return nil, nil }
func (s *stubStaffRepo) GetByFaculty(facultyID uint, limit, offset int) ([]models.Staff, error) {
	return nil, nil
}
func (s *stubStaffRepo) GetWithModules(id uint) (*models.Staff, error) {
	return nil, errors.New("not found")
}
func (s *stubStaffRepo) UpdatePreferences(id uint, p string) error                { return nil }
func (s *stubStaffRepo) AssignModule(staffID, moduleID uint) error                { return nil }
func (s *stubStaffRepo) UnassignModule(staffID, moduleID uint) error              { return nil }
func (s *stubStaffRepo) ListModules(staffID uint) ([]models.Module, error)        { return nil, nil }
func (s *stubStaffRepo) ListStaffForModule(moduleID uint) ([]models.Staff, error) { return nil, nil }

type stubRoomRepo struct{}

func (s *stubRoomRepo) Create(x *models.Room) error { return nil }
func (s *stubRoomRepo) GetByID(id uint) (*models.Room, error) {
	return nil, errors.New("not found")
}
func (s *stubRoomRepo) Update(x *models.Room) error                     { return nil }
func (s *stubRoomRepo) Delete(id uint) error                            { return nil }
func (s *stubRoomRepo) GetAll(limit, offset int) ([]models.Room, error) { return nil, nil }
func (s *stubRoomRepo) GetByCapacity(min int) ([]models.Room, error)    { return nil, nil }
func (s *stubRoomRepo) GetLabRooms() ([]models.Room, error)             { return nil, nil }
func (s *stubRoomRepo) GetStickyRooms() ([]models.Room, error)          { return nil, nil }
func (s *stubRoomRepo) GetAvailableRooms(d models.Weekday, st, et string) ([]models.Room, error) {
	return nil, nil
}

type stubSettingsRepo struct {
	settings *models.GenerationSettings
	err      error
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

func (s *stubSettingsRepo) Upsert(x *models.GenerationSettings) error { return nil }

func newServiceForBuildTest(settingsRepo repositories.GenerationSettingsRepository) *TimetableService {
	return NewTimetableService(
		nil, &stubStaffRepo{}, &stubClassRepo{}, &stubModuleRepo{},
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

	req, err := svc.buildSolverRequest(1, false)
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

// TestBuildSolverRequest_NotConfiguredFallsBackToDefaults: fresh deployment
// (no settings row) must NOT fail generation — defaults are used.
func TestBuildSolverRequest_NotConfiguredFallsBackToDefaults(t *testing.T) {
	repo := &stubSettingsRepo{err: repositories.ErrNotConfigured}
	svc := newServiceForBuildTest(repo)

	req, err := svc.buildSolverRequest(1, false)
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

	req, err := svc.buildSolverRequest(1, false)
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

	if _, err := svc.buildSolverRequest(1, false); err == nil {
		t.Fatalf("expected error on settings repo failure, got nil")
	}
}
