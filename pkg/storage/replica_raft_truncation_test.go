// Copyright 2019 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package storage

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/podomy/tardigrade/pkg/keys"
	"github.com/podomy/tardigrade/pkg/roachpb"
	"github.com/podomy/tardigrade/pkg/storage/engine"
	"github.com/podomy/tardigrade/pkg/storage/stateloader"
	"github.com/podomy/tardigrade/pkg/util/hlc"
	"github.com/podomy/tardigrade/pkg/util/leaktest"
	"github.com/cockroachdb/datadriven"
	"github.com/stretchr/testify/assert"
)

func TestHandleTruncatedStateBelowRaft(t *testing.T) {
	defer leaktest.AfterTest(t)()

	// This test verifies the expected behavior of the downstream-of-Raft log
	// truncation code.

	ctx := context.Background()

	// neither exists
	// new one exists
	// truncstate regresses

	var prevTruncatedState roachpb.RaftTruncatedState
	datadriven.Walk(t, "testdata/truncated_state_migration", func(t *testing.T, path string) {
		const rangeID = 12
		loader := stateloader.Make(rangeID)
		eng := engine.NewInMem(roachpb.Attributes{}, 1<<20)
		defer eng.Close()

		datadriven.RunTest(t, path, func(d *datadriven.TestData) string {
			switch d.Cmd {
			case "prev":
				d.ScanArgs(t, "index", &prevTruncatedState.Index)
				d.ScanArgs(t, "term", &prevTruncatedState.Term)
				return ""
			case "put":
				var index uint64
				var term uint64
				d.ScanArgs(t, "index", &index)
				d.ScanArgs(t, "term", &term)

				truncState := &roachpb.RaftTruncatedState{
					Index: index,
					Term:  term,
				}

				assert.NoError(t, loader.SetRaftTruncatedState(ctx, eng, truncState))
				return ""
			case "handle":
				var buf bytes.Buffer

				var index uint64
				var term uint64
				d.ScanArgs(t, "index", &index)
				d.ScanArgs(t, "term", &term)

				newTruncatedState := &roachpb.RaftTruncatedState{
					Index: index,
					Term:  term,
				}

				apply, err := handleTruncatedStateBelowRaft(ctx, &prevTruncatedState, newTruncatedState, loader, eng)
				if err != nil {
					return err.Error()
				}
				fmt.Fprintf(&buf, "apply: %t\n", apply)

				key := keys.RaftTruncatedStateKey(rangeID)
				var truncatedState roachpb.RaftTruncatedState
				ok, err := engine.MVCCGetProto(ctx, eng, key, hlc.Timestamp{}, &truncatedState, engine.MVCCGetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if ok {
					fmt.Fprintf(&buf, "%s -> index=%d term=%d\n", key, truncatedState.Index, truncatedState.Term)
				}
				return buf.String()
			default:
			}
			return fmt.Sprintf("unsupported: %s", d.Cmd)
		})
	})
}
