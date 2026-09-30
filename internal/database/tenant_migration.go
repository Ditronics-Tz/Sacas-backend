package database

import (
	"fmt"
	"log"

	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// Versioned migration support.
//
// The schema is migrated in two phases:
//
//	Phase 1 (pre-AutoMigrate)  — structural steps that AutoMigrate cannot
//	                             express safely, namely adding a column to a
//	                             table that already has rows. These run first,
//	                             adding columns as NULLABLE, backfilling, and
//	                             only then tightening to NOT NULL.
//	Phase 2 (AutoMigrate)      — normal GORM schema sync against models that
//	                             are already consistent with the database.
//	Phase 3 (post-AutoMigrate) — indexes and constraints GORM does not model
//	                             (partial and composite unique indexes).
//
// Every versioned step is recorded in schema_migrations so it runs exactly
// once per database, while the whole file remains safe to re-run.

const schemaVersionTenant = 1

// tenantTables are the timetable-domain tables that belong to exactly one
// institution and therefore get a non-null institution_id.
var tenantTables = []string{
	"faculties", "staffs", "courses", "modules", "classes",
	"rooms", "subjects", "timetables",
}

// ensureSchemaMigrationsTable creates the version tracking table.
func ensureSchemaMigrationsTable(db *gorm.DB) error {
	return db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`).Error
}

func schemaVersionApplied(db *gorm.DB, version int) (bool, error) {
	var count int64
	if err := db.Table("schema_migrations").Where("version = ?", version).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func recordSchemaVersion(db *gorm.DB, version int, name string) error {
	return db.Exec(
		`INSERT INTO schema_migrations (version, name) VALUES (?, ?) ON CONFLICT (version) DO NOTHING`,
		version, name,
	).Error
}

// RunPreAutoMigrateMigrations runs structural steps that must happen before
// GORM syncs the schema. Safe to call on every boot.
func RunPreAutoMigrateMigrations(db *gorm.DB) error {
	if err := ensureSchemaMigrationsTable(db); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	applied, err := schemaVersionApplied(db, schemaVersionTenant)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	if applied {
		return nil
	}
	if err := migrateTenancy(db); err != nil {
		return err
	}
	if err := recordSchemaVersion(db, schemaVersionTenant, "add multi-tenancy institution_id"); err != nil {
		return err
	}
	log.Printf("Applied migration %d: add multi-tenancy institution_id", schemaVersionTenant)
	return nil
}

// migrateTenancy adds institution_id across the schema in a safe order:
// create the tenant table, add nullable columns, seed the default tenant,
// backfill existing rows, then tighten to NOT NULL.
// migrateTenancy adds institution_id across the schema in a safe order:
// create the tenant table, add nullable columns, seed the default tenant,
// backfill existing rows, then tighten to NOT NULL.
//
// This runs BEFORE AutoMigrate, so on a fresh database the domain tables do not
// exist yet. Every step that touches one of them is therefore guarded by
// tableExists: a table that is not there yet gets the column from AutoMigrate
// instead, with the model already carrying the NOT NULL default.
func migrateTenancy(db *gorm.DB) error {
	// Step 1 — the institutions table itself. Created by GORM so the column
	// types stay in sync with the model.
	if err := db.AutoMigrate(&models.Institution{}); err != nil {
		return fmt.Errorf("automigrate institutions: %w", err)
	}

	// Step 2 — the default tenant every existing row is backfilled into.
	if err := ensureDefaultInstitution(db); err != nil {
		return err
	}

	// users must exist for the backfill below; on a fresh database AutoMigrate
	// creates it later, so there is nothing to do.
	usersExist := tableExists(db, "users")
	domainTables := make([]string, 0, len(tenantTables))
	for _, table := range tenantTables {
		if !tableExists(db, table) {
			// Created by AutoMigrate in phase 2, already carrying
			// institution_id with a NOT NULL default.
			continue
		}
		domainTables = append(domainTables, table)
		if err := addColumnIfMissing(db, table, "institution_id", "bigint"); err != nil {
			return err
		}
	}
	if usersExist {
		// users.institution_id stays NULLABLE forever: NULL means platform
		// super_admin, which spans all tenants.
		if err := addColumnIfMissing(db, "users", "institution_id", "bigint"); err != nil {
			return err
		}
	}
	joinExists := tableExists(db, "staff_modules")
	if joinExists {
		// The staff↔module join table also needs the tenant column so
		// association queries cannot cross tenants.
		if err := addColumnIfMissing(db, "staff_modules", "institution_id", "bigint"); err != nil {
			return err
		}
	}

	// Step 4 — backfill existing rows into the default institution.
	for _, table := range domainTables {
		if err := db.Exec(
			`UPDATE `+table+` SET institution_id = ? WHERE institution_id IS NULL`,
			models.DefaultInstitutionID,
		).Error; err != nil {
			return fmt.Errorf("backfill %s: %w", table, err)
		}
	}
	// Every non-super_admin user belongs to the default institution. Platform
	// super_admins are deliberately left NULL so they can span all tenants.
	if usersExist {
		if err := db.Exec(
			`UPDATE users SET institution_id = ? WHERE institution_id IS NULL AND role <> ?`,
			models.DefaultInstitutionID, models.RoleSuperAdmin,
		).Error; err != nil {
			return fmt.Errorf("backfill users: %w", err)
		}
	}
	if joinExists {
		// Staff links are (institution, email) pairs now, so rebuild the join
		// table's tenant column from each staff row before it is tightened.
		if err := db.Exec(`
			UPDATE staff_modules sm
			SET institution_id = s.institution_id
			FROM staffs s
			WHERE s.id = sm.staff_id`).Error; err != nil {
			return fmt.Errorf("backfill staff_modules: %w", err)
		}
		// Any orphan join rows (staff hard-deleted) are removed rather than
		// left NULL, since the column becomes NOT NULL.
		if err := db.Exec(
			`DELETE FROM staff_modules WHERE institution_id IS NULL`).Error; err != nil {
			return fmt.Errorf("prune staff_modules: %w", err)
		}
	}

	// Step 5 — tighten to NOT NULL with a safe default for future inserts.
	for _, table := range domainTables {
		set := fmt.Sprintf(`ALTER TABLE %s
			ALTER COLUMN institution_id SET DEFAULT %d,
			ALTER COLUMN institution_id SET NOT NULL`, table, models.DefaultInstitutionID)
		if err := db.Exec(set).Error; err != nil {
			return fmt.Errorf("tighten %s.institution_id: %w", table, err)
		}
	}
	if joinExists {
		if err := db.Exec(`
			ALTER TABLE staff_modules
			ALTER COLUMN institution_id SET DEFAULT ` + fmt.Sprint(models.DefaultInstitutionID) + `,
			ALTER COLUMN institution_id SET NOT NULL`).Error; err != nil {
			return fmt.Errorf("tighten staff_modules.institution_id: %w", err)
		}
	}
	// users stays nullable, but gets a plain index for tenant lookups.
	if usersExist {
		if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_users_institution_id ON users(institution_id)`).Error; err != nil {
			return fmt.Errorf("index users.institution_id: %w", err)
		}
	}
	for _, table := range domainTables {
		idx := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_institution_id ON %s(institution_id)`, table, table)
		if err := db.Exec(idx).Error; err != nil {
			return fmt.Errorf("index %s.institution_id: %w", table, err)
		}
	}
	// A composite index on the tenant column plus the soft-delete marker keeps
	// the partial unique indexes below cheap to maintain.
	if joinExists {
		if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_staff_modules_institution ON staff_modules(institution_id)`).Error; err != nil {
			return fmt.Errorf("index staff_modules.institution_id: %w", err)
		}
	}
	return nil
}

