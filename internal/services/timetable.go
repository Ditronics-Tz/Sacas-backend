package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"go_boilerplate/pkg/logger"
)

// ErrInfeasible is returned when hard constraints cannot be satisfied.
var ErrInfeasible = errors.New("timetable infeasible")

type TimetableService struct {
	timetableRepo repositories.TimetableRepository
	staffRepo     repositories.StaffRepository
	classRepo     repositories.ClassRepository
	moduleRepo    repositories.ModuleRepository
	roomRepo      repositories.RoomRepository
	subjectRepo   repositories.SubjectRepository
	solver        *SolverClient
}

func NewTimetableService(
	timetableRepo repositories.TimetableRepository,
	staffRepo repositories.StaffRepository,
	classRepo repositories.ClassRepository,
	moduleRepo repositories.ModuleRepository,
	roomRepo repositories.RoomRepository,
	subjectRepo repositories.SubjectRepository,
	solver *SolverClient,
) *TimetableService {
	return &TimetableService{
		timetableRepo: timetableRepo,
		staffRepo:     staffRepo,
		classRepo:     classRepo,
		moduleRepo:    moduleRepo,
		roomRepo:      roomRepo,
		subjectRepo:   subjectRepo,
		solver:        solver,
	}
}

// GenerateResult is returned by generate/preview paths.
type GenerateResult struct {
	Timetables              []models.Timetable
	Status                  string // optimal | feasible | partial | infeasible
	ViolatedSoftConstraints []string
	UnsatReasons            []string
	Engine                  string // "solver" | "greedy"
	RequiredSessions        int
	ScheduledSessions       int
}

// GenerateTimetable generates and persists a timetable for a class (replace-on-write).
func (s *TimetableService) GenerateTimetable(classID uint) (*GenerateResult, error) {
	return s.generate(classID, true)
}

// PreviewTimetable runs the solver (or greedy) without persisting.
func (s *TimetableService) PreviewTimetable(classID uint) (*GenerateResult, error) {
	return s.generate(classID, false)
}

func (s *TimetableService) generate(classID uint, persist bool) (*GenerateResult, error) {
	if s.solver != nil && s.solver.Enabled() {
		result, err := s.generateWithSolver(classID, persist)
		if err == nil {
			return result, nil
		}
		// Never fall back on model infeasibility — only transport/unreachable.
		if errors.Is(err, ErrInfeasible) {
			return result, err
		}
		if errors.Is(err, ErrSolverUnreachable) && s.solver.AllowFallback() {
			logger.Warn("Solver unreachable for class %d: %v — greedy fallback", classID, err)
			return s.generateGreedy(classID, persist)
		}
		// Other solver errors without fallback
		if result != nil {
			return result, err
		}
		return nil, err
	}

	return s.generateGreedy(classID, persist)
}

func (s *TimetableService) generateWithSolver(classID uint, persist bool) (*GenerateResult, error) {
	req, err := s.buildSolverRequest(classID, persist)
	if err != nil {
		return nil, err
	}

	resp, err := s.solver.Solve(*req)
	if err != nil {
		return nil, err
	}

	if resp.Status == "infeasible" || resp.Status == "error" {
		return &GenerateResult{
			Status:       "infeasible",
			UnsatReasons: resp.UnsatReasons,
			Engine:       "solver",
		}, fmt.Errorf("%w: %s", ErrInfeasible, strings.Join(resp.UnsatReasons, "; "))
	}

	var pending []models.Timetable
	for _, a := range resp.Assignments {
		tt := models.Timetable{
			ClassID:   a.ClassID,
			ModuleID:  a.ModuleID,
			SubjectID: a.SubjectID,
			StaffID:   a.StaffID,
			RoomID:    a.RoomID,
			Day:       models.Weekday(a.Day),
			StartTime: a.StartTime,
			EndTime:   a.EndTime,
		}
		pending = append(pending, tt)
	}

	var timetables []models.Timetable
	if persist {
		timetables, err = s.timetableRepo.ReplaceClassTimetable(classID, pending)
		if err != nil {
			return nil, fmt.Errorf("failed to persist assignments: %w", err)
		}
	} else {
		timetables = pending
	}

	return &GenerateResult{
		Timetables:              timetables,
		Status:                  resp.Status,
		ViolatedSoftConstraints: resp.ViolatedSoftConstraints,
		Engine:                  "solver",
		RequiredSessions:        len(pending),
		ScheduledSessions:       len(timetables),
	}, nil
}

