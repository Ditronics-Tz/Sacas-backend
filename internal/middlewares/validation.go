package middlewares

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"go_boilerplate/pkg/logger"
)

// Security patterns
var (
	// Common SQL injection patterns
	sqlInjectionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(\b(union|select|insert|update|delete|drop|create|alter|exec|execute)\b)`),
		regexp.MustCompile(`(?i)(\b(script|javascript|vbscript|onload|onerror|onclick)\b)`),
		regexp.MustCompile(`(?i)(<script[^>]*>.*?</script>)`),
		regexp.MustCompile(`(?i)(javascript:)`),
		regexp.MustCompile(`(?i)(\bor\s+\d+\s*=\s*\d+)`),
		regexp.MustCompile(`(?i)(\'\s*or\s*\'\d+\'\s*=\s*\'\d+)`),
	}

	// XSS patterns
	xssPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(<script[^>]*>.*?</script>)`),
		regexp.MustCompile(`(?i)(javascript:)`),
		regexp.MustCompile(`(?i)(on\w+\s*=)`),
		regexp.MustCompile(`(?i)(<iframe[^>]*>)`),
		regexp.MustCompile(`(?i)(<object[^>]*>)`),
		regexp.MustCompile(`(?i)(<embed[^>]*>)`),
	}

	// Path traversal patterns
	pathTraversalPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\.\.(/|\\)`),
		regexp.MustCompile(`%2e%2e(%2f|%5c)`),
		regexp.MustCompile(`\.\./`),
		regexp.MustCompile(`\.\.\\`),
	}
)

type SecurityConfig struct {
	EnableSQLInjectionProtection bool
	EnableXSSProtection          bool
	EnablePathTraversalProtection bool
	MaxRequestSize               int64
	BlockedUserAgents            []string
	AllowedContentTypes          []string
}

func DefaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		EnableSQLInjectionProtection:  true,
		EnableXSSProtection:           true,
		EnablePathTraversalProtection: true,
		MaxRequestSize:                10 * 1024 * 1024, // 10MB
		BlockedUserAgents: []string{
			"sqlmap",
			"nmap",
			"nikto",
			"gobuster",
			"dirbuster",
		},
		AllowedContentTypes: []string{
			"application/json",
			"application/x-www-form-urlencoded",
			"multipart/form-data",
			"text/plain",
		},
	}
}

// SecurityMiddleware provides comprehensive input validation and security checks
func SecurityMiddleware(config SecurityConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check request size
		if c.Request.ContentLength > config.MaxRequestSize {
			logger.Warn("Request size too large: %d bytes from %s", c.Request.ContentLength, c.ClientIP())
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Request entity too large"})
			c.Abort()
			return
		}

		// Check User-Agent
		userAgent := c.GetHeader("User-Agent")
		for _, blockedUA := range config.BlockedUserAgents {
			if strings.Contains(strings.ToLower(userAgent), strings.ToLower(blockedUA)) {
				logger.Warn("Blocked user agent detected: %s from %s", userAgent, c.ClientIP())
				c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
				c.Abort()
				return
			}
		}

		// Check Content-Type for POST/PUT requests
		if c.Request.Method == "POST" || c.Request.Method == "PUT" || c.Request.Method == "PATCH" {
			contentType := c.GetHeader("Content-Type")
			if contentType != "" {
				isAllowed := false
				for _, allowedType := range config.AllowedContentTypes {
					if strings.HasPrefix(contentType, allowedType) {
						isAllowed = true
						break
					}
				}
				if !isAllowed {
					logger.Warn("Invalid content type: %s from %s", contentType, c.ClientIP())
					c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "Unsupported content type"})
					c.Abort()
					return
				}
			}
		}

		// Validate URL path
		if config.EnablePathTraversalProtection {
			path := c.Request.URL.Path
			for _, pattern := range pathTraversalPatterns {
				if pattern.MatchString(path) {
					logger.Warn("Path traversal attempt detected in path: %s from %s", path, c.ClientIP())
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request path"})
					c.Abort()
					return
				}
			}
		}

		// Validate query parameters
		for key, values := range c.Request.URL.Query() {
			for _, value := range values {
				if !isValidInput(value, config) {
					logger.Warn("Malicious input detected in query param %s: %s from %s", key, value, c.ClientIP())
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input detected"})
					c.Abort()
					return
				}
			}
		}

		// Validate form data for POST requests
		if c.Request.Method == "POST" || c.Request.Method == "PUT" || c.Request.Method == "PATCH" {
			if err := c.Request.ParseForm(); err == nil {
				for key, values := range c.Request.Form {
					for _, value := range values {
						if !isValidInput(value, config) {
							logger.Warn("Malicious input detected in form param %s: %s from %s", key, value, c.ClientIP())
							c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input detected"})
							c.Abort()
							return
						}
					}
				}
			}
		}

		// Set security headers
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:;")

		c.Next()
	}
}

// isValidInput checks if input contains malicious patterns
func isValidInput(input string, config SecurityConfig) bool {
	if config.EnableSQLInjectionProtection {
		for _, pattern := range sqlInjectionPatterns {
			if pattern.MatchString(input) {
				return false
			}
		}
	}

	if config.EnableXSSProtection {
		for _, pattern := range xssPatterns {
			if pattern.MatchString(input) {
				return false
			}
		}
	}

	if config.EnablePathTraversalProtection {
		for _, pattern := range pathTraversalPatterns {
			if pattern.MatchString(input) {
				return false
			}
		}
	}

	return true
}