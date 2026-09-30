package controllers

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"go_boilerplate/internal/services"
	"go_boilerplate/pkg/logger"
)

// maxImportUploadBytes caps the raw upload.
//
// The row limit is enforced during parsing, but the byte limit is enforced
// before parsing so a huge file cannot be read into memory at all. 5 MiB is
// generous for a few thousand rows of names and email addresses.
const maxImportUploadBytes = 5 << 20 // 5 MiB

// ImportController exposes the CSV bulk-import surface.
//
// The flow is deliberately two-step:
//
//	POST .../import/:entity/validate → parse and report every problem, write nothing
//	POST .../import/:entity          → commit an already-clean file
//
// A one-shot "import whatever this is" endpoint would either half-succeed on a
// bad file or silently skip rows, and both are worse for an operator than being
// told exactly what to fix.
type ImportController struct {
	importer *services.Importer
}

func NewImportController(importer *services.Importer) *ImportController {
	return &ImportController{importer: importer}
}

// entityFromPath resolves and validates the :entity path parameter.
func entityFromPath(ctx *gin.Context) (services.ImportEntity, bool) {
	entity := services.ImportEntity(ctx.Param("entity"))
	if !entity.IsValid() {
		entities := make([]string, 0, len(services.AllImportEntities))
		for _, e := range services.AllImportEntities {
			entities = append(entities, string(e))
		}
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error":     fmt.Sprintf("unsupported entity %q", entity),
			"supported": entities,
		})
		return "", false
	}
	return entity, true
}

// readUpload reads the CSV body, enforcing the byte cap before parsing.
func readUpload(ctx *gin.Context) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, maxImportUploadBytes+1))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Could not read the uploaded file", "details": err.Error()})
		return nil, false
	}
	if len(body) > maxImportUploadBytes {
		ctx.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error":   "The file is too large",
			"details": fmt.Sprintf("Maximum upload is %d MiB", maxImportUploadBytes>>20),
		})
		return nil, false
	}
	if len(body) == 0 {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "The file is empty"})
		return nil, false
	}
	return body, true
}

// Schema handles GET /api/protected/import/schema — the expected columns per
// entity, so the frontend can offer a template download and the operator knows
// what a valid file looks like before uploading anything.
func (c *ImportController) Schema(ctx *gin.Context) {
	entity := services.ImportEntity(ctx.Query("entity"))
	if !entity.IsValid() {
		all := map[string][]string{}
		for _, e := range services.AllImportEntities {
			all[string(e)] = services.ImportSchema(e)
		}
		ctx.JSON(http.StatusOK, gin.H{
			"entities":       all,
			"max_rows":       services.MaxImportRows,
			"max_upload_mib": maxImportUploadBytes >> 20,
		})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"entity":         string(entity),
		"columns":        services.ImportSchema(entity),
		"max_rows":       services.MaxImportRows,
		"max_upload_mib": maxImportUploadBytes >> 20,
	})
}

// Validate handles POST /api/protected/import/:entity/validate.
//
// Parses the file, reports every problem with its row number, and writes
// NOTHING. This is the endpoint the UI calls first.
func (c *ImportController) Validate(ctx *gin.Context) {
	entity, ok := entityFromPath(ctx)
	if !ok {
		return
	}
	payload, ok := readUpload(ctx)
	if !ok {
		return
	}

	result, _, err := services.ParseCSV(entity, payload)
	if err != nil {
		// A file-level problem (no header, missing columns). Row errors are
		// still returned so a partial read is not wasted.
		if result != nil {
			ctx.JSON(http.StatusOK, gin.H{
				"valid":      false,
				"file_error": err.Error(),
				"result":     result,
			})
			return
		}
		ctx.JSON(http.StatusBadRequest, gin.H{
			"valid": false,
			"error": err.Error(),
			"hint":  "GET this endpoint's schema for the expected columns",
		})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"valid":  !result.HasErrors(),
		"result": result,
		"next": map[string]any{
			"if_valid": fmt.Sprintf("POST /api/protected/import/%s to commit this file", entity),
			"rows":     result.Valid,
		},
	})
}

// Import handles POST /api/protected/import/:entity — validate, then commit.
//
// Validation runs again here even though the client was told to validate first.
// That is deliberate: the client cannot be trusted to have done it, and
// re-parsing is cheap compared to writing rows from a file nobody checked.
func (c *ImportController) Import(ctx *gin.Context) {
	inst := tenantID(ctx)

	entity, ok := entityFromPath(ctx)
	if !ok {
		return
	}
	payload, ok := readUpload(ctx)
	if !ok {
		return
	}

	result, rows, err := services.ParseCSV(entity, payload)
	if err != nil {
		if result != nil {
			ctx.JSON(http.StatusBadRequest, gin.H{
				"error":      err.Error(),
				"file_error": err.Error(),
				"result":     result,
			})
			return
		}
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if result.HasErrors() {
		// Refuse rather than importing the good rows. A partial import of a
		// roster is the outcome this design exists to prevent.
		ctx.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":   services.ErrImportHasErrors.Error(),
			"details": fmt.Sprintf("%d row(s) failed validation; nothing was imported", result.Invalid),
			"result":  result,
		})
		return
	}

	outcome, err := c.importer.Commit(inst, entity, rows, result)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrImportUnknownReference):
			// A row named a parent that does not exist. The transaction has
			// rolled back, so nothing was written.
			ctx.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "A row references a record that does not exist",
				"details": err.Error(),
				"hint":    "Import the parent records first — for example faculties before courses.",
				"result":  result,
			})
		case errors.Is(err, services.ErrImportNothingToDo):
			ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			logger.Error("CSV import failed: entity=%s institution=%d: %v", entity, inst, err)
			ctx.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Import failed and was rolled back",
				"details": err.Error(),
			})
		}
		return
	}

	ctx.JSON(http.StatusCreated, gin.H{
		"message": fmt.Sprintf("Imported %d %s record(s)", outcome.Created, entity),
		"outcome": outcome,
	})
}
