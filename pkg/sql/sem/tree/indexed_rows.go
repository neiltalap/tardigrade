// Copyright 2019 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package tree

// IndexedRow is a row with a corresponding index.
type IndexedRow interface {
	GetIdx() int                                    // returns index of the row
	GetDatum(idx int) (Datum, error)                // returns a datum at the given index
	GetDatums(startIdx, endIdx int) (Datums, error) // returns datums at indices [startIdx, endIdx)
}
