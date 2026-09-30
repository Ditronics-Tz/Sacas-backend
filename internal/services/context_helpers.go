package services

import "fmt"

// toUint coerces a context value to uint, tolerating the numeric types a JWT
// claim or a hand-set context value may carry.
func toUint(v any) uint {
	switch n := v.(type) {
	case uint:
		return n
	case uint64:
		return uint(n)
	case int:
		if n < 0 {
			return 0
		}
		return uint(n)
	case int64:
		if n < 0 {
			return 0
		}
		return uint(n)
	case float64:
		if n < 0 {
			return 0
		}
		return uint(n)
	case string:
		var parsed uint64
		if _, err := fmt.Sscanf(n, "%d", &parsed); err != nil {
			return 0
		}
		return uint(parsed)
	default:
		return 0
	}
}

// toString renders a context value as a string, used for role and email.
func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
