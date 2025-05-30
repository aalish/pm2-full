package discovery

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aalish/pm2-full/internal/config"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

// Store is the interface your discovery layer uses to hand off data
type Store interface {
	StoreMetrics(job, target string, mfs map[string]*dto.MetricFamily)
	StoreProcesses(job, target string, data []byte)
	StoreLog(job, target, line string)
}

// Start kicks off a perpetual scrape loop for one Job
func Start(job config.Job, store Store) {
	log.Printf("Starting job %q with interval %s", job.JobName, job.Interval)
	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()

	// initial scrape before waiting
	scrape(job, store)

	for range ticker.C {
		log.Printf("Running scrape for job %q", job.JobName)
		scrape(job, store)
	}
}

// scrape fetches metrics, processes, and tail logs in background
func scrape(job config.Job, store Store) {
	for _, t := range job.Targets {
		base := fmt.Sprintf("http://%s:%d", t.Host, t.Port)

		// metrics
		if mf, err := fetchMetrics(base+job.Paths.Metrics, t.BasicAuth); err == nil {
			log.Printf("Fetched metrics from %s", base+job.Paths.Metrics)
			store.StoreMetrics(job.JobName, t.Host, mf)
		} else {
			log.Printf("metrics fetch error: %v", err)
		}

		// processes JSON
		if data, err := fetchJSON(base + job.Paths.Processes); err == nil {
			log.Printf("Fetched processes JSON from %s", base+job.Paths.Processes)
			store.StoreProcesses(job.JobName, t.Host, data)
		} else {
			log.Printf("process fetch error: %v", err)
		}

		// logs tailing in background (reconnect on error)
		go func(target config.Target) {
			url := base + job.Paths.Logs
			for {
				log.Printf("Attempting to tail logs from %s", url)
				if err := tail(url, target, store, job.JobName); err != nil {
					log.Printf("tail error: %v", err)
					time.Sleep(5 * time.Second)
					continue
				}
				log.Printf("tail ended for %s, reconnecting...", url)
			}
		}(t)
	}
}

// fetchMetrics scrapes Prometheus-style text format and parses it
func fetchMetrics(url string, auth config.AuthCreds) (map[string]*dto.MetricFamily, error) {
	req, _ := http.NewRequest("GET", url, nil)
	if auth.Username != "" {
		req.SetBasicAuth(auth.Username, auth.Password)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	parser := &expfmt.TextParser{}
	return parser.TextToMetricFamilies(resp.Body)
}

// fetchJSON fetches a JSON endpoint into a raw byte slice
func fetchJSON(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// tail connects to a Server-Sent Events (SSE) or text-stream log endpoint and
// scans new lines, handing each one to StoreLog immediately
func tail(url string, t config.Target, store Store, jobName string) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "text/event-stream")
	if t.BasicAuth.Username != "" {
		req.SetBasicAuth(t.BasicAuth.Username, t.BasicAuth.Password)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}
	log.Printf("Connected to %s, scanning logs...", url)

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			raw := strings.TrimRight(line, "\r\n")
			log.Printf("RAW LINE: %q", raw)
			store.StoreLog(jobName, t.Host, raw)
		}
		if err != nil {
			if err == io.EOF {
				log.Printf("EOF reached for %s", url)
				return nil
			}
			return fmt.Errorf("scanner error: %w", err)
		}
	}
}
