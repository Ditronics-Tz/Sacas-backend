package security

import (
	"github.com/go-playground/validator/v10"
	"github.com/gin-gonic/gin/binding"
)

const (
	MinPasswordLength     = 8
	PasswordPolicyMessage = "Password must be at least 8 characters and contain at least one uppercase letter, one lowercase letter, and one digit"
)

// ValidPassword enforces: min 8 chars, at least one uppercase, one lowercase, one digit.
func ValidPassword(pw string) bool {
	if len(pw) < MinPasswordLength {
		return false
	}
	var hasUpper, hasLower, hasDigit bool
	for _, r := range pw {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	return hasUpper && hasLower && hasDigit
}

func strongPasswordValidator(fl validator.FieldLevel) bool {
	return ValidPassword(fl.Field().String())
}

func init() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		_ = v.RegisterValidation("strongpassword", strongPasswordValidator)
	}
}
