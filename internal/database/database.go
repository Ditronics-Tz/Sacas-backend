package database

import (
	"log"

	"go_boilerplate/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

func InitDB() *gorm.DB {
	dsn := config.GetEnv("DATABASE_URL", "host=localhost user=postgres password=postgres dbname=SACAS port=5432 sslmode=disable")

	var err error
	db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Println("========================================================")
		log.Println(" FAILED to connect to PostgreSQL")
		log.Println(" Fix DATABASE_URL in your .env file, for example:")
		log.Println(`   DATABASE_URL=host=localhost user=postgres password=YOUR_REAL_PASSWORD dbname=SACAS port=5432 sslmode=disable`)
		log.Println(" Also ensure database exists:")
		log.Println(`   CREATE DATABASE "SACAS";`)
		log.Println("========================================================")
		log.Fatal(err)
	}
	return db
}

func CloseDB(db *gorm.DB) {
	if db == nil {
		return
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Printf("close db: %v", err)
		return
	}
	if err := sqlDB.Close(); err != nil {
		log.Printf("close db: %v", err)
	}
}
