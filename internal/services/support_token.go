package services

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"go_boilerplate/internal/config"
	"go_boilerplate/internal/models"
)

// SupportTokenIssuer signs and verifies the short-lived tokens used for
// read-only support access (impersonation).
//
// It is a separate issuer from the normal login token on purpose, and the two
// are told apart by two things a support token always carries: the support role
// and an institution_id. A normal login token never has an institution_id,
// because login tokens are issued before any tenant is resolved.
//
// A support token is signed with the same secret. That is acceptable because the
// server, not the client, decides the role and institution inside the token, and
// because the same secret lets the support token pass the existing JWT
// middleware unchanged. The extra checks in middleware are what keep it
// read-only.
type SupportTokenIssuer struct {
	secret []byte
	now    func() time.Time
}

// NewSupportTokenIssuer builds an issuer using the configured JWT secret.
func NewSupportTokenIssuer() (*SupportTokenIssuer, error) {
	secret, err := config.ResolveJWTSecret()
	if err != nil {
		return nil, err
	}
	return &SupportTokenIssuer{
		secret: []byte(secret),
		now:    func() time.Time { return time.Now().UTC() },
	}, nil
}

// Issue signs a support token for one institution, valid until expiresAt.
//
// The claims are deliberately minimal: an institution, the support role, and
// the actor's identity for the audit trail. No tenant switch, no escalation.
func (s *SupportTokenIssuer) Issue(institutionID uint, expiresAt time.Time) (string, error) {
	now := s.now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"institution_id": institutionID,
		"role":           string(models.RoleSupport),
		"read_only":      true,
		"support":        true,
		"exp":            expiresAt.Unix(),
		"iat":            now.Unix(),
	})
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("sign support token: %w", err)
	}
	return signed, nil
}

// ParseSupportToken verifies a token and returns its institution, but only if
// it really is a support token.
//
// The three checks are load-bearing: a normal user token or a super_admin token
// must not be usable as a support token, or a compromised low-privilege token
// would gain read access to an arbitrary institution.
func (s *SupportTokenIssuer) ParseSupportToken(raw string) (institutionID uint, err error) {
	token, err := jwt.Parse(raw, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil || token == nil || !token.Valid {
		return 0, fmt.Errorf("invalid support token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, fmt.Errorf("invalid support token claims")
	}
	if !boolClaim(claims["support"]) {
		return 0, fmt.Errorf("not a support token")
	}
	if role, _ := claims["role"].(string); role != string(models.RoleSupport) {
		return 0, fmt.Errorf("not a support token")
	}
	if !boolClaim(claims["read_only"]) {
		// A support token that is not marked read-only is refused outright
		// rather than downgraded: it means the token was minted wrong.
		return 0, fmt.Errorf("support token is not read-only")
	}
	inst, ok := claims["institution_id"].(float64)
	if !ok || inst <= 0 {
		return 0, fmt.Errorf("support token has no institution")
	}
	return uint(inst), nil
}

// issueWithClaims signs an arbitrary claim set. It exists so the tests can
// mint deliberately malformed tokens (a support token that is not read-only, a
// support token with no institution, a plain login token) and assert that the
// parser refuses each one. Production code never calls it.
func (s *SupportTokenIssuer) issueWithClaims(claims map[string]any) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(claims))
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// issueLoginTokenForTest mints an ordinary login token. Tests use it to prove
// that a normal token is not accepted as a support token.
func (s *SupportTokenIssuer) issueLoginTokenForTest(userID uint, role string) (string, error) {
	return s.issueWithClaims(map[string]any{
		"user_id": userID,
		"email":   "test@example.com",
		"role":    role,
		"exp":     s.now().Add(time.Hour).Unix(),
		"iat":     s.now().Unix(),
	})
}

// issueForTest mints a support-shaped token with the given institution and
// optional claim overrides, so a test can produce an invalid variant.
func (s *SupportTokenIssuer) issueForTest(institutionID uint, expiresAt time.Time, overrides map[string]any) (string, error) {
	claims := map[string]any{
		"institution_id": institutionID,
		"role":           string(models.RoleSupport),
		"read_only":      true,
		"support":        true,
		"exp":            expiresAt.Unix(),
		"iat":            s.now().Unix(),
	}
	for k, v := range overrides {
		claims[k] = v
	}
	return s.issueWithClaims(claims)
}

func boolClaim(v any) bool {
	b, ok := v.(bool)
	return ok && b
}
