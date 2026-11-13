// Copyright 2014 The Cockroach Authors.
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
	"context"

	"github.com/podomy/tardigrade/pkg/roachpb"
	"github.com/podomy/tardigrade/pkg/storage/batcheval"
	"github.com/pkg/errors"
)

var importCmdFn ImportCmdFunc = func(context.Context, batcheval.CommandArgs) (*roachpb.ImportResponse, error) {
	return &roachpb.ImportResponse{}, errors.Errorf("unimplemented command: %s", roachpb.Import)
}

// ImportCmdFunc is the type of the function that will be called as the
// implementation of the Import command.
type ImportCmdFunc func(context.Context, batcheval.CommandArgs) (*roachpb.ImportResponse, error)