func (s *TimetableService) buildSolverRequest(classID uint, persist bool) (*SolverRequest, error) {
	class, err := s.classRepo.GetByID(classID)
	if err != nil {
		return nil, fmt.Errorf("failed to get class: %w", err)
	}

	// Curriculum source of truth:
	// - course modules (core/elective for this class's course)
	// - general_subject modules (type general_subject / null course_id)
	// Subjects table is NOT double-scheduled when modules of type general_subject exist;
	// only use Subject rows when no general_subject modules are present (legacy).
	modules, err := s.moduleRepo.GetByCourse(class.CourseID, 200, 0)
	if err != nil {
		return nil, err
	}
	generalMods, _ := s.moduleRepo.GetGeneralModules(200, 0)
	modules = append(modules, generalMods...)

	var subjects []models.Subject
	if len(generalMods) == 0 {
		subjects, err = s.subjectRepo.GetAll(200, 0)
		if err != nil {
			return nil, err
		}
	}

	staffList, err := s.staffRepo.GetAll(500, 0)
	if err != nil {
		return nil, err
	}

	rooms, err := s.roomRepo.GetAll(500, 0)
	if err != nil {
		return nil, err
	}

	req := &SolverRequest{
		ClassID:       classID,
		TimeBudgetSec: 30,
		Persist:       persist,
		WorkingDays:   []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		TimeSlots:     []string{"08:00", "09:00", "10:00", "11:00", "13:00", "14:00", "15:00", "16:00"},
		Class: SolverClass{
			ID:               class.ID,
			CourseID:         class.CourseID,
			NumberOfStudents: class.NumberOfStudents,
		},
	}

	for _, m := range modules {
		req.Modules = append(req.Modules, SolverModule{
			ID:          m.ID,
			CreditHours: m.CreditHours,
			RequiresLab: m.RequiresLab,
			CourseID:    m.CourseID,
		})
	}
	for _, sub := range subjects {
		req.Subjects = append(req.Subjects, SolverSubject{
			ID:          sub.ID,
			CreditHours: sub.CreditHours,
		})
	}
	for _, st := range staffList {
		withMods, _ := s.staffRepo.GetWithModules(st.ID)
		var modIDs []uint
		if withMods != nil {
			for _, m := range withMods.Modules {
				modIDs = append(modIDs, m.ID)
			}
		}
		unavail, preferred := parseStaffPrefs(st.Preferences)
		req.Staff = append(req.Staff, SolverStaff{
			ID:              st.ID,
			MaxHours:        st.MaxHours,
			ModuleIDs:       modIDs,
			UnavailableDays: unavail,
			PreferredStart:  preferred,
		})
	}
	for _, r := range rooms {
		lab := roomHasLab(r.Features)
		courseIDs := roomAllowedCourses(r.AllowedCourses)
		req.Rooms = append(req.Rooms, SolverRoom{
			ID:        r.ID,
			Capacity:  r.Capacity,
			Lab:       lab,
			Sticky:    r.Sticky,
			CourseIDs: courseIDs,
		})
	}

	return req, nil
}

func parseStaffPrefs(raw []byte) (unavailable []string, preferredStart string) {
	if len(raw) == 0 {
		return nil, ""
	}
	var p models.StaffPreferences
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, ""
	}
	unavailable = p.UnavailableDays
	if len(unavailable) == 0 {
		unavailable = p.DayOffs
	}
	preferredStart = p.PreferredStart
	return
}

