package metrics

import "testing"

func TestSummarizeUsesNearestRankAndExactMean(t *testing.T) {
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(i + 1)
	}

	got := Summarize(values)
	if got.Count != 100 || got.Mean != 50.5 || got.P90 != 90 || got.P99 != 99 || got.P999 != 100 {
		t.Fatalf("unexpected summary: %+v", got)
	}
}

func TestSummarizeEmptyInput(t *testing.T) {
	got := Summarize(nil)
	if got.Count != 0 || got.Mean != 0 || got.P90 != 0 || got.P99 != 0 || got.P999 != 0 {
		t.Fatalf("empty summary should be zero-valued: %+v", got)
	}
}

func TestSummarizeDoesNotMutateInput(t *testing.T) {
	input := []float64{3, 1, 2}
	Summarize(input)
	if input[0] != 3 || input[1] != 1 || input[2] != 2 {
		t.Fatalf("input mutated: %v", input)
	}
}
