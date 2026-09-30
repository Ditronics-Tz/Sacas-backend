package services

import (
	"strings"
	"testing"
	"time"

	"go_boilerplate/internal/models"
)

// --- CSV parsing and validation ---------------------------------------------

// TestParseCSV_RejectsUnsupportedEntity: a typo must be a clear error, not an
// empty import.
func TestParseCSV_RejectsUnsupportedEntity(t *testing.T) {
	_, _, err := ParseCSV(ImportEntity("lecturers"), []byte("name\nA\n"))
	if err == nil {
		t.Fatal("expected an error for an unsupported entity")
	}
	// The message must list what IS supported, so the operator can fix it.
	for _, e := range AllImportEntities {
		if !strings.Contains(err.Error(), string(e)) {
			t.Errorf("the error should list %q as supported: %v", e, err)
		}
	}
}

func TestParseCSV_EmptyFileIsAnError(t *testing.T) {
	_, _, err := ParseCSV(ImportStaff, []byte(""))
	if err == nil {
		t.Fatal("expected an error for an empty file")
	}
}

// TestParseCSV_MissingRequiredColumns lists what is missing, because the most
// common upload failure is a header the operator did not expect.
func TestParseCSV_MissingRequiredColumns(t *testing.T) {
	result, _, err := ParseCSV(ImportStaff, []byte("name,email\nDr A,a@x.com\n"))
	if err == nil {
		t.Fatal("expected an error for missing required columns")
	}
	if !strings.Contains(err.Error(), "faculty_name") {
		t.Errorf("the error should name the missing column: %v", err)
	}
	_ = result
}

// TestParseCSV_NormalisesHumanHeaders is the difference between an import that
// works and one that fails for a spreadsheet authored by a person: "Credit
// Hours" must be accepted for credit_hours.
func TestParseCSV_NormalisesHumanHeaders(t *testing.T) {
	csv := "Name,Credit Hours,Faculty Name\nEngineering,3,Engineering Faculty\n"
	result, rows, err := ParseCSV(ImportSubject, []byte(csv))
	_ = result
	_ = rows
	// subject requires name + credit_hours, and the header normalises to those.
	if err != nil {
		t.Fatalf("normalised headers were not accepted: %v", err)
	}
}

