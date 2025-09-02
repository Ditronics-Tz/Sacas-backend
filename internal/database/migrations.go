package database

import (
	"log"

	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// RunMigrations runs all database migrations
func RunMigrations(db *gorm.DB) error {
	log.Println("Starting database migrations...")

	// Auto migrate models
	err := db.AutoMigrate(
		&models.User{},
	)
	if err != nil {
		log.Printf("Migration failed: %v", err)
		return err
	}

	log.Println("Database migrations completed successfully")
	return nil
}

// CreateInitialData creates initial data for the application
func CreateInitialData(db *gorm.DB) error {
	log.Println("Creating initial data...")

	// Check if super admin exists
	var count int64
	db.Model(&models.User{}).Where("role = ?", models.RoleSuperAdmin).Count(&count)
	
	if count == 0 {
		// Create default super admin user
		superAdmin := models.User{
			Email:       "admin@example.com",
			Password:    "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi", // password: "password"
			FirstName:   "Super",
			LastName:    "Admin",
			Role:        models.RoleSuperAdmin,
			IsActive:    true,
			IsVerified:  true,
		}

		if err := db.Create(&superAdmin).Error; err != nil {
			log.Printf("Failed to create super admin: %v", err)
			return err
		}
		
		log.Println("Default super admin created (email: admin@example.com, password: password)")
	}

	log.Println("Initial data creation completed")
	return nil
}

// DropAllTables drops all tables (use with caution)
func DropAllTables(db *gorm.DB) error {
	log.Println("Dropping all tables...")
	
	return db.Migrator().DropTable(
		&models.User{},
	)
}