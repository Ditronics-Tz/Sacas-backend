package database

import (
	"log"

	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

// SeedDB is a legacy helper. Prefer CreateInitialData used by main.
// Role aligned to super_admin (not invalid "admin") so it does not conflict
// with CreateInitialData's seed. Same credentials: admin@example.com / password.
func SeedDB(db *gorm.DB) {
	err := db.AutoMigrate(&models.User{})
	if err != nil {
		log.Fatalf("Failed to auto migrate: %v", err)
	}

	adminUser := models.User{
		Email:      "admin@example.com",
		Password:   "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi", // password
		FirstName:  "Super",
		LastName:   "Admin",
		Role:       models.RoleSuperAdmin,
		IsActive:   true,
		IsVerified: true,
	}

	if err := db.FirstOrCreate(&adminUser, models.User{Email: adminUser.Email}).Error; err != nil {
		log.Printf("Failed to seed admin user: %v", err)
	} else {
		log.Println("Admin user seeded successfully (super_admin)")
	}
}
