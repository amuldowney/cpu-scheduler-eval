package runner

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExecuteRecordsCgroupAndPressureDeltasPerRole(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-workload")
	body := "#!/bin/sh\nprintf '%s\\n' '{\"role\":\"victim\",\"operations\":1,\"throughput_ops_per_sec\":1,\"application_latency\":{\"count\":1},\"go_scheduler_latency\":{\"count\":1}}'\n"
	if err := os.WriteFile(binary, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	borrowerGroup := filepath.Join(dir, "borrower")
	victimGroup := filepath.Join(dir, "victim")
	for _, path := range []string{borrowerGroup, victimGroup} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "cgroup.procs"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	config := DefaultConfig()
	config.WorkloadBinary = binary
	config.Output = filepath.Join(dir, "runs")
	config.StartDelay = 100 * time.Millisecond
	config.Borrower.Duration = time.Second
	config.Victim.Duration = time.Second
	config.BorrowerCgroup = borrowerGroup
	config.VictimCgroup = victimGroup
	observer := &cgroupObserver{reads: make(map[string]int)}
	got, err := Execute(context.Background(), config, observer)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{borrowerGroup, victimGroup} {
		pid, err := os.ReadFile(filepath.Join(path, "cgroup.procs"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := strconv.Atoi(strings.TrimSpace(string(pid))); err != nil {
			t.Fatalf("workload PID was not attached to %s: %q", path, pid)
		}
	}
	for _, role := range []string{"borrower", "victim"} {
		metrics := got.Roles[role]
		if !metrics.CPUStatStatus.Available || metrics.CPUWeight != 250 || metrics.CPUUsageUsec != 200 || metrics.CPUThrottledUsec != 30 || metrics.CPUThrottledPeriods != 3 {
			t.Errorf("%s CPU stat delta incorrect: %+v", role, metrics)
		}
		if !metrics.CPUPressureStatus.Available || metrics.CPUPressure == nil || metrics.CPUPressure.Some.Avg10 != 2.5 || metrics.CPUPressure.Some.Total != 100 {
			t.Errorf("%s pressure delta incorrect: %+v", role, metrics)
		}
	}
}
