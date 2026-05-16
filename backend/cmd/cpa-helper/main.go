package main

import (
	"context"
	"log"
	"os"

	backendApp "cpa-helper/backend/internal/app"
	"cpa-helper/backend/internal/httpserver"
)

func main() {
	app, err := backendApp.New()
	if err != nil {
		log.Fatalf("init app: %v", err)
	}
	defer app.Close()

	addr := ":18317"
	if value := os.Getenv("CPA_HELPER_ADDR"); value != "" {
		addr = value
	}

	if err := httpserver.Run(context.Background(), httpserver.Config{
		Addr:    addr,
		Handler: app.Routes(),
	}); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
