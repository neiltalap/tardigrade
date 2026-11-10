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

import "context"

// IndexedRows are rows with the corresponding indices.
type IndexedRows interface {
	Len() int                                                // returns number of rows
	GetRow(ctx context.Context, idx int) (IndexedRow, error) // returns a row at the given index or an error
}

// IndexedRow is a row with a corresponding index.
type IndexedRow interface {
	GetIdx() int                                    // returns index of the row
	GetDatum(idx int) (Datum, error)                // returns a datum at the given index
	GetDatums(startIdx, endIdx int) (Datums, error) // returns datums at indices [startIdx, endIdx)
}
