package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"cpu-scheduler-eval/internal/runner"
)

func main() {
	config := runner.DefaultConfig()
	duration, borrowerPeriod, borrowerCPU := config.Borrower.Duration, config.Borrower.Period, config.Borrower.CPUTime
	victimPeriod, victimCPU := config.Victim.Period, config.Victim.CPUTime
	borrowerWorkers, borrowerProcs := config.Borrower.Workers, config.Borrower.GOMAXPROCS
	victimWorkers, victimProcs := config.Victim.Workers, config.Victim.GOMAXPROCS
	var confirm bool
	var repeats int
	flag.StringVar(&config.Policy, "policy", config.Policy, "scheduler policy label for this run")
	flag.StringVar(&config.SchedulerRevision, "scheduler-revision", config.SchedulerRevision, "scheduler source/build revision")
	flag.StringVar(&config.Scenario, "scenario", config.Scenario, "scenario label")
	flag.StringVar(&config.Output, "output", config.Output, "directory for versioned JSON run artifacts")
	flag.StringVar(&config.WorkloadBinary, "workload-bin", "./bin/workload", "path to the built Go workload binary")
	flag.IntVar(&config.AllocatableMilliCPU, "allocatable-mcpu", config.AllocatableMilliCPU, "node allocatable CPU in millicpu")
	flag.IntVar(&config.BorrowerRequestMilliCPU, "borrower-request-mcpu", config.BorrowerRequestMilliCPU, "borrower CPU request in millicpu")
	flag.IntVar(&config.VictimRequestMilliCPU, "victim-request-mcpu", config.VictimRequestMilliCPU, "victim CPU request in millicpu")
	flag.StringVar(&config.BorrowerCgroup, "borrower-cgroup", "", "existing borrower cgroup v2 directory (optional)")
	flag.StringVar(&config.VictimCgroup, "victim-cgroup", "", "existing victim cgroup v2 directory (optional)")
	flag.DurationVar(&config.StartDelay, "start-delay", config.StartDelay, "delay before measurement, allowing cgroup placement and tracing setup")
	flag.DurationVar(&duration, "duration", duration, "measurement duration per role")
	flag.DurationVar(&borrowerPeriod, "borrower-period", borrowerPeriod, "borrower work release period")
	flag.DurationVar(&borrowerCPU, "borrower-cpu-time", borrowerCPU, "borrower CPU time per work item")
	flag.IntVar(&borrowerWorkers, "borrower-workers", borrowerWorkers, "concurrent borrower Go workers")
	flag.IntVar(&borrowerProcs, "borrower-gomaxprocs", borrowerProcs, "borrower GOMAXPROCS (0 uses Go default)")
	flag.DurationVar(&victimPeriod, "victim-period", victimPeriod, "victim work release period")
	flag.DurationVar(&victimCPU, "victim-cpu-time", victimCPU, "victim CPU time per work item")
	flag.IntVar(&victimWorkers, "victim-workers", victimWorkers, "concurrent victim Go workers")
	flag.IntVar(&victimProcs, "victim-gomaxprocs", victimProcs, "victim GOMAXPROCS (0 uses Go default)")
	flag.IntVar(&repeats, "repeat", 1, "number of sequential trials to record")
	flag.BoolVar(&confirm, "confirm-load-test", false, "required acknowledgement that this starts CPU contention workloads")
	flag.Parse()

	if !confirm {
		fmt.Fprintln(os.Stderr, "refusing to start CPU contention; pass -confirm-load-test after checking the target host and cgroups")
		os.Exit(2)
	}
	if repeats < 1 {
		fmt.Fprintln(os.Stderr, "-repeat must be at least 1")
		os.Exit(2)
	}
	config.Borrower.Duration, config.Borrower.Period, config.Borrower.CPUTime = duration, borrowerPeriod, borrowerCPU
	config.Borrower.Workers, config.Borrower.GOMAXPROCS = borrowerWorkers, borrowerProcs
	config.Victim.Duration, config.Victim.Period, config.Victim.CPUTime = duration, victimPeriod, victimCPU
	config.Victim.Workers, config.Victim.GOMAXPROCS = victimWorkers, victimProcs

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	observer := runner.LinuxObserver{}
	for trial := 1; trial <= repeats; trial++ {
		run, err := runner.Execute(ctx, config, observer)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("wrote trial %d/%d (%s) to %s\n", trial, repeats, run.ID, config.Output)
	}
}
