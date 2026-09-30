package services

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
)

// stubAuditRepo captures what the recorder writes.
type stubAuditRepo struct {
	entries   []*models.AuditLog
	createErr error
}

func (r *stubAuditRepo) Create(entry *models.AuditLog) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.entries = append(r.entries, entry)
	return nil
}
func (r *stubAuditRepo) List(repositories.AuditLogFilter, int, int) ([]models.AuditLog, error) {
	return nil, nil
}
func (r *stubAuditRepo) Count(repositories.AuditLogFilter) (int64, error) { return 0, nil }
func (r *stubAuditRepo) ListForTarget(*uint, string, string, int) ([]models.AuditLog, error) {
	return nil, nil
}

func init() { gin.SetMode(gin.TestMode) }

// actorContext builds a request context that looks like one TenantMiddleware
// produced: the user object, the role, and a resolved institution.
func actorContext(role models.UserRole, instID *uint) (*gin.Context, *models.User) {
	user := &models.User{
		ID: 42, Email: "actor@x.com", Role: role, IsActive: true,
		InstitutionID: instID,
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/x", nil)
	c.Set("user", user)
	c.Set("user_id", user.ID)
	c.Set("role", string(role))
	c.Set("email", user.Email)
	if instID != nil {
		c.Set("institution_id", *instID)
	}
	return c, user
}

func TestAuditRecorder_RecordsActorAndTenant(t *testing.T) {
	repo := &stubAuditRepo{}
	rec := NewAuditRecorder(repo)
	inst := uint(7)

	c, _ := actorContext(models.RoleAdmin, &inst)
	rec.Record(c, models.AuditRoleChange, "user", "99", "target@example.com", map[string]any{
		"from": "user",
		"to":   "administrator",
	})

	if len(repo.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(repo.entries))
	}
	e := repo.entries[0]
	if e.ActorID != 42 || e.ActorEmail != "actor@x.com" || e.ActorRole != string(models.RoleAdmin) {
		t.Errorf("actor not captured correctly: %+v", e)
	}
	if e.InstitutionID == nil || *e.InstitutionID != 7 {
		t.Errorf("expected institution 7, got %v", e.InstitutionID)
	}
	if e.Action != models.AuditRoleChange {
		t.Errorf("wrong action: %s", e.Action)
	}
	if e.Outcome != models.AuditOutcomeSuccess {
		t.Errorf("wrong outcome: %s", e.Outcome)
	}
	if e.TargetID != "99" || e.TargetType != "user" {
		t.Errorf("target not captured: %+v", e)
	}
	if e.RecordedAt.IsZero() {
		t.Error("RecordedAt must be set")
	}
	if e.IPAddress == "" {
		t.Error("expected the request IP to be recorded")
	}
}

// TestAuditRecorder_PlatformActionHasNoInstitution is what distinguishes a
// platform action from a tenant action in the trail.
func TestAuditRecorder_PlatformActionHasNoInstitution(t *testing.T) {
	repo := &stubAuditRepo{}
	rec := NewAuditRecorder(repo)

	// Platform scope is 0, and no institution_id is set on the context.
	c, _ := actorContext(models.RoleSuperAdmin, nil)
	rec.Record(c, models.AuditInstitutionCreate, "institution", "5", "New College", nil)

	if len(repo.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(repo.entries))
	}
	if repo.entries[0].InstitutionID != nil {
		t.Errorf("a platform action must have a NULL institution, got %v", *repo.entries[0].InstitutionID)
	}
}

func TestAuditRecorder_RecordDeniedSetsOutcome(t *testing.T) {
	repo := &stubAuditRepo{}
	rec := NewAuditRecorder(repo)
	inst := uint(1)

	c, _ := actorContext(models.RoleAdmin, &inst)
	rec.RecordDenied(c, models.AuditUserDelete, "user", "5", "victim@x.com", "not permitted")

	e := repo.entries[0]
	if e.Outcome != models.AuditOutcomeDenied {
		t.Errorf("expected outcome denied, got %s", e.Outcome)
	}
	if e.Detail != "not permitted" {
		t.Errorf("expected the reason in Detail, got %q", e.Detail)
	}
}

// TestAuditRecorder_FailedWriteDoesNotPanic is the governing trade-off: losing
// an audit row must not turn a committed business operation into an error the
// caller sees. The recorder logs and continues.
func TestAuditRecorder_FailedWriteDoesNotPanic(t *testing.T) {
	repo := &stubAuditRepo{createErr: errFakeDB}
	rec := NewAuditRecorder(repo)
	inst := uint(1)

	c, _ := actorContext(models.RoleAdmin, &inst)
	// Must not panic.
	rec.Record(c, models.AuditUserCreate, "user", "1", "x@x.com", nil)
	if len(repo.entries) != 0 {
		t.Error("no entry should have been stored")
	}
}

