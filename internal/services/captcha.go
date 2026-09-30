package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go_boilerplate/internal/config"
	"go_boilerplate/pkg/logger"
)

// Captcha verification for public, unauthenticated, database-writing endpoints.
//
// Why this exists: institution registration creates a tenant row and an admin
// account. Rate limiting alone bounds the rate but not the volume — an attacker
// with a pool of proxies can still create thousands of pending institutions.
// A captcha makes each signup cost something.
//
// Design:
//
//   - The interface is small so a different provider (hCaptcha, reCAPTCHA, a
//     self-hosted proof-of-work) can be dropped in without touching callers.
//   - When no provider is configured, Verify returns true — the endpoint stays
//     usable in development and in a self-hosted deployment, which may have its
//     own front-door protection. This is a deliberate availability choice, and
//     it is why CAPTCHA_ENABLED defaults to false rather than the other way.
//   - When CAPTCHA_ENABLED is true in production, ValidateProductionConfig
//     refuses to boot without keys, so "enabled" can never mean "silently
//     passing everything".
//   - Verification failures are fail-CLOSED: an unreachable captcha provider
//     rejects the request rather than waving it through, because waving through
//     is exactly what an outage would otherwise buy an attacker.
type CaptchaVerifier interface {
	// Name identifies the provider in logs and error messages.
	Name() string
	// Verify reports whether the token is valid for the given remote IP.
	Verify(ctx context.Context, token, remoteIP string) (bool, error)
}

// CaptchaService dispatches to the configured verifier.
type CaptchaService struct {
	verifier CaptchaVerifier
	// enabled reports whether verification is required. A service with no
	// verifier is never enabled, so a wiring mistake cannot fail open.
	enabled bool
}

// NewCaptchaService builds the service from configuration.
func NewCaptchaService() (*CaptchaService, error) {
	if !captchaEnabled() {
		logger.Info("Captcha is disabled (CAPTCHA_ENABLED not true) — public signups are protected by rate limiting only")
		return &CaptchaService{enabled: false}, nil
	}
	return newCaptchaServiceWithVerifier(turnstileVerifierFromEnv())
}

// newCaptchaServiceWithVerifier is the seam the tests use.
func newCaptchaServiceWithVerifier(v CaptchaVerifier) (*CaptchaService, error) {
	if v == nil {
		return nil, fmt.Errorf("captcha enabled but no provider configured")
	}
	return &CaptchaService{verifier: v, enabled: true}, nil
}

// Enabled reports whether a captcha is required.
func (s *CaptchaService) Enabled() bool { return s != nil && s.enabled }

// Verify checks a captcha token.
//
// Returns false with an error when verification is required and did not
// succeed. Callers turn that into a 400 (bad token) or a 503 (provider down);
// the distinction is in the wrapped error, not in a boolean.
func (s *CaptchaService) Verify(ctx context.Context, token, remoteIP string) (bool, error) {
	if !s.Enabled() {
		return true, nil
	}
	token = strings.TrimSpace(token)
	if token == "" {
		// A typed error so the handler can answer 400 "captcha required" rather
		// than 503 "verification failed".
		return false, &CaptchaRequiredError{
			Reason: "a captcha token is required for this endpoint",
		}
	}
	ok, err := s.verifier.Verify(ctx, token, remoteIP)
	if err != nil {
		// Fail closed: a provider outage must not become an open door.
		return false, fmt.Errorf("captcha verification failed: %w", err)
	}
	return ok, nil
}

// CaptchaRequiredError is returned when a captcha token is missing, so the
// handler can answer 400 with a clear message rather than a generic failure.
type CaptchaRequiredError struct{ Reason string }

func (e *CaptchaRequiredError) Error() string { return e.Reason }

// IsCaptchaRequired reports whether an error is a missing-token error.
func IsCaptchaRequired(err error) bool {
	var target *CaptchaRequiredError
	return errors.As(err, &target)
}

// --- Turnstile (Cloudflare) --------------------------------------------------

// turnstileVerifier implements CaptchaVerifier against Cloudflare Turnstile.
// Turnstile is a single POST to a siteverify endpoint, works without visible
// widgets for most cases, and is free at the scale this product needs.
type turnstileVerifier struct {
	secret   string
	endpoint string
	client   *http.Client
}

func (v *turnstileVerifier) Name() string { return "turnstile" }

func (v *turnstileVerifier) Verify(ctx context.Context, token, remoteIP string) (bool, error) {
	form := url.Values{}
	form.Set("secret", v.secret)
	form.Set("response", token)
	// The client IP is optional for Turnstile but strengthens the check and
	// costs nothing.
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return false, fmt.Errorf("build verify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := v.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("captcha provider unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("captcha provider returned %d", resp.StatusCode)
	}

	var result struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("decode captcha response: %w", err)
	}
	if !result.Success {
		return false, fmt.Errorf("captcha rejected: %s", strings.Join(result.ErrorCodes, ", "))
	}
	return true, nil
}

func turnstileVerifierFromEnv() CaptchaVerifier {
	secret := config.GetEnv("CAPTCHA_SECRET", "")
	if secret == "" {
		return nil
	}
	endpoint := config.GetEnv("CAPTCHA_VERIFY_URL", "https://challenges.cloudflare.com/turnstile/v0/siteverify")
	return &turnstileVerifier{
		secret:   secret,
		endpoint: endpoint,
		// A short timeout: a slow captcha provider should not hold a signup
		// open. Combined with fail-closed, a timeout rejects rather than
		// admits.
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func captchaEnabled() bool {
	return strings.EqualFold(config.GetEnv("CAPTCHA_ENABLED", "false"), "true")
}
