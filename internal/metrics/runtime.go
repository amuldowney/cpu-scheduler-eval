package metrics

import (
	"math"

	runtimemetrics "runtime/metrics"
)

// SummarizeRuntimeHistogram estimates latency statistics from a cumulative Go
// runtime histogram. The bucket midpoint method is necessarily approximate.
func SummarizeRuntimeHistogram(hist *runtimemetrics.Float64Histogram) Summary {
	if hist == nil || len(hist.Counts) == 0 || len(hist.Buckets) != len(hist.Counts)+1 {
		return Summary{}
	}

	var count uint64
	var weightedSum float64
	for i, bucketCount := range hist.Counts {
		if bucketCount == 0 {
			continue
		}
		count += bucketCount
		weightedSum += bucketValue(hist.Buckets, i) * float64(bucketCount)
	}
	if count == 0 {
		return Summary{}
	}

	return Summary{
		Count: int(count),
		Mean:  weightedSum / float64(count),
		P90:   histogramRank(hist.Buckets, hist.Counts, count, 0.90),
		P99:   histogramRank(hist.Buckets, hist.Counts, count, 0.99),
		P999:  histogramRank(hist.Buckets, hist.Counts, count, 0.999),
		Min:   firstBucketValue(hist.Buckets, hist.Counts),
		Max:   lastBucketValue(hist.Buckets, hist.Counts),
	}
}

func histogramRank(bounds []float64, counts []uint64, total uint64, percentile float64) float64 {
	rank := uint64(math.Ceil(percentile * float64(total)))
	if rank == 0 {
		rank = 1
	}
	var cumulative uint64
	for i, count := range counts {
		cumulative += count
		if cumulative >= rank {
			return bucketValue(bounds, i)
		}
	}
	return lastBucketValue(bounds, counts)
}

func bucketValue(bounds []float64, i int) float64 {
	low, high := bounds[i], bounds[i+1]
	switch {
	case math.IsInf(low, -1):
		if math.IsInf(high, 1) || high <= 0 {
			return 0
		}
		return high * 1e9 / 2
	case math.IsInf(high, 1):
		width := low - bounds[i-1]
		if width <= 0 || math.IsInf(width, 0) || math.IsNaN(width) {
			width = low
		}
		return (low + width/2) * 1e9
	default:
		return (low + (high-low)/2) * 1e9
	}
}

func firstBucketValue(bounds []float64, counts []uint64) float64 {
	for i, count := range counts {
		if count != 0 {
			return bucketValue(bounds, i)
		}
	}
	return 0
}

func lastBucketValue(bounds []float64, counts []uint64) float64 {
	for i := len(counts) - 1; i >= 0; i-- {
		if counts[i] != 0 {
			return bucketValue(bounds, i)
		}
	}
	return 0
}
