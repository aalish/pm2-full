
package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/aalish/pm2-full/config"
	"github.com/aalish/pm2-full/logs"
	"github.com/aalish/pm2-full/metrics"
	"github.com/aalish/pm2-full/pm2"
	"log"
)

type Server struct {
	cfg       config.ServerConfig
	pm2Client *pm2.Client
	metrics   *metrics.Exporter
	logs      *logs.Streamer
}

func New(cfg config.ServerConfig, pm2Client *pm2.Client, metricsExporter *metrics.Exporter, logStreamer *logs.Streamer) *Server {
	return &Server{
		cfg:       cfg,
		pm2Client: pm2Client,
		metrics:   metricsExporter,
		logs:      logStreamer,
	}
}

func (s *Server) Run() error {
    // Processes
    http.HandleFunc("/processes", s.handleProcesses())

    // Metrics
    var metricsHandler http.Handler = s.metrics.Handler()
    if s.cfg.BasicAuth.Enabled {
        metricsHandler = BasicAuthMiddleware(metricsHandler, s.cfg.BasicAuth.Username, s.cfg.BasicAuth.Password)
    }
    http.Handle("/metrics", metricsHandler)

    // Logs
    var logsHandler http.Handler = http.HandlerFunc(s.logs.StreamHandler)
    if s.cfg.BasicAuth.Enabled {
        logsHandler = BasicAuthMiddleware(logsHandler, s.cfg.BasicAuth.Username, s.cfg.BasicAuth.Password)
    }
    http.Handle("/logs", logsHandler)

    fmt.Printf("listening on %s\n", s.cfg.Listen)
    if s.cfg.TLSCert != "" && s.cfg.TLSKey != "" {
        return http.ListenAndServeTLS(s.cfg.Listen, s.cfg.TLSCert, s.cfg.TLSKey, nil)
    }
    return http.ListenAndServe(s.cfg.Listen, nil)
}
func (s *Server) handleProcesses() http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        procs, err := s.pm2Client.List()
        if err != nil {
            // Log full error to console
            log.Printf("ERROR fetching PM2 processes: %v", err)
            http.Error(w, "failed to list PM2 processes", http.StatusInternalServerError)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(procs)
    }
}
