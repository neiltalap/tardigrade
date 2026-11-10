// Copyright 2018 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package norm_test

import (
	"testing"

	"github.com/podomy/tardigrade/pkg/settings/cluster"
	"github.com/podomy/tardigrade/pkg/sql/opt"
	"github.com/podomy/tardigrade/pkg/sql/opt/memo"
	"github.com/podomy/tardigrade/pkg/sql/opt/norm"
	"github.com/podomy/tardigrade/pkg/sql/opt/testutils/testcat"
	"github.com/podomy/tardigrade/pkg/sql/sem/tree"
)

// TestSimplifyFilters tests factory.SimplifyFilters. It's hard to fully test
// using SQL, as And operator rules simplify the expression before the Filters
// operator is created.
func TestSimplifyFilters(t *testing.T) {
	evalCtx := tree.MakeTestingEvalContext(cluster.MakeTestingClusterSettings())
	var f norm.Factory
	f.Init(&evalCtx)

	cat := testcat.New()
	if _, err := cat.ExecuteDDL("CREATE TABLE a (x INT PRIMARY KEY, y INT)"); err != nil {
		t.Fatal(err)
	}
	tn := tree.NewTableName("t", "a")
	a := f.Metadata().AddTable(cat.Table(tn), tn)
	ax := a.ColumnID(0)

	variable := f.ConstructVariable(ax)
	constant := f.ConstructConst(tree.NewDInt(1))
	eq := f.ConstructEq(variable, constant)

	// Filters expression evaluates to False if any operand is False.
	vals := f.ConstructValues(memo.ScalarListWithEmptyTuple, &memo.ValuesPrivate{
		Cols: opt.ColList{},
		ID:   f.Metadata().NextValuesID(),
	})
	filters := memo.FiltersExpr{{Condition: eq}, {Condition: memo.FalseSingleton}, {Condition: eq}}
	sel := f.ConstructSelect(vals, filters)
	if sel.Relational().Cardinality.Max != 0 {
		t.Fatalf("result should have been collapsed to zero cardinality rowset")
	}

	// Filters operator skips True operands.
	filters = memo.FiltersExpr{{Condition: eq}, {Condition: memo.TrueSingleton}}
	sel = f.ConstructSelect(vals, filters)
	if len(sel.(*memo.SelectExpr).Filters) != 1 {
		t.Fatalf("filters result should have filtered True operator")
	}
}
