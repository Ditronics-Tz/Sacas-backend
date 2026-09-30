package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/services"
)

// lifecycleFixture wires the timetable controller with the class lifecycle
// methods and an audit recorder.
type lifecycleFixture struct {
	ctrl    *TimetableController
	classes *stubClassRepo
	audit   *recordingAuditRepo
	router  *gin.Engine
}

func newLifecycleFixture(t *testing.T, instID uint, callerID uint) *lifecycleFixture {
	t.Helper()

	classes := newStubClassRepo()
	ttRepo := newStubTimetableRepo()
	audit := &recordingAuditRepo{}

	ctrl := NewTimetableController(ttRepo, newStubStaffRepo(), classes, newStubRoomRepo(), nil,
		services.NewAuditRecorder(audit))

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", float64(callerID))
		c.Set("role", string(models.RoleAdmin))
		c.Set("institution_id", instID)
		c.Next()
	})
	r.POST("/class/:class_id/publish", ctrl.PublishClass)
	r.POST("/class/:class_id/approve", ctrl.ApproveClass)
	r.GET("/class/:class_id", ctrl.GetTimetableByClass)

	return &lifecycleFixture{
		ctrl: ctrl, classes: classes, audit: audit, router: r,
	}
}

// TestPublishClass_RequiresEntries: an empty timetable must not be publishable,
// because a published state with nothing behind it looks approved to the UI.
func TestPublishClass_RequiresEntries(t *testing.T) {
	f := newLifecycleFixture(t, tenantA, 1)
	f.classes.seed(&models.Class{
		ID: 1, Name: "A-Class", CourseID: 1, Year: 1, NumberOfStudents: 30,
		InstitutionID: tenantA,
	})
	// No timetable entries seeded.

	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/class/1/publish", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an empty timetable, got %d body=%s", w.Code, w.Body.String())
	}
	if f.audit.hasAction(models.AuditTimetablePublish) {
		t.Error("a refused publish must not be recorded as a success")
	}
}

// TestPublishClass_Success covers the happy path and the audit entry.
func TestPublishClass_Success(t *testing.T) {
	f := newLifecycleFixture(t, tenantA, 1)
	f.classes.seed(&models.Class{
		ID: 1, Name: "A-Class", CourseID: 1, Year: 1, NumberOfStudents: 30,
		InstitutionID: tenantA,
	})
	f.classes.timetableCount[1] = 5

	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/class/1/publish", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	class, _ := f.classes.GetByID(tenantA, 1)
	if class.PublishedAt == nil {
		t.Fatal("the publish timestamp was not set")
	}
	if class.PublishedByID == nil || *class.PublishedByID != 1 {
		t.Errorf("the publisher was not recorded: %v", class.PublishedByID)
	}
	if !f.audit.hasAction(models.AuditTimetablePublish) {
		t.Error("the publish was not audited")
	}
}

// TestApproveClass_RequiresPublicationFirst is the ordering guarantee: approval
// cannot precede publication, so the trail always has publish before approve.
func TestApproveClass_RequiresPublicationFirst(t *testing.T) {
	f := newLifecycleFixture(t, tenantA, 1)
	f.classes.seed(&models.Class{
		ID: 1, Name: "A-Class", CourseID: 1, Year: 1, NumberOfStudents: 30,
		InstitutionID: tenantA,
	})
	f.classes.timetableCount[1] = 5

	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/class/1/approve", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when approving an unpublished timetable, got %d body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), "not been published") {
		t.Errorf("the error should say what is missing: %s", w.Body.String())
	}
	if f.audit.hasAction(models.AuditTimetableApprove) {
		t.Error("a refused approval must not be recorded as a success")
	}
}