func roomHasLab(features []byte) bool {
	if len(features) == 0 {
		return false
	}
	var f models.RoomFeatures
	if err := json.Unmarshal(features, &f); err != nil {
		return false
	}
	return f.Lab
}

func roomAllowedCourses(raw []byte) []uint {
	if len(raw) == 0 {
		return nil
	}
	var ac models.AllowedCourses
	if err := json.Unmarshal(raw, &ac); err != nil {
		return nil
	}
	return ac.CourseIDs
}

// generateGreedy is the legacy first-fit engine (used when solver is off/unreachable with fallback).
func (s *TimetableService) generateGreedy(classID uint, persist bool) (*GenerateResult, error) {
	class, err := s.classRepo.GetByID(classID)
	if err != nil {
		return nil, fmt.Errorf("failed to get class: %w", err)
	}

	modules, err := s.moduleRepo.GetByCourse(class.CourseID, 100, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get modules: %w", err)
	}
	generalMods, _ := s.moduleRepo.GetGeneralModules(100, 0)
	modules = append(modules, generalMods...)

	// Align with solver: subjects table only if no general_subject modules
	var subjects []models.Subject
	if len(generalMods) == 0 {
		subjects, err = s.subjectRepo.GetAll(100, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to get subjects: %w", err)
		}
	}

	workingDays := []models.Weekday{models.Monday, models.Tuesday, models.Wednesday, models.Thursday, models.Friday}
	timeSlots := []string{"08:00", "09:00", "10:00", "11:00", "13:00", "14:00", "15:00", "16:00"}

	// In-memory pending slots for conflict checks during generation (preview path
	// and pre-persist validation). Also track provisional hours per staff.
	var pending []models.Timetable
	staffHours := map[uint]int{}
	var unsat []string
	required := 0

	// Seed staff hours from DB
	allStaff, _ := s.staffRepo.GetAll(500, 0)
	for _, st := range allStaff {
		existing, _ := s.timetableRepo.GetByStaff(st.ID)
		// exclude current class if we will replace
		count := 0
		for _, e := range existing {
			if e.ClassID != classID {
				count++
			}
		}
		staffHours[st.ID] = count
	}

	trySchedule := func(moduleID, subjectID *uint, students int, requiresLab bool, label string) {
		required++
		tt, err := s.scheduleSessionInMemory(
			classID, moduleID, subjectID, workingDays, timeSlots,
			students, requiresLab, pending, staffHours, classID,
		)
		if err != nil {
			unsat = append(unsat, fmt.Sprintf("Could not schedule %s: %v", label, err))
			return
		}
		pending = append(pending, *tt)
		staffHours[tt.StaffID]++
	}

	for _, module := range modules {
		for i := 0; i < module.CreditHours; i++ {
			mid := module.ID
			trySchedule(&mid, nil, class.NumberOfStudents, module.RequiresLab,
				fmt.Sprintf("module %d session %d", module.ID, i+1))
		}
	}
	for _, subject := range subjects {
		for i := 0; i < subject.CreditHours; i++ {
			sid := subject.ID
			trySchedule(nil, &sid, class.NumberOfStudents, false,
				fmt.Sprintf("subject %d session %d", subject.ID, i+1))
		}
	}

	if len(unsat) > 0 {
		return &GenerateResult{
			Timetables:        nil,
			Status:            "infeasible",
			UnsatReasons:      unsat,
			Engine:            "greedy",
			RequiredSessions:  required,
			ScheduledSessions: len(pending),
		}, fmt.Errorf("%w: scheduled %d/%d sessions", ErrInfeasible, len(pending), required)
	}

	var timetables []models.Timetable
	if persist {
		timetables, err = s.timetableRepo.ReplaceClassTimetable(classID, pending)
		if err != nil {
			return nil, fmt.Errorf("failed to persist: %w", err)
		}
	} else {
		timetables = pending
	}

	return &GenerateResult{
		Timetables:        timetables,
		Status:            "feasible",
		Engine:            "greedy",
		RequiredSessions:  required,
		ScheduledSessions: len(timetables),
	}, nil
}

