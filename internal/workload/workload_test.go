package workload

import (
	"context"
	"testing"
	"time"
)

func TestRunProducesApplicationAndGoSchedulerMetrics(t *testing.T) {
	result, err := Run(context.Background(), Config{
		Role:       "victim",
		Duration:   25 * time.Millisecond,
		Period:     time.Millisecond,
		CPUTime:    100 * time.Microsecond,
		Workers:    1,
		GOMAXPROCS: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Role != "victim" || result.Operations == 0 {
		t.Fatalf("no workload operations recorded: %+v", result)
	}
	if result.ApplicationLatency.Count != result.Operations {
		t.Fatalf("application sample count %d != operations %d", result.ApplicationLatency.Count, result.Operations)
	}
	if result.ApplicationLatency.P99 < result.ApplicationLatency.P90 {
		t.Fatalf("p99 below p90: %+v", result.ApplicationLatency)
	}
	if result.GoSchedulerLatency.Count == 0 {
		t.Fatalf("no Go scheduler latency recorded: %+v", result.GoSchedulerLatency)
	}
}

func TestRunRejectsInvalidConfig(t *testing.T) {
	_, err := Run(context.Background(), Config{Role: "borrower"})
	if err == nil {
		t.Fatal("expected invalid workload config to be rejected")
	}
}
