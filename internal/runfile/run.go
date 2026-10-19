// Package runfile defines the versioned JSON interchange format for trials.
package runfile

import (
	"errors"
	"fmt"
	"time"
)

const CurrentSchemaVersion = 1

// Run is a single benchmark trial. Role measurements are kept separate so a
// borrower cannot hide regressions in victims (or vice versa).
type Run struct {
	SchemaVersion      int                    `json:"schema_version"`
	ID                 string                 `json:"id"`
	StartedAt          time.Time              `json:"started_at"`
	DurationSecs       float64                `json:"duration_seconds"`
	Policy             string                 `json:"policy"`
	Scenario           string                 `json:"scenario"`
	Host               Host                   `json:"host"`
	HostCPUPressure    *Pressure              `json:"host_cpu_pressure,omitempty"`
	HostPressureStatus MetricStatus           `json:"host_pressure_status"`
	Config             map[string]any         `json:"config,omitempty"`
	Roles              map[string]RoleMetrics `json:"roles"`
	Diagnostics        []string               `json:"diagnostics,omitempty"`
}

type Host struct {
	Hostname      string `json:"hostname,omitempty"`
	KernelRelease string `json:"kernel_release,omitempty"`
	Architecture  string `json:"architecture,omitempty"`
	CPUModel      string `json:"cpu_model,omitempty"`
	LogicalCPUs   int    `json:"logical_cpus,omitempty"`
}

type RoleMetrics struct {
	RequestMilliCPU        int          `json:"request_millicpu"`
	CPUWeight              int          `json:"cpu_weight,omitempty"`
	Operations             int          `json:"operations"`
	MissedReleases         int          `json:"missed_releases"`
	ThroughputOpsPerSec    float64      `json:"throughput_ops_per_sec"`
	ElapsedSecs            float64      `json:"elapsed_seconds"`
	EffectiveGOMAXPROCS    int          `json:"effective_gomaxprocs"`
	ApplicationLatency     Latency      `json:"application_latency"`
	GoSchedulerLatency     Latency      `json:"go_scheduler_latency"`
	LinuxSchedulingLatency *Latency     `json:"linux_scheduling_latency,omitempty"`
	LinuxSchedulingStatus  MetricStatus `json:"linux_scheduling_status"`
	CPUUsageUsec           uint64       `json:"cpu_usage_usec,omitempty"`
	CPUThrottledUsec       uint64       `json:"cpu_throttled_usec,omitempty"`
	CPUThrottledPeriods    uint64       `json:"cpu_throttled_periods,omitempty"`
	CPUStatStatus          MetricStatus `json:"cpu_stat_status"`
	CPUPressure            *Pressure    `json:"cpu_pressure,omitempty"`
	CPUPressureStatus      MetricStatus `json:"cpu_pressure_status"`
	GoSchedulerMethod      string       `json:"go_scheduler_method,omitempty"`
}

// Latency values are nanoseconds. Percentiles use nearest rank for raw samples
// and bucket-midpoint estimates for Go runtime histograms.
type Latency struct {
	Count int     `json:"count"`
	Mean  float64 `json:"mean_ns"`
	P90   float64 `json:"p90_ns"`
	P99   float64 `json:"p99_ns"`
	P999  float64 `json:"p999_ns"`
	Min   float64 `json:"min_ns"`
	Max   float64 `json:"max_ns"`
}

type MetricStatus struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type Pressure struct {
	Some PressureLine `json:"some"`
	Full PressureLine `json:"full"`
}

type PressureLine struct {
	Avg10  float64 `json:"avg10"`
	Avg60  float64 `json:"avg60"`
	Avg300 float64 `json:"avg300"`
	Total  uint64  `json:"total_usec"`
}

func (r Run) Validate() error {
	if r.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", r.SchemaVersion)
	}
	if r.ID == "" {
		return errors.New("run id is required")
	}
	if r.Policy == "" {
		return errors.New("policy is required")
	}
	if len(r.Roles) == 0 {
		return errors.New("at least one role result is required")
	}
	if r.HostPressureStatus.Available != (r.HostCPUPressure != nil) {
		return errors.New("host pressure availability must match presence of host pressure data")
	}
	for role, result := range r.Roles {
		if result.LinuxSchedulingStatus.Available != (result.LinuxSchedulingLatency != nil) {
			return fmt.Errorf("role %q Linux scheduling availability must match data presence", role)
		}
		if result.CPUPressureStatus.Available != (result.CPUPressure != nil) {
			return fmt.Errorf("role %q CPU pressure availability must match data presence", role)
		}
		if role != "borrower" && role != "victim" {
			return fmt.Errorf("unknown workload role %q", role)
		}
		if result.Operations < 0 || result.MissedReleases < 0 || result.RequestMilliCPU < 0 {
			return fmt.Errorf("role %q has negative operation/request counts", role)
		}
		if result.CPUWeight < 0 || result.CPUWeight > 10000 {
			return fmt.Errorf("role %q CPU weight must be in 0..10000", role)
		}
	}
	return nil
}
