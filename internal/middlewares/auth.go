package middlewares

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"go_boilerplate/internal/config"
	"go_boilerplate/internal/models"
	"go_boilerplate/pkg/logger"
)

func jwtSecret() []byte {
	secret, err := config.ResolveJWTSecret()
	if err != nil {
		// Should not reach here if main validated production secret;
		// fall back to empty which fails signature validation.
		logger.Error("JWT secret resolution failed: %v", err)
		return []byte{}
	}
	return []byte(secret)
}

func JWTAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		var tokenString string

		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			bearerToken := strings.Split(authHeader, " ")
			if len(bearerToken) == 2 && bearerToken[0] == "Bearer" {
				tokenString = bearerToken[1]
			}
		}

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
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return jwtSecret(), nil
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

		if exp, ok := claims["exp"]; ok {
			var expTime int64
			switch v := exp.(type) {
			case float64:
				expTime = int64(v)
			case int64:
				expTime = v
			}
			if expTime > 0 && expTime < time.Now().Unix() {
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

// roleFromContext returns the JWT role claim as a string (never from client input).
func roleFromContext(c *gin.Context) (string, bool) {
	role, exists := c.Get("role")
	if !exists || role == nil {
		return "", false
	}
	switch v := role.(type) {
	case string:
		return strings.TrimSpace(v), v != ""
	case models.UserRole:
		s := strings.TrimSpace(string(v))
		return s, s != ""
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" || s == "<nil>" {
			return "", false
		}
		return s, true
	}
}

// RoleMiddleware / RequireRole allow only the given roles from verified JWT claims.
func RoleMiddleware(roles ...string) gin.HandlerFunc {
	return RequireRole(roles...)
}

// RequireRole is the preferred name when wiring new routes:
//
//	group.Use(middlewares.RequireRole("administrator", "super_admin"))
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(c *gin.Context) {
		userRole, ok := roleFromContext(c)
		if !ok {
			logger.Warn("Role not found in token")
			c.JSON(http.StatusForbidden, gin.H{"error": "Role not found in token"})
			c.Abort()
			return
		}
		if _, ok := allowed[userRole]; ok {
			c.Next()
			return
		}
		logger.Warn("Insufficient permissions for user with role: %s, required: %v", userRole, roles)
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
		c.Abort()
	}
}

func AdminMiddleware() gin.HandlerFunc {
	return RequireRole(string(models.RoleAdmin), string(models.RoleSuperAdmin))
}

func SuperAdminMiddleware() gin.HandlerFunc {
	return RequireRole(string(models.RoleSuperAdmin))
}

// ActiveUserLookup looks up a user by ID for active checks.
type ActiveUserLookup func(id uint) (*models.User, error)

// ActiveUserMiddleware rejects deactivated users even if JWT is still valid.
func ActiveUserMiddleware(lookup ActiveUserLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		if lookup == nil {
			c.Next()
			return
		}
		raw, exists := c.Get("user_id")
		if !exists {
			c.Next()
			return
		}
		var id uint
		switch v := raw.(type) {
		case float64:
			id = uint(v)
		case int:
			id = uint(v)
		case uint:
			id = v
		case int64:
			id = uint(v)
		default:
			// try parse via fmt
			var n uint64
			_, err := fmt.Sscanf(fmt.Sprint(v), "%d", &n)
			if err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user in token"})
				c.Abort()
				return
			}
			id = uint(n)
		}

		user, err := lookup(id)
		if err != nil || user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not found"})
			c.Abort()
			return
		}
		if !user.IsActive {
			c.JSON(http.StatusForbidden, gin.H{"error": "Account is not active"})
			c.Abort()
			return
		}
		c.Next()
	}
}
