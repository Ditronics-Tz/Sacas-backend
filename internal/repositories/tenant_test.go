package repositories

import (
	"strings"
	"testing"

	"go_boilerplate/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// dryRunDB builds a GORM handle in DryRun mode: it builds the SQL but never
// opens a connection, so the scope's query shape can be asserted in a unit
// test without a live PostgreSQL instance.
func dryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{
		DriverName:       "pgx",
		DSN:              "host=localhost user=nobody dbname=nothing sslmode=disable",
		WithoutReturning: true,
		Conn:             nil,
	}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		// A missing pgx driver is not a failure of the logic under test; fall
		// back to skipping rather than reporting a false negative.
		t.Skipf("postgres driver unavailable: %v", err)
	}
	return db
}

func TestIsPlatformScope(t *testing.T) {
	if !IsPlatformScope(0) {
		t.Error("0 must mean platform scope (all institutions)")
	}
	if IsPlatformScope(1) {
		t.Error("1 is a real institution and must not be platform scope")
	}
}

func TestInTenant(t *testing.T) {
	cases := []struct {
		name          string
		record, scope uint
		want          bool
	}{
		{"same tenant", 5, 5, true},
		{"different tenant", 5, 6, false},
		{"platform sees all", 5, 0, true},
		{"tenant cannot see platform record", 0, 5, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InTenant(tc.record, tc.scope); got != tc.want {
				t.Errorf("InTenant(%d, %d) = %v, want %v", tc.record, tc.scope, got, tc.want)
			}
		})
	}
}

func TestTenantScope_AddsPredicateForTenant(t *testing.T) {
	db := dryRunDB(t)

	stmt := TenantScope(db.Model(&models.Faculty{}), 7).
		Find(&[]models.Faculty{}).Statement

	sql := stmt.SQL.String()
	if !strings.Contains(sql, "institution_id") {
		t.Fatalf("expected the scope to add an institution_id predicate, got: %s", sql)
	}
	// The id is a bound parameter, not interpolated, so check the bindings.
	if len(stmt.Vars) != 1 || stmt.Vars[0] != uint(7) {
		t.Fatalf("expected the scope to bind institution 7, got vars %v", stmt.Vars)
	}
}

func TestTenantScope_PlatformScopeLeavesQueryUnscoped(t *testing.T) {
	db := dryRunDB(t)

	stmt := TenantScope(db.Model(&models.Faculty{}), PlatformScope).
		Find(&[]models.Faculty{}).Statement

	if sql := stmt.SQL.String(); strings.Contains(sql, "institution_id") {
		t.Fatalf("platform scope must not filter by institution, got: %s", sql)
	}
}

func TestTenantScope_NilDBIsSafe(t *testing.T) {
	// A nil handle must not panic; the tenant repositories are constructed
	// before the connection is necessarily live in some tests.
	if got := TenantScope(nil, 7); got != nil {
		t.Fatalf("expected nil for a nil db, got %v", got)
	}
}

func TestTenantScope_ComposesWithOtherPredicates(t *testing.T) {
	db := dryRunDB(t)

	stmt := TenantScope(db.Model(&models.Timetable{}), 3).
		Where("day = ?", models.Monday).
		Find(&[]models.Timetable{}).Statement

	sql := stmt.SQL.String()
	if !strings.Contains(sql, "institution_id") {
		t.Errorf("tenant predicate missing: %s", sql)
	}
	if !strings.Contains(sql, "day") {
		t.Errorf("caller predicate was dropped: %s", sql)
	}
}
