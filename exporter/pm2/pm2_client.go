package pm2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type ProcessInfo struct {
	Name         string            `json:"name"`
	PM2Id        int               `json:"pm_id"`
	PID          int               `json:"pid"`
	Status       string            `json:"status"`
	Monit        struct{ CPU, Memory float64 } `json:"monit"`
	PM2Env       struct {
		ExecArgs []string          `json:"exec_args"`
		Cwd      string            `json:"cwd"`
		Env      map[string]interface{} `json:"env"`
	} `json:"pm2_env"`
	CreatedAt    int64 `json:"created_at"`
	RestartCount int   `json:"restart_time"`
	Uptime       int64 `json:"pm_uptime"`
}

type Client struct {
	socketPath string
	interval   time.Duration
}

// NewClient creates a PM2 control client
func NewClient(socketPath string, interval time.Duration) *Client {
	return &Client{socketPath: socketPath, interval: interval}
}

// List retrieves the current PM2 process list, capturing stderr for better diagnostics
func (c *Client) List() ([]ProcessInfo, error) {
    // Capture stderr alongside stdout
    cmd := exec.Command("pm2", "jlist")
    var stderr bytes.Buffer
    cmd.Stderr = &stderr

    out, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("pm2 jlist failed: %v; stderr=%s", err, stderr.String())
    }

    var procs []ProcessInfo
    if err := json.Unmarshal(out, &procs); err != nil {
        return nil, fmt.Errorf("failed to parse PM2 JSON (%d bytes): %v", len(out), err)
    }
    return procs, nil
}


// StartPolling emits process snapshots on the provided channel
func (c *Client) StartPolling(ch chan<- []ProcessInfo) {
	ticker := time.NewTicker(c.interval)
	go func() {
		for range ticker.C {
			if procs, err := c.List(); err == nil {
				ch <- procs
			}
		}
	}()
}
