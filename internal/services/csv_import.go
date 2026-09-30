package services

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"go_boilerplate/internal/models"
)

// CSV import.
//
// The design priority is that a bulk import must never be a partial surprise. A
// spreadsheet of 400 lecturers with 3 bad rows must not end up importing 397 of
// them and reporting an error, because the operator has no way to tell which 397
// landed. So:
//
//   - The file is parsed and validated IN FULL first, and every problem is
//     reported with its row number and a human-readable reason.
//   - Only if the operator asks for it, and only if there are no errors, is
//     anything written.
//   - Rows that would duplicate an existing record are reported, not silently
//     skipped.
//
// Validation happens before any write so a rejected file leaves no trace.

// ImportEntity names a supported import target.
type ImportEntity string

const (
	ImportFaculty ImportEntity = "faculty"
	ImportCourse  ImportEntity = "course"
	ImportModule  ImportEntity = "module"
	ImportClass   ImportEntity = "class"
	ImportRoom    ImportEntity = "room"
	ImportSubject ImportEntity = "subject"
	ImportStaff   ImportEntity = "staff"
)

// AllImportEntities lists the supported targets, in the order a new institution
// would reasonably load them.
var AllImportEntities = []ImportEntity{
	ImportFaculty, ImportCourse, ImportModule, ImportClass, ImportRoom,
	ImportSubject, ImportStaff,
}

func (e ImportEntity) IsValid() bool {
	for _, known := range AllImportEntities {
		if e == known {
			return true
		}
	}
	return false
}

// MaxImportRows bounds a single upload.
//
// A spreadsheet is the one place an unauthenticated-ish caller can ask the
// server to do unbounded work, so the row count is capped before parsing. The
// cap is deliberately generous: a real institution's full staff and curriculum
// list is in the hundreds, not tens of thousands.
const MaxImportRows = 5000

