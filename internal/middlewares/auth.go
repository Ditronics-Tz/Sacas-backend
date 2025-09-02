package middlewares

import (
	"net/http"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/config"
	"go_boilerplate/internal/models"
	"go_boilerplate/pkg/logger"
)

func JWTAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		var tokenString string

		// Try to get token from Authorization header first
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			bearerToken := strings.Split(authHeader, " ")
			if len(bearerToken) == 2 && bearerToken[0] == "Bearer" {
				tokenString = bearerToken[1]
			}
		}

		// If no authorization header, try to get token from cookie
		if tokenString == "" {
			var err error
			tokenString, err = c.Cookie("token")
			if err != nil {
				logger.Warn("No authentication token provided")
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization required"})
				c.Abort()
				return
			}
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			// Validate signing method
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.NewValidationError("Invalid signing method", jwt.ValidationErrorSignatureInvalid)
			}
			return []byte(config.GetEnv("JWT_SECRET", "mysecretkey")), nil
		})

		if err != nil || !token.Valid {
			logger.Warn("Invalid JWT token: %v", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			logger.Warn("Invalid token claims")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			c.Abort()
			return
		}

		// Check token expiration
		if exp, ok := claims["exp"]; ok {
			if expTime := int64(exp.(float64)); expTime < time.Now().Unix() {
				logger.Warn("Token expired")
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Token expired"})
				c.Abort()
				return
			}
		}

		c.Set("user_id", claims["user_id"])
		c.Set("role", claims["role"])
		c.Set("email", claims["email"])
		c.Next()
	}
}

func RoleMiddleware(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			logger.Warn("Role not found in token")
			c.JSON(http.StatusForbidden, gin.H{"error": "Role not found in token"})
			c.Abort()
			return
		}

		userRole := role.(string)
		
		// Check if user has any of the required roles
		for _, r := range roles {
			if userRole == r {
				c.Next()
				return
			}
		}

		logger.Warn("Insufficient permissions for user with role: %s, required: %v", userRole, roles)
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		c.Abort()
	}
}

// AdminMiddleware allows access to admin and super admin roles
func AdminMiddleware() gin.HandlerFunc {
	return RoleMiddleware(string(models.RoleAdmin), string(models.RoleSuperAdmin))
}

// SuperAdminMiddleware allows access only to super admin role
func SuperAdminMiddleware() gin.HandlerFunc {
	return RoleMiddleware(string(models.RoleSuperAdmin))
}

// ActiveUserMiddleware ensures the user is active (can be combined with other middleware)
func ActiveUserMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// This would require a database lookup, so we'll implement it differently
		// For now, we assume active status is checked during login
		c.Next()
	}
}