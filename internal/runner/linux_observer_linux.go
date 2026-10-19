package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cpu-scheduler-eval/internal/linuxobs"
	"cpu-scheduler-eval/internal/runfile"
)

// LinuxObserver reads cgroup v2 counters and pressure, and optionally starts a
// bpftrace scheduler wakeup-to-running collector. It never changes cgroups.
type LinuxObserver struct{}

// StartTrace starts optional scheduler tracing. Missing bpftrace or tracepoint
// permissions are returned as explicit unavailable errors.
func (LinuxObserver) StartTrace(ctx context.Context) (TraceSession, error) {
	collector, err := linuxobs.StartCollector(ctx)
	if err != nil {
		return nil, err
	}
	return linuxTraceSession{collector: collector}, nil
}

// ReadCgroup reads one cgroup v2 directory. Missing or malformed required
// telemetry returns an error, which the run artifact records as unavailable.
func (LinuxObserver) ReadCgroup(path string) (CgroupSnapshot, error) {
	return readLinuxCgroup(path)
}

// ReadHostPressure returns the cumulative host CPU pressure snapshot.
func (LinuxObserver) ReadHostPressure() (runfile.Pressure, error) {
	data, err := os.ReadFile("/proc/pressure/cpu")
	if err != nil {
		return runfile.Pressure{}, fmt.Errorf("read host CPU pressure: %w", err)
	}
	pressure, err := linuxobs.ParsePressure(data)
	if err != nil {
		return runfile.Pressure{}, fmt.Errorf("parse host CPU pressure: %w", err)
	}
	return convertLinuxPressure(pressure), nil
}

type linuxCollector interface {
	Stop() ([]linuxobs.Sample, error)
}

type linuxTraceSession struct {
	collector linuxCollector
}

func (s linuxTraceSession) Stop() (map[string][]float64, error) {
	samples, err := s.collector.Stop()
	if err != nil {
		return nil, err
	}
	grouped := linuxobs.GroupByRole(samples)
	borrower := make([]float64, len(grouped.Borrower))
	for i, value := range grouped.Borrower {
		borrower[i] = float64(value)
	}
	victim := make([]float64, len(grouped.Victim))
	for i, value := range grouped.Victim {
		victim[i] = float64(value)
	}
	return map[string][]float64{"borrower": borrower, "victim": victim}, nil
}

func readLinuxCgroup(path string) (CgroupSnapshot, error) {
	weightData, err := os.ReadFile(filepath.Join(path, "cpu.weight"))
	if err != nil {
		return CgroupSnapshot{}, fmt.Errorf("read cgroup cpu.weight: %w", err)
	}
	weight, err := strconv.Atoi(strings.TrimSpace(string(weightData)))
	if err != nil || weight < 1 || weight > 10000 {
		return CgroupSnapshot{}, fmt.Errorf("invalid cgroup cpu.weight %q", strings.TrimSpace(string(weightData)))
	}

	statData, err := os.ReadFile(filepath.Join(path, "cpu.stat"))
	if err != nil {
		return CgroupSnapshot{}, fmt.Errorf("read cgroup cpu.stat: %w", err)
	}
	stat, err := linuxobs.ParseCPUStat(statData)
	if err != nil {
		return CgroupSnapshot{}, fmt.Errorf("parse cgroup cpu.stat: %w", err)
	}
	for _, name := range []string{"nr_throttled", "throttled_usec"} {
		if _, ok := stat[name]; !ok {
			return CgroupSnapshot{}, fmt.Errorf("cgroup cpu.stat missing %s counter", name)
		}
	}

	pressureData, err := os.ReadFile(filepath.Join(path, "cpu.pressure"))
	if err != nil {
		return CgroupSnapshot{}, fmt.Errorf("read cgroup cpu.pressure: %w", err)
	}
	pressure, err := linuxobs.ParsePressure(pressureData)
	if err != nil {
		return CgroupSnapshot{}, fmt.Errorf("parse cgroup cpu.pressure: %w", err)
	}
	return CgroupSnapshot{
		CPUWeight:           weight,
		CPUUsageUsec:        stat["usage_usec"],
		CPUThrottledUsec:    stat["throttled_usec"],
		CPUThrottledPeriods: stat["nr_throttled"],
		Pressure:            convertLinuxPressure(pressure),
	}, nil
}

func convertLinuxPressure(value linuxobs.Pressure) runfile.Pressure {
	converted := runfile.Pressure{Some: convertLinuxPressureLine(value.Some)}
	if value.Full != nil {
		converted.Full = convertLinuxPressureLine(*value.Full)
	}
	return converted
}

func convertLinuxPressureLine(value linuxobs.PressureLine) runfile.PressureLine {
	return runfile.PressureLine{
		Avg10: value.Avg10, Avg60: value.Avg60, Avg300: value.Avg300,
		Total: value.TotalUsec,
	}
}
