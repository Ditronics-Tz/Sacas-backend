// SACAS API entrypoint.
//
// Dev server (from this folder):
//
//	go run .
//	go run ./cmd/api
//
// Build:
//
//	go build -o bin/api.exe .
package main

import (
	"log"
	"os"

	"go_boilerplate/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Printf("server failed: %v\n", err)
		os.Exit(1)
	}
}
