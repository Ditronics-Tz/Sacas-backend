package database

import (
	"log"

	"go_boilerplate/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var db *gorm.DB

func InitDB() *gorm.DB {
	var err error
	dsn := config.GetEnv("DATABASE_URL", "host=localhost user=postgres password=postgres dbname=go_boilerplate port=5432 sslmode=disable")
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}
	return db
}

func CloseDB(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatal(err)
	}
	err = sqlDB.Close()
	if err != nil {
		log.Fatal(err)
	}
}