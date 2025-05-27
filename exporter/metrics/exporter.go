package metrics

import (
	"fmt"
	"net/http"

	"github.com/aalish/pm2-full/pm2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Exporter struct {
	procCh   chan []pm2.ProcessInfo
	cpuGauge *prometheus.GaugeVec
	memGauge *prometheus.GaugeVec
}

// NewExporter registers metrics and begins polling
func NewExporter(client *pm2.Client) *Exporter {
	cpu := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Namespace: "pm2_exporter", Name: "process_cpu_percent", Help: "CPU usage percent"},
		[]string{"name", "pm2_id", "pid"},
	)
	mem := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Namespace: "pm2_exporter", Name: "process_memory_bytes", Help: "Memory usage in bytes"},
		[]string{"name", "pm2_id", "pid"},
	)
	prometheus.MustRegister(cpu, mem)

	ch := make(chan []pm2.ProcessInfo)
	client.StartPolling(ch)

	go func() {
		for procs := range ch {
			for _, p := range procs {
				labels := prometheus.Labels{"name": p.Name, "pm2_id": fmt.Sprint(p.PM2Id), "pid": fmt.Sprint(p.PID)}
				cpu.With(labels).Set(p.Monit.CPU)
				mem.With(labels).Set(p.Monit.Memory)
			}
		}
	}()

	return &Exporter{procCh: ch, cpuGauge: cpu, memGauge: mem}
}

// Handler returns the Prometheus HTTP handler
func (e *Exporter) Handler() http.Handler {
	return promhttp.Handler()
}
