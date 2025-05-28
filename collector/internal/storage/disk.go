// internal/storage/disk.go
package storage

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aalish/pm2-full/internal/discovery"
	"github.com/gogo/protobuf/proto"
	dto "github.com/prometheus/client_model/go"
)

// QueryParams defines common parameters for all queries.
// App is the optional application name for logs.
type QueryParams struct {
	Job    string
	Target string
	App    string    // for logs: if empty, will fetch all apps
	Start  time.Time // inclusive
	End    time.Time // inclusive
}

// aliases for clarity
type ProcessQuery = QueryParams
type LogQuery = QueryParams

// Store is the read/query interface.
type Store interface {
	QueryMetrics(q QueryParams) ([]json.RawMessage, error)
	QueryProcesses(q ProcessQuery) ([]json.RawMessage, error)
	QueryLogs(q LogQuery) ([]string, error)
}

// DiskStorage implements both the discovery.Store (write) and storage.Store (read).
type DiskStorage struct {
	dir           string
	retentionDays int
	mu            sync.Mutex
}

// compile‐time assertions
var (
	_ discovery.Store = (*DiskStorage)(nil)
	_ Store           = (*DiskStorage)(nil)
)

// New prepares the root directory and spins up retention cleanup.
func New(dir string, retentionDays int) (*DiskStorage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	ds := &DiskStorage{dir: dir, retentionDays: retentionDays}
	go ds.startRetention()
	return ds, nil
}

// --- discovery.Store implementation ---

// StoreMetrics base64‐encodes each MetricFamily proto and appends to metrics_<job>_<target>.jsonl
func (d *DiskStorage) StoreMetrics(job, target string, mfs map[string]*dto.MetricFamily) {
	rec := struct {
		Timestamp string            `json:"timestamp"`
		Metrics   map[string]string `json:"metrics"`
	}{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Metrics:   make(map[string]string, len(mfs)),
	}
	for name, mf := range mfs {
		if bts, err := proto.Marshal(mf); err == nil {
			rec.Metrics[name] = base64.StdEncoding.EncodeToString(bts)
		} else {
			fmt.Fprintf(os.Stderr, "StoreMetrics: proto.Marshal error for %q: %v\n", name, err)
		}
	}
	d.appendJSONLine("metrics", job, target, rec)
}

// StoreProcesses dumps the raw JSON from /processes into processes_<job>_<target>.jsonl
func (d *DiskStorage) StoreProcesses(job, target string, data []byte) {
	rec := struct {
		Timestamp string          `json:"timestamp"`
		Data      json.RawMessage `json:"data"`
	}{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Data:      json.RawMessage(data),
	}
	d.appendJSONLine("processes", job, target, rec)
}

// StoreLog strips "[app]" prefix, records app name, and appends to logs_<job>_<target>_<app>.jsonl
func (d *DiskStorage) StoreLog(job, target, line string) {
	app := ""
	msg := line
	if strings.HasPrefix(line, "[") {
		if idx := strings.Index(line, "]"); idx > 0 {
			app = line[1:idx]
			msg = strings.TrimSpace(line[idx+1:])
		}
	}
	rec := struct {
		Timestamp string `json:"timestamp"`
		App       string `json:"app"`
		Line      string `json:"line"`
	}{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		App:       app,
		Line:      msg,
	}
	d.appendLogLine(job, target, app, rec)
}

// helper for metrics & processes
func (d *DiskStorage) appendJSONLine(kind, job, target string, v interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	fn := filepath.Join(d.dir, fmt.Sprintf("%s_%s_%s.jsonl", kind, job, target))
	f, err := os.OpenFile(fn, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "appendJSONLine: open %s: %v\n", fn, err)
		return
	}
	defer f.Close()

	line, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "appendJSONLine: marshal: %v\n", err)
		return
	}
	f.Write(line)
	f.Write([]byte("\n"))
}

