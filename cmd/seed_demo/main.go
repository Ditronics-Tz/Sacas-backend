package main

import (
	"fmt"
	"log"

	"github.com/joho/godotenv"
	"go_boilerplate/internal/database"
	"go_boilerplate/internal/models"
)

func main() {
	_ = godotenv.Load()
	db := database.InitDB()
	defer database.CloseDB(db)
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