// tableExists reports whether a table is present. The tenancy migration runs
// before AutoMigrate, so on a fresh database the domain tables do not exist
// yet and every operation on them must be skipped.
func tableExists(db *gorm.DB, table string) bool {
	return db.Migrator().HasTable(table)
}

// ensureDefaultInstitution creates the backfill tenant if it is absent.
func ensureDefaultInstitution(db *gorm.DB) error {
	var count int64
	if err := db.Model(&models.Institution{}).Where("id = ?", models.DefaultInstitutionID).Count(&count).Error; err != nil {
		return fmt.Errorf("check default institution: %w", err)
	}
	if count > 0 {
		return nil
	}
	inst := models.Institution{
		ID:      models.DefaultInstitutionID,
		Name:    models.DefaultInstitutionName,
		Slug:    "default",
		Type:    models.InstitutionTypeCollege,
		Country: "Tanzania",
		Status:  models.InstitutionStatusActive,
		Plan:    models.InstitutionPlanFree,
	}
	// Use a savepoint-tolerant insert: a concurrent boot may win the race.
	if err := db.Create(&inst).Error; err != nil {
		var recheck int64
		if err2 := db.Model(&models.Institution{}).Where("id = ?", models.DefaultInstitutionID).Count(&recheck).Error; err2 == nil && recheck > 0 {
			return nil
		}
		return fmt.Errorf("create default institution: %w", err)
	}
	log.Printf("Created default institution %q (ID %d) for existing data", models.DefaultInstitutionName, models.DefaultInstitutionID)
	return nil
}

