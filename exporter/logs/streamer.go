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

// StreamHandler streams only new log lines (no historical content)
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

// streamFile seeks to the end and then streams only new lines as they arrive
func streamFile(ctx context.Context, path string, w io.Writer, flusher http.Flusher, mu *sync.Mutex) {
	file, err := os.Open(path)
	if err != nil {
		log.Printf("failed to open %s: %v", path, err)
		return
	}
	defer file.Close()

	// Seek to end to skip existing content
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		log.Printf("failed to seek to end of %s: %v", path, err)
		return
	}

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

		// Prefix and write the new line
		prefixed := fmt.Sprintf("[%s] %s", appName, line)
		mu.Lock()
		if _, writeErr := w.Write([]byte(prefixed)); writeErr != nil {
			mu.Unlock()
			log.Printf("error writing to client: %v", writeErr)
			return
		}
		flusher.Flush()
		mu.Unlock()
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