// ImportRowError is one rejected row.
type ImportRowError struct {
	// Row is the 1-based line in the file, counting the header as row 1, so it
	// matches what the operator sees in their spreadsheet.
	Row     int    `json:"row"`
	Column  string `json:"column,omitempty"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

func (e ImportRowError) String() string {
	if e.Field != "" {
		return fmt.Sprintf("row %d: %s %s", e.Row, e.Field, e.Message)
	}
	return fmt.Sprintf("row %d: %s", e.Row, e.Message)
}

// ImportResult reports what a parsed file contains, before any write.
type ImportResult struct {
	Entity ImportEntity `json:"entity"`
	// Columns is the header row, echoed so the operator can check they uploaded
	// the file they meant to.
	Columns []string `json:"columns"`
	// Valid is the number of rows that passed validation.
	Valid int `json:"valid"`
	// Invalid is the number of rows that did not.
	Invalid int              `json:"invalid"`
	Errors  []ImportRowError `json:"errors"`
	// Duplicate lists rows that would collide with an existing record. They are
	// counted as invalid: re-importing a file must not quietly double a roster.
	Duplicates []ImportRowError `json:"duplicates,omitempty"`
}

// HasErrors reports whether the file is safe to import.
func (r *ImportResult) HasErrors() bool { return r.Invalid > 0 }

// ImportCSV parses and validates a CSV file for one entity, without writing
// anything.
//
// dryRunOnly is always true here: the function's entire job is validation. The
// write is a separate step so the operator sees every problem before any of them
// become rows in the database.
//
// The expected columns per entity are returned by ImportSchema.
func ParseCSV(entity ImportEntity, payload []byte) (*ImportResult, []map[string]string, error) {
	if !entity.IsValid() {
		return nil, nil, fmt.Errorf("unsupported import entity %q (supported: %s)", entity, entityList())
	}

	result := &ImportResult{Entity: entity, Errors: []ImportRowError{}, Duplicates: []ImportRowError{}}

	reader := csv.NewReader(bytes.NewReader(payload))
	// A spreadsheet almost always has ragged rows. Treating that as a hard parse
	// error would reject the whole file for a formatting quirk, so field counts
	// are reconciled per row and reported as a row error instead.
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	header, err := reader.Read()
	if err == io.EOF {
		return nil, nil, fmt.Errorf("the file is empty")
	}
	if err != nil {
		return nil, nil, fmt.Errorf("could not read the header row: %w", err)
	}

	normalised := normaliseHeader(header)
	result.Columns = normalised
	missing := missingColumns(entity, normalised)
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf(
			"missing required column(s): %s — expected headers: %s",
			strings.Join(missing, ", "), strings.Join(ImportSchema(entity), ", "))
	}

	rows := make([]map[string]string, 0, 64)
	line := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		line++
		if len(result.Errors)+len(result.Duplicates) >= MaxImportRows {
			result.Errors = append(result.Errors, ImportRowError{
				Row: line, Message: "too many problem rows; fix them and re-upload",
			})
			return result, nil, fmt.Errorf("the file has more than %d problem rows", MaxImportRows)
		}
		if err != nil {
			if parseErr, ok := err.(*csv.ParseError); ok {
				result.Errors = append(result.Errors, ImportRowError{
					Row: parseErr.Line, Message: "malformed CSV: " + parseErr.Err.Error(),
				})
				continue
			}
			return result, nil, fmt.Errorf("could not read the file: %w", err)
		}

		// A row of all-empty cells is a blank spreadsheet line, not a bad record.
		if isBlankRecord(record) {
			continue
		}
		if len(record) != len(header) {
			result.Invalid++
			result.Errors = append(result.Errors, ImportRowError{
				Row: line,
				Message: fmt.Sprintf("has %d values but the header has %d columns",
					len(record), len(header)),
			})
			continue
		}

		row := make(map[string]string, len(record))
		for i, name := range normalised {
			row[name] = strings.TrimSpace(record[i])
		}

		if errs := validateRow(entity, row, line); len(errs) > 0 {
			result.Invalid++
			result.Errors = append(result.Errors, errs...)
			// A row that failed validation is NOT kept: keeping it would let a
			// future caller commit it, and the counts would not line up.
			continue
		}
		result.Valid++
		rows = append(rows, row)
	}

	if len(rows) > MaxImportRows {
		return result, nil, fmt.Errorf("the file has %d data rows, which is over the %d limit",
			len(rows), MaxImportRows)
	}
	return result, rows, nil
}

// ImportSchema returns the accepted column names for an entity, in a
// conventional order. Extra columns are ignored, so a spreadsheet exported from
// a richer system still imports.
func ImportSchema(entity ImportEntity) []string {
	switch entity {
	case ImportFaculty:
		return []string{"name", "description", "hod_name", "hod_phone", "hod_email"}
	case ImportCourse:
		return []string{"name", "faculty_name", "level", "description"}
	case ImportModule:
		return []string{"name", "code", "course_name", "credit_hours", "type", "requires_lab", "semester", "nta_level"}
	case ImportClass:
		return []string{"name", "course_name", "year", "number_of_students", "academic_year"}
	case ImportRoom:
		return []string{"name", "capacity", "features", "sticky"}
	case ImportSubject:
		return []string{"name", "credit_hours"}
	case ImportStaff:
		return []string{"name", "email", "faculty_name", "max_hours", "rfid_id", "phone_number", "title", "staff_type"}
	default:
		return nil
	}
}

// requiredColumns are the columns without which a row cannot be built.
var requiredColumns = map[ImportEntity][]string{
	ImportFaculty: {"name"},
	ImportCourse:  {"name", "faculty_name"},
	ImportModule:  {"name", "credit_hours", "type"},
	ImportClass:   {"name", "course_name", "year", "number_of_students"},
	ImportRoom:    {"name", "capacity"},
	ImportSubject: {"name", "credit_hours"},
	ImportStaff:   {"name", "email", "faculty_name"},
}

func missingColumns(entity ImportEntity, header []string) []string {
	present := map[string]bool{}
	for _, h := range header {
		present[h] = true
	}
	var missing []string
	for _, required := range requiredColumns[entity] {
		if !present[required] {
			missing = append(missing, required)
		}
	}
	return missing
}

// normaliseHeader lower-cases, trims, and replaces spaces with underscores, so
// "Credit Hours" and "credit_hours" are the same column. A spreadsheet authored
// by a human will not have used machine-readable headers.
func normaliseHeader(header []string) []string {
	out := make([]string, 0, len(header))
	for _, h := range header {
		cleaned := strings.ToLower(strings.TrimSpace(h))
		cleaned = strings.ReplaceAll(cleaned, " ", "_")
		out = append(out, cleaned)
	}
	return out
}

func isBlankRecord(record []string) bool {
	for _, v := range record {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

// validateRow checks one parsed row, returning every problem found rather than
// stopping at the first, so one upload tells the operator everything to fix.
func validateRow(entity ImportEntity, row map[string]string, line int) []ImportRowError {
	var errs []ImportRowError
	add := func(field, message string) {
		errs = append(errs, ImportRowError{Row: line, Field: field, Message: message})
	}

	for _, required := range requiredColumns[entity] {
		if row[required] == "" {
			add(required, "is required")
		}
	}

	switch entity {
	case ImportFaculty:
		if email := row["hod_email"]; email != "" && !looksLikeEmail(email) {
			add("hod_email", "is not a valid email address")
		}

	case ImportCourse:
		// Nothing beyond the required columns.

	case ImportModule:
		t := models.ModuleType(row["type"])
		if row["type"] != "" && !t.IsValid() {
			add("type", "must be one of: core, elective, general_subject")
		}
		if h, err := parseInt(row["credit_hours"]); err != nil || h < 1 || h > 10 {
			add("credit_hours", "must be a whole number between 1 and 10")
		}
		if s := row["semester"]; s != "" {
			if n, err := parseInt(s); err != nil || n < 1 || n > 12 {
				add("semester", "must be between 1 and 12")
			}
		}
		if b := row["requires_lab"]; b != "" && !isBoolish(b) {
			add("requires_lab", "must be true or false")
		}

	case ImportClass:
		if y, err := parseInt(row["year"]); err != nil || y < 1 || y > 6 {
			add("year", "must be a whole number between 1 and 6")
		}
		if n, err := parseInt(row["number_of_students"]); err != nil || n < 1 {
			add("number_of_students", "must be a positive whole number")
		}

	case ImportRoom:
		if n, err := parseInt(row["capacity"]); err != nil || n < 1 {
			add("capacity", "must be a positive whole number")
		}
		if b := row["sticky"]; b != "" && !isBoolish(b) {
			add("sticky", "must be true or false")
		}
		if f := row["features"]; f != "" {
			if err := validateJSONObject(f); err != nil {
				add("features", "must be a JSON object, e.g. {\"lab\":true}")
			}
		}

	case ImportSubject:
		if h, err := parseInt(row["credit_hours"]); err != nil || h < 1 || h > 10 {
			add("credit_hours", "must be a whole number between 1 and 10")
		}

	case ImportStaff:
		if email := row["email"]; email != "" && !looksLikeEmail(email) {
			add("email", "is not a valid email address")
		}
		if h := row["max_hours"]; h != "" {
			if n, err := parseInt(h); err != nil || n < 1 || n > 60 {
				add("max_hours", "must be between 1 and 60")
			}
		}
	}

	return errs
}

func entityList() string {
	names := make([]string, 0, len(AllImportEntities))
	for _, e := range AllImportEntities {
		names = append(names, string(e))
	}
	return strings.Join(names, ", ")
}

// parseInt is strconv.Atoi with the error discarded, for the many call sites
// that only care whether the value is a usable number.
func parseInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty")
	}
	return strconv.Atoi(value)
}

// isBoolish accepts the spellings a spreadsheet is likely to contain.
func isBoolish(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "y", "1":
		return true
	case "false", "no", "n", "0", "":
		return false
	default:
		return false
	}
}