func (s *TimetableService) scheduleSessionInMemory(
	classID uint,
	moduleID, subjectID *uint,
	workingDays []models.Weekday,
	timeSlots []string,
	studentCount int,
	requiresLab bool,
	pending []models.Timetable,
	staffHours map[uint]int,
	regenClassID uint,
) (*models.Timetable, error) {
	for _, day := range workingDays {
		for _, startTime := range timeSlots {
			endTime := s.getEndTime(startTime)

			var staffID uint
			var err error
			if moduleID != nil {
				staffID, err = s.findAvailableStaffForModule(*moduleID, day, startTime, endTime, pending, staffHours, regenClassID)
			} else {
				staffID, err = s.findAvailableStaffForSubject(day, startTime, endTime, pending, staffHours, regenClassID)
			}
			if err != nil {
				continue
			}

			roomID, err := s.findAvailableRoom(day, startTime, endTime, studentCount, requiresLab, pending, regenClassID)
			if err != nil {
				continue
			}

			if s.slotConflicts(classID, staffID, roomID, day, startTime, endTime, pending, regenClassID) {
				continue
			}

			return &models.Timetable{
				ClassID:   classID,
				ModuleID:  moduleID,
				SubjectID: subjectID,
				StaffID:   staffID,
				RoomID:    roomID,
				Day:       day,
				StartTime: startTime,
				EndTime:   endTime,
			}, nil
		}
	}
	return nil, errors.New("no feasible slot")
}

func (s *TimetableService) slotConflicts(classID, staffID, roomID uint, day models.Weekday, start, end string, pending []models.Timetable, regenClassID uint) bool {
	conflicts, err := s.timetableRepo.CheckConflicts(classID, staffID, roomID, day, start, end, 0)
	if err == nil {
		for _, c := range conflicts {
			if regenClassID > 0 && c.ClassID == regenClassID {
				continue
			}
			return true
		}
	}
	for _, p := range pending {
		if p.Day != day {
			continue
		}
		if !s.hasTimeOverlap(p.StartTime, p.EndTime, start, end) {
			continue
		}
		if p.ClassID == classID || p.StaffID == staffID || p.RoomID == roomID {
			return true
		}
	}
	return false
}

func (s *TimetableService) findAvailableStaffForModule(
	moduleID uint,
	day models.Weekday,
	startTime, endTime string,
	pending []models.Timetable,
	staffHours map[uint]int,
	regenClassID uint,
) (uint, error) {
	module, err := s.moduleRepo.GetWithStaff(moduleID)
	if err != nil {
		return 0, err
	}
	if len(module.Staff) == 0 {
		return 0, fmt.Errorf("module %d has no allocated staff (assign staff↔module first)", moduleID)
	}
	for _, staff := range module.Staff {
		if s.isStaffAvailable(staff.ID, day, startTime, endTime, pending, staffHours, staff.MaxHours, regenClassID) {
			return staff.ID, nil
		}
	}
	return 0, errors.New("no available allocated staff for module")
}

func (s *TimetableService) findAvailableStaffForSubject(
	day models.Weekday,
	startTime, endTime string,
	pending []models.Timetable,
	staffHours map[uint]int,
	regenClassID uint,
) (uint, error) {
	staff, err := s.staffRepo.GetAll(100, 0)
	if err != nil {
		return 0, err
	}
	for _, staffMember := range staff {
		if s.isStaffAvailable(staffMember.ID, day, startTime, endTime, pending, staffHours, staffMember.MaxHours, regenClassID) {
			return staffMember.ID, nil
		}
	}
	return 0, errors.New("no available staff")
}

