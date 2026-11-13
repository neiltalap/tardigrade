// Copyright 2014 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package roachpb

// IsColumnar returns true if this InternalTimeSeriesData stores its samples
// in columnar format.
func (data *InternalTimeSeriesData) IsColumnar() bool {
	return len(data.Offset) > 0
}

// SampleCount returns the number of samples contained in this
// InternalTimeSeriesData.
func (data *InternalTimeSeriesData) SampleCount() int {
	if data.IsColumnar() {
		return len(data.Offset)
	}
	return len(data.Samples)
}
