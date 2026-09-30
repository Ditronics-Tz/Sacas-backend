package controllers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go_boilerplate/internal/models"
)

// This file holds the per-controller tenant isolation tests. Each one follows
// the same shape: two institutions own one record each, the request runs as
// institution A, and the test asserts that A cannot see, mutate, or delete B's
// record — and never receives B's data in the response.
//
// A 404 (not 403) is the expected result, so the API does not confirm that
// another tenant's record exists.

// isolationRouter builds a router whose handlers all run scoped to institution
// A, the way TenantMiddleware would set it up.
func isolationRouter(t *testing.T, register func(r *gin.Engine)) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	register(r)
	return r
}

// --- Class -------------------------------------------------------------------

func TestClass_CrossTenantIsolation(t *testing.T) {
	repo := newStubClassRepo()
	repo.seed(&models.Class{
		ID: 1, Name: "A-Class", CourseID: 1, Year: 1, NumberOfStudents: 30,
		InstitutionID: tenantA,
	})
	repo.seed(&models.Class{
		ID: 2, Name: "B-Class", CourseID: 1, Year: 1, NumberOfStudents: 30,
		InstitutionID: tenantB,
	})

	ctrl := NewClassController(repo)
	r := isolationRouter(t, func(r *gin.Engine) {
		r.GET("/classes", withTenant(ctrl.GetAllClasses, tenantA))
		r.GET("/classes/:id", withTenant(ctrl.GetClass, tenantA))
		r.PUT("/classes/:id", withTenant(ctrl.UpdateClass, tenantA))
		r.DELETE("/classes/:id", withTenant(ctrl.DeleteClass, tenantA))
	})

	t.Run("list only returns own tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/classes?limit=50", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		if contains(w.Body.String(), "B-Class") {
			t.Fatalf("list leaked another tenant's class: %s", w.Body.String())
		}
		if !contains(w.Body.String(), "A-Class") {
			t.Fatalf("list missing own class: %s", w.Body.String())
		}
	})

	t.Run("get other tenant is 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/classes/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("update other tenant is 404 and does not mutate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/classes/2", bytes.NewBufferString(`{"name":"Hijacked"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if repo.items[2].Name != "B-Class" {
			t.Fatalf("cross-tenant update mutated the record: %q", repo.items[2].Name)
		}
	})

	t.Run("delete other tenant is 404 and does not delete", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/classes/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if _, ok := repo.items[2]; !ok {
			t.Fatal("cross-tenant delete removed the record")
		}
	})
}

// --- Course ------------------------------------------------------------------

func TestCourse_CrossTenantIsolation(t *testing.T) {
	repo := newStubCourseRepo()
	repo.seed(&models.Course{ID: 1, Name: "A-Course", FacultyID: 1, InstitutionID: tenantA})
	repo.seed(&models.Course{ID: 2, Name: "B-Course", FacultyID: 1, InstitutionID: tenantB})

	ctrl := NewCourseController(repo)
	r := isolationRouter(t, func(r *gin.Engine) {
		r.GET("/courses", withTenant(ctrl.GetAllCourses, tenantA))
		r.GET("/courses/:id", withTenant(ctrl.GetCourse, tenantA))
		r.PUT("/courses/:id", withTenant(ctrl.UpdateCourse, tenantA))
		r.DELETE("/courses/:id", withTenant(ctrl.DeleteCourse, tenantA))
	})

	t.Run("list only returns own tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/courses?limit=50", nil))
		if contains(w.Body.String(), "B-Course") {
			t.Fatalf("list leaked another tenant's course: %s", w.Body.String())
		}
		if !contains(w.Body.String(), "A-Course") {
			t.Fatalf("list missing own course: %s", w.Body.String())
		}
	})

	t.Run("get other tenant is 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/courses/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("update other tenant is 404 and does not mutate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/courses/2", bytes.NewBufferString(`{"name":"Hijacked"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if repo.items[2].Name != "B-Course" {
			t.Fatalf("cross-tenant update mutated the record: %q", repo.items[2].Name)
		}
	})

	t.Run("delete other tenant is 404 and does not delete", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/courses/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if _, ok := repo.items[2]; !ok {
			t.Fatal("cross-tenant delete removed the record")
		}
	})
}

// --- Module ------------------------------------------------------------------

