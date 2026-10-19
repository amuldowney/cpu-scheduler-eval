package viewer

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cpu-scheduler-eval/internal/runfile"
)

func TestRunsEndpointLoadsValidArtifactsAndSkipsInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	valid := runfile.Run{
		SchemaVersion: runfile.CurrentSchemaVersion,
		ID:            "trial-1",
		Policy:        "baseline",
		Scenario:      "burst",
		Roles: map[string]runfile.RoleMetrics{
			"victim": {Operations: 1},
		},
	}
	contents, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "trial-1.json"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	NewHandler(dir).ServeHTTP(recorder, httptest.NewRequest("GET", "/api/runs", nil))
	if recorder.Code != 200 {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "trial-1") || !strings.Contains(recorder.Body.String(), "broken.json") {
		t.Fatalf("valid run or skipped-file diagnostic missing: %s", recorder.Body.String())
	}
}

func TestDashboardServesAccessibleInitialState(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewHandler(t.TempDir()).ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 200 {
		t.Fatalf("status = %d", recorder.Code)
	}
	for _, want := range []string{"CPU scheduler trials", "p99.9", "Load run artifacts", "Go wait mean / p90 / p99 / p99.9", "Linux wait mean / p90 / p99 / p99.9", "CPU PSI full avg10/60/300", "Request / offered / cgroup weight", "Operations / elapsed", "Effective GOMAXPROCS"} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}
