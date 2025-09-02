package middlewares

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start)
		status := c.Writer.Status()
		path := c.Request.URL.Path
		method := c.Request.Method

		// Log metrics (could be enhanced with a proper logging system)
		c.Writer.Header().Set("X-Response-Time", duration.String())
		// Here you can store metrics in a global counter or send to a monitoring system
		// For simplicity, we'll just add a context variable (could be used by a /metrics endpoint)
		if metrics, exists := c.Get("metrics"); exists {
			if m, ok := metrics.(map[string]interface{}); ok {
				m["requests"] = m["requests"].(int) + 1
				if status >= 200 && status < 300 {
					m["success"] = m["success"].(int) + 1
				} else {
					m["failure"] = m["failure"].(int) + 1
				}
				m[method+"_"+path] = m[method+"_"+path].(int) + 1
			}
		} else {
			metrics := map[string]interface{}{
				"requests": 1,
				"success": 0,
				"failure": 0,
				method + "_" + path: 1,
			}
			if status >= 200 && status < 300 {
				metrics["success"] = 1
			} else {
				metrics["failure"] = 1
			}
			c.Set("metrics", metrics)
		}
	}
}

func RecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal Server Error"})
				// Log the panic (could be enhanced with a proper logging system)
				// log.Printf("Panic recovered: %v", err)
				c.Abort()
			}
		}()
		c.Next()
	}
}