package models

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

// GenerationSettings is the system-wide singleton for timetable generation
// engine options (solver-tunable knobs). It is deliberately a SINGLE-ROW
// table (fixed primary key ID = 1): the ticket scopes these settings to the
// admin UI globally, with no per-course/per-class dimension mentioned.
// Revisit if per-entity scoping is ever required.
type GenerationSettings struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// TimeBudgetSec bounds the solver's wall-clock budget per generation run.
	// Validated server-side: > 0 and <= MaxTimeBudgetSec so a stray client
	// value cannot lock the solver for an hour.
	TimeBudgetSec float64 `gorm:"default:30" json:"time_budget_sec"`

	// SoftWeights holds named soft-constraint weights sent to the solver as
	// soft_weights (JSONB). Keys are validated server-side against
	// AllowedSoftWeightKeys so admins cannot inject keys the solver will
	// silently ignore.
	SoftWeights datatypes.JSON `json:"soft_weights"`
}

const (
	// SingletonID is the fixed primary key of the single settings row.
	SingletonID uint = 1

	// DefaultTimeBudgetSec is the value hardcoded in buildSolverRequest today.
	DefaultTimeBudgetSec = 30.0

	// MaxTimeBudgetSec caps the configured budget (sanity upper bound).
	MaxTimeBudgetSec = 300.0
)

// AllowedSoftWeightKeys is the explicit allow-list of soft-constraint weight
// names the solver understands (it currently has exactly two hardcoded soft
// objectives: prefer staff preferred_start, penalize >2 sessions/day).
// Reject anything else so unknown keys cannot silently accumulate.
var AllowedSoftWeightKeys = []string{
	"preferred_start_weight",
	"session_spread_weight",
}

// DefaultGenerationSettings returns a populated singleton with the same
// defaults hardcoded in the service today (30s budget, no soft weights).
func DefaultGenerationSettings() *GenerationSettings {
	return &GenerationSettings{
		ID:            SingletonID,
		TimeBudgetSec: DefaultTimeBudgetSec,
		SoftWeights:   datatypes.JSON(`{}`),
	}
}

// SoftWeightsMap decodes SoftWeights into a map, returning an empty map when
// unset or invalid rather than failing (weights are advisory).
func (g *GenerationSettings) SoftWeightsMap() map[string]float64 {
	out := map[string]float64{}
	if len(g.SoftWeights) == 0 {
		return out
	}
	_ = json.Unmarshal(g.SoftWeights, &out)
	return out
}
