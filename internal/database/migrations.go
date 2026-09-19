package database

import (
	"fmt"
	"log"
	"strings"

	"go_boilerplate/internal/config"
	"go_boilerplate/internal/models"
	"go_boilerplate/pkg/security"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// RunMigrations runs all database migrations
func RunMigrations(db *gorm.DB) error {
	err := db.AutoMigrate(
		&models.User{},
		&models.Faculty{},
		&models.Staff{},
		&models.Course{},
		&models.Module{},
		&models.Class{},
		&models.Room{},
		&models.Subject{},
		&models.Timetable{},
		&models.GenerationSettings{},
	)
	if err != nil {
		log.Printf("Migration failed: %v", err)
		return err
	}

	// Soft-delete aware unique constraints: plain UNIQUE fails after a
	// soft-delete (row still occupies the unique slot). Replace with
	// partial unique indexes WHERE deleted_at IS NULL so deleting then
	// re-creating (or CSV re-importing) the same email/phone succeeds.
	if err := ensurePartialUniqueIndexes(db); err != nil {
		log.Printf("partial unique index migration failed: %v", err)
		return err
	}

	// One-time, server-side bootstrap of the User→Staff relationship:
	// link staff rows without a user_id to the login account whose email
	// matches the staff email. Emails are unique on both tables, and this
	// runs on the server only — client input is never involved.
	if err := BackfillStaffUserLinks(db); err != nil {
		log.Printf("Staff→User backfill failed: %v", err)
		return err
	}
	return nil
}

func ensurePartialUniqueIndexes(db *gorm.DB) error {
	// Drop legacy global unique constraints created by earlier AutoMigrate
	// (gorm:"unique"). Ignore errors — they may not exist on fresh DBs.
	db.Exec(`ALTER TABLE users DROP CONSTRAINT IF EXISTS uni_users_email`)
	db.Exec(`ALTER TABLE users DROP CONSTRAINT IF EXISTS uni_users_phone_number`)
	db.Exec(`ALTER TABLE staffs DROP CONSTRAINT IF EXISTS uni_staffs_email`)
	// Also drop indexes that may have been created directly
	db.Exec(`DROP INDEX IF EXISTS uni_users_email`)
	db.Exec(`DROP INDEX IF EXISTS uni_users_phone_number`)
	db.Exec(`DROP INDEX IF EXISTS uni_staffs_email`)
	db.Exec(`DROP INDEX IF EXISTS idx_users_email`)
	db.Exec(`DROP INDEX IF EXISTS idx_users_phone_number`)
	db.Exec(`DROP INDEX IF EXISTS idx_staffs_email`)

	// Create partial unique indexes — only rows where deleted_at IS NULL participate
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_active ON users(email) WHERE deleted_at IS NULL`).Error; err != nil {
		return err
	}
	// Phone: allow multiple soft-deleted rows and empty phones; only non-empty active phones must be unique
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_phone_active ON users(phone_number) WHERE deleted_at IS NULL AND phone_number IS NOT NULL AND phone_number <> ''`).Error; err != nil {
		return err
	}
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_staffs_email_active ON staffs(email) WHERE deleted_at IS NULL`).Error; err != nil {
		return err
	}
	return nil
}

// BackfillStaffUserLinks idempotently sets staff.user_id from matching user
// emails. Safe to run on every boot; existing links are never overwritten.
func BackfillStaffUserLinks(db *gorm.DB) error {
	result := db.Model(&models.Staff{}).
		Where("user_id IS NULL AND deleted_at IS NULL").
		Where("email IN (?)", db.Model(&models.User{}).Select("email").Where("deleted_at IS NULL")).
		Update("user_id", db.Model(&models.User{}).Select("id").
			Where("users.email = staffs.email AND users.deleted_at IS NULL"))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		log.Printf("Backfilled %d staff record(s) with linked user accounts", result.RowsAffected)
	}
	return nil
}

// bcrypt hash for plaintext "password" (demo only — change in production)
const demoPasswordHash = "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi"

// BootstrapSuperAdmin creates the first super_admin from env if none exists.
// Env: SUPERADMIN_EMAIL, SUPERADMIN_PASSWORD (must satisfy password policy),
//      optionally SUPERADMIN_FIRST_NAME / SUPERADMIN_LAST_NAME / SUPERADMIN_PHONE.
// In production, if no super_admin exists and env is missing, it returns an error
// so the operator knows to set the vars. In development it is a no-op.
func BootstrapSuperAdmin(db *gorm.DB) error {
	var count int64
	if err := db.Model(&models.User{}).Where("role = ? AND deleted_at IS NULL", models.RoleSuperAdmin).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	email := strings.TrimSpace(config.GetEnv("SUPERADMIN_EMAIL", ""))
	password := config.GetEnv("SUPERADMIN_PASSWORD", "")
	env := strings.ToLower(config.GetEnv("ENV", "development"))
	isProd := env == "production" || env == "prod"

	if email == "" || password == "" {
		if isProd {
			return fmt.Errorf("no super_admin exists — set SUPERADMIN_EMAIL and SUPERADMIN_PASSWORD to bootstrap first admin (ENV=production)")
		}
		log.Printf("No super_admin found — set SUPERADMIN_EMAIL/SUPERADMIN_PASSWORD to create one (skipping in %s)", env)
		return nil
	}
	if !security.ValidPassword(password) {
		return fmt.Errorf("SUPERADMIN_PASSWORD does not satisfy policy: %s", security.PasswordPolicyMessage)
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash superadmin password: %w", err)
	}
	firstName := config.GetEnv("SUPERADMIN_FIRST_NAME", "Super")
	lastName := config.GetEnv("SUPERADMIN_LAST_NAME", "Admin")
	phone := config.GetEnv("SUPERADMIN_PHONE", "")
	user := models.User{
		Email:       email,
		Password:    string(hashed),
		FirstName:   firstName,
		LastName:    lastName,
		PhoneNumber: phone,
		Role:        models.RoleSuperAdmin,
		IsActive:    true,
		IsVerified:  true,
	}
	if err := db.Create(&user).Error; err != nil {
		return fmt.Errorf("create superadmin %s: %w", email, err)
	}
	log.Printf("Bootstrapped super_admin %s (ID %d) from SUPERADMIN_EMAIL", email, user.ID)
	return nil
}

// CreateInitialData seeds demo users for local testing (idempotent).
// BLOCKER guard: only runs when ENV != production AND SEED_DEMO=true.
// Use SUPERADMIN_EMAIL/PASSWORD for production bootstrap instead.
func CreateInitialData(db *gorm.DB) error {
	env := strings.ToLower(config.GetEnv("ENV", "development"))
	isProd := env == "production" || env == "prod"
	if isProd {
		log.Printf("SEED_DEMO blocked: ENV=production — refusing to seed demo users (use SUPERADMIN_EMAIL)")
		return nil
	}
	if !strings.EqualFold(config.GetEnv("SEED_DEMO", "false"), "true") {
		log.Printf("SEED_DEMO disabled (ENV=%s) — set SEED_DEMO=true to seed demo users", env)
		return nil
	}
	// Distinct phone numbers required (empty string is unique-constrained in DB)
	demos := []models.User{
		{
			Email:       "admin@example.com",
			Password:    demoPasswordHash,
			FirstName:   "Super",
			LastName:    "Admin",
			PhoneNumber: "+255700000001",
			Role:        models.RoleSuperAdmin,
			IsActive:    true,
			IsVerified:  true,
		},
		{
			Email:       "coordinator@sacas.local",
			Password:    demoPasswordHash,
			FirstName:   "Campus",
			LastName:    "Coordinator",
			PhoneNumber: "+255700000002",
			Role:        models.RoleAdmin,
			IsActive:    true,
			IsVerified:  true,
		},
		{
			Email:       "scheduler@sacas.local",
			Password:    demoPasswordHash,
			FirstName:   "Timetable",
			LastName:    "Officer",
			PhoneNumber: "+255700000003",
			Role:        models.RoleAdmin,
			IsActive:    true,
			IsVerified:  true,
		},
		{
			Email:       "lecturer@sacas.local",
			Password:    demoPasswordHash,
			FirstName:   "Jane",
			LastName:    "Lecturer",
			PhoneNumber: "+255700000004",
			Role:        models.RoleUser,
			IsActive:    true,
			IsVerified:  true,
		},
		{
			Email:       "viewer@sacas.local",
			Password:    demoPasswordHash,
			FirstName:   "View",
			LastName:    "Only",
			PhoneNumber: "+255700000005",
			Role:        models.RoleUser,
			IsActive:    true,
			IsVerified:  true,
		},
	}

	for _, u := range demos {
		var existing models.User
		err := db.Where("email = ?", u.Email).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			if err := db.Create(&u).Error; err != nil {
				log.Printf("Failed to seed user %s: %v", u.Email, err)
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		// Ensure demo accounts stay usable for local testing
		existing.Password = demoPasswordHash
		existing.Role = u.Role
		existing.IsActive = true
		existing.IsVerified = true
		existing.FirstName = u.FirstName
		existing.LastName = u.LastName
		existing.PhoneNumber = u.PhoneNumber
		if err := db.Save(&existing).Error; err != nil {
			log.Printf("Failed to refresh demo user %s: %v", u.Email, err)
			return err
		}
	}
	return nil
}

// DropAllTables drops all tables (use with caution)
func DropAllTables(db *gorm.DB) error {
	log.Println("Dropping all tables...")
	
	return db.Migrator().DropTable(
		&models.User{},
		&models.Faculty{},
		&models.Staff{},
		&models.Course{},
		&models.Module{},
		&models.Class{},
		&models.Room{},
		&models.Subject{},
		&models.Timetable{},
		&models.GenerationSettings{},
	)
}