// Copyright 2018 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package execinfrapb

import (
	"strings"

	"github.com/podomy/tardigrade/pkg/sql/sem/builtins"
	"github.com/podomy/tardigrade/pkg/sql/sem/tree"
	"github.com/podomy/tardigrade/pkg/sql/types"
	"github.com/cockroachdb/errors"
)

// GetAggregateInfo returns the aggregate constructor and the return type for
// the given aggregate function when applied on the given type.
func GetAggregateInfo(
	fn AggregatorSpec_Func, inputTypes ...types.T,
) (
	aggregateConstructor func(*tree.EvalContext, tree.Datums) tree.AggregateFunc,
	returnType *types.T,
	err error,
) {
	if fn == AggregatorSpec_ANY_NOT_NULL {
		// The ANY_NOT_NULL builtin does not have a fixed return type;
		// handle it separately.
		if len(inputTypes) != 1 {
			return nil, nil, errors.Errorf("any_not_null aggregate needs 1 input")
		}
		return builtins.NewAnyNotNullAggregate, &inputTypes[0], nil
	}
	datumTypes := make([]*types.T, len(inputTypes))
	for i := range inputTypes {
		datumTypes[i] = &inputTypes[i]
	}

	props, builtins := builtins.GetBuiltinProperties(strings.ToLower(fn.String()))
	for _, b := range builtins {
		typs := b.Types.Types()
		if len(typs) != len(inputTypes) {
			continue
		}
		match := true
		for i, t := range typs {
			if !datumTypes[i].Equivalent(t) {
				if props.NullableArgs && datumTypes[i].IsAmbiguous() {
					continue
				}
				match = false
				break
			}
		}
		if match {
			// Found!
			constructAgg := func(evalCtx *tree.EvalContext, arguments tree.Datums) tree.AggregateFunc {
				return b.AggregateFunc(datumTypes, evalCtx, arguments)
			}

			colTyp := b.FixedReturnType()
			return constructAgg, colTyp, nil
		}
	}
	return nil, nil, errors.Errorf(
		"no builtin aggregate for %s on %+v", fn, inputTypes,
	)
}

// Equals returns true if two aggregation specifiers are identical (and thus
// will always yield the same result).
func (a AggregatorSpec_Aggregation) Equals(b AggregatorSpec_Aggregation) bool {
	if a.Func != b.Func || a.Distinct != b.Distinct {
		return false
	}
	if a.FilterColIdx == nil {
		if b.FilterColIdx != nil {
			return false
		}
	} else {
		if b.FilterColIdx == nil || *a.FilterColIdx != *b.FilterColIdx {
			return false
		}
	}
	if len(a.ColIdx) != len(b.ColIdx) {
		return false
	}
	for i, c := range a.ColIdx {
		if c != b.ColIdx[i] {
			return false
		}
	}
	return true
}

// IsScalar returns whether the aggregate function is in scalar context.
func (spec *AggregatorSpec) IsScalar() bool {
	switch spec.Type {
	case AggregatorSpec_SCALAR:
		return true
	case AggregatorSpec_NON_SCALAR:
		return false
	default:
		// This case exists for backward compatibility.
		return (len(spec.GroupCols) == 0)
	}
}

// IsRowCount returns true if the aggregator spec is scalar and has a single
// COUNT_ROWS aggregation with no FILTER or DISTINCT.
func (spec *AggregatorSpec) IsRowCount() bool {
	return len(spec.Aggregations) == 1 &&
		spec.Aggregations[0].FilterColIdx == nil &&
		spec.Aggregations[0].Func == AggregatorSpec_COUNT_ROWS &&
		!spec.Aggregations[0].Distinct &&
		spec.IsScalar()
}
