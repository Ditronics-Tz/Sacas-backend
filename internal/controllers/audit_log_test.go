package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
)

// listAuditRepo serves a fixed set of entries so the controller's scoping and
// filter handling can be observed.
type listAuditRepo struct {
	entries []models.AuditLog
	// lastFilter records the filter the controller built, so a test can assert
	// the tenant scope was applied rather than trusting the response body.
	lastFilter auditFilterCapture
	lastTarget struct {
		institutionID *uint
		targetType    string
		targetID      string
	}
}

type auditFilterCapture struct {
	institutionID *uint
	action        *models.AuditAction
	outcome       *models.AuditOutcome
	actorID       *uint
	targetType    string
	targetID      string
	fromSet       bool
	toSet         bool
}

func (r *listAuditRepo) Create(*models.AuditLog) error { return nil }
func (r *listAuditRepo) List(filter repositories.AuditLogFilter, limit, offset int) ([]models.AuditLog, error) {
	r.lastFilter = auditFilterCapture{
		institutionID: filter.InstitutionID,
		action:        filter.Action,
		outcome:       filter.Outcome,
		actorID:       filter.ActorID,
		targetType:    filter.TargetType,
		targetID:      filter.TargetID,
		fromSet:       !filter.From.IsZero(),
		toSet:         !filter.To.IsZero(),
	}
	return r.entries, nil
}
func (r *listAuditRepo) Count(repositories.AuditLogFilter) (int64, error) {
	return int64(len(r.entries)), nil
}
func (r *listAuditRepo) ListForTarget(institutionID *uint, targetType, targetID string, limit int) ([]models.AuditLog, error) {
	r.lastTarget.institutionID = institutionID
	r.lastTarget.targetType = targetType
	r.lastTarget.targetID = targetID
	return r.entries, nil
}

func auditRouter(repo *listAuditRepo, role models.UserRole, instID uint) *gin.Engine {
	ctrl := NewAuditLogController(repo, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("role", string(role))
		c.Set("user_id", float64(1))
		c.Set("institution_id", instID)
		c.Next()
	})
	r.GET("/audit", ctrl.List)
	r.GET("/audit/target/:type/:id", ctrl.ListForTarget)
	return r
}

// TestAuditList_TenantIsPinnedToSession is the isolation property: an
// institution's trail is scoped by the session, never by a query parameter.
func TestAuditList_TenantIsPinnedToSession(t *testing.T) {
	repo := &listAuditRepo{}
	r := auditRouter(repo, models.RoleAdmin, tenantA)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/audit", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.lastFilter.institutionID == nil {
		t.Fatal("a tenant request must carry an institution scope")
	}
	if *repo.lastFilter.institutionID != tenantA {
		t.Errorf("expected scope %d, got %d", tenantA, *repo.lastFilter.institutionID)
	}
}

// TestAuditList_PlatformSeesEveryInstitution is the other shape: a platform
// account reads the whole trail.
func TestAuditList_PlatformSeesEveryInstitution(t *testing.T) {
	repo := &listAuditRepo{}
	r := auditRouter(repo, models.RoleSuperAdmin, 0)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/audit", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// No institution filter means "every institution, including platform rows".
	if repo.lastFilter.institutionID != nil {
		t.Errorf("a platform read must not be scoped to one institution, got %v", *repo.lastFilter.institutionID)
	}
}

// TestAuditList_TenantCannotFilterByInstitution stops a tenant from asking for
// another institution's trail. Being told "forbidden" rather than silently
// having the parameter ignored is deliberate.
func TestAuditList_TenantCannotFilterByInstitution(t *testing.T) {
	repo := &listAuditRepo{}
	r := auditRouter(repo, models.RoleAdmin, tenantA)

	req := httptest.NewRequest(http.MethodGet, "/audit?institution_id=2", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when a tenant filters by institution, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAuditList_PlatformCanFilterByInstitution(t *testing.T) {
	repo := &listAuditRepo{}
	r := auditRouter(repo, models.RoleSuperAdmin, 0)

	req := httptest.NewRequest(http.MethodGet, "/audit?institution_id=7", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if repo.lastFilter.institutionID == nil || *repo.lastFilter.institutionID != 7 {
		t.Errorf("expected the filter applied, got %v", repo.lastFilter.institutionID)
	}
}

func TestAuditList_RejectsInvalidFilters(t *testing.T) {
	cases := []string{
		"/audit?action=not_a_real_action",
		"/audit?outcome=maybe",
		"/audit?institution_id=abc",
		"/audit?actor_id=xyz",
		"/audit?from=not-a-date",
		"/audit?to=yesterday",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			repo := &listAuditRepo{}
			r := auditRouter(repo, models.RoleSuperAdmin, 0)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

			// A bad filter is a client error, not a silently ignored one: an
			// ignored date filter would return more than the caller asked for.
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAuditList_ValidFiltersPassThrough(t *testing.T) {
	repo := &listAuditRepo{}
	r := auditRouter(repo, models.RoleSuperAdmin, 0)

	path := "/audit?action=role.change&outcome=denied&actor_id=3&target_type=user&target_id=9" +
		"&from=2026-01-01T00:00:00Z&to=2026-12-31T23:59:59Z"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	f := repo.lastFilter
	if f.action == nil || *f.action != models.AuditRoleChange {
		t.Errorf("action filter not applied: %v", f.action)
	}
	if f.outcome == nil || *f.outcome != models.AuditOutcomeDenied {
		t.Errorf("outcome filter not applied: %v", f.outcome)
	}
	if f.actorID == nil || *f.actorID != 3 {
		t.Errorf("actor filter not applied: %v", f.actorID)
	}
	if f.targetType != "user" || f.targetID != "9" {
		t.Errorf("target filter not applied: %v/%v", f.targetType, f.targetID)
	}
	if !f.fromSet || !f.toSet {
		t.Error("date filters not applied")
	}
}

// TestAuditListForTarget_ScopedToSession covers the object-history shape.
func TestAuditListForTarget_ScopedToSession(t *testing.T) {
	repo := &listAuditRepo{}

	t.Run("tenant", func(t *testing.T) {
		r := auditRouter(repo, models.RoleAdmin, tenantA)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/audit/target/user/42", nil))

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if repo.lastTarget.institutionID == nil || *repo.lastTarget.institutionID != tenantA {
			t.Errorf("expected tenant scope, got %v", repo.lastTarget.institutionID)
		}
		if repo.lastTarget.targetType != "user" || repo.lastTarget.targetID != "42" {
			t.Errorf("target not passed through: %+v", repo.lastTarget)
		}
	})

	t.Run("platform", func(t *testing.T) {
		r := auditRouter(repo, models.RoleSuperAdmin, 0)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/audit/target/user/42", nil))

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if repo.lastTarget.institutionID != nil {
			t.Error("a platform history read should span institutions")
		}
	})
}
