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

// stubGenerationSettingsRepo implements repositories.GenerationSettingsRepository.
type stubGenerationSettingsRepo struct {
	row       *models.GenerationSettings
	err       error // hard DB error to return from Get/Upsert
	upsertErr error
	upserts   int
}

func (r *stubGenerationSettingsRepo) Get() (*models.GenerationSettings, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.row == nil {
		return models.DefaultGenerationSettings(), repositories.ErrNotConfigured
	}
	cp := *r.row
	return &cp, nil
}

func (r *stubGenerationSettingsRepo) Upsert(s *models.GenerationSettings) error {
	r.upserts++
	if r.upsertErr != nil {
		return r.upsertErr
	}
	cp := *s
	r.row = &cp
	return nil
}

func newGenerationSettingsRouter(repo *stubGenerationSettingsRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	ctrl := NewGenerationSettingsController(repo)
	r := gin.New()
	r.GET("/generation-settings", ctrl.Get)
	r.PUT("/generation-settings", ctrl.Update)
	return r
}

func TestGetGenerationSettings_NotConfiguredReturnsDefaults(t *testing.T) {
	r := newGenerationSettingsRouter(&stubGenerationSettingsRepo{})

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
	r := newGenerationSettingsRouter(&stubGenerationSettingsRepo{err: errors.New("db down")})

	req := httptest.NewRequest(http.MethodGet, "/generation-settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on DB error, got %d", w.Code)
	}
}

func TestUpdateGenerationSettings_HappyPath(t *testing.T) {
	repo := &stubGenerationSettingsRepo{}
	r := newGenerationSettingsRouter(repo)

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
	if repo.row.TimeBudgetSec != 60 {
		t.Fatalf("expected stored time_budget_sec 60, got %v", repo.row.TimeBudgetSec)
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
			r := newGenerationSettingsRouter(repo)

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
	r := newGenerationSettingsRouter(&stubGenerationSettingsRepo{})

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
	repo := &stubGenerationSettingsRepo{row: &models.GenerationSettings{
		ID:            models.SingletonID,
		TimeBudgetSec: 45,
		SoftWeights:   datatypes.JSON(`{"preferred_start_weight": 3}`),
	}}
	r := newGenerationSettingsRouter(repo)

	// Only update the budget; weights must be preserved.
	req := httptest.NewRequest(http.MethodPut, "/generation-settings",
		bytes.NewBufferString(`{"time_budget_sec": 90}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if repo.row.TimeBudgetSec != 90 {
		t.Fatalf("expected budget 90, got %v", repo.row.TimeBudgetSec)
	}
	var weights map[string]float64
	if err := json.Unmarshal(repo.row.SoftWeights, &weights); err != nil {
		t.Fatalf("stored weights not valid JSON: %v", err)
	}
	if weights["preferred_start_weight"] != 3 || len(weights) != 1 {
		t.Fatalf("expected weights preserved, got %s", repo.row.SoftWeights)
	}
}
