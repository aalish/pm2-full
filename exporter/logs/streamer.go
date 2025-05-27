// -------------------- logs/streamer.go --------------------
package logs

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Streamer struct {
	paths []string
}

// NewStreamer sets up log file patterns
func NewStreamer(paths []string) *Streamer {
	return &Streamer{paths: paths}
}

// StreamHandler writes existing log lines and streams new ones with [app-name] prefix
func (s *Streamer) StreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	for _, pattern := range s.paths {
		files, _ := filepath.Glob(pattern)
		for _, f := range files {
			go streamFile(f, w, flusher)
		}
	}

	<-r.Context().Done()
}

// streamFile reads from the file (including existing content) and streams each line
func streamFile(path string, w io.Writer, flusher http.Flusher) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	appName := extractAppName(path)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// No new data: wait and retry
				time.Sleep(500 * time.Millisecond)
				continue
			}
			// Other errors: stop this file
			return
		}
		// Prefix with [app-name]
		prefixed := fmt.Sprintf("[%s] %s", appName, line)
		w.Write([]byte(prefixed))
		flusher.Flush()
	}
}

// extractAppName derives the application name from the filename
func extractAppName(path string) string {
	base := filepath.Base(path)
	// Remove common PM2 suffixes
	name := strings.TrimSuffix(base, "-out.log")
	name = strings.TrimSuffix(name, "-error.log")
	// Remove any remaining extension
	name = strings.TrimSuffix(name, filepath.Ext(name))
	return name
}
