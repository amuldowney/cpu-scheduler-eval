package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"cpu-scheduler-eval/internal/metrics"
	"cpu-scheduler-eval/internal/runfile"
	"cpu-scheduler-eval/internal/workload"
)

// Observer supplies optional Linux tracepoint and cgroup observations.
type Observer interface {
	StartTrace(context.Context) (TraceSession, error)
	ReadCgroup(path string) (CgroupSnapshot, error)
	ReadHostPressure() (runfile.Pressure, error)
}

type TraceSession interface {
	Stop() (map[string][]float64, error)
}

type CgroupSnapshot struct {
	CPUWeight           int
	CPUUsageUsec        uint64
	CPUThrottledUsec    uint64
	CPUThrottledPeriods uint64
	Pressure            runfile.Pressure
}

func Execute(ctx context.Context, config Config, observer Observer) (runfile.Run, error) {
	if err := config.Validate(); err != nil {
		return runfile.Run{}, err
	}
	if _, err := os.Stat(config.WorkloadBinary); err != nil {
		return runfile.Run{}, fmt.Errorf("workload binary: %w", err)
	}
	for role, path := range map[string]string{"borrower": config.BorrowerCgroup, "victim": config.VictimCgroup} {
		if path != "" {
			if err := requireEmptyCgroup(path); err != nil {
				return runfile.Run{}, fmt.Errorf("%s cgroup: %w", role, err)
			}
		}
	}

	started := time.Now()
	runID := started.UTC().Format("20060102T150405.000000000Z")
	run := runfile.Run{
		SchemaVersion: runfile.CurrentSchemaVersion,
		ID:            runID,
		StartedAt:     started.Add(config.StartDelay).UTC(),
		Policy:        config.Policy,
		Scenario:      config.Scenario,
		Host:          hostMetadata(),
		Config: map[string]any{
			"scheduler_revision":            config.SchedulerRevision,
			"allocatable_millicpu":          config.AllocatableMilliCPU,
			"borrower_request_millicpu":     config.BorrowerRequestMilliCPU,
			"victim_request_millicpu":       config.VictimRequestMilliCPU,
			"borrower_cgroup":               config.BorrowerCgroup,
			"victim_cgroup":                 config.VictimCgroup,
			"start_delay":                   config.StartDelay.String(),
			"workload_binary":               config.WorkloadBinary,
			"application_percentile_method": "nearest-rank",
			"linux_wait_percentile_method":  "nearest-rank",
			"duration":                      config.Borrower.Duration.String(),
			"borrower_period":               config.Borrower.Period.String(),
			"borrower_cpu_time":             config.Borrower.CPUTime.String(),
			"borrower_workers":              config.Borrower.Workers,
			"borrower_offered_millicpu":     offeredMilliCPU(config.Borrower),
			"borrower_gomaxprocs":           config.Borrower.GOMAXPROCS,
			"victim_period":                 config.Victim.Period.String(),
			"victim_cpu_time":               config.Victim.CPUTime.String(),
			"victim_workers":                config.Victim.Workers,
			"victim_offered_millicpu":       offeredMilliCPU(config.Victim),
			"victim_gomaxprocs":             config.Victim.GOMAXPROCS,
		},
		Roles: map[string]runfile.RoleMetrics{},
	}

	traceUnavailableReason := "no Linux trace samples captured"
	var trace TraceSession
	if observer != nil {
		var err error
		trace, err = observer.StartTrace(ctx)
		if err != nil {
			traceUnavailableReason = err.Error()
			run.Diagnostics = append(run.Diagnostics, "Linux scheduling trace unavailable: "+err.Error())
		}
	} else {
		traceUnavailableReason = "Linux observer not configured"
		run.Diagnostics = append(run.Diagnostics, traceUnavailableReason)
	}

	hostPressureBefore, hostPressureBeforeErr := readHostPressure(observer)
	before := map[string]CgroupSnapshot{}
	beforeErr := map[string]error{}
	for role, path := range map[string]string{"borrower": config.BorrowerCgroup, "victim": config.VictimCgroup} {
		if path == "" || observer == nil {
			continue
		}
		before[role], beforeErr[role] = observer.ReadCgroup(path)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan roleExecution, 2)
	var group sync.WaitGroup
	for _, item := range []struct {
		role    string
		config  workload.Config
		path    string
		request int
	}{
		{role: "borrower", config: config.Borrower, path: config.BorrowerCgroup, request: config.BorrowerRequestMilliCPU},
		{role: "victim", config: config.Victim, path: config.VictimCgroup, request: config.VictimRequestMilliCPU},
	} {
		item := item
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := runRole(ctx, config.WorkloadBinary, item.config, config.StartDelay, item.path)
			results <- roleExecution{role: item.role, request: item.request, result: result, err: err}
		}()
	}
	go func() { group.Wait(); close(results) }()

	var executionErr error
	for completed := range results {
		if completed.err != nil {
			if executionErr == nil {
				executionErr = completed.err
				cancel()
			}
			continue
		}
		role := runfile.RoleMetrics{
			RequestMilliCPU:       completed.request,
			Operations:            completed.result.Operations,
			MissedReleases:        completed.result.MissedReleases,
			ThroughputOpsPerSec:   completed.result.ThroughputOpsPerSec,
			ElapsedSecs:           float64(completed.result.ElapsedNanoseconds) / float64(time.Second),
			EffectiveGOMAXPROCS:   completed.result.EffectiveGOMAXPROCS,
			ApplicationLatency:    convertLatency(completed.result.ApplicationLatency),
			GoSchedulerLatency:    convertLatency(completed.result.GoSchedulerLatency),
			GoSchedulerMethod:     completed.result.GoSchedulerMethod,
			LinuxSchedulingStatus: runfile.MetricStatus{Available: false, Reason: traceUnavailableReason},
			CPUStatStatus:         runfile.MetricStatus{Available: false, Reason: "cgroup telemetry unavailable"},
			CPUPressureStatus:     runfile.MetricStatus{Available: false, Reason: "cgroup telemetry unavailable"},
		}
		if config.BorrowerCgroup != "" && completed.role == "borrower" {
			applyCgroupDelta(&role, before[completed.role], beforeErr[completed.role], config.BorrowerCgroup, observer)
		}
		if config.VictimCgroup != "" && completed.role == "victim" {
			applyCgroupDelta(&role, before[completed.role], beforeErr[completed.role], config.VictimCgroup, observer)
		}
		run.Roles[completed.role] = role
	}
	if executionErr != nil {
		if trace != nil {
			_, _ = trace.Stop()
		}
		return runfile.Run{}, executionErr
	}

	if trace != nil {
		samples, err := trace.Stop()
		if err != nil {
			run.Diagnostics = append(run.Diagnostics, "Linux scheduling trace failed: "+err.Error())
			for _, role := range []string{"borrower", "victim"} {
				roleMetrics := run.Roles[role]
				roleMetrics.LinuxSchedulingStatus = runfile.MetricStatus{Available: false, Reason: err.Error()}
				run.Roles[role] = roleMetrics
			}
		} else {
			for role, values := range samples {
				if roleMetrics, ok := run.Roles[role]; ok {
					if len(values) == 0 {
						roleMetrics.LinuxSchedulingStatus = runfile.MetricStatus{Available: false, Reason: "no matching scheduler events"}
						run.Roles[role] = roleMetrics
						continue
					}
					latency := convertLatency(metrics.Summarize(values))
					roleMetrics.LinuxSchedulingLatency = &latency
					roleMetrics.LinuxSchedulingStatus = runfile.MetricStatus{Available: true}
					run.Roles[role] = roleMetrics
				}
			}
		}
	}
	if observer != nil {
		hostPressure, err := observer.ReadHostPressure()
		if err != nil {
			run.HostPressureStatus = runfile.MetricStatus{Available: false, Reason: err.Error()}
		} else if hostPressureBeforeErr != nil {
			run.HostPressureStatus = runfile.MetricStatus{Available: false, Reason: "baseline unavailable: " + hostPressureBeforeErr.Error()}
		} else {
			pressure := pressureDelta(hostPressure, hostPressureBefore)
			run.HostCPUPressure = &pressure
			run.HostPressureStatus = runfile.MetricStatus{Available: true}
		}
	} else {
		run.HostPressureStatus = runfile.MetricStatus{Available: false, Reason: "Linux observer not configured"}
	}
	if hostPressureBeforeErr != nil {
		run.Diagnostics = append(run.Diagnostics, "host CPU pressure baseline unavailable: "+hostPressureBeforeErr.Error())
	}
	run.DurationSecs = config.Borrower.Duration.Seconds()
	if err := run.Validate(); err != nil {
		return runfile.Run{}, err
	}
	if err := writeRun(config.Output, run); err != nil {
		return runfile.Run{}, err
	}
	return run, nil
}

