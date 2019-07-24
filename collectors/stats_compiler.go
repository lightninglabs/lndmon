package collectors

import (
	"math"
	"sort"
)

// statsReport is a simple report of basic statics derived from some existing
// data set.
type statsReport struct {
	min, max, avg, median float64
}

// statsCompiler generates a statsReport given one more observations from an
// existing data set.
type statsCompiler struct {
	statsReport

	// sum is the running count of all items in the data set.
	sum float64

	// samples are pointers to all observed samples.
	samples []float64
}

// newStatsCompiler makes a new statsCompiler given a number (optional) of
// estimated samples. One or more calls to Observe() should be made for each
// sample, followed by a call to Report() to generate the final statsReport for
// the data set.
func newStatsCompiler(numSamples uint32) statsCompiler {
	return statsCompiler{
		statsReport: statsReport{
			min: math.MaxFloat64,
			max: 0,
		},
		samples: make([]float64, 0, numSamples),
	}
}

// Observe logs a new value in the set of existing samples.
func (s *statsCompiler) Observe(value float64) {

	// First, we'll update our min/max, if needed.
	if value > s.max {
		s.max = value
	}
	if value < s.min {
		s.min = value
	}

	// Finally, we'll accumulate this value so we can compute the average
	// later, and accumulate this new sample.
	s.sum += value
	s.samples = append(s.samples, value)
}

// Report generates the final stats report. This should only be called after
// all samples have been fed into the Observe()  method.
func (s *statsCompiler) Report() statsReport {
	s.avg = s.sum / float64(len(s.samples))

	s.median = (func() float64 {
		sort.Float64s(s.samples)

		num := len(s.samples)
		pivot := num / 2

		switch {
		case num == 0:
			return 0

		case num%2 == 0:
			return (s.samples[pivot-1] + s.samples[pivot]) / 2

		default:
			return s.samples[pivot]
		}
	}())

	return s.statsReport
}
