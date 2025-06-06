package pm2

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

type EnvMap map[string]interface{}

func (e *EnvMap) UnmarshalJSON(data []byte) error {
	// Try map first
	var asMap map[string]interface{}
	if err := json.Unmarshal(data, &asMap); err == nil {
		if isLikelyBrokenStringMap(asMap) {
			*e = map[string]interface{}{}
			return nil
		}
		*e = asMap
		return nil
	}

	// Try plain string
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		*e = map[string]interface{}{"RAW_ENV": asString}
		return nil
	}

	// Default fallback
	*e = map[string]interface{}{}
	return nil
}

func isLikelyBrokenStringMap(m map[string]interface{}) bool {
	countNumeric := 0
	for k := range m {
		if _, err := strconv.Atoi(k); err == nil {
			countNumeric++
		}
	}
	return countNumeric > len(m)/2
}

type ProcessInfo struct {
	Name   string `json:"name"`
	PM2Id  int    `json:"pm_id"`
	PID    int    `json:"pid"`
	Status string `json:"status"`
	Monit  struct {
		CPU    float64 `json:"cpu"`
		Memory float64 `json:"memory"`
	} `json:"monit"`
	PM2Env struct {
		ExecArgs []string `json:"exec_args"`
		Cwd      string   `json:"cwd"`
		Env      EnvMap   `json:"env"`
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
