package linuxobs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartCollectorReturnsUnavailableWhenBPFTraceIsMissing(t *testing.T) {
	if _, err := exec.LookPath("bpftrace"); err == nil {
		t.Skip("bpftrace is installed; do not start a real scheduler trace in unit tests")
	}
	_, err := StartCollector(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unavailable error, got %v", err)
	}
	if !strings.Contains(err.Error(), "bpftrace executable not found") {
		t.Fatalf("missing-tool error should explain unavailability: %v", err)
	}
}

func TestStartCollectorReturnsUnavailableForPermissionFailure(t *testing.T) {
	executable := writeFakeBPFTrace(t, "#!/bin/sh\necho 'ERROR: permission denied attaching sched tracepoint' >&2\nexit 1\n")
	_, err := startCollectorAt(context.Background(), executable)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unavailable error, got %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "permission denied") {
		t.Fatalf("unavailable error should include permission diagnostic: %v", err)
	}
}

func TestCollectorParsesRawEventsAndStopsCleanly(t *testing.T) {
	executable := writeFakeBPFTrace(t, "#!/bin/sh\nif [ \"$1\" = \"--dry-run\" ]; then exit 0; fi\necho linuxobs_ready\necho 'linuxobs_delay borrower 11'\necho 'linuxobs_delay victim 22'\ntrap 'exit 0' INT\nwhile :; do sleep 0.05; done\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector, err := startCollectorAt(ctx, executable)
	if err != nil {
		t.Fatalf("start fake collector: %v", err)
	}
	cancel()
	select {
	case <-collector.done:
	case <-time.After(3 * time.Second):
		t.Fatal("context cancellation did not stop fake bpftrace process")
	}
	samples, err := collector.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if len(samples) != 2 || samples[0] != (Sample{Role: RoleBorrower, DelayNS: 11}) || samples[1] != (Sample{Role: RoleVictim, DelayNS: 22}) {
		t.Fatalf("unexpected raw samples: %+v", samples)
	}
}

func writeFakeBPFTrace(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bpftrace-fake")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
