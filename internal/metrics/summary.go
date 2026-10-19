package metrics

import (
	"math"
	"sort"
)

// Summary describes a latency sample set. All latency values are nanoseconds.
// Percentiles use nearest-rank (ceil(p*n), one-based) so independently
// generated run artifacts use the same convention.
type Summary struct {
	Count int     `json:"count"`
	Mean  float64 `json:"mean_ns"`
	P90   float64 `json:"p90_ns"`
	P99   float64 `json:"p99_ns"`
	P999  float64 `json:"p999_ns"`
	Min   float64 `json:"min_ns"`
	Max   float64 `json:"max_ns"`
}

// Summarize returns exact sample statistics without changing values.
func Summarize(values []float64) Summary {
	if len(values) == 0 {
		return Summary{}
	}

	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)

	var sum float64
	for _, value := range sorted {
		sum += value
	}

	return Summary{
		Count: len(sorted),
		Mean:  sum / float64(len(sorted)),
		P90:   nearestRank(sorted, 0.90),
		P99:   nearestRank(sorted, 0.99),
		P999:  nearestRank(sorted, 0.999),
		Min:   sorted[0],
		Max:   sorted[len(sorted)-1],
	}
}

func nearestRank(sorted []float64, percentile float64) float64 {
	index := int(math.Ceil(percentile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
