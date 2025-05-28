package main

import (
	"log"

	"github.com/aalish/pm2-full/internal/api"
	"github.com/aalish/pm2-full/internal/config"
	"github.com/aalish/pm2-full/internal/discovery"
	"github.com/aalish/pm2-full/internal/storage"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("config load error: %v", err)
	}

	store, err := storage.New(cfg.Storage.Directory, cfg.Storage.RetentionDays)
	if err != nil {
		log.Fatalf("storage init error: %v", err)
	}

	// Kick off all scrape jobs
	for _, job := range cfg.Scrape.Jobs {
		go discovery.Start(job, store)
	}

	// Start API server
	if err := api.Start(cfg.API, store); err != nil {
		log.Fatalf("API server error: %v", err)
	}
}
