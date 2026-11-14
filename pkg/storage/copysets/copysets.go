// Copyright 2019 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package copysets

// CopysetID is a custom type for a cockroach copyset ID.
// Copyset is a group of stores where a range should be contained within
// if copyset based rebalancing is enabled.
type CopysetID int32
