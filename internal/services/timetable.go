package services

import (
	"errors"
	"fmt"
	"go_boilerplate/internal/models"
	"go_boilerplate/internal/repositories"
	"strconv"
	"strings"
)

type TimetableService struct {
	timetableRepo repositories.TimetableRepository
	staffRepo     repositories.StaffRepository
	classRepo     repositories.ClassRepository
	moduleRepo    repositories.ModuleRepository
	roomRepo      repositories.RoomRepository
	subjectRepo   repositories.SubjectRepository
}

func NewTimetableService(
	timetableRepo repositories.TimetableRepository,
	staffRepo repositories.StaffRepository,
	classRepo repositories.ClassRepository,
	moduleRepo repositories.ModuleRepository,
	roomRepo repositories.RoomRepository,
	subjectRepo repositories.SubjectRepository,
) *TimetableService {
	return &TimetableService{
		timetableRepo: timetableRepo,
		staffRepo:     staffRepo,
		classRepo:     classRepo,
		moduleRepo:    moduleRepo,
		roomRepo:      roomRepo,
		subjectRepo:   subjectRepo,
	}
}

// GenerateTimetable generates a complete timetable for a class
func (s *TimetableService) GenerateTimetable(classID uint) ([]models.Timetable, error) {
	// Get class details
	class, err := s.classRepo.GetByID(classID)
	if err != nil {
		return nil, fmt.Errorf("failed to get class: %w", err)
	}

	// Get modules for the course
	modules, err := s.moduleRepo.GetByCourse(class.CourseID, 100, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get modules: %w", err)
	}

	// Get general subjects
	generalSubjects, err := s.subjectRepo.GetAll(100, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to get subjects: %w", err)
	}

	var timetables []models.Timetable
	workingDays := []models.Weekday{models.Monday, models.Tuesday, models.Wednesday, models.Thursday, models.Friday}
	timeSlots := []string{"08:00", "09:00", "10:00", "11:00", "13:00", "14:00", "15:00", "16:00"}

	// Generate timetable for course modules
	for _, module := range modules {
		for i := 0; i < module.CreditHours; i++ {
			timetable, err := s.scheduleSession(classID, &module.ID, nil, workingDays, timeSlots, class.NumberOfStudents, module.RequiresLab)
			if err != nil {
				continue // Skip if can't schedule
			}
			timetables = append(timetables, *timetable)
		}
	}

	// Generate timetable for general subjects
	for _, subject := range generalSubjects {
		for i := 0; i < subject.CreditHours; i++ {
			timetable, err := s.scheduleSession(classID, nil, &subject.ID, workingDays, timeSlots, class.NumberOfStudents, false)
			if err != nil {
				continue // Skip if can't schedule
			}
			timetables = append(timetables, *timetable)
		}
	}

	return timetables, nil
}

// scheduleSession schedules a single session
func (s *TimetableService) scheduleSession(classID uint, moduleID, subjectID *uint, workingDays []models.Weekday, timeSlots []string, studentCount int, requiresLab bool) (*models.Timetable, error) {
	for _, day := range workingDays {
		for _, startTime := range timeSlots {
			endTime := s.getEndTime(startTime)

			// Find available staff
			var staffID uint
			var err error

			if moduleID != nil {
				staffID, err = s.findAvailableStaffForModule(*moduleID, day, startTime, endTime)
			} else {
				staffID, err = s.findAvailableStaffForSubject(day, startTime, endTime)
			}

			if err != nil {
				continue
			}

			// Find available room
			roomID, err := s.findAvailableRoom(day, startTime, endTime, studentCount, requiresLab)
			if err != nil {
				continue
			}

			// Check for conflicts
			conflicts, err := s.timetableRepo.CheckConflicts(classID, staffID, roomID, day, startTime, endTime)
			if err != nil || len(conflicts) > 0 {
				continue
			}

			// Create timetable entry
			timetable := &models.Timetable{
				ClassID:   classID,
				ModuleID:  moduleID,
				SubjectID: subjectID,
				StaffID:   staffID,
				RoomID:    roomID,
				Day:       day,
				StartTime: startTime,
				EndTime:   endTime,
			}

			err = s.timetableRepo.Create(timetable)
			if err != nil {
				continue
			}

			return timetable, nil
		}
	}

	return nil, errors.New("unable to schedule session")
}