// specialized helper for logs (includes app in filename)
func (d *DiskStorage) appendLogLine(job, target, app string, v interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	fn := filepath.Join(d.dir, fmt.Sprintf("logs_%s_%s_%s.jsonl", job, target, app))
	f, err := os.OpenFile(fn, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "appendLogLine: open %s: %v\n", fn, err)
		return
	}
	defer f.Close()

	line, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "appendLogLine: marshal: %v\n", err)
		return
	}
	f.Write(line)
	f.Write([]byte("\n"))
}

// --- storage.Store implementation ---

func (d *DiskStorage) QueryMetrics(q QueryParams) ([]json.RawMessage, error) {
	return d.queryJSONLines("metrics", q.Job, q.Target, q.Start, q.End)
}

func (d *DiskStorage) QueryProcesses(q ProcessQuery) ([]json.RawMessage, error) {
	return d.queryJSONLines("processes", q.Job, q.Target, q.Start, q.End)
}

func (d *DiskStorage) QueryLogs(q LogQuery) ([]string, error) {
	var out []string

	pattern := fmt.Sprintf("logs_%s_%s_*.jsonl", q.Job, q.Target)
	if q.App != "" {
		pattern = fmt.Sprintf("logs_%s_%s_%s.jsonl", q.Job, q.Target, q.App)
	}
	files, _ := filepath.Glob(filepath.Join(d.dir, pattern))
	for _, fn := range files {
		f, err := os.Open(fn)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			raw := scanner.Bytes()
			var head struct {
				Timestamp string `json:"timestamp"`
			}
			if err := json.Unmarshal(raw, &head); err != nil {
				continue
			}
			ts, err := time.Parse(time.RFC3339Nano, head.Timestamp)
			if err != nil {
				continue
			}
			if (q.Start.IsZero() || !ts.Before(q.Start)) && (q.End.IsZero() || !ts.After(q.End)) {
				var rec struct {
					Line string `json:"line"`
				}
				if err := json.Unmarshal(raw, &rec); err == nil {
					out = append(out, rec.Line)
				}
			}
		}
		f.Close()
	}
	return out, nil
}

// shared JSON-lines reader for metrics & processes
func (d *DiskStorage) queryJSONLines(kind, job, target string, start, end time.Time) ([]json.RawMessage, error) {
	fn := filepath.Join(d.dir, fmt.Sprintf("%s_%s_%s.jsonl", kind, job, target))
	f, err := os.Open(fn)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var results []json.RawMessage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		var head struct {
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, head.Timestamp)
		if err != nil {
			continue
		}
		if (start.IsZero() || !ts.Before(start)) && (end.IsZero() || !ts.After(end)) {
			results = append(results, append([]byte(nil), line...))
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return nil, err
	}
	return results, nil
}

// --- retention ---

func (d *DiskStorage) startRetention() {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for range t.C {
		d.pruneOld()
	}
}

func (d *DiskStorage) pruneOld() {
	cutoff := time.Now().UTC().Add(-time.Duration(d.retentionDays) * 24 * time.Hour)
	kinds := []string{"metrics", "processes", "logs"}

	for _, kind := range kinds {
		pat := fmt.Sprintf("%s_*.jsonl", kind)
		matches, _ := filepath.Glob(filepath.Join(d.dir, pat))
		for _, fn := range matches {
			d.mu.Lock()
			data, err := os.ReadFile(fn)
			if err != nil {
				d.mu.Unlock()
				continue
			}
			lines := strings.Split(string(data), "\n")
			var kept []string
			for _, l := range lines {
				if strings.TrimSpace(l) == "" {
					continue
				}
				var head struct {
					Timestamp string `json:"timestamp"`
				}
				if err := json.Unmarshal([]byte(l), &head); err != nil {
					continue
				}
				ts, err := time.Parse(time.RFC3339Nano, head.Timestamp)
				if err != nil {
					continue
				}
				if ts.After(cutoff) {
					kept = append(kept, l)
				}
			}
			if len(kept) > 0 {
				_ = os.WriteFile(fn, []byte(strings.Join(kept, "\n")+"\n"), 0o644)
			} else {
				_ = os.Remove(fn)
			}
			d.mu.Unlock()
		}
	}
}