type roleExecution struct {
	role    string
	request int
	result  workload.Result
	err     error
}

func runRole(ctx context.Context, binary string, config workload.Config, startDelay time.Duration, cgroup string) (workload.Result, error) {
	args := []string{
		"-role", config.Role,
		"-duration", config.Duration.String(),
		"-period", config.Period.String(),
		"-cpu-time", config.CPUTime.String(),
		"-workers", fmt.Sprint(config.Workers),
		"-gomaxprocs", fmt.Sprint(config.GOMAXPROCS),
		"-start-delay", startDelay.String(),
		"-output", "-",
	}
	command := exec.CommandContext(ctx, binary, args...)
	var stdout bytes.Buffer
	command.Stdout = &stdout
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return workload.Result{}, err
	}
	if cgroup != "" {
		if err := attachToCgroup(cgroup, command.Process.Pid); err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return workload.Result{}, fmt.Errorf("attach %s workload PID to %s: %w", config.Role, cgroup, err)
		}
	}
	if err := command.Wait(); err != nil {
		return workload.Result{}, fmt.Errorf("%s workload failed: %w: %s", config.Role, err, strings.TrimSpace(stderr.String()))
	}
	var result workload.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return workload.Result{}, fmt.Errorf("decode %s workload result: %w", config.Role, err)
	}
	return result, nil
}