func (s *TimetableService) isStaffAvailable(
	staffID uint,
	day models.Weekday,
	startTime, endTime string,
	pending []models.Timetable,
	staffHours map[uint]int,
	maxHours int,
	regenClassID uint,
) bool {
	if maxHours <= 0 {
		maxHours = 40
	}
	if staffHours[staffID] >= maxHours {
		return false
	}

	staff, err := s.staffRepo.GetByID(staffID)
	if err == nil && staff != nil {
		unavail, _ := parseStaffPrefs(staff.Preferences)
		dayStr := string(day)
		for _, d := range unavail {
			if strings.EqualFold(d, dayStr) {
				return false
			}
		}
	}

	existing, err := s.timetableRepo.GetByStaff(staffID)
	if err != nil {
		return false
	}
	for _, entry := range existing {
		if regenClassID > 0 && entry.ClassID == regenClassID {
			continue
		}
		if entry.Day == day && s.hasTimeOverlap(entry.StartTime, entry.EndTime, startTime, endTime) {
			return false
		}
	}
	for _, p := range pending {
		if p.StaffID == staffID && p.Day == day && s.hasTimeOverlap(p.StartTime, p.EndTime, startTime, endTime) {
			return false
		}
	}
	return true
}

func (s *TimetableService) findAvailableRoom(
	day models.Weekday,
	startTime, endTime string,
	studentCount int,
	requiresLab bool,
	pending []models.Timetable,
	regenClassID uint,
) (uint, error) {
	var rooms []models.Room
	var err error

	if requiresLab {
		rooms, err = s.roomRepo.GetLabRooms()
		if err != nil {
			return 0, err
		}
		if len(rooms) == 0 {
			return 0, errors.New("no lab rooms available for lab-required session")
		}
	} else {
		rooms, err = s.roomRepo.GetByCapacity(studentCount)
		if err != nil {
			return 0, err
		}
	}

	for _, room := range rooms {
		if room.Capacity < studentCount {
			continue
		}
		if requiresLab && !roomHasLab(room.Features) {
			continue
		}

		busy := false
		conflicts, err := s.timetableRepo.GetByRoom(room.ID)
		if err != nil {
			continue
		}
		for _, conflict := range conflicts {
			if regenClassID > 0 && conflict.ClassID == regenClassID {
				continue
			}
			if conflict.Day == day && s.hasTimeOverlap(conflict.StartTime, conflict.EndTime, startTime, endTime) {
				busy = true
				break
			}
		}
		if busy {
			continue
		}
		for _, p := range pending {
			if p.RoomID == room.ID && p.Day == day && s.hasTimeOverlap(p.StartTime, p.EndTime, startTime, endTime) {
				busy = true
				break
			}
		}
		if !busy {
			return room.ID, nil
		}
	}

	if requiresLab {
		return 0, errors.New("no free lab room with sufficient capacity")
	}
	return 0, errors.New("no available room")
}

func (s *TimetableService) hasTimeOverlap(start1, end1, start2, end2 string) bool {
	return start1 < end2 && start2 < end1
}

func (s *TimetableService) getEndTime(startTime string) string {
	parts := strings.Split(startTime, ":")
	if len(parts) != 2 {
		return startTime
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return startTime
	}
	return fmt.Sprintf("%02d:%s", hour+1, parts[1])
}

// ValidateTimeSlot validates a timetable entry for conflicts.
// Pass excludeID on updates to ignore the row being updated.
func (s *TimetableService) ValidateTimeSlot(timetable *models.Timetable, excludeID uint) error {
	conflicts, err := s.timetableRepo.CheckConflicts(
		timetable.ClassID,
		timetable.StaffID,
		timetable.RoomID,
		timetable.Day,
		timetable.StartTime,
		timetable.EndTime,
		excludeID,
	)
	if err != nil {
		return fmt.Errorf("failed to check conflicts: %w", err)
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("scheduling conflict: class, staff, or room already booked in this slot")
	}
	return nil
}
