package middlewares

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/config"
	"go_boilerplate/pkg/logger"
)

// RequestLogger logs HTTP requests only (no Gin debug route dumps).
// - Skips OPTIONS preflight by default (set LOG_OPTIONS=true to include)
// - Status >= 400 logged as ERROR
func RequestLogger() gin.HandlerFunc {
	logOptions := strings.EqualFold(config.GetEnv("LOG_OPTIONS", "false"), "true")

	return func(c *gin.Context) {
		if !logOptions && c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method
		clientIP := c.ClientIP()
		if raw != "" {
			path = path + "?" + raw
		}

		msg := fmt.Sprintf("%-6s %s → %d (%s)", method, path, status, latency.Round(time.Microsecond))

		if status >= 400 {
			logger.Error("%s | ip=%s", msg, clientIP)
		} else {
			logger.Info("%s", msg)
		}

		if len(c.Errors) > 0 {
			for _, e := range c.Errors {
				logger.Error("  detail: %v", e.Err)
			}
		}
	}
}
