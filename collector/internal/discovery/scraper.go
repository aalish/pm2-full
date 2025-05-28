// internal/discovery/scraper.go
package discovery

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
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
	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()

	// initial scrape before waiting
	scrape(job, store)

	for range ticker.C {
		scrape(job, store)
	}
}

func scrape(job config.Job, store Store) {
	for _, t := range job.Targets {
		base := fmt.Sprintf("http://%s:%d", t.Host, t.Port)

		// metrics
		if mf, err := fetchMetrics(base+job.Paths.Metrics, t.BasicAuth); err == nil {
			store.StoreMetrics(job.JobName, t.Host, mf)
		}

		// processes JSON
		if data, err := fetchJSON(base + job.Paths.Processes); err == nil {
			store.StoreProcesses(job.JobName, t.Host, data)
		}

		// logs tailing in background (reconnect on error)
		go func(target config.Target) {
			for {
				if err := tail(base+job.Paths.Logs, target, store, job.JobName); err != nil {
					time.Sleep(5 * time.Second)
					continue
				}
				break
			}
		}(t)
	}
}

// fetchMetrics scrapes Prometheus‐style text format and parses it into MetricFamily objects.
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

// fetchJSON fetches a JSON endpoint (e.g. your /processes) into a raw byte slice.
func fetchJSON(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// tail connects to a Server‐Sent Events (SSE) or text‐stream log endpoint and
// scans new lines, handing each one to StoreLog.
func tail(url string, t config.Target, store Store, jobName string) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Accept", "text/event-stream")
	if t.BasicAuth.Username != "" {
		req.SetBasicAuth(t.BasicAuth.Username, t.BasicAuth.Password)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		store.StoreLog(jobName, t.Host, scanner.Text())
	}
	return scanner.Err()
}
