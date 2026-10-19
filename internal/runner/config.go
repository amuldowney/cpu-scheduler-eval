package runner

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"cpu-scheduler-eval/internal/workload"
)

// Config is a controlled, single-node trial. Request admission is checked
// before any workload process is started.
type Config struct {
	Policy                  string
	SchedulerRevision       string
	Scenario                string
	Output                  string
	WorkloadBinary          string
	AllocatableMilliCPU     int
	BorrowerRequestMilliCPU int
	VictimRequestMilliCPU   int
	BorrowerCgroup          string
	VictimCgroup            string
	StartDelay              time.Duration
	Borrower                workload.Config
	Victim                  workload.Config
}

func DefaultConfig() Config {
	return Config{
		Policy:                  "native-weight",
		SchedulerRevision:       "unrecorded",
		Scenario:                "slack-borrowing",
		Output:                  "runs",
		WorkloadBinary:          "workload",
		AllocatableMilliCPU:     runtime.NumCPU() * 1000,
		BorrowerRequestMilliCPU: 500,
		VictimRequestMilliCPU:   500,
		StartDelay:              2 * time.Second,
		Borrower: workload.Config{
			Role: "borrower", Duration: 30 * time.Second, Period: time.Millisecond,
			CPUTime: 950 * time.Microsecond, Workers: 2 * runtime.NumCPU(),
		},
		Victim: workload.Config{
			Role: "victim", Duration: 30 * time.Second, Period: time.Millisecond,
			CPUTime: 450 * time.Microsecond, Workers: 1,
		},
	}
}

func cgroupPathsOverlap(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	contains := func(parent, child string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
	}
	return contains(a, b) || contains(b, a)
}

func (c Config) Validate() error {
	if c.Policy == "" || c.SchedulerRevision == "" || c.Scenario == "" || c.Output == "" || c.WorkloadBinary == "" {
		return errors.New("policy, scheduler revision, scenario, output path, and workload binary are required")
	}
	if c.AllocatableMilliCPU <= 0 || c.BorrowerRequestMilliCPU < 500 || c.VictimRequestMilliCPU < 500 {
		return errors.New("allocatable CPU must be positive and each role request must be at least 500m")
	}
	if c.BorrowerRequestMilliCPU+c.VictimRequestMilliCPU > c.AllocatableMilliCPU {
		return fmt.Errorf("requests total %dm exceeds allocatable %dm", c.BorrowerRequestMilliCPU+c.VictimRequestMilliCPU, c.AllocatableMilliCPU)
	}
	if c.Borrower.Role != "borrower" || c.Victim.Role != "victim" {
		return errors.New("workload configs must have borrower and victim roles")
	}
	if c.Borrower.Duration != c.Victim.Duration {
		return errors.New("borrower and victim measurement durations must match")
	}
	for _, role := range []workload.Config{c.Borrower, c.Victim} {
		if role.Duration <= 0 || role.Period <= 0 || role.CPUTime <= 0 || role.Workers <= 0 {
			return fmt.Errorf("%s workload duration, period, CPU time, and workers must be positive", role.Role)
		}
		if role.CPUTime > role.Duration {
			return fmt.Errorf("%s workload CPU time must not exceed duration", role.Role)
		}
	}
	if c.StartDelay < 0 {
		return errors.New("start delay cannot be negative")
	}
	if c.BorrowerCgroup != "" && c.VictimCgroup != "" && cgroupPathsOverlap(c.BorrowerCgroup, c.VictimCgroup) {
		return errors.New("borrower and victim require separate cgroups")
	}
	if (c.BorrowerCgroup != "" || c.VictimCgroup != "") && c.StartDelay < 100*time.Millisecond {
		return errors.New("cgroup-attached workloads require at least 100ms start delay for process placement")
	}
	return nil
}
