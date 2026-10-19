package metrics

import (
	"math"
	"runtime/metrics"
	"testing"
)

func TestSummarizeRuntimeHistogramUsesBucketMidpointsAndNanoseconds(t *testing.T) {
	hist := &metrics.Float64Histogram{
		Buckets: []float64{0, 1e-6, 3e-6, 5e-6},
		Counts:  []uint64{1, 2, 1},
	}

	got := SummarizeRuntimeHistogram(hist)
	if got.Count != 4 || math.Abs(got.Mean-2.125e3) > 1e-9 || math.Abs(got.P90-4e3) > 1e-9 || math.Abs(got.P99-4e3) > 1e-9 || math.Abs(got.P999-4e3) > 1e-9 {
		t.Fatalf("unexpected runtime histogram summary: %+v", got)
	}
}

func TestSummarizeRuntimeHistogramEmptyAndMalformed(t *testing.T) {
	for _, hist := range []*metrics.Float64Histogram{
		nil,
		{Buckets: []float64{0, 1}, Counts: nil},
		{Buckets: []float64{0, 1}, Counts: []uint64{1, 2}},
	} {
		if got := SummarizeRuntimeHistogram(hist); got.Count != 0 {
			t.Fatalf("expected empty summary for %+v, got %+v", hist, got)
		}
	}
}
