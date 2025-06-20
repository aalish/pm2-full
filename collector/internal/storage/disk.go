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
	"sort"
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
	Job      string
	Target   string
	App      string    // for logs: if empty, will fetch all apps
	Start    time.Time // inclusive
	End      time.Time // inclusive
	NumLines int
}
type logRecord struct {
	Timestamp string `json:"timestamp"`
	App       string `json:"app"`
	Line      string `json:"line"`
}

// aliases for clarity
type ProcessQuery = QueryParams
type LogQuery = QueryParams
type AppQuery = QueryParams
type TargetQuery = QueryParams
type JobQuery = QueryParams

// Store is the read/query interface.
type Store interface {
	QueryMetrics(q QueryParams) ([]json.RawMessage, error)
	QueryProcesses(q ProcessQuery) ([]json.RawMessage, error)
	QueryLogs(q LogQuery) ([]logRecord, error)
	QueryApps(q AppQuery) ([]json.RawMessage, error)
	ListAllTargets() ([]json.RawMessage, error)
	ListJobsByTarget(q JobQuery) ([]json.RawMessage, error)
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

// --- target and jobs query implementation ---

// queryTarget returns all unique <target> values (as JSON) from filenames matching
// “logs_<job>_<target>_<app>.jsonl” in d.dir.
func (d *DiskStorage) queryTarget() ([]json.RawMessage, error) {
	// Pattern: logs_<job>_<target>_<app>.jsonl  (one “*” for job, one for target, one for app)
	pattern := filepath.Join(d.dir, "logs_*_*_*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	for _, fn := range matches {
		base := filepath.Base(fn) // e.g. "logs_myJob_myTarget_my_app_name.jsonl"
		core := strings.TrimSuffix(strings.TrimPrefix(base, "logs_"), ".jsonl")
		// Now core == "myJob_myTarget_my_app_name"
		parts := strings.SplitN(core, "_", 3)
		// Expect exactly three parts: [job, target, appRest]
		if len(parts) < 3 {
			continue
		}
		target := parts[1]
		seen[target] = struct{}{}
	}

	var result []json.RawMessage
	for t := range seen {
		b, err := json.Marshal(t)
		if err != nil {
			continue
		}
		result = append(result, json.RawMessage(b))
	}
	return result, nil
}

// queryJobByTarget returns all unique <job> values (as JSON) for files matching
// “logs_<job>_<target>_<app>.jsonl” in d.dir. It takes a target string to filter.
// Even if <app> contains underscores, SplitN(..., "_", 3) ensures correct job/target/appRest.
func (d *DiskStorage) queryJobByTarget(target string) ([]json.RawMessage, error) {
	// Pattern: logs_*_<target>_*.jsonl
	pattern := filepath.Join(d.dir, "logs_*_"+target+"_*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	for _, fn := range matches {
		base := filepath.Base(fn) // e.g. "logs_myJob_myTarget_my_app_name.jsonl"
		core := strings.TrimSuffix(strings.TrimPrefix(base, "logs_"), ".jsonl")
		// Now core == "myJob_myTarget_my_app_name"
		parts := strings.SplitN(core, "_", 3)
		// Expect exactly three parts: [job, target, appRest]
		if len(parts) < 3 {
			continue
		}
		// parts[1] is guaranteed to equal the passed-in target (due to the glob),
		// so we only need to extract parts[0] as the job name.
		job := parts[0]
		seen[job] = struct{}{}
	}

	var result []json.RawMessage
	for j := range seen {
		b, err := json.Marshal(j)
		if err != nil {
			continue
		}
		result = append(result, json.RawMessage(b))
	}
	return result, nil
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
	d.overwriteJSONLine("processes", job, target, rec)
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

// helper for metrics & processes (overwrite mode)
func (d *DiskStorage) overwriteJSONLine(kind, job, target string, v interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	fn := filepath.Join(d.dir, fmt.Sprintf("%s_%s_%s.jsonl", kind, job, target))
	// Use O_TRUNC instead of O_APPEND to clear the file on open
	f, err := os.OpenFile(fn, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		// fmt.Fprintf(os.Stderr, "overwriteJSONLine: open %s: %v\n", fn, err)
		return
	}
	defer f.Close()

	line, err := json.Marshal(v)
	if err != nil {
		// fmt.Fprintf(os.Stderr, "overwriteJSONLine: marshal: %v\n", err)
		return
	}
	if _, err := f.Write(line); err != nil {
		// fmt.Fprintf(os.Stderr, "overwriteJSONLine: write: %v\n", err)
		return
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		// fmt.Fprintf(os.Stderr, "overwriteJSONLine: write newline: %v\n", err)
	}
}

// specialized helper for logs (includes app in filename)
func (d *DiskStorage) appendLogLine(job, target, app string, v interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	fn := filepath.Join(d.dir, fmt.Sprintf("logs_%s_%s_%s.jsonl", job, target, app))
	f, err := os.OpenFile(fn, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		// fmt.Fprintf(os.Stderr, "appendLogLine: open %s: %v\n", fn, err)
		return
	}
	defer f.Close()

	line, err := json.Marshal(v)
	if err != nil {
		// fmt.Fprintf(os.Stderr, "appendLogLine: marshal: %v\n", err)
		return
	}
	f.Write(line)
	f.Write([]byte("\n"))
}

// --- storage.Store implementation ---

func (d *DiskStorage) QueryMetrics(q QueryParams) ([]json.RawMessage, error) {
	return d.queryJSONLines("metrics", q.Job, q.Target, q.Start, q.End)
}
func (d *DiskStorage) QueryApps(q QueryParams) ([]json.RawMessage, error) {
	return d.queryAppLines("processes", q.Job, q.Target, q.Start, q.End)
}
func (d *DiskStorage) QueryProcesses(q ProcessQuery) ([]json.RawMessage, error) {
	return d.queryJSONLines("processes", q.Job, q.Target, q.Start, q.End)
}

func (d *DiskStorage) ListAllTargets() ([]json.RawMessage, error) {
	return d.queryTarget()
}

func (d *DiskStorage) ListJobsByTarget(q JobQuery) ([]json.RawMessage, error) {
	return d.queryJobByTarget(q.Target)
}

func (d *DiskStorage) QueryLogs(q LogQuery) ([]logRecord, error) {
	var records []logRecord

	// Determine file‐matching pattern
	var pattern string
	if q.App == "" {
		pattern = fmt.Sprintf("logs_%s_%s_*.jsonl", q.Job, q.Target)
	} else {
		pattern = fmt.Sprintf("logs_%s_%s_%s.jsonl", q.Job, q.Target, q.App)
	}

	// 1) Glob for matching files
	matches, err := filepath.Glob(filepath.Join(d.dir, pattern))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return []logRecord{}, nil
	}

	// 2) Sort file names lexicographically so "oldest" come first
	sort.Strings(matches)

	// If NumLines <= 0, return empty slice (or you can choose to return all lines)
	N := q.NumLines
	if N <= 0 {
		return []logRecord{}, nil
	}

	// 3) Create a circular buffer to hold the last N raw JSON lines
	buffer := make([][]byte, N)
	startIdx := 0
	count := 0

	// 4) Iterate each file in order, streaming line by line
	for _, fn := range matches {
		f, err := os.Open(fn)
		if err != nil {
			// skip if file doesn’t exist; but abort on other errors
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			raw := append([]byte(nil), scanner.Bytes()...) // copy bytes

			if count < N {
				buffer[count] = raw
				count++
			} else {
				// overwrite the oldest slot
				buffer[startIdx] = raw
				startIdx = (startIdx + 1) % N
			}
		}
		f.Close()
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}

	// 5) Now “count” holds how many total lines we saw (capped at N), and
	//    startIdx points at where the oldest buffered line sits.
	//    We need to read them in chronological order:
	for i := 0; i < count; i++ {
		idx := (startIdx + i) % N
		raw := buffer[idx]

		// Parse the JSONL line into an intermediate struct
		var recJSON struct {
			Timestamp string `json:"timestamp"`
			App       string `json:"app"`
			Line      string `json:"line"`
		}
		if err := json.Unmarshal(raw, &recJSON); err != nil {
			// skip any invalid JSON
			continue
		}

		records = append(records, logRecord{
			Timestamp: recJSON.Timestamp,
			App:       recJSON.App,
			Line:      recJSON.Line,
		})
	}

	return records, nil
}

// shared JSON-lines reader for metrics & processes, returns only names
func (d *DiskStorage) queryAppLines(kind, job, target string, start, end time.Time) ([]json.RawMessage, error) {
	fn := filepath.Join(d.dir, fmt.Sprintf("%s_%s_%s.jsonl", kind, job, target))
	// fmt.Println(fn)
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

		// 1) peek at the timestamp
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
			// 2) now extract just the .data[].name fields
			var payload struct {
				Data []struct {
					Name string `json:"name"`
				} `json:"data"`
			}
			if err := json.Unmarshal(line, &payload); err != nil {
				continue
			}
			for _, d := range payload.Data {
				// marshal each name into {"name":"..."}
				nm, err := json.Marshal(struct {
					Name string `json:"name"`
				}{Name: d.Name})
				if err != nil {
					continue
				}
				results = append(results, json.RawMessage(nm))
			}
		}
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		return nil, err
	}
	return results, nil
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
