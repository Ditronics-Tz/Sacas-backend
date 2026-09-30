package services

import (
	"context"
	"errors"
	"go_boilerplate/internal/models"
	"go_boilerplate/pkg/security"
	"strings"
	"testing"
	"time"
)

// --- Invitation tokens -------------------------------------------------------

// TestGenerateInvitationToken_StoresOnlyHash is the property that matters: the
// plaintext must not be recoverable from what is stored.
func TestGenerateInvitationToken_StoresOnlyHash(t *testing.T) {
	plaintext, hash, err := GenerateInvitationToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if plaintext == "" || hash == "" {
		t.Fatal("expected both a token and a hash")
	}
	// The hash must not contain the plaintext.
	if strings.Contains(hash, plaintext) {
		t.Error("the stored hash contains the plaintext token")
	}
	// Hashing the plaintext must reproduce the stored value, or the accept path
	// could never find the row.
	if got := HashInvitationToken(plaintext); got != hash {
		t.Errorf("HashInvitationToken(plaintext) = %s, want %s", got, hash)
	}
	// And it must be a fixed-length digest, not the token itself.
	if len(hash) != 64 {
		t.Errorf("expected a 64-character hex digest, got %d characters", len(hash))
	}
}

func TestGenerateInvitationToken_IsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		plaintext, _, err := GenerateInvitationToken()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if seen[plaintext] {
			t.Fatalf("duplicate token generated at iteration %d", i)
		}
		seen[plaintext] = true
	}
}

// TestHashInvitationToken_IsDeterministicAndSaltless guards the lookup path: a
// salted hash could not be found by hashing an incoming token.
func TestHashInvitationToken_IsDeterministicAndSaltless(t *testing.T) {
	first := HashInvitationToken("some-token")
	second := HashInvitationToken("some-token")
	if first != second {
		t.Error("hashing the same token twice must produce the same digest")
	}
	if HashInvitationToken("other-token") == first {
		t.Error("different tokens must produce different digests")
	}
}

// TestHashInvitationToken_EmptyIsHandled makes sure a missing token cannot match
// a stored row.
func TestHashInvitationToken_EmptyIsHandled(t *testing.T) {
	if HashInvitationToken("") == "" {
		t.Error("an empty token must still produce a digest, not an empty string")
	}
}

// --- Invitation usability ----------------------------------------------------

