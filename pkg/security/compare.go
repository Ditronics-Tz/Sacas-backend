// Package security contains small shared security primitives used across
// middlewares and controllers (constant-time comparison, etc.).
package security

import "crypto/subtle"

// SecureCompare reports whether two strings are equal, without leaking
// timing information about how many leading bytes matched. It is the single
// shared implementation used by the CSRF middleware and OTP verification so
// secret comparisons never use plain `==`.
func SecureCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
