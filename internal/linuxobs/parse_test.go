package linuxobs

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParsePressure(t *testing.T) {
	got, err := ParsePressure([]byte("some avg10=0.12 avg60=1.25 avg300=0.50 total=9876\nfull avg10=0.00 avg60=0.01 avg300=0.02 total=12\n"))
	if err != nil {
		t.Fatalf("ParsePressure: %v", err)
	}
	if got.Some != (PressureLine{Avg10: 0.12, Avg60: 1.25, Avg300: 0.50, TotalUsec: 9876}) {
		t.Fatalf("unexpected some pressure: %+v", got.Some)
	}
	if got.Full == nil || *got.Full != (PressureLine{Avg10: 0, Avg60: 0.01, Avg300: 0.02, TotalUsec: 12}) {
		t.Fatalf("unexpected full pressure: %+v", got.Full)
	}
}

func TestParsePressureRejectsMalformedOrMissingSome(t *testing.T) {
	for _, input := range []string{
		"",
		"full avg10=0 avg60=0 avg300=0 total=0\n",
		"some avg10=NaN avg60=0 avg300=0 total=0\n",
		"some avg10=0 avg60=0 avg300=0 total=not-a-number\n",
		"some avg10=0 avg60=0 avg300=0 total=0\nsome avg10=0 avg60=0 avg300=0 total=0\n",
	} {
		if _, err := ParsePressure([]byte(input)); err == nil {
			t.Errorf("ParsePressure(%q) unexpectedly succeeded", input)
		}
	}
}

func TestParseCPUStat(t *testing.T) {
	got, err := ParseCPUStat([]byte("usage_usec 1234\nuser_usec 1000\nsystem_usec 234\nnr_periods 9\nnr_throttled 2\nthrottled_usec 56\nfuture_counter 7\n"))
	if err != nil {
		t.Fatalf("ParseCPUStat: %v", err)
	}
	want := CPUStat{
		"usage_usec": 1234, "user_usec": 1000, "system_usec": 234,
		"nr_periods": 9, "nr_throttled": 2, "throttled_usec": 56, "future_counter": 7,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseCPUStatRejectsMalformedOrMissingUsage(t *testing.T) {
	for _, input := range []string{
		"",
		"user_usec 1\n",
		"usage_usec nope\n",
		"usage_usec 1 extra\n",
		"usage_usec 1\nusage_usec 2\n",
	} {
		if _, err := ParseCPUStat([]byte(input)); err == nil {
			t.Errorf("ParseCPUStat(%q) unexpectedly succeeded", input)
		}
	}
}

func TestSnapshotKeepsUnavailableMeasurementsExplicit(t *testing.T) {
	dir := t.TempDir()
	hostPath := filepath.Join(dir, "host.pressure")
	cgroupDir := filepath.Join(dir, "cgroup")
	if err := os.Mkdir(cgroupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostPath, []byte("some avg10=0 avg60=0 avg300=0 total=0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cgroupDir, "cpu.stat"), []byte("usage_usec 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := readSnapshotAt(hostPath, cgroupDir)
	if !got.HostPressure.Available || !got.CPUStat.Available {
		t.Fatalf("valid measurements unavailable: %+v", got)
	}
	if got.CgroupPressure.Available || strings.TrimSpace(got.CgroupPressure.Reason) == "" {
		t.Fatalf("missing cgroup pressure must have an explicit unavailable reason: %+v", got.CgroupPressure)
	}

	missing := readSnapshotAt(filepath.Join(dir, "missing.pressure"), filepath.Join(dir, "missing-cgroup"))
	for name, measurement := range map[string]Measurement[any]{
		"host pressure":   eraseMeasurement(missing.HostPressure),
		"cgroup pressure": eraseMeasurement(missing.CgroupPressure),
		"cpu stat":        eraseMeasurement(missing.CPUStat),
	} {
		if measurement.Available || strings.TrimSpace(measurement.Reason) == "" {
			t.Errorf("%s missing measurement lacks unavailable reason: %+v", name, measurement)
		}
	}
}

func TestCurrentSnapshotAlwaysExplainsUnavailableMeasurements(t *testing.T) {
	snapshot := CurrentSnapshot()
	if !snapshot.HostPressure.Available && strings.TrimSpace(snapshot.HostPressure.Reason) == "" {
		t.Fatal("unavailable host pressure has no reason")
	}
	if !snapshot.CgroupPressure.Available && strings.TrimSpace(snapshot.CgroupPressure.Reason) == "" {
		t.Fatal("unavailable cgroup pressure has no reason")
	}
	if !snapshot.CPUStat.Available && strings.TrimSpace(snapshot.CPUStat.Reason) == "" {
		t.Fatal("unavailable cpu.stat has no reason")
	}
}

func eraseMeasurement[T any](m Measurement[T]) Measurement[any] {
	return Measurement[any]{Available: m.Available, Reason: m.Reason}
}

func TestParseDelayLineAndGroupByRole(t *testing.T) {
	borrower, err := ParseDelayLine("linuxobs_delay borrower 123456")
	if err != nil {
		t.Fatalf("ParseDelayLine: %v", err)
	}
	victim, err := ParseDelayLine("linuxobs_delay victim 987654")
	if err != nil {
		t.Fatalf("ParseDelayLine: %v", err)
	}
	if borrower != (Sample{Role: RoleBorrower, DelayNS: 123456}) || victim != (Sample{Role: RoleVictim, DelayNS: 987654}) {
		t.Fatalf("unexpected samples: %+v, %+v", borrower, victim)
	}

	got := GroupByRole([]Sample{borrower, victim, borrower})
	if !reflect.DeepEqual(got.Borrower, []uint64{123456, 123456}) || !reflect.DeepEqual(got.Victim, []uint64{987654}) {
		t.Fatalf("unexpected role samples: %+v", got)
	}
}

func TestParseDelayLineRejectsMalformedEvents(t *testing.T) {
	for _, input := range []string{
		"",
		"other borrower 1",
		"linuxobs_delay helper 1",
		"linuxobs_delay borrower -1",
		"linuxobs_delay victim not-a-number",
		"linuxobs_delay victim 1 extra",
	} {
		if _, err := ParseDelayLine(input); err == nil {
			t.Errorf("ParseDelayLine(%q) unexpectedly succeeded", input)
		}
	}
}
