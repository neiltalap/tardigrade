// Copyright 2016 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package rowexec

import (
	"github.com/podomy/tardigrade/pkg/sql/sem/tree"
	"github.com/podomy/tardigrade/pkg/sql/sqlbase"
	"github.com/podomy/tardigrade/pkg/sql/types"
	"github.com/podomy/tardigrade/pkg/util/encoding"
	"github.com/pkg/errors"
)

// CompareEncDatumRowForMerge EncDatumRow compares two EncDatumRows for merging.
// When merging two streams and preserving the order (as in a MergeSort or
// a MergeJoin) compare the head of the streams, emitting the one that sorts
// first. It allows for the EncDatumRow to be nil if one of the streams is
// exhausted (and hence nil). CompareEncDatumRowForMerge returns 0 when both
// rows are nil, and a nil row is considered greater than any non-nil row.
// CompareEncDatumRowForMerge assumes that the two rows have the same columns
// in the same orders, but can handle different ordering directions. It takes
// a DatumAlloc which is used for decoding if any underlying EncDatum is not
// yet decoded.
func CompareEncDatumRowForMerge(
	lhsTypes []types.T,
	lhs, rhs sqlbase.EncDatumRow,
	leftOrdering, rightOrdering sqlbase.ColumnOrdering,
	nullEquality bool,
	da *sqlbase.DatumAlloc,
	evalCtx *tree.EvalContext,
) (int, error) {
	if lhs == nil && rhs == nil {
		return 0, nil
	}
	if lhs == nil {
		return 1, nil
	}
	if rhs == nil {
		return -1, nil
	}
	if len(leftOrdering) != len(rightOrdering) {
		return 0, errors.Errorf(
			"cannot compare two EncDatumRow types that have different length ColumnOrderings",
		)
	}

	for i, ord := range leftOrdering {
		lIdx := ord.ColIdx
		rIdx := rightOrdering[i].ColIdx
		// If both datums are NULL, we need to follow SQL semantics where
		// they are not equal. This differs from our datum semantics where
		// they are equal. In the case where we want to consider NULLs to be
		// equal, we continue and skip to the next datums in the row.
		if lhs[lIdx].IsNull() && rhs[rIdx].IsNull() {
			if !nullEquality {
				// We can return either -1 or 1, it does not change the behavior.
				return -1, nil
			}
			continue
		}
		cmp, err := lhs[lIdx].Compare(&lhsTypes[lIdx], da, evalCtx, &rhs[rIdx])
		if err != nil {
			return 0, err
		}
		if cmp != 0 {
			if leftOrdering[i].Direction == encoding.Descending {
				cmp = -cmp
			}
			return cmp, nil
		}
	}
	return 0, nil
}