func TestModule_CrossTenantIsolation(t *testing.T) {
	courseRepo := newStubCourseRepo()
	// Each tenant has its own course 1 in this fixture, so the only thing
	// distinguishing them is the institution on the module itself.
	moduleRepo := newStubModuleRepo()
	moduleRepo.seed(&models.Module{
		ID: 1, Name: "A-Module", Type: models.ModuleTypeCore, CreditHours: 2,
		InstitutionID: tenantA,
	})
	moduleRepo.seed(&models.Module{
		ID: 2, Name: "B-Module", Type: models.ModuleTypeCore, CreditHours: 2,
		InstitutionID: tenantB,
	})

	ctrl := NewModuleController(moduleRepo, courseRepo)
	r := isolationRouter(t, func(r *gin.Engine) {
		r.GET("/modules", withTenant(ctrl.GetAllModules, tenantA))
		r.GET("/modules/:id", withTenant(ctrl.GetModule, tenantA))
		r.PUT("/modules/:id", withTenant(ctrl.UpdateModule, tenantA))
		r.DELETE("/modules/:id", withTenant(ctrl.DeleteModule, tenantA))
		r.POST("/modules", withTenant(ctrl.CreateModule, tenantA))
	})

	t.Run("list only returns own tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/modules?limit=50", nil))
		if contains(w.Body.String(), "B-Module") {
			t.Fatalf("list leaked another tenant's module: %s", w.Body.String())
		}
		if !contains(w.Body.String(), "A-Module") {
			t.Fatalf("list missing own module: %s", w.Body.String())
		}
	})

	t.Run("get other tenant is 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/modules/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("update other tenant is 404 and does not mutate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/modules/2", bytes.NewBufferString(`{"name":"Hijacked"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if moduleRepo.items[2].Name != "B-Module" {
			t.Fatalf("cross-tenant update mutated the record: %q", moduleRepo.items[2].Name)
		}
	})

	t.Run("delete other tenant is 404 and does not delete", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/modules/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if _, ok := moduleRepo.items[2]; !ok {
			t.Fatal("cross-tenant delete removed the record")
		}
	})

	t.Run("cannot attach to another tenant's course", func(t *testing.T) {
		// Course 5 belongs to institution B.
		courseRepo.seed(&models.Course{ID: 5, Name: "B-Course", FacultyID: 1, InstitutionID: tenantB})

		req := httptest.NewRequest(http.MethodPost, "/modules",
			bytes.NewBufferString(`{"name":"Sneaky","course_id":5,"credit_hours":2,"type":"core"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 when referencing a cross-tenant course, got %d body=%s", w.Code, w.Body.String())
		}
		for _, m := range moduleRepo.items {
			if m.Name == "Sneaky" {
				t.Fatal("module was created against another tenant's course")
			}
		}
	})
}

// --- Room --------------------------------------------------------------------

func TestRoom_CrossTenantIsolation(t *testing.T) {
	repo := newStubRoomRepo()
	repo.seed(&models.Room{ID: 1, Name: "A-Room", Capacity: 50, InstitutionID: tenantA})
	repo.seed(&models.Room{ID: 2, Name: "B-Room", Capacity: 50, InstitutionID: tenantB})

	ctrl := NewRoomController(repo)
	r := isolationRouter(t, func(r *gin.Engine) {
		r.GET("/rooms", withTenant(ctrl.GetAllRooms, tenantA))
		r.GET("/rooms/:id", withTenant(ctrl.GetRoom, tenantA))
		r.PUT("/rooms/:id", withTenant(ctrl.UpdateRoom, tenantA))
		r.DELETE("/rooms/:id", withTenant(ctrl.DeleteRoom, tenantA))
	})

	t.Run("list only returns own tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rooms?limit=50", nil))
		if contains(w.Body.String(), "B-Room") {
			t.Fatalf("list leaked another tenant's room: %s", w.Body.String())
		}
		if !contains(w.Body.String(), "A-Room") {
			t.Fatalf("list missing own room: %s", w.Body.String())
		}
	})

	t.Run("get other tenant is 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rooms/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("update other tenant is 404 and does not mutate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/rooms/2", bytes.NewBufferString(`{"name":"Hijacked"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if repo.items[2].Name != "B-Room" {
			t.Fatalf("cross-tenant update mutated the record: %q", repo.items[2].Name)
		}
	})

	t.Run("delete other tenant is 404 and does not delete", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/rooms/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if _, ok := repo.items[2]; !ok {
			t.Fatal("cross-tenant delete removed the record")
		}
	})
}

// --- Subject -----------------------------------------------------------------

func TestSubject_CrossTenantIsolation(t *testing.T) {
	repo := newStubSubjectRepo()
	repo.seed(&models.Subject{ID: 1, Name: "A-Subject", CreditHours: 2, InstitutionID: tenantA})
	repo.seed(&models.Subject{ID: 2, Name: "B-Subject", CreditHours: 2, InstitutionID: tenantB})

	ctrl := NewSubjectController(repo)
	r := isolationRouter(t, func(r *gin.Engine) {
		r.GET("/subjects", withTenant(ctrl.GetAllSubjects, tenantA))
		r.GET("/subjects/:id", withTenant(ctrl.GetSubject, tenantA))
		r.PUT("/subjects/:id", withTenant(ctrl.UpdateSubject, tenantA))
		r.DELETE("/subjects/:id", withTenant(ctrl.DeleteSubject, tenantA))
	})

	t.Run("list only returns own tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/subjects?limit=50", nil))
		if contains(w.Body.String(), "B-Subject") {
			t.Fatalf("list leaked another tenant's subject: %s", w.Body.String())
		}
		if !contains(w.Body.String(), "A-Subject") {
			t.Fatalf("list missing own subject: %s", w.Body.String())
		}
	})

	t.Run("get other tenant is 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/subjects/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("update other tenant is 404 and does not mutate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/subjects/2", bytes.NewBufferString(`{"name":"Hijacked"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if repo.items[2].Name != "B-Subject" {
			t.Fatalf("cross-tenant update mutated the record: %q", repo.items[2].Name)
		}
	})

	t.Run("delete other tenant is 404 and does not delete", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/subjects/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if _, ok := repo.items[2]; !ok {
			t.Fatal("cross-tenant delete removed the record")
		}
	})
}

