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
	// Run the schema migrations first so the institution table and the
	// institution_id columns exist before anything is seeded.
	if err := database.RunMigrations(db); err != nil {
		log.Fatal(err)
	}
	// Also bootstrap superadmin if configured
	if err := database.BootstrapSuperAdmin(db); err != nil {
		log.Printf("superadmin bootstrap: %v", err)
	}
	if err := database.CreateInitialData(db); err != nil {
		log.Fatal(err)
	}
	var users []models.User
	db.Order("id").Find(&users)
	fmt.Println("email|role|institution_id|active")
	for _, u := range users {
		var inst any = "platform"
		if u.InstitutionID != nil {
			inst = *u.InstitutionID
		}
		fmt.Printf("%s|%s|%v|%v\n", u.Email, u.Role, inst, u.IsActive)
	}
}
