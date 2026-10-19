package runner

import "testing"

func TestConfigRejectsRequestsAboveAllocatableCapacity(t *testing.T) {
	config := DefaultConfig()
	config.WorkloadBinary = "/tmp/workload"
	config.AllocatableMilliCPU = 999
	if err := config.Validate(); err == nil {
		t.Fatal("expected a request sum above allocatable capacity to fail")
	}
}

func TestConfigAcceptsFitRequestsAtHalfCPUMinimum(t *testing.T) {
	config := DefaultConfig()
	config.WorkloadBinary = "/tmp/workload"
	config.AllocatableMilliCPU = 1000
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultBorrowerWorkloadUsesNodeCapacity(t *testing.T) {
	config := DefaultConfig()
	borrowerMilliCPU := int64(config.Borrower.Workers) * int64(1000) * int64(config.Borrower.CPUTime) / int64(config.Borrower.Period)
	if borrowerMilliCPU != int64(config.AllocatableMilliCPU)*190/100 {
		t.Fatalf("default borrower demand = %dm, want 190%% of allocatable %dm", borrowerMilliCPU, config.AllocatableMilliCPU)
	}
}

func TestDefaultVictimWorkloadNearlyConsumesItsRequest(t *testing.T) {
	config := DefaultConfig()
	requestLoadMilliCPU := int64(1000) * int64(config.Victim.CPUTime) / int64(config.Victim.Period)
	if requestLoadMilliCPU != 450 || requestLoadMilliCPU > int64(config.VictimRequestMilliCPU) {
		t.Fatalf("default victim load = %dm, request = %dm", requestLoadMilliCPU, config.VictimRequestMilliCPU)
	}
}

func TestConfigRequiresMatchedMeasurementWindows(t *testing.T) {
	config := DefaultConfig()
	config.Borrower.Duration += 1
	if err := config.Validate(); err == nil {
		t.Fatal("expected mismatched role measurement windows to fail")
	}
}

func TestConfigRejectsMissingPolicy(t *testing.T) {
	config := DefaultConfig()
	config.WorkloadBinary = "/tmp/workload"
	config.Policy = ""
	if err := config.Validate(); err == nil {
		t.Fatal("expected missing policy to fail")
	}
}
