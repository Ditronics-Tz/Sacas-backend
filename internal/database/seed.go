package database

import (
	"log"

	"go_boilerplate/internal/models"
	"gorm.io/gorm"
)

func SeedDB(db *gorm.DB) {
	// Auto Migrate
	err := db.AutoMigrate(&models.User{})
	if err != nil {
		log.Fatalf("Failed to auto migrate: %v", err)
	}

	// Seed default admin user
	adminUser := models.User{
		Email:    "admin@example.com",
		Password: "$2a$10$3XvXBA2lX5bXwI1H0kxrN.vp4R.X1I7Mo7S3d62M9U7Zc3Y6q5VKa", // bcrypt hash for 'admin123'
		Role:     "admin",
		IsActive: true,
	}

	if err := db.FirstOrCreate(&adminUser, models.User{Email: adminUser.Email}).Error; err != nil {
		log.Printf("Failed to seed admin user: %v", err)
	} else {
		log.Println("Admin user seeded successfully")
	}
}