// TestParseCSV_ValidFile is the happy path.
func TestParseCSV_ValidFile(t *testing.T) {
	csv := strings.Join([]string{
		"name,email,faculty_name,max_hours",
		"Dr A,a@x.com,Engineering,40",
		"Dr B,b@x.com,Engineering,30",
	}, "\n")

	result, rows, err := ParseCSV(ImportStaff, []byte(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasErrors() {
		t.Fatalf("expected no errors, got %v", result.Errors)
	}
	if result.Valid != 2 || result.Invalid != 0 {
		t.Errorf("expected 2 valid rows, got valid=%d invalid=%d", result.Valid, result.Invalid)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 parsed rows, got %d", len(rows))
	}
	if rows[0]["name"] != "Dr A" || rows[0]["email"] != "a@x.com" {
		t.Errorf("row not parsed correctly: %+v", rows[0])
	}
}

// TestParseCSV_ReportsEveryProblemInARow is what makes the endpoint useful: one
// upload should tell the operator everything to fix, not the first thing.
func TestParseCSV_ReportsEveryProblemInARow(t *testing.T) {
	csv := strings.Join([]string{
		"name,email,faculty_name,max_hours",
		"Dr A,not-an-email,Engineering,999",
	}, "\n")

	result, _, err := ParseCSV(ImportStaff, []byte(csv))
	if err != nil {
		t.Fatalf("unexpected file error: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected the row to be rejected")
	}
	// Two independent problems on one row: the bad email and the out-of-range
	// max_hours.
	if len(result.Errors) < 2 {
		t.Errorf("expected both problems to be reported, got %v", result.Errors)
	}
	fields := map[string]bool{}
	for _, e := range result.Errors {
		fields[e.Field] = true
	}
	if !fields["email"] {
		t.Error("the invalid email was not reported")
	}
	if !fields["max_hours"] {
		t.Error("the out-of-range max_hours was not reported")
	}
}

// TestParseCSV_RowNumbersMatchTheSpreadsheet is what lets an operator actually
// find the bad row: the header is row 1, so the first data row is row 2.
func TestParseCSV_RowNumbersMatchTheSpreadsheet(t *testing.T) {
	csv := strings.Join([]string{
		"name,email,faculty_name",    // row 1
		"Dr A,a@x.com,Engineering",   // row 2 — fine
		"Dr B,bad-email,Engineering", // row 3 — bad
	}, "\n")

	result, _, _ := ParseCSV(ImportStaff, []byte(csv))
	if !result.HasErrors() {
		t.Fatal("expected an error")
	}
	found := false
	for _, e := range result.Errors {
		if e.Row == 3 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the problem reported on row 3, got %v", result.Errors)
	}
}

// TestParseCSV_RaggedRowIsReportedNotFatal: spreadsheets have ragged rows, and a
// whole file must not be rejected for one of them.
func TestParseCSV_RaggedRowIsReportedNotFatal(t *testing.T) {
	csv := strings.Join([]string{
		"name,email,faculty_name",
		"Dr A,a@x.com,Engineering",
		"Dr B,b@x.com", // one value short
	}, "\n")

	result, _, err := ParseCSV(ImportStaff, []byte(csv))
	if err != nil {
		t.Fatalf("a ragged row must not fail the whole file: %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("expected the ragged row to be reported")
	}
	// And the good row still counted, so the operator knows what would import.
	if result.Valid != 1 {
		t.Errorf("expected the good row to be counted valid, got %d", result.Valid)
	}
}

// TestParseCSV_BlankLinesAreSkipped: a spreadsheet has trailing empty lines and
// they are not bad records.
func TestParseCSV_BlankLinesAreSkipped(t *testing.T) {
	csv := "name,email,faculty_name\nDr A,a@x.com,Engineering\n\n\n"
	result, _, err := ParseCSV(ImportStaff, []byte(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasErrors() {
		t.Errorf("blank lines must not be errors: %v", result.Errors)
	}
	if result.Valid != 1 {
		t.Errorf("expected 1 valid row, got %d", result.Valid)
	}
}

// TestParseCSV_ValidatesPerEntity covers the entity-specific rules.
func TestParseCSV_ValidatesPerEntity(t *testing.T) {
	cases := []struct {
		name   string
		entity ImportEntity
		csv    string
		column string
	}{
		{"module type", ImportModule,
			"name,credit_hours,type\nAlgorithms,3,nonsense\n", "type"},
		{"module credit hours range", ImportModule,
			"name,credit_hours,type\nAlgorithms,99,core\n", "credit_hours"},
		{"class year range", ImportClass,
			"name,course_name,year,number_of_students\nA,Eng,9,30\n", "year"},
		{"class students positive", ImportClass,
			"name,course_name,year,number_of_students\nA,Eng,1,0\n", "number_of_students"},
		{"room capacity positive", ImportRoom,
			"name,capacity\nA,0\n", "capacity"},
		{"subject credit hours", ImportSubject,
			"name,credit_hours\nA,50\n", "credit_hours"},
		{"room features JSON", ImportRoom,
			"name,capacity,features\nA,30,not json\n", "features"},
		{"faculty hod email", ImportFaculty,
			"name,hod_email\nEng,nope\n", "hod_email"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, _, err := ParseCSV(tc.entity, []byte(tc.csv))
			if err != nil {
				t.Fatalf("unexpected file error: %v", err)
			}
			if !result.HasErrors() {
				t.Fatalf("expected %s to be rejected", tc.column)
			}
			found := false
			for _, e := range result.Errors {
				if e.Field == tc.column {
					found = true
				}
			}
			if !found {
				t.Errorf("expected an error on %s, got %v", tc.column, result.Errors)
			}
		})
	}
}

// TestParseCSV_GeneralSubjectNeedsNoCourse, so a curriculum's general subjects
// import without a bogus course reference.
func TestParseCSV_GeneralSubjectNeedsNoCourse(t *testing.T) {
	csv := "name,credit_hours,type\nResearch Methods,2,general_subject\n"
	result, _, err := ParseCSV(ImportModule, []byte(csv))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.HasErrors() {
		t.Errorf("a general_subject needs no course: %v", result.Errors)
	}
}

// --- Helpers used by the importer -------------------------------------------

func TestLooksLikeEmail(t *testing.T) {
	valid := []string{"a@x.com", "first.last@sub.domain.ac.tz", "a+tag@x.co.tz"}
	invalid := []string{"", "nope", "@x.com", "a@", "a@x", "a@.com", "a@x."}
	for _, v := range valid {
		if !looksLikeEmail(v) {
			t.Errorf("expected %q to be a valid email", v)
		}
	}
	for _, v := range invalid {
		if looksLikeEmail(v) {
			t.Errorf("expected %q to be invalid", v)
		}
	}
}

func TestIsBoolish(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "yes", "Y", "1"} {
		if !isBoolish(v) {
			t.Errorf("expected %q to be true", v)
		}
	}
	// Anything unrecognised is false, and the row validator reports it, so a
	// typo like "ture" becomes a validation error rather than a silent false.
	for _, v := range []string{"false", "no", "0", "n"} {
		if isBoolish(v) {
			t.Errorf("expected %q to be false", v)
		}
	}
	if isBoolish("ture") {
		t.Error("a typo must not be silently accepted as a boolean")
	}
}

func TestValidateJSONObject(t *testing.T) {
	if err := validateJSONObject(`{"lab":true}`); err != nil {
		t.Errorf("valid JSON object rejected: %v", err)
	}
	for _, bad := range []string{`not json`, `{"unclosed":`, `[1,2]`} {
		if err := validateJSONObject(bad); err == nil {
			t.Errorf("expected %q to be rejected", bad)
		}
	}
}

// --- Timetable and exam lifecycle -------------------------------------------

func TestClass_TimetableStateIsDerived(t *testing.T) {
	now := timeNowForTest()
	cls := &models.Class{}

	// No entries is empty regardless of any timestamp.
	if got := cls.TimetableState(false); got != models.TimetableEmpty {
		t.Errorf("no entries should be empty, got %s", got)
	}

	// Entries with no publish stamp is a draft.
	if got := cls.TimetableState(true); got != models.TimetableDraft {
		t.Errorf("expected draft, got %s", got)
	}

	cls.PublishedAt = &now
	if got := cls.TimetableState(true); got != models.TimetablePublished {
		t.Errorf("expected published, got %s", got)
	}

	cls.ApprovedAt = &now
	if got := cls.TimetableState(true); got != models.TimetableApproved {
		t.Errorf("expected approved, got %s", got)
	}

	// Approval wins even if a publication stamp were later cleared, because the
	// state is derived in a fixed order.
	cls.PublishedAt = nil
	if got := cls.TimetableState(true); got != models.TimetableApproved {
		t.Errorf("expected approved, got %s", got)
	}
}

// TestExamStatus_TransitionsAreForwardOnly guards the governance property: an
// approved exam is terminal, and a draft cannot jump straight to approved.
func TestExamStatus_TransitionsAreForwardOnly(t *testing.T) {
	cases := []struct {
		from models.ExamStatus
		to   models.ExamStatus
		want bool
	}{
		{models.ExamStatusDraft, models.ExamStatusScheduled, true},
		{models.ExamStatusScheduled, models.ExamStatusPublished, true},
		{models.ExamStatusPublished, models.ExamStatusApproved, true},
		// No skipping a stage on the way up.
		{models.ExamStatusDraft, models.ExamStatusPublished, false},
		{models.ExamStatusDraft, models.ExamStatusApproved, false},
		{models.ExamStatusScheduled, models.ExamStatusApproved, false},
		// Approved is terminal.
		{models.ExamStatusApproved, models.ExamStatusDraft, false},
		{models.ExamStatusApproved, models.ExamStatusPublished, false},
		// A correction may send a scheduled or published exam back to draft,
		// because an error is often found too late.
		{models.ExamStatusScheduled, models.ExamStatusDraft, true},
		{models.ExamStatusPublished, models.ExamStatusDraft, true},
		// Draft may not go backwards.
		{models.ExamStatusDraft, models.ExamStatusDraft, false},
	}
	for _, tc := range cases {
		if got := tc.from.CanTransitionTo(tc.to); got != tc.want {
			t.Errorf("%s -> %s = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestExam_Helpers(t *testing.T) {
	room, invig := uint(1), uint(2)
	exam := &models.Exam{StartTime: "09:00", EndTime: "10:00"}
	if exam.IsScheduled() {
		t.Error("an exam with no room or invigilator is not scheduled")
	}
	if !exam.HasValidTimeWindow() {
		t.Error("09:00-10:00 is a valid window")
	}
	exam.RoomID = &room
	exam.InvigilatorID = &invig
	if !exam.IsScheduled() {
		t.Error("an exam with a room and invigilator is scheduled")
	}
	exam.StartTime = "11:00"
	if exam.HasValidTimeWindow() {
		t.Error("11:00-10:00 is not a valid window")
	}
}

func timeNowForTest() time.Time { return time.Now().UTC() }
