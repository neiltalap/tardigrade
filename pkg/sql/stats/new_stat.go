// Copyright 2018 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package stats

import (
	"github.com/podomy/tardigrade/pkg/gossip"
	"github.com/podomy/tardigrade/pkg/sql/sqlbase"
)

// GossipTableStatAdded causes the statistic caches for this table to be
// invalidated.
func GossipTableStatAdded(g *gossip.Gossip, tableID sqlbase.ID) error {
	// TODO(radu): perhaps use a TTL here to avoid having a key per table floating
	// around forever (we would need the stat cache to evict old entries
	// automatically though).
	return g.AddInfo(
		gossip.MakeTableStatAddedKey(uint32(tableID)),
		nil, /* value */
		0,   /* ttl */
	)
}
