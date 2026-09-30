package controllers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
)

// stubGenerationSettingsRepo implements repositories.GenerationSettingsRepository
// with a platform default row plus per-institution overrides, mirroring the real
// layering.
type stubGenerationSettingsRepo struct {
	// platform is the platform default row (institution_id 0).
	platform *models.GenerationSettings
	// overrides maps institution ID → that institution's override row.
	overrides map[uint]*models.GenerationSettings

	err       error // hard DB error to return from every read
	upsertErr error
	upserts   int
	// lastTarget records the institution the last write targeted, so tests can
	// assert the controller did not cross tenants.
	lastTarget uint
}

func (r *stubGenerationSettingsRepo) Get() (*models.GenerationSettings, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.platform == nil {
		return models.DefaultGenerationSettings(), repositories.ErrNotConfigured
	}
	cp := *r.platform
	return &cp, nil
}

func (r *stubGenerationSettingsRepo) GetOverride(institutionID uint) (*models.GenerationSettings, error) {
	if r.err != nil {
		return nil, r.err
	}
	if repositories.IsPlatformScope(institutionID) {
		return nil, nil
	}
	row, ok := r.overrides[institutionID]
	if !ok {
		return nil, nil
	}
	cp := *row
	return &cp, nil
}

func (r *stubGenerationSettingsRepo) GetForInstitution(institutionID uint) (*models.GenerationSettings, error) {
	platform, platformErr := r.Get()
	if platformErr != nil && !errors.Is(platformErr, repositories.ErrNotConfigured) {
		return nil, platformErr
	}
	override, err := r.GetOverride(institutionID)
	if err != nil {
		return nil, err
	}
	if override == nil {
		return platform, platformErr
	}
	if override.TimeBudgetSec <= 0 {
		override.TimeBudgetSec = platform.TimeBudgetSec
	}
	if len(override.SoftWeights) == 0 {
		override.SoftWeights = platform.SoftWeights
	}
	return override, nil
}

func (r *stubGenerationSettingsRepo) Upsert(s *models.GenerationSettings) error {
	return r.UpsertForInstitution(repositories.PlatformScope, s)
}

func (r *stubGenerationSettingsRepo) UpsertForInstitution(institutionID uint, s *models.GenerationSettings) error {
	r.upserts++
	r.lastTarget = institutionID
	if r.upsertErr != nil {
		return r.upsertErr
	}
	cp := *s
	cp.InstitutionID = institutionID
	if repositories.IsPlatformScope(institutionID) {
		r.platform = &cp
		return nil
	}
	if r.overrides == nil {
		r.overrides = map[uint]*models.GenerationSettings{}
	}
	r.overrides[institutionID] = &cp
	return nil
}

func (r *stubGenerationSettingsRepo) DeleteOverride(institutionID uint) error {
	delete(r.overrides, institutionID)
	return nil
}

// newGenerationSettingsRouter wires the controller as an institution admin
// scoped to institutionID, which is the case the layered settings exist for.
func newGenerationSettingsRouter(repo *stubGenerationSettingsRepo, institutionID uint) *gin.Engine {
	gin.SetMode(gin.TestMode)
	ctrl := NewGenerationSettingsController(repo)
	r := gin.New()
	r.GET("/generation-settings", withTenant(ctrl.Get, institutionID))
	r.PUT("/generation-settings", withTenant(ctrl.Update, institutionID))
	return r
}

func TestGetGenerationSettings_NotConfiguredReturnsDefaults(t *testing.T) {
	r := newGenerationSettingsRouter(&stubGenerationSettingsRepo{}, tenantA)

	req := httptest.NewRequest(http.MethodGet, "/generation-settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with defaults, got %d body=%s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"time_budget_sec":30`)) {
		t.Fatalf("expected default time_budget_sec 30, got %s", w.Body.String())
	}
}

func TestGetGenerationSettings_DBError(t *testing.T) {
	r := newGenerationSettingsRouter(&stubGenerationSettingsRepo{err: errors.New("db down")}, tenantA)

	req := httptest.NewRequest(http.MethodGet, "/generation-settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on DB error, got %d", w.Code)
	}
}

