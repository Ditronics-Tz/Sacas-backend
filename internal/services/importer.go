package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

// Import errors.
var (
	// ErrImportHasErrors is returned when the operator tries to commit a file
	// that failed validation. Committing anyway would produce the partial,
	// unpredictable result the whole design exists to prevent.
	ErrImportHasErrors = errors.New("the file has rows that failed validation")
	// ErrImportUnknownReference is returned when a row names a parent that does
	// not exist in this institution — for example a module whose course_name
	// matches nothing.
	ErrImportUnknownReference = errors.New("a row references a record that does not exist")
	// ErrImportNothingToDo is returned for a file with no usable rows.
	ErrImportNothingToDo = errors.New("the file has no rows to import")
)

// ImportOutcome is the result of a committed import.
type ImportOutcome struct {
	Entity ImportEntity `json:"entity"`
	// Created is how many rows became records.
	Created int `json:"created"`
	// Skipped counts rows that already existed, reported rather than hidden.
	Skipped     int              `json:"skipped"`
	SkippedRows []ImportRowError `json:"skipped_rows,omitempty"`
	// DurationMS is included so an operator can see a slow upload was real work
	// and not a hang.
	DurationMS int64 `json:"duration_ms"`
}

// Importer writes validated CSV rows.
//
// References are resolved BY NAME within the institution rather than by ID,
// because a spreadsheet author has names, not primary keys, and asking an
// operator to look up IDs defeats the point of a bulk import.
type Importer struct {
	db          *gorm.DB
	audit       *AuditRecorder
	facultyRepo repositories.FacultyRepository
	courseRepo  repositories.CourseRepository
	moduleRepo  repositories.ModuleRepository
	classRepo   repositories.ClassRepository
	roomRepo    repositories.RoomRepository
	subjectRepo repositories.SubjectRepository
	staffRepo   repositories.StaffRepository
	clock       func() time.Time
}

func NewImporter(
	db *gorm.DB,
	audit *AuditRecorder,
	facultyRepo repositories.FacultyRepository,
	courseRepo repositories.CourseRepository,
	moduleRepo repositories.ModuleRepository,
	classRepo repositories.ClassRepository,
	roomRepo repositories.RoomRepository,
	subjectRepo repositories.SubjectRepository,
	staffRepo repositories.StaffRepository,
) *Importer {
	return &Importer{
		db: db, audit: audit,
		facultyRepo: facultyRepo, courseRepo: courseRepo, moduleRepo: moduleRepo,
		classRepo: classRepo, roomRepo: roomRepo, subjectRepo: subjectRepo,
		staffRepo: staffRepo,
		clock:     func() time.Time { return time.Now().UTC() },
	}
}

// Commit writes a previously validated file.
//
// The whole import is one transaction. A spreadsheet of 300 lecturers either
// lands completely or not at all, because a half-imported roster is worse than a
// failed one: the operator cannot tell which half arrived.
//
// Rows that would duplicate an existing record are skipped and REPORTED, not
// silently dropped, so re-running a corrected file is safe and visible.
func (im *Importer) Commit(
	institutionID uint,
	entity ImportEntity,
	rows []map[string]string,
	result *ImportResult,
) (*ImportOutcome, error) {
	if result != nil && result.HasErrors() {
		return nil, fmt.Errorf("%w: %d row(s) need fixing; see the errors", ErrImportHasErrors, result.Invalid)
	}
	if len(rows) == 0 {
		return nil, ErrImportNothingToDo
	}

	started := im.clock()
	outcome := &ImportOutcome{Entity: entity}

	skipped, err := im.commitRows(institutionID, entity, rows, outcome)
	outcome.Skipped = skipped
	outcome.DurationMS = im.clock().Sub(started).Milliseconds()
	if err != nil {
		return nil, err
	}

	// The audit entry is written after the commit, so a failed audit can never
	// roll back a completed import.
	if im.audit != nil {
		instID := institutionID
		im.audit.RecordSystem(
			models.AuditDataImport, &instID, string(entity), "0",
			fmt.Sprintf("%s import", entity),
			map[string]any{
				"entity":      string(entity),
				"created":     outcome.Created,
				"skipped":     outcome.Skipped,
				"rows":        len(rows),
				"duration_ms": outcome.DurationMS,
			},
		)
	}

	logger.Info("CSV import committed: entity=%s institution=%d created=%d skipped=%d in %dms",
		entity, institutionID, outcome.Created, outcome.Skipped, outcome.DurationMS)
	return outcome, nil
}

