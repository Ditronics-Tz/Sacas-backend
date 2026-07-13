package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"go_boilerplate/internal/config"
	"go_boilerplate/pkg/logger"
)

// ErrSolverUnreachable is returned when HTTP to the solver fails (safe for greedy fallback).
var ErrSolverUnreachable = errors.New("solver unreachable")

// SolverClient calls the Python OR-Tools microservice.
type SolverClient struct {
	baseURL    string
	httpClient *http.Client
	fallback   bool
}

func NewSolverClient() *SolverClient {
	timeoutSec, _ := strconv.Atoi(config.GetEnv("SOLVER_TIMEOUT_SECONDS", "35"))
	if timeoutSec <= 0 {
		timeoutSec = 35
	}
	// Empty SOLVER_URL disables the solver (no default localhost).
	url := config.GetEnv("SOLVER_URL", "")
	// When solver is configured, default SOLVER_FALLBACK=false so infeasibility is not masked.
	fallbackDefault := "false"
	if url == "" {
		fallbackDefault = "true" // only greedy path exists
	}
	return &SolverClient{
		baseURL: url,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
		fallback: config.GetEnv("SOLVER_FALLBACK", fallbackDefault) == "true",
	}
}

// Enabled reports whether a solver URL is explicitly configured.
func (c *SolverClient) Enabled() bool {
	return c != nil && c.baseURL != ""
}

// AllowFallback reports whether transport-level failures may use greedy.
func (c *SolverClient) AllowFallback() bool {
	return c != nil && c.fallback
}

// SolverRequest is the JSON body sent to the solver service.
type SolverRequest struct {
	ClassID       uint               `json:"class_id"`
	TimeBudgetSec float64            `json:"time_budget_sec"`
	Persist       bool               `json:"persist"`
	WorkingDays   []string           `json:"working_days"`
	TimeSlots     []string           `json:"time_slots"`
	Class         SolverClass        `json:"class"`
	Modules       []SolverModule     `json:"modules"`
	Subjects      []SolverSubject    `json:"subjects"`
	Staff         []SolverStaff      `json:"staff"`
	Rooms         []SolverRoom       `json:"rooms"`
	PinnedEntries []SolverAssignment `json:"pinned_entries,omitempty"`
	SoftWeights   map[string]float64 `json:"soft_weights,omitempty"`
}

type SolverClass struct {
	ID               uint `json:"id"`
	CourseID         uint `json:"course_id"`
	NumberOfStudents int  `json:"number_of_students"`
}

type SolverModule struct {
	ID          uint  `json:"id"`
	CreditHours int   `json:"credit_hours"`
	RequiresLab bool  `json:"requires_lab"`
	CourseID    *uint `json:"course_id,omitempty"`
}

type SolverSubject struct {
	ID          uint `json:"id"`
	CreditHours int  `json:"credit_hours"`
}

type SolverStaff struct {
	ID              uint     `json:"id"`
	MaxHours        int      `json:"max_hours"`
	ModuleIDs       []uint   `json:"module_ids"`
	UnavailableDays []string `json:"unavailable_days"`
	PreferredStart  string   `json:"preferred_start,omitempty"`
}

type SolverRoom struct {
	ID        uint   `json:"id"`
	Capacity  int    `json:"capacity"`
	Lab       bool   `json:"lab"`
	Sticky    bool   `json:"sticky"`
	CourseIDs []uint `json:"course_ids,omitempty"`
}

type SolverAssignment struct {
	ClassID   uint   `json:"class_id"`
	ModuleID  *uint  `json:"module_id,omitempty"`
	SubjectID *uint  `json:"subject_id,omitempty"`
	StaffID   uint   `json:"staff_id"`
	RoomID    uint   `json:"room_id"`
	Day       string `json:"day"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// SolverResponse is returned by the solver service.
type SolverResponse struct {
	Status                  string             `json:"status"`
	Assignments             []SolverAssignment `json:"assignments"`
	ViolatedSoftConstraints []string           `json:"violated_soft_constraints"`
	UnsatReasons            []string           `json:"unsat_reasons"`
	Message                 string             `json:"message,omitempty"`
}

// Solve posts a problem instance to the solver microservice.
func (c *SolverClient) Solve(req SolverRequest) (*SolverResponse, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("%w: solver not configured", ErrSolverUnreachable)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := c.baseURL + "/solve"
	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	logger.Info("Calling solver at %s for class %d", url, req.ClassID)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSolverUnreachable, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", ErrSolverUnreachable, err)
	}

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: status %d: %s", ErrSolverUnreachable, resp.StatusCode, string(raw))
	}

	var out SolverResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("%w: invalid response: %v", ErrSolverUnreachable, err)
	}
	return &out, nil
}
