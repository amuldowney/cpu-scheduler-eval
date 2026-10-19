package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"cpu-scheduler-eval/internal/linuxobs"
)

func TestLinuxObserverReadsCgroupV2Snapshot(t *testing.T) {
	cgroup := t.TempDir()
	files := map[string]string{
		"cpu.weight":   "250\n",
		"cpu.stat":     "usage_usec 1200\nuser_usec 900\nsystem_usec 300\nnr_throttled 2\nthrottled_usec 40\n",
		"cpu.pressure": "some avg10=1.25 avg60=2.5 avg300=3.75 total=900\nfull avg10=0.5 avg60=1 avg300=1.5 total=200\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(cgroup, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := readLinuxCgroup(cgroup)
	if err != nil {
		t.Fatal(err)
	}
	if got.CPUWeight != 250 || got.CPUUsageUsec != 1200 || got.CPUThrottledUsec != 40 || got.CPUThrottledPeriods != 2 {
		t.Fatalf("unexpected cgroup counters: %+v", got)
	}
	if got.Pressure.Some.Total != 900 || got.Pressure.Full.Total != 200 || got.Pressure.Some.Avg10 != 1.25 {
		t.Fatalf("unexpected cgroup pressure: %+v", got.Pressure)
	}
}

func TestLinuxTraceSessionConvertsExactRoleSamples(t *testing.T) {
	session := linuxTraceSession{collector: &fakeLinuxCollector{samples: []linuxobs.Sample{
		{Role: linuxobs.RoleBorrower, DelayNS: 11},
		{Role: linuxobs.RoleVictim, DelayNS: 23},
		{Role: linuxobs.RoleVictim, DelayNS: 29},
	}}}
	got, err := session.Stop()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]float64{"borrower": {11}, "victim": {23, 29}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("trace samples = %#v, want %#v", got, want)
	}
}

func TestLinuxObserverTraceUnavailableWithoutBPFTrace(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := (LinuxObserver{}).StartTrace(context.Background())
	if err == nil {
		t.Fatal("StartTrace() unexpectedly succeeded without bpftrace")
	}
	if !errors.Is(err, linuxobs.ErrUnavailable) {
		t.Fatalf("StartTrace() error = %v, want unavailable error", err)
	}
}

type fakeLinuxCollector struct {
	samples []linuxobs.Sample
}

func (c *fakeLinuxCollector) Stop() ([]linuxobs.Sample, error) { return c.samples, nil }