func TestAuditRecorder_NilRepoAndNilRecorderAreNoOps(t *testing.T) {
	// A nil recorder must be safe, so a controller wired without one does not
	// panic mid-request.
	var nilRec *AuditRecorder
	nilRec.Record(actorCtxOnly(), models.AuditUserCreate, "user", "1", "x", nil)
	nilRec.RecordDenied(actorCtxOnly(), models.AuditUserCreate, "user", "1", "x", "no")

	// A recorder with a nil repo drops entries rather than dereferencing nil.
	rec := NewAuditRecorder(nil)
	rec.Record(actorCtxOnly(), models.AuditUserCreate, "user", "1", "x", nil)
}

func TestAuditRecorder_RecordSystemHasNoActor(t *testing.T) {
	repo := &stubAuditRepo{}
	rec := NewAuditRecorder(repo)

	rec.RecordSystem(models.AuditDataImport, nil, "staff", "0", "csv", map[string]any{
		"rows": 10,
	})
	e := repo.entries[0]
	if e.ActorID != 0 {
		t.Errorf("a system action has no actor, got %d", e.ActorID)
	}
	if e.InstitutionID != nil {
		t.Error("expected a NULL institution for a system action")
	}
}

// TestEncodeDetail_HandlesUnserialisableValue guards the fallback: a row with a
// missing field is more useful than no row at all.
func TestEncodeDetail_HandlesUnserialisableValue(t *testing.T) {
	got := encodeDetail(map[string]any{"bad": make(chan int)})
	if got == "" {
		t.Fatal("expected a placeholder rather than an empty string")
	}
	if !contains(got, "_encode_error") {
		t.Errorf("expected an encode-error note, got %s", got)
	}
}

func TestEncodeDetail_EmptyMapIsEmpty(t *testing.T) {
	if got := encodeDetail(nil); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestInstitutionFromContext_Coercions(t *testing.T) {
	cases := []struct {
		name  string
		set   any
		want  *uint
		label string
	}{
		{"unset", nil, nil, "no institution_id"},
		{"zero means platform", uint(0), nil, "zero is platform scope"},
		{"uint", uint(7), ptrUint(7), "uint value"},
		{"float64 from a claim", float64(7), ptrUint(7), "numeric claim"},
		{"pointer", ptrUint(7), ptrUint(7), "pointer value"},
		{"negative", -1, nil, "negative is treated as none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tc.set != nil {
				c.Set("institution_id", tc.set)
			}
			got := institutionFromContext(c)
			if tc.want == nil {
				if got != nil {
					t.Errorf("%s: expected nil, got %v", tc.label, *got)
				}
				return
			}
			if got == nil || *got != *tc.want {
				t.Errorf("%s: expected %d, got %v", tc.label, *tc.want, got)
			}
		})
	}
}

func TestToUint_Coercions(t *testing.T) {
	cases := []struct {
		in   any
		want uint
	}{
		{uint(5), 5}, {uint64(5), 5}, {int(5), 5}, {int64(5), 5},
		{float64(5), 5}, {"5", 5},
		{-1, 0}, {"not a number", 0}, {nil, 0}, {true, 0},
	}
	for _, tc := range cases {
		if got := toUint(tc.in); got != tc.want {
			t.Errorf("toUint(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestToString(t *testing.T) {
	if got := toString("administrator"); got != "administrator" {
		t.Errorf("got %q", got)
	}
	if got := toString(nil); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
	if got := toString(models.RoleAdmin); got != "administrator" {
		t.Errorf("got %q", got)
	}
}

// TestAuditRecorder_OrderingIsPreserved is a small sanity check that a sequence
// of actions lands in order, since the trail is read chronologically.
func TestAuditRecorder_OrderingIsPreserved(t *testing.T) {
	repo := &stubAuditRepo{}
	rec := NewAuditRecorder(repo)
	inst := uint(1)
	c, _ := actorContext(models.RoleAdmin, &inst)

	actions := []models.AuditAction{
		models.AuditUserCreate, models.AuditRoleChange,
		models.AuditUserSuspend, models.AuditUserDelete,
	}
	for _, a := range actions {
		rec.Record(c, a, "user", "1", "x@x.com", nil)
	}
	for i, want := range actions {
		if repo.entries[i].Action != want {
			t.Errorf("entry %d: got %s want %s", i, repo.entries[i].Action, want)
		}
	}
}

func ptrUint(v uint) *uint { return &v }

func actorCtxOnly() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/x", nil)
	return c
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

type fakeError struct{}

func (fakeError) Error() string { return "fake db failure" }

var errFakeDB = fakeError{}
