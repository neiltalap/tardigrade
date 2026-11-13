// Copyright 2018 The Cockroach Authors.
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
	"time"

	"github.com/podomy/tardigrade/pkg/config"
	"github.com/podomy/tardigrade/pkg/gossip"
	"github.com/podomy/tardigrade/pkg/internal/client"
	"github.com/podomy/tardigrade/pkg/settings"
	"github.com/podomy/tardigrade/pkg/util/hlc"
)

const (
	// mergeQueuePurgatoryCheckInterval is the interval at which replicas in
	// purgatory make merge attempts. Since merges are relatively untested, the
	// reasons that a range may fail to merge are unknown, so the merge queue has
	// a large purgatory interval.
	mergeQueuePurgatoryCheckInterval = 1 * time.Minute

	// The current implementation of merges requires rewriting the right-hand data
	// onto the left-hand range, even when the ranges are collocated. This is
	// expensive, so limit to one merge at a time.
	mergeQueueConcurrency = 1
)

// MergeQueueInterval is a setting that controls how often the merge queue waits
// between processing replicas.
var MergeQueueInterval = func() *settings.DurationSetting {
	s := settings.RegisterNonNegativeDurationSetting(
		"kv.range_merge.queue_interval",
		"how long the merge queue waits between processing replicas",
		time.Second,
	)
	s.SetSensitive()
	return s
}()

// mergeQueue manages a queue of ranges slated to be merged with their right-
// hand neighbor.
//
// A range will only be queued if it is beneath the minimum size threshold. Once
// queued, the size of the right-hand neighbor will additionally be checked;
// merges can only proceed if a) the right-hand neighbor is beneath the minimum
// size threshold, and b) the merged range would not need to be immediately
// split, e.g. because the new range would exceed the maximum size threshold.
//
// Note that the merge queue is not capable of initiating all possible merges.
// Consider the example below:
//
//      /Table/51/1    /Table/51/2    /Table/52
//         32MB            0MB           32MB
//
// The range beginning at /Table/51/2 is empty and would, ideally, be merged
// away. The range to its left, /Table/51/1, will not propose a merge because it
// is over the minimum size threshold. And /Table/51/2 will not propose a merge
// because the next range, /Table/52, is a new table and thus the split is
// mandatory.
//
// There are several ways to solve this. /Table/51/2 could look both left and
// right to find a merge partner, but discovering ones left neighbor is rather
// difficult and involves scanning the meta ranges. /Table/51/1 could propose a
// merge even though it's over the minimum size threshold, but this would result
// in a lot more RangeStats requests--essentially every range would send a
// RangeStats request on every scanner cycle.
//
// The current approach seems to be a nice balance of finding nearly all
// mergeable ranges without sending many RPCs. It has the additional nice
// property of not sending any RPCs to meta ranges until a merge is actually
// initiated.
type mergeQueue struct {
	*baseQueue
	db       *client.DB
	purgChan <-chan time.Time
}

func newMergeQueue(store *Store, db *client.DB, gossip *gossip.Gossip) *mergeQueue {
	mq := &mergeQueue{
		db:       db,
		purgChan: time.NewTicker(mergeQueuePurgatoryCheckInterval).C,
	}
	mq.baseQueue = newBaseQueue(
		"merge", mq, store, gossip,
		queueConfig{
			maxSize:        defaultQueueMaxSize,
			maxConcurrency: mergeQueueConcurrency,
			// TODO(ajwerner): Sometimes the merge queue needs to send multiple
			// snapshots, but the timeout function here is configured based on the
			// duration required to send a single snapshot. That being said, this
			// timeout provides leeway for snapshots to be 10x slower than the
			// specified rate and still respects the queue processing minimum timeout.
			// While using the below function is certainly better than just using the
			// default timeout, it would be better to have a function which takes into
			// account how many snapshots processing will need to send. That might be
			// hard to determine ahead of time. An alternative would be to calculate
			// the timeout with a function that additionally considers the replication
			// factor.
			processTimeoutFunc:   makeQueueSnapshotTimeoutFunc(rebalanceSnapshotRate),
			needsLease:           true,
			needsSystemConfig:    true,
			acceptsUnsplitRanges: false,
			successes:            store.metrics.MergeQueueSuccesses,
			failures:             store.metrics.MergeQueueFailures,
			pending:              store.metrics.MergeQueuePending,
			processingNanos:      store.metrics.MergeQueueProcessingNanos,
			purgatory:            store.metrics.MergeQueuePurgatory,
		},
	)
	return mq
}

func (mq *mergeQueue) shouldQueue(
	ctx context.Context, now hlc.Timestamp, repl *Replica, sysCfg *config.SystemConfig,
) (shouldQ bool, priority float64) {
	// The background merge queue has been removed. Ranges are never
	// automatically queued for merging; manual AdminMerge requests continue
	// to work through the normal admin path.
	return false, 0
}

func (mq *mergeQueue) process(
	ctx context.Context, lhsRepl *Replica, sysCfg *config.SystemConfig,
) error {
	// The background merge queue has been removed. This is a no-op;
	// manual AdminMerge requests continue to work through the normal admin
	// path.
	return nil
}

func (mq *mergeQueue) timer(time.Duration) time.Duration {
	return MergeQueueInterval.Get(&mq.store.ClusterSettings().SV)
}

func (mq *mergeQueue) purgatoryChan() <-chan time.Time {
	return mq.purgChan
}