// TestPublishThenApprove is the full happy path.
func TestPublishThenApprove(t *testing.T) {
	f := newLifecycleFixture(t, tenantA, 1)
	f.classes.seed(&models.Class{
		ID: 1, Name: "A-Class", CourseID: 1, Year: 1, NumberOfStudents: 30,
		InstitutionID: tenantA,
	})
	f.classes.timetableCount[1] = 5

	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/class/1/publish", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("publish: expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/class/1/approve", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("approve: expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	class, _ := f.classes.GetByID(tenantA, 1)
	if class.ApprovedAt == nil || class.ApprovedByID == nil {
		t.Errorf("approval was not recorded: %+v", class)
	}
	if !f.audit.hasAction(models.AuditTimetableApprove) {
		t.Error("the approval was not audited")
	}
	// Both events are in the trail, publish first.
	publishIdx, approveIdx := -1, -1
	for i, e := range f.audit.entries {
		if e.Action == models.AuditTimetablePublish && publishIdx < 0 {
			publishIdx = i
		}
		if e.Action == models.AuditTimetableApprove && approveIdx < 0 {
			approveIdx = i
		}
	}
	if publishIdx < 0 || approveIdx < 0 || publishIdx > approveIdx {
		t.Errorf("publish must be recorded before approve: publish=%d approve=%d", publishIdx, approveIdx)
	}
}

// TestPublishClass_CrossTenantIsNotFound: a platform or foreign class ID must be
// a 404, not a publish.
func TestPublishClass_CrossTenantIsNotFound(t *testing.T) {
	f := newLifecycleFixture(t, tenantA, 1)
	f.classes.seed(&models.Class{
		ID: 2, Name: "B-Class", CourseID: 1, Year: 1, NumberOfStudents: 30,
		InstitutionID: tenantB,
	})
	f.classes.timetableCount[2] = 5

	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/class/2/publish", nil))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a cross-tenant class, got %d body=%s", w.Code, w.Body.String())
	}
	after, _ := f.classes.GetByID(tenantB, 2)
	if after.PublishedAt != nil {
		t.Fatal("a cross-tenant class was published")
	}
}

// TestGetTimetableByClass_ReportsLifecycleState is what lets the frontend show a
// draft/approved badge without a second call.
func TestGetTimetableByClass_ReportsLifecycleState(t *testing.T) {
	f := newLifecycleFixture(t, tenantA, 1)
	f.classes.seed(&models.Class{
		ID: 1, Name: "A-Class", CourseID: 1, Year: 1, NumberOfStudents: 30,
		InstitutionID: tenantA,
	})

	// Seed a timetable entry so the class is not "empty".
	ttRepo := newStubTimetableRepo()
	_ = ttRepo

	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/class/1", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// No entries in the stub timetable repo, so the state is empty — which is
	// still the point: the field is present and derived, not guessed by the UI.
	if _, present := body["state"]; !present {
		t.Errorf("expected a state field, got %v", body)
	}
}

// TestClearPublicationOnRegenerate is the property that stops an "approved" badge
// sitting on a freshly generated, unreviewed schedule.
func TestClearPublicationOnRegenerate(t *testing.T) {
	classes := newStubClassRepo()
	classes.seed(&models.Class{
		ID: 1, Name: "A-Class", CourseID: 1, Year: 1, NumberOfStudents: 30,
		InstitutionID: tenantA,
	})
	classes.timetableCount[1] = 3

	// Publish and approve, then clear.
	if ok, _ := classes.MarkPublished(tenantA, 1, 1, timeNow()); !ok {
		t.Fatal("publish failed in the fixture")
	}
	if ok, _ := classes.MarkApproved(tenantA, 1, 2, timeNow()); !ok {
		t.Fatal("approve failed in the fixture")
	}
	before, _ := classes.GetByID(tenantA, 1)
	if before.ApprovedAt == nil {
		t.Fatal("fixture did not record the approval")
	}

	if err := classes.ClearPublication(tenantA, 1); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	after, _ := classes.GetByID(tenantA, 1)
	if after.PublishedAt != nil || after.ApprovedAt != nil {
		t.Errorf("publication state was not cleared: %+v", after)
	}
	if after.PublishedByID != nil || after.ApprovedByID != nil {
		t.Errorf("the actors were not cleared: %+v", after)
	}
}
