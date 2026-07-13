package config

import (
	"fmt"
	"strings"
)

// weak secrets that must never be used in production
var knownWeakSecrets = map[string]struct{}{
	"":            {},
	"mysecretkey": {},
	"secret":      {},
	"changeme":    {},
	"password":    {},
	"jwt_secret":  {},
}

// ResolveJWTSecret returns JWT signing secret or error if production posture fails.
func ResolveJWTSecret() (string, error) {
	env := strings.ToLower(GetEnv("ENV", "development"))
	secret := GetEnv("JWT_SECRET", "")

	if env == "production" || env == "prod" {
		if secret == "" {
			return "", fmt.Errorf("JWT_SECRET is required when ENV=production")
		}
		if _, weak := knownWeakSecrets[secret]; weak {
			return "", fmt.Errorf("JWT_SECRET is too weak for production")
		}
		if len(secret) < 32 {
			return "", fmt.Errorf("JWT_SECRET must be at least 32 characters in production")
		}
		return secret, nil
	}

	// development: allow default but log via caller
	if secret == "" {
		secret = "dev-only-insecure-jwt-secret-change-me"
	}
	return secret, nil
}

// IsWeakJWTSecret reports whether secret is a known insecure default.
func IsWeakJWTSecret(secret string) bool {
	_, ok := knownWeakSecrets[secret]
	return ok || secret == "dev-only-insecure-jwt-secret-change-me"
}
