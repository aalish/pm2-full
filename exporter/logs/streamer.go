// logs/streamer.go
package logs

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Streamer struct {
	paths []string
	mu    sync.Mutex
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

	ctx := r.Context()
	for _, pattern := range s.paths {
		files, err := filepath.Glob(pattern)
		if err != nil {
			log.Printf("glob error for pattern %q: %v", pattern, err)
			continue
		}
		for _, f := range files {
			go streamFile(ctx, f, w, flusher, &s.mu)
		}
	}

	<-ctx.Done()
}

// streamFile reads from the file (including existing content) and streams each line
func streamFile(ctx context.Context, path string, w io.Writer, flusher http.Flusher, mu *sync.Mutex) {
	file, err := os.Open(path)
	if err != nil {
		log.Printf("failed to open %s: %v", path, err)
		return
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	appName := extractAppName(path)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			log.Printf("error reading %s: %v", path, err)
			return
		}

		prefixed := fmt.Sprintf("[%s] %s", appName, line)
		mu.Lock()
		_, writeErr := w.Write([]byte(prefixed))
		flusher.Flush()
		mu.Unlock()
		if writeErr != nil {
			log.Printf("error writing to client: %v", writeErr)
			return
		}
	}
}

// extractAppName derives the application name from the filename
func extractAppName(path string) string {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, "-out.log")
	name = strings.TrimSuffix(name, "-error.log")
	name = strings.TrimSuffix(name, filepath.Ext(name))
	return name
}
