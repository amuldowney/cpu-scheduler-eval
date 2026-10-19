package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"cpu-scheduler-eval/internal/workload"
)

func main() {
	role := flag.String("role", "victim", "workload role: borrower or victim")
	duration := flag.Duration("duration", 10*time.Second, "measurement duration")
	period := flag.Duration("period", time.Millisecond, "scheduled work period per worker")
	cpuTime := flag.Duration("cpu-time", 500*time.Microsecond, "CPU busy time per work item")
	workers := flag.Int("workers", 1, "number of concurrent work goroutines")
	gomaxprocs := flag.Int("gomaxprocs", 0, "set Go GOMAXPROCS (0 keeps the runtime default)")
	startDelay := flag.Duration("start-delay", 0, "wait before measurement (for cgroup placement and tracer startup)")
	output := flag.String("output", "-", "JSON result path, or - for stdout")
	flag.Parse()

	if *startDelay > 0 {
		time.Sleep(*startDelay)
	}
	result, err := workload.Run(context.Background(), workload.Config{
		Role:       *role,
		Duration:   *duration,
		Period:     *period,
		CPUTime:    *cpuTime,
		Workers:    *workers,
		GOMAXPROCS: *gomaxprocs,
	})
	if err != nil {
		fatal(err)
	}

	var file *os.File
	if *output == "-" {
		file = os.Stdout
	} else {
		file, err = os.Create(*output)
		if err != nil {
			fatal(err)
		}
		defer file.Close()
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