func TestUpdateGenerationSettings_HappyPath(t *testing.T) {
	repo := &stubGenerationSettingsRepo{}
	r := newGenerationSettingsRouter(repo, tenantA)

	body := `{"time_budget_sec": 60, "soft_weights": {"preferred_start_weight": 2.5, "session_spread_weight": 1}}`
	req := httptest.NewRequest(http.MethodPut, "/generation-settings", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if repo.upserts != 1 {
		t.Fatalf("expected 1 upsert, got %d", repo.upserts)
	}
	// The write must land on this institution's override, never on the
	// platform default row.
	if repo.lastTarget != tenantA {
		t.Fatalf("expected override for institution %d, got target %d", tenantA, repo.lastTarget)
	}
	if repo.overrides[tenantA].TimeBudgetSec != 60 {
		t.Fatalf("expected stored time_budget_sec 60, got %v", repo.overrides[tenantA].TimeBudgetSec)
	}
}

func TestUpdateGenerationSettings_ValidationFailures(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"zero budget", `{"time_budget_sec": 0}`},
		{"negative budget", `{"time_budget_sec": -5}`},
		{"budget over cap", `{"time_budget_sec": 301}`},
		{"unknown weight key", `{"soft_weights": {"nonsense_weight": 1}}`},
		{"negative weight", `{"soft_weights": {"preferred_start_weight": -1}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubGenerationSettingsRepo{}
			r := newGenerationSettingsRouter(repo, tenantA)

			req := httptest.NewRequest(http.MethodPut, "/generation-settings", bytes.NewBufferString(tc.body))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
			}
			if repo.upserts != 0 {
				t.Fatalf("invalid payload must not reach the repository")
			}
		})
	}
}

func TestUpdateGenerationSettings_UnknownKeyListsAllowedKeys(t *testing.T) {
	r := newGenerationSettingsRouter(&stubGenerationSettingsRepo{}, tenantA)

	req := httptest.NewRequest(http.MethodPut, "/generation-settings",
		bytes.NewBufferString(`{"soft_weights": {"bogus": 1}}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	for _, key := range models.AllowedSoftWeightKeys {
		if !bytes.Contains(w.Body.Bytes(), []byte(key)) {
			t.Fatalf("error should list allowed key %q, got %s", key, w.Body.String())
		}
	}
}

func TestUpdateGenerationSettings_PartialUpdateKeepsOtherFields(t *testing.T) {
	repo := &stubGenerationSettingsRepo{
		platform: &models.GenerationSettings{
			ID:            models.SingletonID,
			InstitutionID: models.PlatformScope,
			TimeBudgetSec: 30,
			SoftWeights:   datatypes.JSON(`{}`),
		},
		overrides: map[uint]*models.GenerationSettings{
			tenantA: {
				ID:            2,
				InstitutionID: tenantA,
				TimeBudgetSec: 45,
				SoftWeights:   datatypes.JSON(`{"preferred_start_weight": 3}`),
			},
		},
	}
	r := newGenerationSettingsRouter(repo, tenantA)

	// Only update the budget; weights must be preserved.
	req := httptest.NewRequest(http.MethodPut, "/generation-settings",
		bytes.NewBufferString(`{"time_budget_sec": 90}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if repo.overrides[tenantA].TimeBudgetSec != 90 {
		t.Fatalf("expected budget 90, got %v", repo.overrides[tenantA].TimeBudgetSec)
	}
	var weights map[string]float64
	if err := json.Unmarshal(repo.overrides[tenantA].SoftWeights, &weights); err != nil {
		t.Fatalf("stored weights not valid JSON: %v", err)
	}
	if weights["preferred_start_weight"] != 3 || len(weights) != 1 {
		t.Fatalf("expected weights preserved, got %s", repo.overrides[tenantA].SoftWeights)
	}
}

// TestGenerationSettings_InheritsPlatformDefault verifies the layering: an
// institution with no override sees the platform default.
func TestGenerationSettings_InheritsPlatformDefault(t *testing.T) {
	repo := &stubGenerationSettingsRepo{
		platform: &models.GenerationSettings{
			ID:            models.SingletonID,
			InstitutionID: models.PlatformScope,
			TimeBudgetSec: 120,
			SoftWeights:   datatypes.JSON(`{"session_spread_weight": 2}`),
		},
		overrides: map[uint]*models.GenerationSettings{},
	}
	r := newGenerationSettingsRouter(repo, tenantB)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/generation-settings", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"time_budget_sec":120`)) {
		t.Fatalf("expected inherited platform budget 120, got %s", w.Body.String())
	}
}

// TestGenerationSettings_CrossTenantWriteRejected verifies the isolation rule
// for this controller: an institution admin cannot write another institution's
// override, nor the platform default row.
func TestGenerationSettings_CrossTenantWriteRejected(t *testing.T) {
	t.Run("cannot target another institution", func(t *testing.T) {
		repo := &stubGenerationSettingsRepo{overrides: map[uint]*models.GenerationSettings{}}
		r := newGenerationSettingsRouter(repo, tenantA)

		req := httptest.NewRequest(http.MethodPut, "/generation-settings",
			bytes.NewBufferString(`{"time_budget_sec": 10, "institution_id": 2}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 when targeting another institution, got %d body=%s", w.Code, w.Body.String())
		}
		if repo.upserts != 0 {
			t.Fatal("cross-tenant write reached the repository")
		}
	})

	t.Run("cannot target the platform default", func(t *testing.T) {
		repo := &stubGenerationSettingsRepo{overrides: map[uint]*models.GenerationSettings{}}
		r := newGenerationSettingsRouter(repo, tenantA)

		req := httptest.NewRequest(http.MethodPut, "/generation-settings",
			bytes.NewBufferString(`{"time_budget_sec": 10, "institution_id": 0}`))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 when targeting the platform row, got %d", w.Code)
		}
		if repo.upserts != 0 {
			t.Fatal("write to the platform row reached the repository")
		}
	})
}