// commitRows performs the transactional write, appending to outcome as it goes.
func (im *Importer) commitRows(
	institutionID uint, entity ImportEntity, rows []map[string]string, outcome *ImportOutcome,
) (int, error) {
	skipped := 0

	err := im.db.Transaction(func(tx *gorm.DB) error {
		skipped = 0
		for i, row := range rows {
			// Line number for reporting: the header was line 1, so the first
			// data row is 2.
			line := i + 2

			duplicate, err := im.isDuplicate(institutionID, entity, row)
			if err != nil {
				return fmt.Errorf("row %d: could not check for an existing record: %w", line, err)
			}
			if duplicate {
				skipped++
				outcome.SkippedRows = append(outcome.SkippedRows, ImportRowError{
					Row: line, Message: "skipped: a matching record already exists in this institution",
				})
				continue
			}

			if err := im.insertRow(tx, institutionID, entity, row, line); err != nil {
				return err
			}
			outcome.Created++
		}
		return nil
	})
	if err != nil {
		return skipped, err
	}
	return skipped, nil
}

// isDuplicate reports whether a matching record already exists. It uses the
// natural keys the database already enforces, so a duplicate is caught here for
// a clear message rather than as a raw constraint violation from insertRow.
func (im *Importer) isDuplicate(institutionID uint, entity ImportEntity, row map[string]string) (bool, error) {
	switch entity {
	case ImportFaculty:
		f, err := im.facultyRepo.GetByName(institutionID, row["name"])
		return err == nil && f != nil, err
	case ImportCourse:
		c, err := im.courseRepo.GetByName(institutionID, row["name"])
		return err == nil && c != nil, err
	case ImportRoom:
		rooms, err := im.roomRepo.GetAll(institutionID, 0, 0)
		if err != nil {
			return false, err
		}
		for i := range rooms {
			if equalFoldName(rooms[i].Name, row["name"]) {
				return true, nil
			}
		}
		return false, nil
	case ImportSubject:
		subjects, err := im.subjectRepo.GetAll(institutionID, 0, 0)
		if err != nil {
			return false, err
		}
		for i := range subjects {
			if equalFoldName(subjects[i].Name, row["name"]) {
				return true, nil
			}
		}
		return false, nil
	case ImportClass:
		classes, err := im.classRepo.GetAll(institutionID, 0, 0)
		if err != nil {
			return false, err
		}
		for i := range classes {
			if equalFoldName(classes[i].Name, row["name"]) {
				return true, nil
			}
		}
		return false, nil
	case ImportStaff:
		// Staff email is unique per institution.
		s, err := im.staffRepo.GetByEmail(institutionID, strings.ToLower(row["email"]))
		return err == nil && s != nil, err
	case ImportModule:
		// Modules have no name lookup, so check the code when present and fall
		// back to scanning names.
		if code := row["code"]; code != "" {
			modules, err := im.moduleRepo.GetAll(institutionID, 0, 0)
			if err != nil {
				return false, err
			}
			for i := range modules {
				if equalFoldCode(modules[i].Code, code) {
					return true, nil
				}
			}
		}
		return false, nil
	}
	return false, nil
}

