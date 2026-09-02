package services

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	// OTPAttemptKeyPrefix namespaces per-identifier failed-attempt counters.
	// Key layout: otp_attempts:<purpose>:<identifier> where purpose is one of
	// "verify" (email verification), "reset" (password reset), "otp" (generic
	// OTP controller flow) and identifier is the account email.
	OTPAttemptKeyPrefix = "otp_attempts:"

	// DefaultOTPMaxAttempts is the number of failed verifications allowed
	// before the OTP for that identifier is invalidated.
	DefaultOTPMaxAttempts = 5
)

// MaxOTPAttempts returns the configured maximum failed OTP attempts per
// identifier (env OTP_MAX_ATTEMPTS, default 5).
func MaxOTPAttempts() int {
	if v := os.Getenv("OTP_MAX_ATTEMPTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return DefaultOTPMaxAttempts
}

// OTPAttemptKey builds the Redis key for a purpose/identifier attempt counter.
func OTPAttemptKey(purpose, identifier string) string {
	return OTPAttemptKeyPrefix + purpose + ":" + identifier
}

// OTPAttemptGuard limits brute-force attempts against a single account's OTP
// in Redis. The per-IP rate limit middleware does not stop distributed or
// rotating-IP attempts against one specific email/OTP, so verification
// endpoints must also enforce a per-account counter.
//
// All methods are nil-receiver and nil-Redis safe (no-op) so callers do not
// need to guard every call.
type OTPAttemptGuard struct {
	redis       *redis.Client
	maxAttempts int
}

// NewOTPAttemptGuard creates a guard backed by the given Redis client.
func NewOTPAttemptGuard(client *redis.Client) *OTPAttemptGuard {
	return &OTPAttemptGuard{redis: client, maxAttempts: MaxOTPAttempts()}
}

// AttemptsExceeded reports whether the identifier has reached the maximum
// number of failed verification attempts (i.e. further attempts are locked
// out).
func (g *OTPAttemptGuard) AttemptsExceeded(ctx context.Context, purpose, identifier string) bool {
	if g == nil || g.redis == nil {
		return false
	}
	count, err := g.redis.Get(ctx, OTPAttemptKey(purpose, identifier)).Int()
	if err != nil {
		return false // missing key or transient Redis error: fail open
	}
	return count >= g.maxAttempts
}

// RecordFailure increments the failed-attempt counter for the identifier with
// the given expiry. Pass the remaining TTL of the OTP itself so the counter
// resets exactly when the OTP would have expired anyway.
func (g *OTPAttemptGuard) RecordFailure(ctx context.Context, purpose, identifier string, ttl time.Duration) {
	if g == nil || g.redis == nil {
		return
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	g.redis.Incr(ctx, OTPAttemptKey(purpose, identifier))
	g.redis.Expire(ctx, OTPAttemptKey(purpose, identifier), ttl)
}

// Reset clears the failed-attempt counter after a successful verification.
func (g *OTPAttemptGuard) Reset(ctx context.Context, purpose, identifier string) {
	if g == nil || g.redis == nil {
		return
	}
	g.redis.Del(ctx, OTPAttemptKey(purpose, identifier))
}

// RemainingTTL returns the remaining TTL of the given Redis key, falling back
// to fallback when unavailable.
func RemainingTTL(ctx context.Context, client *redis.Client, key string, fallback time.Duration) time.Duration {
	if client == nil {
		return fallback
	}
	if ttl, err := client.TTL(ctx, key).Result(); err == nil && ttl > 0 {
		return ttl
	}
	return fallback
}
