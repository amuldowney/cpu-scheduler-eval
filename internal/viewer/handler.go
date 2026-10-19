package viewer

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"cpu-scheduler-eval/internal/runfile"
)

//go:embed web/index.html
var assets embed.FS

// NewHandler serves the dashboard and validated run artifacts from runsDir.
func NewHandler(runsDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := fs.ReadFile(assets, "web/index.html")
		if err != nil {
			http.Error(w, "dashboard asset unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, _ *http.Request) {
		payload := readRuns(runsDir)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(payload)
	})
	return mux
}

type skippedRun struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

type runList struct {
	Runs    []runfile.Run `json:"runs"`
	Skipped []skippedRun  `json:"skipped"`
}

func readRuns(runsDir string) runList {
	payload := runList{Runs: []runfile.Run{}, Skipped: []skippedRun{}}
	paths, err := filepath.Glob(filepath.Join(runsDir, "*.json"))
	if err != nil {
		payload.Skipped = append(payload.Skipped, skippedRun{File: runsDir, Reason: err.Error()})
		return payload
	}
	sort.Strings(paths)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			payload.Skipped = append(payload.Skipped, skippedRun{File: filepath.Base(path), Reason: err.Error()})
			continue
		}
		var run runfile.Run
		if err := json.Unmarshal(data, &run); err != nil {
			payload.Skipped = append(payload.Skipped, skippedRun{File: filepath.Base(path), Reason: err.Error()})
			continue
		}
		if err := run.Validate(); err != nil {
			payload.Skipped = append(payload.Skipped, skippedRun{File: filepath.Base(path), Reason: err.Error()})
			continue
		}
		payload.Runs = append(payload.Runs, run)
	}
	return payload
}
