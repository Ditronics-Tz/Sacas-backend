package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"go_boilerplate/internal/database"
	"go_boilerplate/internal/models"
)

func main() {
	_ = godotenv.Load()
	// Force demo seed regardless of SEED_DEMO gate (manual tool)
	_ = os.Setenv("SEED_DEMO", "true")
	db := database.InitDB()
	defer database.CloseDB(db)
	// Also bootstrap superadmin if configured
	if err := database.BootstrapSuperAdmin(db); err != nil {
		log.Printf("superadmin bootstrap: %v", err)
	}
	if err := database.CreateInitialData(db); err != nil {
		log.Fatal(err)
	}
	var users []models.User
	db.Order("id").Find(&users)
	fmt.Println("email|role|active")
	for _, u := range users {
		fmt.Printf("%s|%s|%v\n", u.Email, u.Role, u.IsActive)
	}
}
