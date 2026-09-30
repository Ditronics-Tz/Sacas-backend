package controllers

import (
	"encoding/json"
	"strings"

	"gorm.io/gorm"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
)

// Tenant-aware in-memory stubs shared by the controller tests.
//
// These stubs deliberately ENFORCE the same isolation rule as the real
// repositories: a record whose institution_id does not match the caller's
// scope is reported as gorm.ErrRecordNotFound. That makes the cross-tenant
// tests meaningful — they fail if a controller ever stops passing the
// institutionID through, not merely if the stub is lenient.

const (
	// tenantA and tenantB are the two institutions used in isolation tests.
	tenantA uint = 1
	tenantB uint = 2
)

// visibleTo reports whether a record is reachable under a given scope,
// mirroring repositories.TenantScope.
func visibleTo(recordInstitutionID, scope uint) bool {
	return repositories.InTenant(recordInstitutionID, scope)
}

// equalFold is a case-insensitive name comparison, matching the case-insensitive
// unique indexes the real repositories enforce.
func equalFold(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

// scopedPage applies limit/offset to a slice, matching the real repositories.
func scopedPage[T any](items []T, limit, offset int) []T {
	if offset >= len(items) {
		return []T{}
	}
	end := len(items)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return items[offset:end]
}

// --- Faculty -----------------------------------------------------------------

type stubFacultyRepo struct {
	items  map[uint]*models.Faculty
	nextID uint
	// lastScope records the institutionID of the most recent call, so tests
	// can assert the controller threaded the tenant through.
	lastScope uint
}

func newStubFacultyRepo() *stubFacultyRepo {
	return &stubFacultyRepo{items: map[uint]*models.Faculty{}, nextID: 1}
}

// seed inserts a faculty directly, bypassing the tenant checks, so a test can
// set up data that belongs to a specific institution. An explicit non-zero ID
// is respected.
func (r *stubFacultyRepo) seed(f *models.Faculty) *models.Faculty {
	if f.ID == 0 {
		f.ID = r.nextID
	}
	if f.ID >= r.nextID {
		r.nextID = f.ID + 1
	}
	cp := *f
	r.items[f.ID] = &cp
	return &cp
}

func (r *stubFacultyRepo) Create(institutionID uint, f *models.Faculty) error {
	if !repositories.IsPlatformScope(institutionID) {
		f.InstitutionID = institutionID
	}
	r.seed(f)
	return nil
}

func (r *stubFacultyRepo) GetByID(institutionID, id uint) (*models.Faculty, error) {
	r.lastScope = institutionID
	f, ok := r.items[id]
	if !ok || !visibleTo(f.InstitutionID, institutionID) {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *f
	return &cp, nil
}

func (r *stubFacultyRepo) GetByName(institutionID uint, name string) (*models.Faculty, error) {
	for _, f := range r.items {
		if visibleTo(f.InstitutionID, institutionID) && equalFold(f.Name, name) {
			cp := *f
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *stubFacultyRepo) Update(institutionID uint, f *models.Faculty) error {
	existing, ok := r.items[f.ID]
	if !ok || !visibleTo(existing.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	cp := *f
	// A record must not be moved between tenants by an update.
	cp.InstitutionID = existing.InstitutionID
	r.items[f.ID] = &cp
	return nil
}

func (r *stubFacultyRepo) Delete(institutionID, id uint) error {
	f, ok := r.items[id]
	if !ok || !visibleTo(f.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *stubFacultyRepo) GetAll(institutionID uint, limit, offset int) ([]models.Faculty, error) {
	var out []models.Faculty
	for _, f := range r.items {
		if visibleTo(f.InstitutionID, institutionID) {
			out = append(out, *f)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubFacultyRepo) GetWithCourses(institutionID, id uint) (*models.Faculty, error) {
	return r.GetByID(institutionID, id)
}

// --- Course ------------------------------------------------------------------

type stubCourseRepo struct {
	items  map[uint]*models.Course
	nextID uint
}

func newStubCourseRepo() *stubCourseRepo {
	return &stubCourseRepo{items: map[uint]*models.Course{}, nextID: 1}
}

func (r *stubCourseRepo) seed(c *models.Course) *models.Course {
	c.ID = r.nextID
	r.nextID++
	cp := *c
	r.items[c.ID] = &cp
	return &cp
}

func (r *stubCourseRepo) Create(institutionID uint, c *models.Course) error {
	if !repositories.IsPlatformScope(institutionID) {
		c.InstitutionID = institutionID
	}
	r.seed(c)
	return nil
}

func (r *stubCourseRepo) GetByID(institutionID, id uint) (*models.Course, error) {
	c, ok := r.items[id]
	if !ok || !visibleTo(c.InstitutionID, institutionID) {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *c
	return &cp, nil
}

func (r *stubCourseRepo) GetByName(institutionID uint, name string) (*models.Course, error) {
	for _, c := range r.items {
		if visibleTo(c.InstitutionID, institutionID) && equalFold(c.Name, name) {
			cp := *c
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *stubCourseRepo) Update(institutionID uint, c *models.Course) error {
	existing, ok := r.items[c.ID]
	if !ok || !visibleTo(existing.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	cp := *c
	cp.InstitutionID = existing.InstitutionID
	r.items[c.ID] = &cp
	return nil
}

func (r *stubCourseRepo) Delete(institutionID, id uint) error {
	c, ok := r.items[id]
	if !ok || !visibleTo(c.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *stubCourseRepo) GetAll(institutionID uint, limit, offset int) ([]models.Course, error) {
	var out []models.Course
	for _, c := range r.items {
		if visibleTo(c.InstitutionID, institutionID) {
			out = append(out, *c)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubCourseRepo) GetByFaculty(institutionID, facultyID uint, limit, offset int) ([]models.Course, error) {
	var out []models.Course
	for _, c := range r.items {
		if visibleTo(c.InstitutionID, institutionID) && c.FacultyID == facultyID {
			out = append(out, *c)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubCourseRepo) GetWithModules(institutionID, id uint) (*models.Course, error) {
	return r.GetByID(institutionID, id)
}

// --- Class -------------------------------------------------------------------

type stubClassRepo struct {
	items  map[uint]*models.Class
	nextID uint
}

func newStubClassRepo() *stubClassRepo {
	return &stubClassRepo{items: map[uint]*models.Class{}, nextID: 1}
}

func (r *stubClassRepo) seed(c *models.Class) *models.Class {
	c.ID = r.nextID
	r.nextID++
	cp := *c
	r.items[c.ID] = &cp
	return &cp
}

func (r *stubClassRepo) Create(institutionID uint, c *models.Class) error {
	if !repositories.IsPlatformScope(institutionID) {
		c.InstitutionID = institutionID
	}
	r.seed(c)
	return nil
}

func (r *stubClassRepo) GetByID(institutionID, id uint) (*models.Class, error) {
	c, ok := r.items[id]
	if !ok || !visibleTo(c.InstitutionID, institutionID) {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *c
	return &cp, nil
}

func (r *stubClassRepo) Update(institutionID uint, c *models.Class) error {
	existing, ok := r.items[c.ID]
	if !ok || !visibleTo(existing.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	cp := *c
	cp.InstitutionID = existing.InstitutionID
	r.items[c.ID] = &cp
	return nil
}

func (r *stubClassRepo) Delete(institutionID, id uint) error {
	c, ok := r.items[id]
	if !ok || !visibleTo(c.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *stubClassRepo) GetAll(institutionID uint, limit, offset int) ([]models.Class, error) {
	var out []models.Class
	for _, c := range r.items {
		if visibleTo(c.InstitutionID, institutionID) {
			out = append(out, *c)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubClassRepo) GetByCourse(institutionID, courseID uint, limit, offset int) ([]models.Class, error) {
	var out []models.Class
	for _, c := range r.items {
		if visibleTo(c.InstitutionID, institutionID) && c.CourseID == courseID {
			out = append(out, *c)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubClassRepo) GetByYear(institutionID uint, year int, limit, offset int) ([]models.Class, error) {
	var out []models.Class
	for _, c := range r.items {
		if visibleTo(c.InstitutionID, institutionID) && c.Year == year {
			out = append(out, *c)
		}
	}
	return scopedPage(out, limit, offset), nil
}

// --- Room --------------------------------------------------------------------

type stubRoomRepo struct {
	items  map[uint]*models.Room
	nextID uint
}

func newStubRoomRepo() *stubRoomRepo {
	return &stubRoomRepo{items: map[uint]*models.Room{}, nextID: 1}
}

func (r *stubRoomRepo) seed(room *models.Room) *models.Room {
	room.ID = r.nextID
	r.nextID++
	cp := *room
	r.items[room.ID] = &cp
	return &cp
}

func (r *stubRoomRepo) Create(institutionID uint, room *models.Room) error {
	if !repositories.IsPlatformScope(institutionID) {
		room.InstitutionID = institutionID
	}
	r.seed(room)
	return nil
}

func (r *stubRoomRepo) GetByID(institutionID, id uint) (*models.Room, error) {
	room, ok := r.items[id]
	if !ok || !visibleTo(room.InstitutionID, institutionID) {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *room
	return &cp, nil
}

func (r *stubRoomRepo) Update(institutionID uint, room *models.Room) error {
	existing, ok := r.items[room.ID]
	if !ok || !visibleTo(existing.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	cp := *room
	cp.InstitutionID = existing.InstitutionID
	r.items[room.ID] = &cp
	return nil
}

func (r *stubRoomRepo) Delete(institutionID, id uint) error {
	room, ok := r.items[id]
	if !ok || !visibleTo(room.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *stubRoomRepo) GetAll(institutionID uint, limit, offset int) ([]models.Room, error) {
	var out []models.Room
	for _, room := range r.items {
		if visibleTo(room.InstitutionID, institutionID) {
			out = append(out, *room)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubRoomRepo) GetByCapacity(institutionID uint, minCapacity int) ([]models.Room, error) {
	var out []models.Room
	for _, room := range r.items {
		if visibleTo(room.InstitutionID, institutionID) && room.Capacity >= minCapacity {
			out = append(out, *room)
		}
	}
	return out, nil
}

func (r *stubRoomRepo) GetLabRooms(institutionID uint) ([]models.Room, error) {
	var out []models.Room
	for _, room := range r.items {
		if visibleTo(room.InstitutionID, institutionID) && hasLabFeature(room.Features) {
			out = append(out, *room)
		}
	}
	return out, nil
}

func (r *stubRoomRepo) GetStickyRooms(institutionID uint) ([]models.Room, error) {
	var out []models.Room
	for _, room := range r.items {
		if visibleTo(room.InstitutionID, institutionID) && room.Sticky {
			out = append(out, *room)
		}
	}
	return out, nil
}

func (r *stubRoomRepo) GetAvailableRooms(institutionID uint, day models.Weekday, startTime, endTime string) ([]models.Room, error) {
	return r.GetAll(institutionID, 0, 0)
}

func hasLabFeature(raw []byte) bool {
	var f models.RoomFeatures
	if len(raw) == 0 {
		return false
	}
	if err := jsonUnmarshal(raw, &f); err != nil {
		return false
	}
	return f.Lab
}

// --- Subject -----------------------------------------------------------------

type stubSubjectRepo struct {
	items  map[uint]*models.Subject
	nextID uint
}

func newStubSubjectRepo() *stubSubjectRepo {
	return &stubSubjectRepo{items: map[uint]*models.Subject{}, nextID: 1}
}

func (r *stubSubjectRepo) seed(s *models.Subject) *models.Subject {
	s.ID = r.nextID
	r.nextID++
	cp := *s
	r.items[s.ID] = &cp
	return &cp
}

func (r *stubSubjectRepo) Create(institutionID uint, s *models.Subject) error {
	if !repositories.IsPlatformScope(institutionID) {
		s.InstitutionID = institutionID
	}
	r.seed(s)
	return nil
}

func (r *stubSubjectRepo) GetByID(institutionID, id uint) (*models.Subject, error) {
	s, ok := r.items[id]
	if !ok || !visibleTo(s.InstitutionID, institutionID) {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *stubSubjectRepo) Update(institutionID uint, s *models.Subject) error {
	existing, ok := r.items[s.ID]
	if !ok || !visibleTo(existing.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	cp := *s
	cp.InstitutionID = existing.InstitutionID
	r.items[s.ID] = &cp
	return nil
}

func (r *stubSubjectRepo) Delete(institutionID, id uint) error {
	s, ok := r.items[id]
	if !ok || !visibleTo(s.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *stubSubjectRepo) GetAll(institutionID uint, limit, offset int) ([]models.Subject, error) {
	var out []models.Subject
	for _, s := range r.items {
		if visibleTo(s.InstitutionID, institutionID) {
			out = append(out, *s)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubSubjectRepo) GetByCreditHours(institutionID uint, creditHours int) ([]models.Subject, error) {
	var out []models.Subject
	for _, s := range r.items {
		if visibleTo(s.InstitutionID, institutionID) && s.CreditHours == creditHours {
			out = append(out, *s)
		}
	}
	return out, nil
}

// --- Staff -------------------------------------------------------------------

// stubStaffRepo is tenant-aware: a staff record in another institution is
// reported as not found, which is what makes the cross-tenant staff tests
// meaningful.
type stubStaffRepo struct {
	staff   map[uint]*models.Staff
	nextID  uint
	modules map[uint][]models.Staff
}

func newStubStaffRepo() *stubStaffRepo {
	return &stubStaffRepo{
		staff:   map[uint]*models.Staff{},
		nextID:  1,
		modules: map[uint][]models.Staff{},
	}
}

// seedStaff inserts a staff record directly, bypassing the tenant checks.
// An explicit non-zero ID is respected so tests can pin cross-tenant IDs.
func (r *stubStaffRepo) seedStaff(s *models.Staff) *models.Staff {
	if s.ID == 0 {
		s.ID = r.nextID
	}
	if s.ID >= r.nextID {
		r.nextID = s.ID + 1
	}
	cp := *s
	r.staff[s.ID] = &cp
	return &cp
}

func (r *stubStaffRepo) Create(institutionID uint, s *models.Staff) error {
	if !repositories.IsPlatformScope(institutionID) {
		s.InstitutionID = institutionID
	}
	r.seedStaff(s)
	return nil
}

func (r *stubStaffRepo) GetByID(institutionID, id uint) (*models.Staff, error) {
	s, ok := r.staff[id]
	if !ok || !visibleTo(s.InstitutionID, institutionID) {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *stubStaffRepo) GetByEmail(institutionID uint, email string) (*models.Staff, error) {
	for _, s := range r.staff {
		if visibleTo(s.InstitutionID, institutionID) && s.Email == email {
			cp := *s
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *stubStaffRepo) GetByUserID(institutionID, userID uint) (*models.Staff, error) {
	for _, s := range r.staff {
		if s.UserID != nil && *s.UserID == userID && visibleTo(s.InstitutionID, institutionID) {
			cp := *s
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *stubStaffRepo) Update(institutionID uint, s *models.Staff) error {
	existing, ok := r.staff[s.ID]
	if !ok || !visibleTo(existing.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	cp := *s
	// Neither the tenant nor the login link may be changed by a plain update.
	cp.InstitutionID = existing.InstitutionID
	cp.UserID = existing.UserID
	r.staff[s.ID] = &cp
	return nil
}

func (r *stubStaffRepo) SetUserLink(institutionID, staffID uint, userID *uint) error {
	existing, ok := r.staff[staffID]
	if !ok || !visibleTo(existing.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	cp := *existing
	cp.UserID = userID
	r.staff[staffID] = &cp
	return nil
}

func (r *stubStaffRepo) Delete(institutionID, id uint) error {
	s, ok := r.staff[id]
	if !ok || !visibleTo(s.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	delete(r.staff, id)
	return nil
}

func (r *stubStaffRepo) GetAll(institutionID uint, limit, offset int) ([]models.Staff, error) {
	var out []models.Staff
	for _, s := range r.staff {
		if visibleTo(s.InstitutionID, institutionID) {
			out = append(out, *s)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubStaffRepo) GetByFaculty(institutionID, facultyID uint, limit, offset int) ([]models.Staff, error) {
	var out []models.Staff
	for _, s := range r.staff {
		if visibleTo(s.InstitutionID, institutionID) && s.FacultyID == facultyID {
			out = append(out, *s)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubStaffRepo) GetWithModules(institutionID, id uint) (*models.Staff, error) {
	return r.GetByID(institutionID, id)
}

func (r *stubStaffRepo) UpdatePreferences(institutionID, id uint, preferences string) error {
	s, err := r.GetByID(institutionID, id)
	if err != nil {
		return err
	}
	s.Preferences = []byte(preferences)
	return nil
}

// AssignModule records the link only when both sides are in the caller's
// institution, mirroring the real join-table scoping.
func (r *stubStaffRepo) AssignModule(institutionID, staffID, moduleID uint) error {
	s, err := r.GetByID(institutionID, staffID)
	if err != nil {
		return err
	}
	for _, existing := range r.modules[staffID] {
		if existing.ID == moduleID {
			return nil
		}
	}
	r.modules[staffID] = append(r.modules[staffID], models.Staff{ID: moduleID, InstitutionID: s.InstitutionID})
	return nil
}

func (r *stubStaffRepo) UnassignModule(institutionID, staffID, moduleID uint) error {
	if _, err := r.GetByID(institutionID, staffID); err != nil {
		return err
	}
	var next []models.Staff
	for _, m := range r.modules[staffID] {
		if m.ID != moduleID {
			next = append(next, m)
		}
	}
	r.modules[staffID] = next
	return nil
}

func (r *stubStaffRepo) ListModules(institutionID, staffID uint) ([]models.Module, error) {
	if _, err := r.GetByID(institutionID, staffID); err != nil {
		return nil, err
	}
	var out []models.Module
	for _, link := range r.modules[staffID] {
		if visibleTo(link.InstitutionID, institutionID) {
			out = append(out, models.Module{ID: link.ID, InstitutionID: link.InstitutionID})
		}
	}
	return out, nil
}

func (r *stubStaffRepo) ListStaffForModule(institutionID, moduleID uint) ([]models.Staff, error) {
	var out []models.Staff
	for _, s := range r.staff {
		if !visibleTo(s.InstitutionID, institutionID) {
			continue
		}
		for _, link := range r.modules[s.ID] {
			if link.ID == moduleID {
				out = append(out, *s)
				break
			}
		}
	}
	return out, nil
}

// --- Timetable ---------------------------------------------------------------

type stubTimetableRepo struct {
	items  map[uint]*models.Timetable
	nextID uint
	// classCourse maps class ID → course ID, standing in for the subquery the
	// real repository runs through the classes table.
	classCourse map[uint]uint
}

func newStubTimetableRepo() *stubTimetableRepo {
	return &stubTimetableRepo{
		items:       map[uint]*models.Timetable{},
		nextID:      1,
		classCourse: map[uint]uint{},
	}
}

// seed inserts a timetable entry directly, bypassing the tenant checks. An
// explicit non-zero ID is respected so tests can pin cross-tenant IDs.
func (r *stubTimetableRepo) seed(t *models.Timetable) *models.Timetable {
	if t.ID == 0 {
		t.ID = r.nextID
	}
	if t.ID >= r.nextID {
		r.nextID = t.ID + 1
	}
	cp := *t
	r.items[t.ID] = &cp
	return &cp
}

func (r *stubTimetableRepo) Create(institutionID uint, t *models.Timetable) error {
	if !repositories.IsPlatformScope(institutionID) {
		t.InstitutionID = institutionID
	}
	r.seed(t)
	return nil
}

func (r *stubTimetableRepo) GetByID(institutionID, id uint) (*models.Timetable, error) {
	t, ok := r.items[id]
	if !ok || !visibleTo(t.InstitutionID, institutionID) {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *t
	return &cp, nil
}

func (r *stubTimetableRepo) Update(institutionID uint, t *models.Timetable) error {
	existing, ok := r.items[t.ID]
	if !ok || !visibleTo(existing.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	cp := *t
	cp.InstitutionID = existing.InstitutionID
	r.items[t.ID] = &cp
	return nil
}

func (r *stubTimetableRepo) Delete(institutionID, id uint) error {
	t, ok := r.items[id]
	if !ok || !visibleTo(t.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *stubTimetableRepo) DeleteByClass(institutionID, classID uint) error {
	for id, t := range r.items {
		if t.ClassID == classID && visibleTo(t.InstitutionID, institutionID) {
			delete(r.items, id)
		}
	}
	return nil
}

func (r *stubTimetableRepo) ReplaceClassTimetable(institutionID, classID uint, entries []models.Timetable) ([]models.Timetable, error) {
	if err := r.DeleteByClass(institutionID, classID); err != nil {
		return nil, err
	}
	out := make([]models.Timetable, 0, len(entries))
	for i := range entries {
		entries[i].ID = 0
		entries[i].ClassID = classID
		if !repositories.IsPlatformScope(institutionID) {
			entries[i].InstitutionID = institutionID
		}
		saved := r.seed(&entries[i])
		out = append(out, *saved)
	}
	return out, nil
}

func (r *stubTimetableRepo) GetAll(institutionID uint, limit, offset int) ([]models.Timetable, error) {
	var out []models.Timetable
	for _, t := range r.items {
		if visibleTo(t.InstitutionID, institutionID) {
			out = append(out, *t)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubTimetableRepo) GetByClass(institutionID, classID uint) ([]models.Timetable, error) {
	var out []models.Timetable
	for _, t := range r.items {
		if t.ClassID == classID && visibleTo(t.InstitutionID, institutionID) {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (r *stubTimetableRepo) GetByStaff(institutionID, staffID uint) ([]models.Timetable, error) {
	var out []models.Timetable
	for _, t := range r.items {
		if t.StaffID == staffID && visibleTo(t.InstitutionID, institutionID) {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (r *stubTimetableRepo) GetByCourse(institutionID, courseID uint) ([]models.Timetable, error) {
	// The class lookup is the one the controller would do; here the stub keeps
	// its own class→course map so the subquery semantics are exercised.
	var out []models.Timetable
	for _, t := range r.items {
		if !visibleTo(t.InstitutionID, institutionID) {
			continue
		}
		if r.classCourse[t.ClassID] == courseID {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (r *stubTimetableRepo) GetByRoom(institutionID, roomID uint) ([]models.Timetable, error) {
	var out []models.Timetable
	for _, t := range r.items {
		if t.RoomID == roomID && visibleTo(t.InstitutionID, institutionID) {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (r *stubTimetableRepo) GetByDay(institutionID uint, day models.Weekday) ([]models.Timetable, error) {
	var out []models.Timetable
	for _, t := range r.items {
		if t.Day == day && visibleTo(t.InstitutionID, institutionID) {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (r *stubTimetableRepo) CheckConflicts(institutionID, classID, staffID, roomID uint, day models.Weekday, startTime, endTime string, excludeID uint) ([]models.Timetable, error) {
	var out []models.Timetable
	for _, t := range r.items {
		if !visibleTo(t.InstitutionID, institutionID) {
			continue
		}
		if t.Day != day {
			continue
		}
		if !(t.ClassID == classID || t.StaffID == staffID || t.RoomID == roomID) {
			continue
		}
		if excludeID > 0 && t.ID == excludeID {
			continue
		}
		if t.StartTime < endTime && startTime < t.EndTime {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (r *stubTimetableRepo) GetByDateRange(institutionID uint, startDate, endDate string) ([]models.Timetable, error) {
	return r.GetAll(institutionID, 0, 0)
}

func (r *stubTimetableRepo) DB() *gorm.DB { return nil }

// --- Module ------------------------------------------------------------------

type stubModuleRepo struct {
	items  map[uint]*models.Module
	nextID uint
	// staffByModule maps a module ID to the staff assigned to it.
	staffByModule map[uint][]models.Staff
}

func newStubModuleRepo() *stubModuleRepo {
	return &stubModuleRepo{
		items:         map[uint]*models.Module{},
		nextID:        1,
		staffByModule: map[uint][]models.Staff{},
	}
}

// seed inserts a module directly, bypassing the tenant checks. An explicit
// non-zero ID is respected.
func (r *stubModuleRepo) seed(m *models.Module) *models.Module {
	if m.ID == 0 {
		m.ID = r.nextID
	}
	if m.ID >= r.nextID {
		r.nextID = m.ID + 1
	}
	cp := *m
	r.items[m.ID] = &cp
	return &cp
}

func (r *stubModuleRepo) Create(institutionID uint, m *models.Module) error {
	if !repositories.IsPlatformScope(institutionID) {
		m.InstitutionID = institutionID
	}
	r.seed(m)
	return nil
}

func (r *stubModuleRepo) GetByID(institutionID, id uint) (*models.Module, error) {
	m, ok := r.items[id]
	if !ok || !visibleTo(m.InstitutionID, institutionID) {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *m
	return &cp, nil
}

func (r *stubModuleRepo) Update(institutionID uint, m *models.Module) error {
	existing, ok := r.items[m.ID]
	if !ok || !visibleTo(existing.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	cp := *m
	cp.InstitutionID = existing.InstitutionID
	r.items[m.ID] = &cp
	return nil
}

func (r *stubModuleRepo) Delete(institutionID, id uint) error {
	m, ok := r.items[id]
	if !ok || !visibleTo(m.InstitutionID, institutionID) {
		return gorm.ErrRecordNotFound
	}
	delete(r.items, id)
	return nil
}

func (r *stubModuleRepo) GetAll(institutionID uint, limit, offset int) ([]models.Module, error) {
	var out []models.Module
	for _, m := range r.items {
		if visibleTo(m.InstitutionID, institutionID) {
			out = append(out, *m)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubModuleRepo) GetByCourse(institutionID, courseID uint, limit, offset int) ([]models.Module, error) {
	var out []models.Module
	for _, m := range r.items {
		if visibleTo(m.InstitutionID, institutionID) && m.CourseID != nil && *m.CourseID == courseID {
			out = append(out, *m)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubModuleRepo) GetByType(institutionID uint, moduleType models.ModuleType, limit, offset int) ([]models.Module, error) {
	var out []models.Module
	for _, m := range r.items {
		if visibleTo(m.InstitutionID, institutionID) && m.Type == moduleType {
			out = append(out, *m)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubModuleRepo) GetGeneralModules(institutionID uint, limit, offset int) ([]models.Module, error) {
	var out []models.Module
	for _, m := range r.items {
		if visibleTo(m.InstitutionID, institutionID) && m.CourseID == nil {
			out = append(out, *m)
		}
	}
	return scopedPage(out, limit, offset), nil
}

func (r *stubModuleRepo) GetWithStaff(institutionID, id uint) (*models.Module, error) {
	m, err := r.GetByID(institutionID, id)
	if err != nil {
		return nil, err
	}
	// Only staff from the caller's institution may be attached.
	var staff []models.Staff
	for _, s := range r.staffByModule[id] {
		if visibleTo(s.InstitutionID, institutionID) {
			staff = append(staff, s)
		}
	}
	m.Staff = staff
	return m, nil
}
