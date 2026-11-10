// Copyright 2018 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package norm

import (
	"github.com/podomy/tardigrade/pkg/sql/opt/memo"
	"github.com/podomy/tardigrade/pkg/sql/opt/props/physical"
)

// CanSimplifyLimitOffsetOrdering returns true if the ordering required by the
// Limit or Offset operator can be made less restrictive, so that the input
// operator has more ordering choices.
func (c *CustomFuncs) CanSimplifyLimitOffsetOrdering(
	in memo.RelExpr, ordering physical.OrderingChoice,
) bool {
	return c.canSimplifyOrdering(in, ordering)
}

// SimplifyLimitOffsetOrdering makes the ordering required by the Limit or
// Offset operator less restrictive by removing optional columns, adding
// equivalent columns, and removing redundant columns.
func (c *CustomFuncs) SimplifyLimitOffsetOrdering(
	input memo.RelExpr, ordering physical.OrderingChoice,
) physical.OrderingChoice {
	return c.simplifyOrdering(input, ordering)
}

// CanSimplifyGroupingOrdering returns true if the ordering required by the
// grouping operator can be made less restrictive, so that the input operator
// has more ordering choices.
func (c *CustomFuncs) CanSimplifyGroupingOrdering(
	in memo.RelExpr, private *memo.GroupingPrivate,
) bool {
	return c.canSimplifyOrdering(in, private.Ordering)
}

// SimplifyGroupingOrdering makes the ordering required by the grouping operator
// less restrictive by removing optional columns, adding equivalent columns, and
// removing redundant columns.
func (c *CustomFuncs) SimplifyGroupingOrdering(
	in memo.RelExpr, private *memo.GroupingPrivate,
) *memo.GroupingPrivate {
	// Copy GroupingPrivate to stack and replace Ordering field.
	copy := *private
	copy.Ordering = c.simplifyOrdering(in, private.Ordering)
	return &copy
}

// CanSimplifyOrdinalityOrdering returns true if the ordering required by the
// Ordinality operator can be made less restrictive, so that the input operator
// has more ordering choices.
func (c *CustomFuncs) CanSimplifyOrdinalityOrdering(
	in memo.RelExpr, private *memo.OrdinalityPrivate,
) bool {
	return c.canSimplifyOrdering(in, private.Ordering)
}

// SimplifyOrdinalityOrdering makes the ordering required by the Ordinality
// operator less restrictive by removing optional columns, adding equivalent
// columns, and removing redundant columns.
func (c *CustomFuncs) SimplifyOrdinalityOrdering(
	in memo.RelExpr, private *memo.OrdinalityPrivate,
) *memo.OrdinalityPrivate {
	// Copy OrdinalityPrivate to stack and replace Ordering field.
	copy := *private
	copy.Ordering = c.simplifyOrdering(in, private.Ordering)
	return &copy
}

// CanSimplifyExplainOrdering returns true if the ordering required by the
// Explain operator can be made less restrictive, so that the input operator
// has more ordering choices.
func (c *CustomFuncs) CanSimplifyExplainOrdering(
	in memo.RelExpr, private *memo.ExplainPrivate,
) bool {
	return c.canSimplifyOrdering(in, private.Props.Ordering)
}

// SimplifyExplainOrdering makes the ordering required by the Explain operator
// less restrictive by removing optional columns, adding equivalent columns, and
// removing redundant columns.
func (c *CustomFuncs) SimplifyExplainOrdering(
	in memo.RelExpr, private *memo.ExplainPrivate,
) *memo.ExplainPrivate {
	// Copy ExplainPrivate and its physical properties to stack and replace
	// Ordering field in the copied properties.
	copy := *private
	copyProps := *private.Props
	copyProps.Ordering = c.simplifyOrdering(in, private.Props.Ordering)
	copy.Props = &copyProps
	return &copy
}

func (c *CustomFuncs) canSimplifyOrdering(in memo.RelExpr, ordering physical.OrderingChoice) bool {
	// If any ordering is allowed, nothing to simplify.
	if ordering.Any() {
		return false
	}
	return ordering.CanSimplify(&in.Relational().FuncDeps)
}

func (c *CustomFuncs) simplifyOrdering(
	in memo.RelExpr, ordering physical.OrderingChoice,
) physical.OrderingChoice {
	simplified := ordering.Copy()
	simplified.Simplify(&in.Relational().FuncDeps)
	return simplified
}
