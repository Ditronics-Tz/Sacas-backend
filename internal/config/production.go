package config

import (
	"fmt"
	"strings"
)

// ValidateProductionConfig verifies that production-critical settings are safe.
// It is called at startup when ENV=production (or prod). Returns error if
// any unsafe default would be used in production.
func ValidateProductionConfig() error {
	env := strings.ToLower(GetEnv("ENV", "development"))
	if env != "production" && env != "prod" {
		return nil
	}
	var errs []string

	// CSRF must be enabled in production
	if !strings.EqualFold(GetEnv("CSRF_ENABLED", "false"), "true") {
		errs = append(errs, "CSRF_ENABLED must be true when ENV=production")
	}

	// JWT secret already validated via ResolveJWTSecret, but double-check
	if _, err := ResolveJWTSecret(); err != nil {
		errs = append(errs, fmt.Sprintf("JWT: %v", err))
	}

	// DATABASE_URL must not use sslmode=disable in production
	dbURL := GetEnv("DATABASE_URL", "")
	if strings.Contains(strings.ToLower(dbURL), "sslmode=disable") {
		errs = append(errs, "DATABASE_URL must not use sslmode=disable in production (use sslmode=require or verify-full)")
	}
	if strings.Contains(strings.ToLower(dbURL), "password=postgres") {
		errs = append(errs, "DATABASE_URL uses default password 'postgres' in production")
	}

	// Seeding must not be on in production
	if strings.EqualFold(GetEnv("SEED_DEMO", "false"), "true") {
		errs = append(errs, "SEED_DEMO must not be true when ENV=production (demo accounts must not be seeded in production)")
	}

	// SOLVER_FALLBACK: in production, falling back to greedy on solver failure
	// masks real scheduling infeasibility and operator visibility. Require false
	// when a solver is configured. Warn if true.
	if strings.EqualFold(GetEnv("SOLVER_FALLBACK", "false"), "true") && GetEnv("SOLVER_URL", "") != "" {
		errs = append(errs, "SOLVER_FALLBACK must be false when ENV=production and SOLVER_URL is set (fail loudly instead of silent greedy fallback)")
	}

	// Email branding
	fromEmail := GetEnv("FROM_EMAIL", "")
	if fromEmail == "" || strings.EqualFold(fromEmail, "noreply@example.com") {
		errs = append(errs, "FROM_EMAIL must be set to a real domain address when ENV=production (not noreply@example.com)")
	}
	emailFromName := GetEnv("EMAIL_FROM_NAME", "")
	if strings.EqualFold(emailFromName, "Go Boilerplate") {
		errs = append(errs, "EMAIL_FROM_NAME must not be 'Go Boilerplate' in production (use SACAS)")
	}

	// SMS / Email provider keys are validated by NewNotificationService, but
	// also surface here for early fail-fast with a clear message
	if GetEnv("SENDGRID_API_KEY", "") == "" {
		errs = append(errs, "SENDGRID_API_KEY is required when ENV=production")
	}
	// At least one SMS provider required unless explicitly disabled
	hasSMS := GetEnv("TWILIO_ACCOUNT_SID", "") != "" || GetEnv("BEEM_API_KEY", "") != ""
	if !hasSMS && !strings.EqualFold(GetEnv("SMS_PROVIDER", ""), "none") {
		errs = append(errs, "no SMS provider configured when ENV=production (set TWILIO_* or BEEM_* , or SMS_PROVIDER=none to explicitly disable)")
	}

	// Captcha: if it is switched on, it must actually be configured. Without
	// this check "CAPTCHA_ENABLED=true" with a missing secret would silently
	// degrade to "no captcha", which is the opposite of what the operator asked
	// for and the opposite of the fail-closed behaviour the captcha service
	// itself implements.
	//
	// It is NOT required to be on: a self-hosted deployment may put its own
	// front-door protection in place, and forcing it would break that. The
	// signup endpoints are rate limited either way.
	if strings.EqualFold(GetEnv("CAPTCHA_ENABLED", "false"), "true") {
		if GetEnv("CAPTCHA_SECRET", "") == "" {
			errs = append(errs, "CAPTCHA_SECRET is required when CAPTCHA_ENABLED=true in production")
		}
		if GetEnv("CAPTCHA_VERIFY_URL", "") == "" {
			errs = append(errs, "CAPTCHA_VERIFY_URL is required when CAPTCHA_ENABLED=true in production")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("production config invalid:\n - %s", strings.Join(errs, "\n - "))
	}
	return nil
}
