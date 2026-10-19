package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cpu-scheduler-eval/internal/runfile"
)

type emptyTraceObserver struct{}
type emptyTraceSession struct{}

func (emptyTraceObserver) StartTrace(context.Context) (TraceSession, error) {
	return emptyTraceSession{}, nil
}
func (emptyTraceObserver) ReadCgroup(string) (CgroupSnapshot, error) {
	return CgroupSnapshot{}, nil
}
func (emptyTraceObserver) ReadHostPressure() (runfile.Pressure, error) {
	return runfile.Pressure{}, nil
}
func (emptyTraceSession) Stop() (map[string][]float64, error) {
	return map[string][]float64{"borrower": {}, "victim": {}}, nil
}

func TestEmptyLinuxTraceSamplesStayUnavailableWithoutZeroLatency(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-workload")
	body := "#!/bin/sh\nprintf '%s\\n' '{\"role\":\"victim\",\"operations\":1,\"throughput_ops_per_sec\":1,\"application_latency\":{\"count\":1},\"go_scheduler_latency\":{\"count\":1}}'\n"
	if err := os.WriteFile(binary, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.WorkloadBinary = binary
	config.Output = filepath.Join(dir, "runs")
	config.StartDelay = 0
	config.Borrower.Duration = time.Second
	config.Victim.Duration = time.Second

	got, err := Execute(context.Background(), config, emptyTraceObserver{})
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"borrower", "victim"} {
		metrics := got.Roles[role]
		if metrics.LinuxSchedulingStatus.Available || metrics.LinuxSchedulingLatency != nil {
			t.Fatalf("empty trace was represented as data for %s: %+v", role, metrics)
		}
		if metrics.LinuxSchedulingStatus.Reason != "no matching scheduler events" {
			t.Fatalf("missing trace reason for %s: %+v", role, metrics.LinuxSchedulingStatus)
		}
	}
}