// findAvailableStaffForModule finds staff qualified for a specific module
func (s *TimetableService) findAvailableStaffForModule(moduleID uint, day models.Weekday, startTime, endTime string) (uint, error) {
	module, err := s.moduleRepo.GetWithStaff(moduleID)
	if err != nil {
		return 0, err
	}

	for _, staff := range module.Staff {
		if s.isStaffAvailable(staff.ID, day, startTime, endTime) {
			return staff.ID, nil
		}
	}

	return 0, errors.New("no available staff for module")
}

// findAvailableStaffForSubject finds staff available for general subjects
func (s *TimetableService) findAvailableStaffForSubject(day models.Weekday, startTime, endTime string) (uint, error) {
	staff, err := s.staffRepo.GetAll(100, 0)
	if err != nil {
		return 0, err
	}

	for _, staffMember := range staff {
		if s.isStaffAvailable(staffMember.ID, day, startTime, endTime) {
			return staffMember.ID, nil
		}
	}

	return 0, errors.New("no available staff")
}

// isStaffAvailable checks if staff is available at the given time
func (s *TimetableService) isStaffAvailable(staffID uint, day models.Weekday, startTime, endTime string) bool {
	// Check existing timetable conflicts
	existing, err := s.timetableRepo.GetByStaff(staffID)
	if err != nil {
		return false
	}

	for _, entry := range existing {
		if entry.Day == day && s.hasTimeOverlap(entry.StartTime, entry.EndTime, startTime, endTime) {
			return false
		}
	}

	// TODO: Check staff preferences from JSON field
	// This would involve parsing the preferences JSON and checking day-offs, unavailable slots, etc.

	return true
}

// findAvailableRoom finds an available room with sufficient capacity
func (s *TimetableService) findAvailableRoom(day models.Weekday, startTime, endTime string, studentCount int, requiresLab bool) (uint, error) {
	var rooms []models.Room
	var err error

	if requiresLab {
		rooms, err = s.roomRepo.GetLabRooms()
	} else {
		rooms, err = s.roomRepo.GetByCapacity(studentCount)
	}

	if err != nil {
		return 0, err
	}

	for _, room := range rooms {
		if room.Capacity >= studentCount {
			// Check if room is available
			conflicts, err := s.timetableRepo.GetByRoom(room.ID)
			if err != nil {
				continue
			}

			available := true
			for _, conflict := range conflicts {
				if conflict.Day == day && s.hasTimeOverlap(conflict.StartTime, conflict.EndTime, startTime, endTime) {
					available = false
					break
				}
			}

			if available {
				return room.ID, nil
			}
		}
	}

	return 0, errors.New("no available room")
}

// hasTimeOverlap checks if two time ranges overlap
func (s *TimetableService) hasTimeOverlap(start1, end1, start2, end2 string) bool {
	return start1 < end2 && start2 < end1
}

// getEndTime calculates end time based on start time (assumes 1-hour sessions)
func (s *TimetableService) getEndTime(startTime string) string {
	parts := strings.Split(startTime, ":")
	if len(parts) != 2 {
		return startTime
	}

	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return startTime
	}

	endHour := hour + 1
	return fmt.Sprintf("%02d:%s", endHour, parts[1])
}

// ValidateTimeSlot validates a timetable entry for conflicts
func (s *TimetableService) ValidateTimeSlot(timetable *models.Timetable) error {
	conflicts, err := s.timetableRepo.CheckConflicts(
		timetable.ClassID,
		timetable.StaffID,
		timetable.RoomID,
		timetable.Day,
		timetable.StartTime,
		timetable.EndTime,
	)

	if err != nil {
		return fmt.Errorf("failed to check conflicts: %w", err)
	}

	if len(conflicts) > 0 {
		return errors.New("scheduling conflict detected")
	}

	return nil
}