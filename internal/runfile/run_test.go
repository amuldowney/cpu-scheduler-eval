package runfile

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRunArtifactRoundTripPreservesRoleMetricsAndUnavailableStatus(t *testing.T) {
	input := Run{
		SchemaVersion: 1,
		ID:            "trial-001",
		Policy:        "default",
		Roles: map[string]RoleMetrics{
			"victim": {
				Operations:            20,
				ApplicationLatency:    Latency{Count: 20, Mean: 1000, P90: 1800, P99: 2000, P999: 2000},
				GoSchedulerLatency:    Latency{Count: 3, Mean: 500, P90: 800, P99: 800, P999: 800},
				LinuxSchedulingStatus: MetricStatus{Available: false, Reason: "bpftrace not installed"},
			},
		},
	}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "bpftrace not installed") {
		t.Fatalf("unavailable reason missing from artifact: %s", encoded)
	}
	var got Run
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
	if got.Roles["victim"].ApplicationLatency.P999 != 2000 || got.Roles["victim"].LinuxSchedulingStatus.Available {
		t.Fatalf("round trip lost metrics or availability state: %+v", got.Roles["victim"])
	}
}

func TestUnavailableTelemetryDoesNotSerializeZeroMeasurements(t *testing.T) {
	input := Run{
		SchemaVersion: 1,
		ID:            "unavailable",
		Policy:        "baseline",
		Roles: map[string]RoleMetrics{
			"victim": {
				LinuxSchedulingStatus: MetricStatus{Available: false, Reason: "no tracer"},
				CPUPressureStatus:     MetricStatus{Available: false, Reason: "no cgroup"},
			},
		},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"linux_scheduling_latency"`, `"cpu_pressure"`, `"host_cpu_pressure"`} {
		if strings.Contains(string(encoded), key) {
			t.Fatalf("unavailable measurement %s serialized as a zero value: %s", key, encoded)
		}
	}
}

func TestRunArtifactRejectsMissingSchemaAndRoles(t *testing.T) {
	if err := (Run{}).Validate(); err == nil {
		t.Fatal("expected empty run artifact to be rejected")
	}
	if err := (Run{SchemaVersion: 1, ID: "empty"}).Validate(); err == nil {
		t.Fatal("expected artifact without roles to be rejected")
	}
}
