package main

import (
	"log"

	"github.com/aalish/pm2-full/config"
	"github.com/aalish/pm2-full/logs"
	"github.com/aalish/pm2-full/metrics"
	"github.com/aalish/pm2-full/pm2"
	"github.com/aalish/pm2-full/server"
)

func main() {
	// Load configuration
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("unable to load config: %v", err)
	}

	// Initialize PM2 client
	pm2Client := pm2.NewClient(cfg.PM2.SocketPath, cfg.PM2.PollInterval)

	// Create exporters and streamers
	metricsExporter := metrics.NewExporter(pm2Client)
	logStreamer := logs.NewStreamer(cfg.Log.Paths)

	// Start HTTP server with PM2 client
	srv := server.New(cfg.Server, pm2Client, metricsExporter, logStreamer)
	log.Printf("starting exporter on %s", cfg.Server.Listen)
	if err := srv.Run(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}