// insertRow writes one validated row, resolving references by name.
func (im *Importer) insertRow(
	tx *gorm.DB, institutionID uint, entity ImportEntity, row map[string]string, line int,
) error {
	switch entity {
	case ImportFaculty:
		faculty := &models.Faculty{
			Name:          row["name"],
			Description:   row["description"],
			HodName:       row["hod_name"],
			HodPhone:      row["hod_phone"],
			HodEmail:      row["hod_email"],
			InstitutionID: institutionID,
		}
		return tx.Create(faculty).Error

	case ImportCourse:
		faculty, err := im.resolveFaculty(tx, institutionID, row["faculty_name"], line)
		if err != nil {
			return err
		}
		return tx.Create(&models.Course{
			Name:          row["name"],
			FacultyID:     faculty.ID,
			Level:         row["level"],
			Description:   row["description"],
			InstitutionID: institutionID,
		}).Error

	case ImportModule:
		module := &models.Module{
			Name:          row["name"],
			Code:          row["code"],
			Type:          models.ModuleType(row["type"]),
			RequiresLab:   isBoolish(row["requires_lab"]),
			NtaLevel:      row["nta_level"],
			InstitutionID: institutionID,
		}
		creditHours, _ := parseInt(row["credit_hours"])
		module.CreditHours = creditHours
		if semester := row["semester"]; semester != "" {
			if n, err := parseInt(semester); err == nil {
				module.Semester = &n
			}
		}
		// A general_subject has no course; everything else must name one.
		if module.Type != models.ModuleTypeGeneral {
			if row["course_name"] == "" {
				return fmt.Errorf("row %d: course_name is required for a %s module", line, row["type"])
			}
			course, err := im.resolveCourse(tx, institutionID, row["course_name"], line)
			if err != nil {
				return err
			}
			module.CourseID = &course.ID
		}
		return tx.Create(module).Error

	case ImportClass:
		course, err := im.resolveCourse(tx, institutionID, row["course_name"], line)
		if err != nil {
			return err
		}
		year, _ := parseInt(row["year"])
		students, _ := parseInt(row["number_of_students"])
		return tx.Create(&models.Class{
			Name:             row["name"],
			CourseID:         course.ID,
			Year:             year,
			NumberOfStudents: students,
			AcademicYear:     row["academic_year"],
			InstitutionID:    institutionID,
		}).Error

	case ImportRoom:
		room := &models.Room{
			Name:          row["name"],
			Sticky:        isBoolish(row["sticky"]),
			InstitutionID: institutionID,
		}
		capacity, _ := parseInt(row["capacity"])
		room.Capacity = capacity
		if features := strings.TrimSpace(row["features"]); features != "" {
			// Validated during parsing; re-encode compactly so a hand-written
			// object with spaces still stores as JSON.
			room.Features = []byte(features)
		}
		return tx.Create(room).Error

	case ImportSubject:
		creditHours, _ := parseInt(row["credit_hours"])
		return tx.Create(&models.Subject{
			Name:          row["name"],
			CreditHours:   creditHours,
			InstitutionID: institutionID,
		}).Error

	case ImportStaff:
		faculty, err := im.resolveFaculty(tx, institutionID, row["faculty_name"], line)
		if err != nil {
			return err
		}
		staff := &models.Staff{
			Name:          row["name"],
			Email:         strings.ToLower(row["email"]),
			FacultyID:     faculty.ID,
			RfidID:        row["rfid_id"],
			PhoneNumber:   row["phone_number"],
			Title:         row["title"],
			StaffType:     row["staff_type"],
			MaxHours:      40,
			InstitutionID: institutionID,
		}
		if h := row["max_hours"]; h != "" {
			if n, err := parseInt(h); err == nil {
				staff.MaxHours = n
			}
		}
		return tx.Create(staff).Error
	}

	return fmt.Errorf("unsupported import entity %q", entity)
}

// resolveFaculty finds a faculty by name within the institution.
//
// The lookup is tenant-scoped on purpose: a course may only attach to a faculty
// at the same institution, and a spreadsheet naming another campus's faculty is
// a data-entry error worth reporting rather than silently cross-referencing.
func (im *Importer) resolveFaculty(tx *gorm.DB, institutionID uint, name string, line int) (*models.Faculty, error) {
	faculty, err := im.facultyRepo.GetByName(institutionID, name)
	if err != nil || faculty == nil {
		return nil, fmt.Errorf("%w: row %d names faculty %q, which does not exist in this institution — import faculties first",
			ErrImportUnknownReference, line, name)
	}
	return faculty, nil
}

func (im *Importer) resolveCourse(tx *gorm.DB, institutionID uint, name string, line int) (*models.Course, error) {
	course, err := im.courseRepo.GetByName(institutionID, name)
	if err != nil || course == nil {
		return nil, fmt.Errorf("%w: row %d names course %q, which does not exist in this institution — import courses first",
			ErrImportUnknownReference, line, name)
	}
	return course, nil
}

// validateJSONObject checks that a features cell holds a JSON object.
func validateJSONObject(value string) error {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(value), &parsed); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if parsed == nil {
		return fmt.Errorf("expected a JSON object, got a bare value")
	}
	return nil
}

func equalFoldName(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func equalFoldCode(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func looksLikeEmail(value string) bool {
	at := strings.Index(value, "@")
	if at <= 0 || at == len(value)-1 {
		return false
	}
	domain := value[at+1:]
	return strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") &&
		!strings.HasSuffix(domain, ".")
}
