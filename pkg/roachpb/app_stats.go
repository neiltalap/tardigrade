// Copyright 2017 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package roachpb

// GetVariance retrieves the variance of the values.
func (l *NumericStat) GetVariance(count int64) float64 {
	return l.SquaredDiffs / (float64(count) - 1)
}

// Record updates the underlying running counts, incorporating the given value.
// It follows Welford's algorithm (Technometrics, 1962). The running count must
// be stored as it is required to finalize and retrieve the variance.
func (l *NumericStat) Record(count int64, val float64) {
	delta := val - l.Mean
	l.Mean += delta / float64(count)
	l.SquaredDiffs += delta * (val - l.Mean)
}

// Add combines b into this derived statistics.
func (l *NumericStat) Add(b NumericStat, countA, countB int64) {
	*l = AddNumericStats(*l, b, countA, countB)
}

// AddNumericStats combines derived statistics.
// Adapted from https://www.johndcook.com/blog/skewness_kurtosis/
func AddNumericStats(a, b NumericStat, countA, countB int64) NumericStat {
	total := float64(countA + countB)
	delta := b.Mean - a.Mean

	return NumericStat{
		Mean: ((a.Mean * float64(countA)) + (b.Mean * float64(countB))) / total,
		SquaredDiffs: (a.SquaredDiffs + b.SquaredDiffs) +
			delta*delta*float64(countA)*float64(countB)/total,
	}
}
