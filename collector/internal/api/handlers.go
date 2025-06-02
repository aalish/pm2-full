// File: internal/api/handlers.go
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/aalish/pm2-full/internal/storage"
)

// docsHandler returns available endpoints and their parameter requirements
func docsHandler(w http.ResponseWriter, r *http.Request) {
	docs := map[string]interface{}{
		"/docs":      map[string]interface{}{"method": "GET", "description": "lists available endpoints"},
		"/query":     map[string]interface{}{"method": "GET", "params": []string{"job", "target", "metric", "start (RFC3339)", "end (RFC3339)"}},
		"/processes": map[string]interface{}{"method": "GET", "params": []string{"job (optional)", "target (optional)"}},
		"/logs":      map[string]interface{}{"method": "GET", "params": []string{"job (optional)", "target (optional)"}},
		"/apps":      map[string]interface{}{"method": "GET", "params": []string{"job (optional)", "target (optional)"}},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(docs)
}

// queryHandler returns metrics matching query parameters
func queryHandler(store storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		job := r.URL.Query().Get("job")
		target := r.URL.Query().Get("target")
		// metric := r.URL.Query().Get("metric")
		start, _ := time.Parse(time.RFC3339, r.URL.Query().Get("start"))
		end, _ := time.Parse(time.RFC3339, r.URL.Query().Get("end"))

		data, err := store.QueryMetrics(storage.QueryParams{Job: job, Target: target, Start: start, End: end})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}
}

// queryHandler returns metrics matching query parameters
func appsHandler(store storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		job := r.URL.Query().Get("job")
		target := r.URL.Query().Get("target")
		// metric := r.URL.Query().Get("metric")
		start, _ := time.Parse(time.RFC3339, r.URL.Query().Get("start"))
		end, _ := time.Parse(time.RFC3339, r.URL.Query().Get("end"))

		data, err := store.QueryApps(storage.QueryParams{Job: job, Target: target, Start: start, End: end})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}
}

// processesHandler returns process snapshots filtered by job and/or target
func processesHandler(store storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		job := r.URL.Query().Get("job")
		target := r.URL.Query().Get("target")
		app := r.URL.Query().Get("app")

		data, err := store.QueryProcesses(storage.ProcessQuery{Job: job, Target: target, App: app})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}
}

// logsHandler returns log lines filtered by job and/or target
func logsHandler(store storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		job := r.URL.Query().Get("job")
		target := r.URL.Query().Get("target")
		app := r.URL.Query().Get("app")
		start, _ := time.Parse(time.RFC3339, r.URL.Query().Get("start"))
		end, _ := time.Parse(time.RFC3339, r.URL.Query().Get("end"))

		lines, err := store.QueryLogs(storage.LogQuery{Job: job, Target: target, App: app, Start: start, End: end})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lines)
	}
}