// addColumnIfMissing adds a column only when it does not already exist, so
// re-running against a partly-migrated database is safe.
func addColumnIfMissing(db *gorm.DB, table, column, columnType string) error {
	if db.Migrator().HasColumn(table, column) {
		return nil
	}
	stmt := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, columnType)
	if err := db.Exec(stmt).Error; err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

// RunPostAutoMigrateMigrations creates indexes and constraints that GORM does
// not express. Safe to call on every boot.
func RunPostAutoMigrateMigrations(db *gorm.DB) error {
	if err := ensurePartialUniqueIndexes(db); err != nil {
		return err
	}
	if err := ensureTenantUniqueIndexes(db); err != nil {
		return err
	}
	return nil
}

// ensureTenantUniqueIndexes creates per-institution uniqueness rules. Names and
// codes are only unique within an institution, and only among live rows.
func ensureTenantUniqueIndexes(db *gorm.DB) error {
	// Drop the pre-tenancy global staff email index — email is now unique per
	// institution, so the same address may exist at two institutions.
	db.Exec(`DROP INDEX IF EXISTS idx_staffs_email_active`)

	indexes := []string{
		// Staff: email unique per institution.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_staffs_inst_email_active
			ON staffs(institution_id, email) WHERE deleted_at IS NULL`,
		// Natural keys unique per institution, soft-delete aware.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_faculties_inst_name_active
			ON faculties(institution_id, lower(name)) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_courses_inst_name_active
			ON courses(institution_id, lower(name)) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_classes_inst_name_active
			ON classes(institution_id, lower(name)) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_modules_inst_name_active
			ON modules(institution_id, lower(name)) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_subjects_inst_name_active
			ON subjects(institution_id, lower(name)) WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_rooms_inst_name_active
			ON rooms(institution_id, lower(name)) WHERE deleted_at IS NULL`,
		// Module and institution slugs are user-facing identifiers, so they are
		// case-insensitively unique.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_modules_inst_code_active
			ON modules(institution_id, lower(code))
			WHERE deleted_at IS NULL AND code IS NOT NULL AND code <> ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_institutions_slug_active
			ON institutions(lower(slug)) WHERE deleted_at IS NULL`,
	}
	for _, stmt := range indexes {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("create tenant unique index: %w", err)
		}
	}
	return nil
}
