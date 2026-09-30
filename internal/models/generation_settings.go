package models

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

// GenerationSettings holds the timetable generation engine options
// (solver-tunable knobs).
//
// There are two kinds of row:
//
//	Platform default (the single row with ID = SingletonID, InstitutionID = 0)
//	    — the baseline every institution inherits.
//	Per-institution override (InstitutionID > 0)
//	    — an optional row an institution's admin may set. Any field it
//	      overrides wins over the platform default; anything it leaves unset
//	      falls back to the platform value.
//
// The platform row keeps the fixed primary key ID = 1 so existing deployments
// and the singleton API shape are unaffected.
type GenerationSettings struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// InstitutionID is the owning institution. Zero means the platform-wide
	// default row, which is not itself an institution.
	InstitutionID uint `gorm:"not null;default:0;index" json:"institution_id"`

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
	// SingletonID is the fixed primary key of the platform default row.
	SingletonID uint = 1

	// PlatformScope is the InstitutionID of the platform default row. It is
	// never a real institution ID — real institutions start at 1.
	PlatformScope uint = 0

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

// DefaultGenerationSettings returns a populated platform default with the same
// defaults hardcoded in the service today (30s budget, no soft weights).
func DefaultGenerationSettings() *GenerationSettings {
	return &GenerationSettings{
		ID:            SingletonID,
		InstitutionID: PlatformScope,
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
