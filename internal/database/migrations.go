package database

import (
	"log"

	"go_boilerplate/internal/models"
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
	)
	if err != nil {
		log.Printf("Migration failed: %v", err)
		return err
	}
	return nil
}

// bcrypt hash for plaintext "password" (demo only — change in production)
const demoPasswordHash = "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi"

// CreateInitialData seeds demo users for local testing (idempotent).
func CreateInitialData(db *gorm.DB) error {
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
	)
}