func requireEmptyCgroup(path string) error {
	data, err := os.ReadFile(filepath.Join(path, "cgroup.procs"))
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(data)) != "" {
		return errors.New("cgroup.procs is not empty; use a dedicated benchmark cgroup")
	}
	return nil
}

func attachToCgroup(path string, pid int) error {
	file, err := os.OpenFile(filepath.Join(path, "cgroup.procs"), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(file, "%d\n", pid); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func applyCgroupDelta(role *runfile.RoleMetrics, before CgroupSnapshot, beforeErr error, path string, observer Observer) {
	if observer == nil {
		role.CPUStatStatus = runfile.MetricStatus{Available: false, Reason: "Linux observer not configured"}
		role.CPUPressureStatus = role.CPUStatStatus
		return
	}
	if beforeErr != nil {
		role.CPUStatStatus = runfile.MetricStatus{Available: false, Reason: beforeErr.Error()}
		role.CPUPressureStatus = role.CPUStatStatus
		return
	}
	after, err := observer.ReadCgroup(path)
	if err != nil {
		role.CPUStatStatus = runfile.MetricStatus{Available: false, Reason: err.Error()}
		role.CPUPressureStatus = role.CPUStatStatus
		return
	}
	role.CPUWeight = after.CPUWeight
	role.CPUUsageUsec = delta(after.CPUUsageUsec, before.CPUUsageUsec)
	role.CPUThrottledUsec = delta(after.CPUThrottledUsec, before.CPUThrottledUsec)
	role.CPUThrottledPeriods = delta(after.CPUThrottledPeriods, before.CPUThrottledPeriods)
	role.CPUStatStatus = runfile.MetricStatus{Available: true}
	pressure := pressureDelta(after.Pressure, before.Pressure)
	role.CPUPressure = &pressure
	role.CPUPressureStatus = runfile.MetricStatus{Available: true}
}

func offeredMilliCPU(config workload.Config) int64 {
	return int64(config.Workers) * 1000 * int64(config.CPUTime) / int64(config.Period)
}

func delta(after, before uint64) uint64 {
	if after < before {
		return 0
	}
	return after - before
}

func pressureDelta(after, before runfile.Pressure) runfile.Pressure {
	after.Some.Total = delta(after.Some.Total, before.Some.Total)
	after.Full.Total = delta(after.Full.Total, before.Full.Total)
	return after
}

func convertLatency(summary metrics.Summary) runfile.Latency {
	return runfile.Latency{
		Count: summary.Count, Mean: summary.Mean, P90: summary.P90,
		P99: summary.P99, P999: summary.P999, Min: summary.Min, Max: summary.Max,
	}
}

func writeRun(output string, run runfile.Run) error {
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	path := filepath.Join(output, run.ID+".json")
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(run); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func hostMetadata() runfile.Host {
	hostname, _ := os.Hostname()
	release, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	cpuinfo, _ := os.ReadFile("/proc/cpuinfo")
	model := ""
	for _, line := range strings.Split(string(cpuinfo), "\n") {
		if strings.HasPrefix(line, "model name\t:") || strings.HasPrefix(line, "Model\t:") {
			model = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			break
		}
	}
	return runfile.Host{
		Hostname: hostname, KernelRelease: strings.TrimSpace(string(release)),
		Architecture: runtime.GOARCH, CPUModel: model, LogicalCPUs: runtime.NumCPU(),
	}
}

func readHostPressure(observer Observer) (runfile.Pressure, error) {
	if observer == nil {
		return runfile.Pressure{}, errors.New("Linux observer not configured")
	}
	return observer.ReadHostPressure()
}
