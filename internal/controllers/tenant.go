package controllers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_boilerplate/internal/middlewares"
)

// tenantID returns the institution ID resolved for this request by
// TenantMiddleware.
//
// The value comes from the authenticated user's database row, never from the
// request body, a header, or the JWT. Every controller passes it as the first
// argument to the repositories, which is what enforces isolation.
//
// A platform super_admin resolves to middlewares.PlatformScope (0), meaning
// "all institutions" for the repository layer.
func tenantID(ctx *gin.Context) uint {
	return middlewares.InstitutionIDFromContext(ctx)
}

// currentUserID returns the authenticated user's ID.
func currentUserID(ctx *gin.Context) (uint, bool) {
	return middlewares.UserIDFromContext(ctx)
}

// parseIDParam reads a positive integer route parameter. It is shared by every
// controller so ID parsing (and its error message) is identical everywhere.
func parseIDParam(ctx *gin.Context, name string) (uint, error) {
	raw := ctx.Param(name)
	parsed, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("invalid %s: %q", name, raw)
	}
	return uint(parsed), nil
}

// parsePagination reads ?limit=&offset= with sane bounds, so an oversized
// limit cannot be used to pull a whole tenant's table.
func parsePagination(ctx *gin.Context) (limit, offset int) {
	limit, _ = strconv.Atoi(ctx.DefaultQuery("limit", "10"))
	offset, _ = strconv.Atoi(ctx.DefaultQuery("offset", "0"))
	if limit <= 0 {
		limit = 10
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// maxPageSize caps how many records a single list request may return.
const maxPageSize = 200

// parseRFC3339 parses an RFC3339 timestamp. Used by the audit filters, where
// a date bound must be explicit rather than guessed.
func parseRFC3339(raw string) (time.Time, error) {
	return time.Parse(time.RFC3339, raw)
}

// respondNotFound writes the canonical cross-tenant / missing-record response.
//
// A record that exists but belongs to another institution is deliberately
// reported as 404 rather than 403, so the API never confirms the existence of
// another tenant's data.
func respondNotFound(ctx *gin.Context, message string) {
	ctx.JSON(http.StatusNotFound, gin.H{"error": message})
}

// respondRepoError maps a repository error to a status code.
//
//   - not found (real miss, or a record owned by another tenant) → 404
//   - anything else → 500
//
// Keeping this in one place means every controller treats a cross-tenant hit
// the same way, instead of one endpoint leaking 403 and the rest 500.
func respondRepoError(ctx *gin.Context, notFoundMessage string, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondNotFound(ctx, notFoundMessage)
		return
	}
	ctx.JSON(http.StatusInternalServerError, gin.H{"error": notFoundMessage})
}
