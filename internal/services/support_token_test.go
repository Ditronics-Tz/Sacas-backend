package services

import (
	"testing"
	"time"

	"go_boilerplate/internal/models"
)

const supportTestSecret = "test-jwt-secret-for-support-token-tests"

func newTestIssuer(t *testing.T) *SupportTokenIssuer {
	t.Helper()
	t.Setenv("JWT_SECRET", supportTestSecret)
	t.Setenv("ENV", "development")
	issuer, err := NewSupportTokenIssuer()
	if err != nil {
		t.Skipf("JWT secret unavailable: %v", err)
	}
	return issuer
}

func TestSupportToken_RoundTrips(t *testing.T) {
	issuer := newTestIssuer(t)
	expires := time.Now().UTC().Add(10 * time.Minute)

	token, err := issuer.Issue(7, expires)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	inst, err := issuer.ParseSupportToken(token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if inst != 7 {
		t.Errorf("expected institution 7, got %d", inst)
	}
}

func TestSupportToken_RejectsExpired(t *testing.T) {
	issuer := newTestIssuer(t)
	// Already expired: the token must not be usable.
	token, err := issuer.Issue(7, time.Now().UTC().Add(-time.Minute))
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := issuer.ParseSupportToken(token); err == nil {
		t.Fatal("an expired support token must be rejected")
	}
}

func TestSupportToken_RejectsGarbage(t *testing.T) {
	issuer := newTestIssuer(t)
	for _, bad := range []string{"", "not-a-token", "a.b.c"} {
		if _, err := issuer.ParseSupportToken(bad); err == nil {
			t.Errorf("expected %q to be rejected", bad)
		}
	}
}

// TestSupportToken_RejectsNormalUserToken is load-bearing: if an ordinary login
// token were accepted as a support token, a compromised low-privilege token
// would gain read access to an arbitrary institution.
func TestSupportToken_RejectsNormalUserToken(t *testing.T) {
	issuer := newTestIssuer(t)

	// A plain login token: no support flag, no institution, user role.
	normal, err := issuer.issueLoginTokenForTest(1, string(models.RoleUser))
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := issuer.ParseSupportToken(normal); err == nil {
		t.Fatal("a normal user token must NOT be usable as a support token")
	}

	// A super_admin login token likewise.
	adminToken, err := issuer.issueLoginTokenForTest(1, string(models.RoleSuperAdmin))
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := issuer.ParseSupportToken(adminToken); err == nil {
		t.Fatal("a super_admin token must NOT be usable as a support token")
	}
}

// TestSupportToken_RejectsTokenWithWriteIntent guards the case where a token is
// minted with the support role but is not marked read-only. It is refused
// outright rather than silently downgraded, because it means the token was
// minted incorrectly.
func TestSupportToken_RejectsTokenWithWriteIntent(t *testing.T) {
	issuer := newTestIssuer(t)

	bad, err := issuer.issueForTest(9, time.Now().UTC().Add(time.Minute), map[string]any{
		"support":   true,
		"read_only": false,
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := issuer.ParseSupportToken(bad); err == nil {
		t.Fatal("a support token not marked read-only must be rejected")
	}
}

func TestSupportToken_RejectsSupportTokenWithoutInstitution(t *testing.T) {
	issuer := newTestIssuer(t)

	bad, err := issuer.issueForTest(0, time.Now().UTC().Add(time.Minute), nil)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := issuer.ParseSupportToken(bad); err == nil {
		t.Fatal("a support token with no institution must be rejected")
	}
}

func TestSupportToken_RejectsForeignSignature(t *testing.T) {
	issuer := newTestIssuer(t)
	t.Setenv("JWT_SECRET", "a-completely-different-secret-value-32")
	other, err := NewSupportTokenIssuer()
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	token, err := other.Issue(7, time.Now().UTC().Add(time.Minute))
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := issuer.ParseSupportToken(token); err == nil {
		t.Fatal("a token signed with a different secret must be rejected")
	}
}