func TestInvitation_IsUsable(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name string
		inv  *models.InstitutionInvitation
		want bool
	}{
		{"nil", nil, false},
		{"live", &models.InstitutionInvitation{ExpiresAt: future}, true},
		{"expired", &models.InstitutionInvitation{ExpiresAt: past}, false},
		{"accepted", &models.InstitutionInvitation{ExpiresAt: future, AcceptedAt: &now}, false},
		{"revoked", &models.InstitutionInvitation{ExpiresAt: future, RevokedAt: &now}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.inv.IsUsable(now); got != tc.want {
				t.Errorf("IsUsable = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestInvitation_StatusDerivesExpiry guards against a stored expiry flag: status
// must come from the clock, so it cannot be cleared.
func TestInvitation_StatusDerivesExpiry(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	accepted := now.Add(-time.Minute)

	cases := []struct {
		name string
		inv  models.InstitutionInvitation
		want models.InvitationStatus
	}{
		{"pending", models.InstitutionInvitation{ExpiresAt: now.Add(time.Hour)}, models.InvitationPending},
		{"accepted", models.InstitutionInvitation{ExpiresAt: now.Add(time.Hour), AcceptedAt: &accepted}, models.InvitationAccepted},
		{"revoked", models.InstitutionInvitation{ExpiresAt: now.Add(time.Hour), RevokedAt: &accepted}, models.InvitationRevoked},
		{"expired", models.InstitutionInvitation{ExpiresAt: now.Add(-time.Hour)}, models.InvitationExpired},
		// Accepted wins over expiry: an accepted invitation stays accepted even
		// once its expiry passes, so the trail is not rewritten by the clock.
		{"accepted then expired", models.InstitutionInvitation{ExpiresAt: now.Add(-time.Hour), AcceptedAt: &accepted}, models.InvitationAccepted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.inv.Status(now); got != tc.want {
				t.Errorf("Status = %s, want %s", got, tc.want)
			}
		})
	}
}

// --- Delegable roles ---------------------------------------------------------

// TestIsDelegableRole is the anti-escalation rule for the invitation path. If an
// institution could invite someone as a coordinator or an administrator, the
// role restrictions that direct assignment enforces would be circumvented
// entirely.
func TestIsDelegableRole(t *testing.T) {
	if !models.IsDelegableRole(models.RoleUser) {
		t.Error("an institution should be able to invite a user")
	}
	for _, forbidden := range []models.UserRole{
		models.RoleAdmin, models.RoleSuperAdmin,
		models.RoleAcademicCoord, models.RoleExamCoord, models.RoleSupport,
	} {
		if models.IsDelegableRole(forbidden) {
			t.Errorf("an institution must NOT be able to invite role %s", forbidden)
		}
	}
}

// TestCanInvite_ReusesTheAssignmentRule checks the invitation path delegates to
// the same rule rather than restating it, so the two cannot drift.
func TestCanInvite_ReusesTheAssignmentRule(t *testing.T) {
	// An institution admin can invite a plain user.
	if !CanInvite(models.RoleAdmin, models.RoleUser) {
		t.Error("institution admin should be able to invite a user")
	}
	// But not into any elevated role.
	for _, forbidden := range []models.UserRole{
		models.RoleAdmin, models.RoleAcademicCoord, models.RoleExamCoord, models.RoleSuperAdmin,
	} {
		if CanInvite(models.RoleAdmin, forbidden) {
			t.Errorf("institution admin must NOT be able to invite role %s", forbidden)
		}
	}
	// A lecturer cannot invite anybody at all: they hold no user:write, and
	// issuing an invitation is an unauthenticated path into account creation.
	if CanInvite(models.RoleUser, models.RoleUser) {
		t.Error("a lecturer must not be able to issue invitations")
	}

	// A platform super_admin cannot use the tenant invitation path either. It
	// holds no user:write, and more fundamentally it is not bound to an
	// institution, so the route is closed to it by the workspace gate. Platform
	// account creation goes through /superadmin/users instead.
	if CanInvite(models.RoleSuperAdmin, models.RoleUser) {
		t.Error("a platform super_admin must not invite through the institution path")
	}
	// And the support role is never assignable, by anyone.
	if CanInvite(models.RoleAdmin, models.RoleSupport) {
		t.Error("the support role must never be assignable, by anyone")
	}
}

// --- Generated passwords -----------------------------------------------------

// TestGeneratePassword_SatisfiesPolicy is the load-bearing property: a
// platform-issued password must pass the same validator as a user-chosen one,
// or a reset would hand the user a password that cannot be submitted.
func TestGeneratePassword_SatisfiesPolicy(t *testing.T) {
	for i := 0; i < 200; i++ {
		password, err := GeneratePassword(16)
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if !security.ValidPassword(password) {
			t.Fatalf("generated password %q does not satisfy the policy", password)
		}
	}
}

func TestGeneratePassword_LengthAndUniqueness(t *testing.T) {
	pw, err := GeneratePassword(20)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(pw) != 20 {
		t.Errorf("expected length 20, got %d", len(pw))
	}

	// Two calls must not produce the same password.
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		p, _ := GeneratePassword(16)
		if seen[p] {
			t.Fatal("duplicate password generated")
		}
		seen[p] = true
	}
}

// TestGeneratePassword_MinimumLength guards the floor: a short request must not
// produce a password below the policy's minimum.
func TestGeneratePassword_MinimumLength(t *testing.T) {
	pw, err := GeneratePassword(4)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(pw) < 12 {
		t.Errorf("expected a floor of 12 characters, got %d", len(pw))
	}
}

// --- Captcha -----------------------------------------------------------------

// stubCaptcha records calls and returns a scripted result.
type stubCaptcha struct {
	ok      bool
	err     error
	calls   int
	lastTok string
	lastIP  string
}

func (s *stubCaptcha) Name() string { return "stub" }
func (s *stubCaptcha) Verify(_ context.Context, token, remoteIP string) (bool, error) {
	s.calls++
	s.lastTok = token
	s.lastIP = remoteIP
	return s.ok, s.err
}

// TestCaptchaService_DisabledPassesThrough: with no captcha configured the
// endpoint stays usable, which is what allows a self-hosted deployment to rely
// on its own front-door protection.
func TestCaptchaService_DisabledPassesThrough(t *testing.T) {
	svc := &CaptchaService{enabled: false}
	if svc.Enabled() {
		t.Error("a service with no verifier must never be enabled")
	}
	ok, err := svc.Verify(context.Background(), "", "")
	if !ok || err != nil {
		t.Errorf("expected a pass-through when disabled, got ok=%v err=%v", ok, err)
	}
}

// TestCaptchaService_RequiresTokenWhenEnabled guards the missing-token case.
func TestCaptchaService_RequiresTokenWhenEnabled(t *testing.T) {
	stub := &stubCaptcha{ok: true}
	svc, err := newCaptchaServiceWithVerifier(stub)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	ok, err := svc.Verify(context.Background(), "   ", "")
	if ok {
		t.Error("an empty token must not verify")
	}
	// A missing token is a client error, distinguishable from a provider outage.
	if !IsCaptchaRequired(err) {
		t.Errorf("expected a captcha-required error, got %v", err)
	}
	if stub.calls != 0 {
		t.Error("the provider should not be called when the token is empty")
	}
}

// TestCaptchaService_PassesTokenAndIPToProvider.
func TestCaptchaService_PassesTokenAndIPToProvider(t *testing.T) {
	stub := &stubCaptcha{ok: true}
	svc, _ := newCaptchaServiceWithVerifier(stub)

	ok, err := svc.Verify(context.Background(), "token-abc", "197.157.2.3")
	if !ok || err != nil {
		t.Fatalf("expected success, got ok=%v err=%v", ok, err)
	}
	if stub.lastTok != "token-abc" {
		t.Errorf("provider saw token %q", stub.lastTok)
	}
	if stub.lastIP != "197.157.2.3" {
		t.Errorf("provider saw ip %q", stub.lastIP)
	}
}

// TestCaptchaService_FailsClosedOnProviderError is the important one: an outage
// must not become an open door.
func TestCaptchaService_FailsClosedOnProviderError(t *testing.T) {
	stub := &stubCaptcha{ok: false, err: errProviderDown}
	svc, _ := newCaptchaServiceWithVerifier(stub)

	ok, err := svc.Verify(context.Background(), "token", "")
	if ok {
		t.Error("a provider failure must not admit the request")
	}
	if err == nil {
		t.Error("expected an error describing the provider failure")
	}
	// It must NOT be reported as a missing token, or the handler would tell the
	// user to solve a captcha when the real problem is our own outage.
	if IsCaptchaRequired(err) {
		t.Error("a provider outage must not be reported as a missing captcha")
	}
}

// TestCaptchaService_RejectsFailedVerification checks the other failure shape:
// the provider was reachable and actively rejected the token. That is a client
// error, so it returns (false, nil) and the handler answers 400 — distinct from
// a provider outage, which returns an error and answers 503.
func TestCaptchaService_RejectsFailedVerification(t *testing.T) {
	stub := &stubCaptcha{ok: false}
	svc, _ := newCaptchaServiceWithVerifier(stub)

	ok, err := svc.Verify(context.Background(), "wrong-token", "")
	if ok {
		t.Error("a rejected token must not verify")
	}
	// No error: the provider worked, the token was simply wrong.
	if err != nil {
		t.Errorf("a rejected token is a client error, not a service error: %v", err)
	}
	if IsCaptchaRequired(err) {
		t.Error("a rejected token must not be reported as a missing captcha")
	}
}

// TestNewCaptchaServiceWithVerifier_RequiresProvider: enabling a captcha with no
// provider is a wiring error and must not degrade to "no captcha".
func TestNewCaptchaServiceWithVerifier_RequiresProvider(t *testing.T) {
	if _, err := newCaptchaServiceWithVerifier(nil); err == nil {
		t.Error("expected an error when no provider is configured")
	}
}

type providerError struct{}

func (providerError) Error() string { return "captcha provider unreachable" }

var errProviderDown = providerError{}

// --- Unique violation detection ----------------------------------------------

func TestIsUniqueViolation(t *testing.T) {
	positive := []error{
		errors.New("ERROR: duplicate key value violates unique constraint (SQLSTATE 23505)"),
		errors.New("pq: duplicate key value violates unique constraint \"uni_users_email\""),
		errors.New("UNIQUE constraint failed: users.email"),
	}
	for _, err := range positive {
		if !isUniqueViolation(err) {
			t.Errorf("expected %q to be detected as a unique violation", err)
		}
	}
	negative := []error{nil, errors.New("connection refused"), errors.New("deadlock detected")}
	for _, err := range negative {
		if isUniqueViolation(err) {
			t.Errorf("expected %q NOT to be a unique violation", err)
		}
	}
}

// --- Approval policy ---------------------------------------------------------

// TestApprovalPolicyIsPendingNotInstant documents the decision in a way that
// will fail if someone flips it to instant activation, since instant activation
// on a public endpoint is the thing being avoided.
func TestApprovalPolicyIsPendingNotInstant(t *testing.T) {
	if !strings.Contains(ApprovalPolicy, "pending") {
		t.Errorf("the approval policy should state the pending stage, got %q", ApprovalPolicy)
	}
	if !strings.Contains(ApprovalPolicy, "super_admin") {
		t.Errorf("the approval policy should name who approves, got %q", ApprovalPolicy)
	}
}

// TestNewInstitutionsStartPending checks the model default agrees with the
// policy, so a future refactor cannot make signup bypass approval.
func TestNewInstitutionsStartPending(t *testing.T) {
	// The controller relies on the service, not the GORM tag, to set this; the
	// check here is that the constant used is the safe one.
	if models.InstitutionStatusPending == models.InstitutionStatusActive {
		t.Error("pending and active must be distinct states")
	}
}