// --- Timetable ---------------------------------------------------------------

func TestTimetable_CrossTenantIsolation(t *testing.T) {
	classRepo := newStubClassRepo()
	classRepo.seed(&models.Class{
		ID: 1, Name: "A-Class", CourseID: 1, Year: 1, NumberOfStudents: 30,
		InstitutionID: tenantA,
	})
	classRepo.seed(&models.Class{
		ID: 2, Name: "B-Class", CourseID: 1, Year: 1, NumberOfStudents: 30,
		InstitutionID: tenantB,
	})

	staffRepo := newStubStaffRepo()
	staffRepo.seedStaff(&models.Staff{ID: 1, Name: "A-Staff", InstitutionID: tenantA})
	staffRepo.seedStaff(&models.Staff{ID: 2, Name: "B-Staff", InstitutionID: tenantB})

	roomRepo := newStubRoomRepo()
	roomRepo.seed(&models.Room{ID: 1, Name: "A-Room", Capacity: 50, InstitutionID: tenantA})
	roomRepo.seed(&models.Room{ID: 2, Name: "B-Room", Capacity: 50, InstitutionID: tenantB})

	ttRepo := newStubTimetableRepo()
	ttRepo.seed(&models.Timetable{
		ID: 1, ClassID: 1, StaffID: 1, RoomID: 1, InstitutionID: tenantA,
		Day: models.Monday, StartTime: "08:00", EndTime: "09:00",
	})
	ttRepo.seed(&models.Timetable{
		ID: 2, ClassID: 2, StaffID: 2, RoomID: 2, InstitutionID: tenantB,
		Day: models.Tuesday, StartTime: "10:00", EndTime: "11:00",
	})

	ctrl := NewTimetableController(ttRepo, staffRepo, classRepo, roomRepo, nil)
	r := isolationRouter(t, func(r *gin.Engine) {
		r.GET("/timetable/:id", withTenant(ctrl.GetTimetable, tenantA))
		r.GET("/class/:class_id", withTenant(ctrl.GetTimetableByClass, tenantA))
		r.GET("/by-staff/:staff_id", withTenant(ctrl.GetTimetableByStaff, tenantA))
		r.PUT("/timetable/:id", withTenant(ctrl.UpdateTimetable, tenantA))
		r.DELETE("/timetable/:id", withTenant(ctrl.DeleteTimetable, tenantA))
	})

	t.Run("get other tenant's entry is 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/timetable/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if contains(w.Body.String(), "B-Room") {
			t.Fatalf("response leaked another tenant's entry: %s", w.Body.String())
		}
	})

	t.Run("get other tenant's class timetable is 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/class/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("get other tenant's staff timetable is 404", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/by-staff/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("own class returns only own entries", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/class/1", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		if contains(w.Body.String(), `"id":2`) {
			t.Fatalf("own class response included another tenant's entry: %s", w.Body.String())
		}
	})

	t.Run("update other tenant is 404 and does not mutate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/timetable/2",
			bytes.NewBufferString(`{"start_time":"07:00"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if ttRepo.items[2].StartTime != "10:00" {
			t.Fatalf("cross-tenant update mutated the record: %q", ttRepo.items[2].StartTime)
		}
	})

	t.Run("delete other tenant is 404 and does not delete", func(t *testing.T) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/timetable/2", nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if _, ok := ttRepo.items[2]; !ok {
			t.Fatal("cross-tenant delete removed the record")
		}
	})
}
