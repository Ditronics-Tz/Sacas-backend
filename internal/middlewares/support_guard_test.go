package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/auth"
	"go_boilerplate/internal/models"
)

// TestDenyWriteForSupport is the read/write split for a support session: reads
// pass, writes are refused, and the refusal is method-based so it holds even if
// the support role is later granted a write permission by mistake.
func TestDenyWriteForSupport(t *testing.T) {
	cases := []struct {
		method     string
		wantStatus int
	}{
		{http.MethodGet, http.StatusOK},
		{http.MethodHead, http.StatusOK},
		{http.MethodOptions, http.StatusOK},
		{http.MethodPost, http.StatusForbidden},
		{http.MethodPut, http.StatusForbidden},
		{http.MethodPatch, http.StatusForbidden},
		{http.MethodDelete, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			r := gin.New()
			r.Handle(tc.method, "/x",
				attachSupportRequest(1),
				DenyWriteForSupport(),
				func(c *gin.Context) { c.Status(http.StatusOK) },
			)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, "/x", nil))
			if w.Code != tc.wantStatus {
				t.Fatalf("%s: got %d, want %d", tc.method, w.Code, tc.wantStatus)
			}
		})
	}
}

// TestDenyWriteForSupport_PassesRealUsersThrough: the guard must not affect an
// ordinary account.
func TestDenyWriteForSupport_PassesRealUsersThrough(t *testing.T) {
	for _, role := range []models.UserRole{models.RoleAdmin, models.RoleAcademicCoord, models.RoleUser} {
		t.Run(string(role), func(t *testing.T) {
			r := gin.New()
			r.POST("/x",
				attachRole(string(role), 1),
				DenyWriteForSupport(),
				func(c *gin.Context) { c.Status(http.StatusOK) },
			)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/x", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("role=%s: a real user was blocked, got %d", role, w.Code)
			}
		})
	}
}

// TestDenySupportSession_BlocksEverything covers the exams and import groups,
// where a support session is refused outright rather than read-only.
func TestDenySupportSession_BlocksEverything(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			r := gin.New()
			r.Handle(method, "/x",
				attachSupportRequest(1),
				DenySupportSession(),
				func(c *gin.Context) { c.Status(http.StatusOK) },
			)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(method, "/x", nil))
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s: a support session reached the handler, got %d", method, w.Code)
			}
		})
	}
}

// TestDenyWriteForSupport_RequiresSupportRead: the read allowance is positive —
// it is granted by holding support:read, not merely by not holding a write.
func TestDenyWriteForSupport_RequiresSupportRead(t *testing.T) {
	// A support-shaped request whose role holds no support:read gets no read
	// path either. This is what makes the guard self-describing rather than
	// dependent on the absence of write permissions.
	r := gin.New()
	r.GET("/x",
		func(c *gin.Context) {
			c.Set(ContextKeyIsSupport, true)
			c.Set(ContextKeyRole, string(models.RoleUser)) // holds reads, not support:read
			c.Set(ContextKeyInstitutionID, uint(1))
			c.Next()
		},
		DenyWriteForSupport(),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without support:read, got %d", w.Code)
	}

	// And the real support role does hold it.
	if !auth.HasPermission(models.RoleSupport, auth.PermSupportRead) {
		t.Error("the support role should hold support:read")
	}
}

func TestIsSafeMethod(t *testing.T) {
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		if !isSafeMethod(m) {
			t.Errorf("%s should be safe", m)
		}
	}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if isSafeMethod(m) {
			t.Errorf("%s should not be safe", m)
		}
	}
}

func TestCallerRole(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(ContextKeyRole, string(models.RoleExamCoord))
	role, ok := CallerRole(c)
	if !ok || role != models.RoleExamCoord {
		t.Fatalf("got %s ok=%v", role, ok)
	}

	// No role resolved is reported as such, so a misconfigured chain is visible.
	empty, _ := gin.CreateTestContext(httptest.NewRecorder())
	if _, ok := CallerRole(empty); ok {
		t.Error("expected ok=false when no role is set")
	}
}
