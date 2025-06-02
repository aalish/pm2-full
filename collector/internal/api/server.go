package api

import (
	"net/http"

	"github.com/aalish/pm2-full/internal/config"
	"github.com/aalish/pm2-full/internal/storage"
	"github.com/gorilla/mux"
)

func Start(cfg config.APIConfig, store storage.Store) error {
	r := mux.NewRouter()
	// Documentation
	r.HandleFunc("/docs", docsHandler).Methods("GET")
	// Metrics
	api := r.PathPrefix("/query").Subrouter()
	api.Use(BasicAuth(cfg.BasicAuth.Username, cfg.BasicAuth.Password))
	api.HandleFunc("", queryHandler(store)).Methods("GET")
	// Processes
	p := r.PathPrefix("/processes").Subrouter()
	p.Use(BasicAuth(cfg.BasicAuth.Username, cfg.BasicAuth.Password))
	p.HandleFunc("", processesHandler(store)).Methods("GET")
	// Logs
	l := r.PathPrefix("/logs").Subrouter()
	l.Use(BasicAuth(cfg.BasicAuth.Username, cfg.BasicAuth.Password))
	l.HandleFunc("", logsHandler(store)).Methods("GET")

	a := r.PathPrefix("/apps").Subrouter()
	a.Use(BasicAuth(cfg.BasicAuth.Username, cfg.BasicAuth.Password))
	a.HandleFunc("", appsHandler(store)).Methods("GET")

	t := r.PathPrefix("/target").Subrouter()
	t.Use(BasicAuth(cfg.BasicAuth.Username, cfg.BasicAuth.Password))
	t.HandleFunc("", targetHandler(store)).Methods("GET")

	j := r.PathPrefix("/jobs").Subrouter()
	j.Use(BasicAuth(cfg.BasicAuth.Username, cfg.BasicAuth.Password))
	j.HandleFunc("", jobsHandler(store)).Methods("GET")

	return http.ListenAndServe(cfg.Listen, r)

}
