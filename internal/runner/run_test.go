package runner

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"cpu-scheduler-eval/internal/runfile"
)

type fakeObserver struct{}

type fakeTrace struct{}

type cgroupObserver struct {
	mu    sync.Mutex
	reads map[string]int
}

func (fakeObserver) StartTrace(context.Context) (TraceSession, error) { return fakeTrace{}, nil }
func (fakeObserver) ReadCgroup(string) (CgroupSnapshot, error) {
	return CgroupSnapshot{CPUWeight: 100, CPUUsageUsec: 250, Pressure: runfile.Pressure{}}, nil
}
func (c *cgroupObserver) StartTrace(context.Context) (TraceSession, error) { return fakeTrace{}, nil }
func (c *cgroupObserver) ReadCgroup(path string) (CgroupSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads[path]++
	if c.reads[path] == 1 {
		return CgroupSnapshot{CPUWeight: 100, CPUUsageUsec: 100, CPUThrottledUsec: 10, CPUThrottledPeriods: 1, Pressure: runfile.Pressure{Some: runfile.PressureLine{Total: 50}}}, nil
	}
	return CgroupSnapshot{CPUWeight: 250, CPUUsageUsec: 300, CPUThrottledUsec: 40, CPUThrottledPeriods: 4, Pressure: runfile.Pressure{Some: runfile.PressureLine{Avg10: 2.5, Total: 150}}}, nil
}
func (c *cgroupObserver) ReadHostPressure() (runfile.Pressure, error) {
	return runfile.Pressure{Some: runfile.PressureLine{Avg10: 1.5, Total: 200}}, nil
}
func (fakeObserver) ReadHostPressure() (runfile.Pressure, error) {
	return runfile.Pressure{Some: runfile.PressureLine{Avg10: 1.25, Total: 100}}, nil
}
func (fakeTrace) Stop() (map[string][]float64, error) {
	return map[string][]float64{"borrower": {10, 20}, "victim": {30, 40}}, nil
}

func TestExecuteWritesSeparateRoleLatencyAndMeasurementAvailability(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-workload")
	body := "#!/bin/sh\nprintf '%s\\n' '{\"role\":\"borrower\",\"operations\":2,\"throughput_ops_per_sec\":3,\"application_latency\":{\"count\":2,\"mean_ns\":15,\"p90_ns\":20,\"p99_ns\":20,\"p999_ns\":20},\"go_scheduler_latency\":{\"count\":1,\"mean_ns\":5,\"p90_ns\":5,\"p99_ns\":5,\"p999_ns\":5},\"go_scheduler_method\":\"test\"}'\n"
	if err := os.WriteFile(binary, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.WorkloadBinary = binary
	config.Output = filepath.Join(dir, "runs")
	config.StartDelay = 0
	config.Borrower.Duration = time.Second
	config.Victim.Duration = time.Second

	got, err := Execute(context.Background(), config, fakeObserver{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Roles["borrower"].LinuxSchedulingLatency == nil || got.Roles["victim"].LinuxSchedulingLatency == nil || got.Roles["borrower"].LinuxSchedulingLatency.P999 != 20 || got.Roles["victim"].LinuxSchedulingLatency.P999 != 40 {
		t.Fatalf("role-specific Linux wait samples were mixed: %+v", got.Roles)
	}
	if !got.HostPressureStatus.Available || got.HostCPUPressure == nil || got.HostCPUPressure.Some.Avg10 != 1.25 {
		t.Fatalf("host pressure not recorded: %+v", got)
	}
	if got.Config["borrower_offered_millicpu"] != offeredMilliCPU(config.Borrower) || got.Config["victim_offered_millicpu"] != offeredMilliCPU(config.Victim) {
		t.Fatalf("offered CPU demand missing from trial metadata: %+v", got.Config)
	}
	if _, err := os.Stat(filepath.Join(config.Output, got.ID+".json")); err != nil {
		t.Fatalf("run artifact not written: %v", err)
	}
}

func TestExecuteWithoutObserverMarksLinuxSignalsUnavailable(t *testing.T) {
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

	got, err := Execute(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.HostPressureStatus.Available || got.Roles["victim"].LinuxSchedulingStatus.Available {
		t.Fatalf("missing observer was presented as measured data: %+v", got)
	}
	if got.Roles["victim"].LinuxSchedulingStatus.Reason == "" {
		t.Fatalf("unavailable Linux measurement needs a reason: %+v", got.Roles["victim"])
	}
}
