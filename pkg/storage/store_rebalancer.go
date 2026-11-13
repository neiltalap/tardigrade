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
	"math"
	"sort"

	"github.com/podomy/tardigrade/pkg/roachpb"
	"github.com/podomy/tardigrade/pkg/settings"
	"github.com/podomy/tardigrade/pkg/settings/cluster"
	"github.com/podomy/tardigrade/pkg/util/hlc"
	"github.com/podomy/tardigrade/pkg/util/log"
	"github.com/podomy/tardigrade/pkg/util/metric"
	"github.com/podomy/tardigrade/pkg/util/stop"
	"go.etcd.io/etcd/raft"
)

var (
	metaStoreRebalancerLeaseTransferCount = metric.Metadata{
		Name:        "rebalancing.lease.transfers",
		Help:        "Number of lease transfers motivated by store-level load imbalances",
		Measurement: "Lease Transfers",
		Unit:        metric.Unit_COUNT,
	}
	metaStoreRebalancerRangeRebalanceCount = metric.Metadata{
		Name:        "rebalancing.range.rebalances",
		Help:        "Number of range rebalance operations motivated by store-level load imbalances",
		Measurement: "Range Rebalances",
		Unit:        metric.Unit_COUNT,
	}
)

// StoreRebalancerMetrics is the set of metrics for the store-level rebalancer.
type StoreRebalancerMetrics struct {
	LeaseTransferCount  *metric.Counter
	RangeRebalanceCount *metric.Counter
}

func makeStoreRebalancerMetrics() StoreRebalancerMetrics {
	return StoreRebalancerMetrics{
		LeaseTransferCount:  metric.NewCounter(metaStoreRebalancerLeaseTransferCount),
		RangeRebalanceCount: metric.NewCounter(metaStoreRebalancerRangeRebalanceCount),
	}
}

// LoadBasedRebalancingMode controls whether range rebalancing takes
// additional variables such as write load and disk usage into account.
// If disabled, rebalancing is done purely based on replica count.
var LoadBasedRebalancingMode = settings.RegisterEnumSetting(
	"kv.allocator.load_based_rebalancing",
	"whether to rebalance based on the distribution of QPS across stores",
	"off",
	map[int64]string{
		int64(LBRebalancingOff):               "off",
		int64(LBRebalancingLeasesOnly):        "leases",
		int64(LBRebalancingLeasesAndReplicas): "leases and replicas",
	},
)

// qpsRebalanceThreshold is much like rangeRebalanceThreshold, but for
// QPS rather than range count. This should be set higher than
// rangeRebalanceThreshold because QPS can naturally vary over time as
// workloads change and clients come and go, so we need to be a little more
// forgiving to avoid thrashing.
var qpsRebalanceThreshold = settings.RegisterNonNegativeFloatSetting(
	"kv.allocator.qps_rebalance_threshold",
	"minimum fraction away from the mean a store's QPS (such as queries per second) can be before it is considered overfull or underfull",
	0.25,
)

// LBRebalancingMode controls if and when we do store-level rebalancing
// based on load.
type LBRebalancingMode int64

const (
	// LBRebalancingOff means that we do not do store-level rebalancing
	// based on load statistics.
	LBRebalancingOff LBRebalancingMode = iota
	// LBRebalancingLeasesOnly means that we rebalance leases based on
	// store-level QPS imbalances.
	LBRebalancingLeasesOnly
	// LBRebalancingLeasesAndReplicas means that we rebalance both leases and
	// replicas based on store-level QPS imbalances.
	LBRebalancingLeasesAndReplicas
)

// StoreRebalancer is responsible for examining how the associated store's load
// compares to the load on other stores in the cluster and transferring leases
// or replicas away if the local store is overloaded.
//
// This isn't implemented as a Queue because the Queues all operate on one
// replica at a time, making a local decision about each replica. Queues don't
// really know how the replica they're looking at compares to other replicas on
// the store. Our goal is balancing stores, though, so it's preferable to make
// decisions about each store and then carefully pick replicas to move that
// will best accomplish the store-level goals.
type StoreRebalancer struct {
	log.AmbientContext
	metrics         StoreRebalancerMetrics
	st              *cluster.Settings
	rq              *replicateQueue
	replRankings    *replicaRankings
	getRaftStatusFn func(replica *Replica) *raft.Status
}

// NewStoreRebalancer creates a StoreRebalancer to work in tandem with the
// provided replicateQueue.
func NewStoreRebalancer(
	ambientCtx log.AmbientContext,
	st *cluster.Settings,
	rq *replicateQueue,
	replRankings *replicaRankings,
) *StoreRebalancer {
	sr := &StoreRebalancer{
		AmbientContext: ambientCtx,
		metrics:        makeStoreRebalancerMetrics(),
		st:             st,
		rq:             rq,
		replRankings:   replRankings,
		getRaftStatusFn: func(replica *Replica) *raft.Status {
			return replica.RaftStatus()
		},
	}
	sr.AddLogTag("store-rebalancer", nil)
	sr.rq.store.metrics.registry.AddMetricStruct(&sr.metrics)
	return sr
}

// Start runs an infinite loop in a goroutine which regularly checks whether
// the store is overloaded along any important dimension (e.g. range count,
// QPS, disk usage), and if so attempts to correct that by moving leases or
// replicas elsewhere.
//
// Load-based rebalancing has been removed: this method now returns
// immediately without starting any background work. Range replication and
// lease transfers driven by zone constraints (replicateQueue and
// Allocator.AllocateTarget) are unchanged.
func (sr *StoreRebalancer) Start(ctx context.Context, stopper *stop.Stopper) {
}

// TODO(a-robinson): Should we take the number of leases on each store into
// account here or just continue to let that happen in allocator.go?
func (sr *StoreRebalancer) chooseLeaseToTransfer(
	ctx context.Context,
	hottestRanges *[]replicaWithStats,
	localDesc *roachpb.StoreDescriptor,
	storeList StoreList,
	storeMap map[roachpb.StoreID]*roachpb.StoreDescriptor,
	minQPS float64,
	maxQPS float64,
) (replicaWithStats, roachpb.ReplicaDescriptor, []replicaWithStats) {
	var considerForRebalance []replicaWithStats
	now := sr.rq.store.Clock().Now()
	for {
		if len(*hottestRanges) == 0 {
			return replicaWithStats{}, roachpb.ReplicaDescriptor{}, considerForRebalance
		}
		replWithStats := (*hottestRanges)[0]
		*hottestRanges = (*hottestRanges)[1:]

		// We're all out of replicas.
		if replWithStats.repl == nil {
			return replicaWithStats{}, roachpb.ReplicaDescriptor{}, considerForRebalance
		}

		if shouldNotMoveAway(ctx, replWithStats, localDesc, now, minQPS) {
			continue
		}

		// Don't bother moving leases whose QPS is below some small fraction of the
		// store's QPS (unless the store has extra leases to spare anyway). It's
		// just unnecessary churn with no benefit to move leases responsible for,
		// for example, 1 qps on a store with 5000 qps.
		const minQPSFraction = .001
		if replWithStats.qps < localDesc.Capacity.QueriesPerSecond*minQPSFraction &&
			float64(localDesc.Capacity.LeaseCount) <= storeList.candidateLeases.mean {
			log.VEventf(ctx, 5, "r%d's %.2f qps is too little to matter relative to s%d's %.2f total qps",
				replWithStats.repl.RangeID, replWithStats.qps, localDesc.StoreID, localDesc.Capacity.QueriesPerSecond)
			continue
		}

		desc, zone := replWithStats.repl.DescAndZone()
		log.VEventf(ctx, 3, "considering lease transfer for r%d with %.2f qps",
			desc.RangeID, replWithStats.qps)

		// Check all the other replicas in order of increasing qps. Learner replicas
		// aren't allowed to become the leaseholder or raft leader, so only consider
		// the `Voters` replicas.
		candidates := desc.Replicas().DeepCopy().Voters()
		sort.Slice(candidates, func(i, j int) bool {
			var iQPS, jQPS float64
			if desc := storeMap[candidates[i].StoreID]; desc != nil {
				iQPS = desc.Capacity.QueriesPerSecond
			}
			if desc := storeMap[candidates[j].StoreID]; desc != nil {
				jQPS = desc.Capacity.QueriesPerSecond
			}
			return iQPS < jQPS
		})

		var raftStatus *raft.Status

		preferred := sr.rq.allocator.preferredLeaseholders(zone, candidates)
		for _, candidate := range candidates {
			if candidate.StoreID == localDesc.StoreID {
				continue
			}

			meanQPS := storeList.candidateQueriesPerSecond.mean
			if shouldNotMoveTo(ctx, storeMap, replWithStats, candidate.StoreID, meanQPS, minQPS, maxQPS) {
				continue
			}

			if raftStatus == nil {
				raftStatus = sr.getRaftStatusFn(replWithStats.repl)
			}
			if replicaIsBehind(raftStatus, candidate.ReplicaID) {
				log.VEventf(ctx, 3, "%v is behind or this store isn't the raft leader for r%d; raftStatus: %v",
					candidate, desc.RangeID, raftStatus)
				continue
			}

			if len(preferred) > 0 && !storeHasReplica(candidate.StoreID, preferred) {
				log.VEventf(ctx, 3, "s%d not a preferred leaseholder for r%d; preferred: %v",
					candidate.StoreID, desc.RangeID, preferred)
				continue
			}

			filteredStoreList := storeList.filter(zone.Constraints)
			if sr.rq.allocator.followTheWorkloadPrefersLocal(
				ctx,
				filteredStoreList,
				*localDesc,
				candidate.StoreID,
				candidates,
				replWithStats.repl.leaseholderStats,
			) {
				log.VEventf(ctx, 3, "r%d is on s%d due to follow-the-workload; skipping",
					desc.RangeID, localDesc.StoreID)
				continue
			}

			return replWithStats, candidate, considerForRebalance
		}

		// If none of the other replicas are valid lease transfer targets, consider
		// this range for replica rebalancing.
		considerForRebalance = append(considerForRebalance, replWithStats)
	}
}

func (sr *StoreRebalancer) chooseReplicaToRebalance(
	ctx context.Context,
	hottestRanges *[]replicaWithStats,
	localDesc *roachpb.StoreDescriptor,
	storeList StoreList,
	storeMap map[roachpb.StoreID]*roachpb.StoreDescriptor,
	minQPS float64,
	maxQPS float64,
) (replicaWithStats, []roachpb.ReplicationTarget) {
	now := sr.rq.store.Clock().Now()
	for {
		if len(*hottestRanges) == 0 {
			return replicaWithStats{}, nil
		}
		replWithStats := (*hottestRanges)[0]
		*hottestRanges = (*hottestRanges)[1:]

		if replWithStats.repl == nil {
			return replicaWithStats{}, nil
		}

		if shouldNotMoveAway(ctx, replWithStats, localDesc, now, minQPS) {
			continue
		}

		// Don't bother moving ranges whose QPS is below some small fraction of the
		// store's QPS (unless the store has extra ranges to spare anyway). It's
		// just unnecessary churn with no benefit to move ranges responsible for,
		// for example, 1 qps on a store with 5000 qps.
		const minQPSFraction = .001
		if replWithStats.qps < localDesc.Capacity.QueriesPerSecond*minQPSFraction &&
			float64(localDesc.Capacity.RangeCount) <= storeList.candidateRanges.mean {
			log.VEventf(ctx, 5, "r%d's %.2f qps is too little to matter relative to s%d's %.2f total qps",
				replWithStats.repl.RangeID, replWithStats.qps, localDesc.StoreID, localDesc.Capacity.QueriesPerSecond)
			continue
		}

		desc, zone := replWithStats.repl.DescAndZone()
		log.VEventf(ctx, 3, "considering replica rebalance for r%d with %.2f qps",
			desc.RangeID, replWithStats.qps)

		clusterNodes := sr.rq.allocator.storePool.ClusterNodeCount()
		desiredReplicas := GetNeededReplicas(*zone.NumReplicas, clusterNodes)
		targets := make([]roachpb.ReplicationTarget, 0, desiredReplicas)
		targetReplicas := make([]roachpb.ReplicaDescriptor, 0, desiredReplicas)
		currentReplicas := desc.Replicas().All()

		// Check the range's existing diversity score, since we want to ensure we
		// don't hurt locality diversity just to improve QPS.
		curDiversity := rangeDiversityScore(
			sr.rq.allocator.storePool.getLocalities(currentReplicas))

		// Check the existing replicas, keeping around those that aren't overloaded.
		for i := range currentReplicas {
			if currentReplicas[i].StoreID == localDesc.StoreID {
				continue
			}
			// Keep the replica in the range if we don't know its QPS or if its QPS
			// is below the upper threshold. Punishing stores not in our store map
			// could cause mass evictions if the storePool gets out of sync.
			storeDesc, ok := storeMap[currentReplicas[i].StoreID]
			if !ok || storeDesc.Capacity.QueriesPerSecond < maxQPS {
				targets = append(targets, roachpb.ReplicationTarget{
					NodeID:  currentReplicas[i].NodeID,
					StoreID: currentReplicas[i].StoreID,
				})
				targetReplicas = append(targetReplicas, roachpb.ReplicaDescriptor{
					NodeID:  currentReplicas[i].NodeID,
					StoreID: currentReplicas[i].StoreID,
				})
			}
		}

		// Then pick out which new stores to add the remaining replicas to.
		options := sr.rq.allocator.scorerOptions()
		options.qpsRebalanceThreshold = qpsRebalanceThreshold.Get(&sr.st.SV)
		for len(targets) < desiredReplicas {
			// Use the preexisting AllocateTarget logic to ensure that considerations
			// such as zone constraints, locality diversity, and full disk come
			// into play.
			target, _ := sr.rq.allocator.allocateTargetFromList(
				ctx,
				storeList,
				zone,
				targetReplicas,
				options,
			)
			if target == nil {
				log.VEventf(ctx, 3, "no rebalance targets found to replace the current store for r%d",
					desc.RangeID)
				break
			}

			meanQPS := storeList.candidateQueriesPerSecond.mean
			if shouldNotMoveTo(ctx, storeMap, replWithStats, target.StoreID, meanQPS, minQPS, maxQPS) {
				break
			}

			targets = append(targets, roachpb.ReplicationTarget{
				NodeID:  target.Node.NodeID,
				StoreID: target.StoreID,
			})
			targetReplicas = append(targetReplicas, roachpb.ReplicaDescriptor{
				NodeID:  target.Node.NodeID,
				StoreID: target.StoreID,
			})
		}

		// If we couldn't find enough valid targets, forget about this range.
		//
		// TODO(a-robinson): Support more incremental improvements -- move what we
		// can if it makes things better even if it isn't great. For example,
		// moving one of the other existing replicas that's on a store with less
		// qps than the max threshold but above the mean would help in certain
		// locality configurations.
		if len(targets) < desiredReplicas {
			log.VEventf(ctx, 3, "couldn't find enough rebalance targets for r%d (%d/%d)",
				desc.RangeID, len(targets), desiredReplicas)
			continue
		}
		newDiversity := rangeDiversityScore(sr.rq.allocator.storePool.getLocalities(targetReplicas))
		if newDiversity < curDiversity {
			log.VEventf(ctx, 3,
				"new diversity %.2f for r%d worse than current diversity %.2f; not rebalancing",
				newDiversity, desc.RangeID, curDiversity)
			continue
		}

		// Pick the replica with the least QPS to be leaseholder;
		// RelocateRange transfers the lease to the first provided target.
		newLeaseIdx := 0
		newLeaseQPS := math.MaxFloat64
		var raftStatus *raft.Status
		for i := 0; i < len(targets); i++ {
			// Ensure we don't transfer the lease to an existing replica that is behind
			// in processing its raft log.
			if replica, ok := desc.GetReplicaDescriptor(targets[i].StoreID); ok {
				if raftStatus == nil {
					raftStatus = sr.getRaftStatusFn(replWithStats.repl)
				}
				if replicaIsBehind(raftStatus, replica.ReplicaID) {
					continue
				}
			}

			storeDesc, ok := storeMap[targets[i].StoreID]
			if ok && storeDesc.Capacity.QueriesPerSecond < newLeaseQPS {
				newLeaseIdx = i
				newLeaseQPS = storeDesc.Capacity.QueriesPerSecond
			}
		}
		targets[0], targets[newLeaseIdx] = targets[newLeaseIdx], targets[0]
		return replWithStats, targets
	}
}

func shouldNotMoveAway(
	ctx context.Context,
	replWithStats replicaWithStats,
	localDesc *roachpb.StoreDescriptor,
	now hlc.Timestamp,
	minQPS float64,
) bool {
	if !replWithStats.repl.OwnsValidLease(now) {
		log.VEventf(ctx, 3, "store doesn't own the lease for r%d", replWithStats.repl.RangeID)
		return true
	}
	if localDesc.Capacity.QueriesPerSecond-replWithStats.qps < minQPS {
		log.VEventf(ctx, 3, "moving r%d's %.2f qps would bring s%d below the min threshold (%.2f)",
			replWithStats.repl.RangeID, replWithStats.qps, localDesc.StoreID, minQPS)
		return true
	}
	return false
}

func shouldNotMoveTo(
	ctx context.Context,
	storeMap map[roachpb.StoreID]*roachpb.StoreDescriptor,
	replWithStats replicaWithStats,
	candidateStore roachpb.StoreID,
	meanQPS float64,
	minQPS float64,
	maxQPS float64,
) bool {
	storeDesc, ok := storeMap[candidateStore]
	if !ok {
		log.VEventf(ctx, 3, "missing store descriptor for s%d", candidateStore)
		return true
	}

	newCandidateQPS := storeDesc.Capacity.QueriesPerSecond + replWithStats.qps
	if storeDesc.Capacity.QueriesPerSecond < minQPS {
		if newCandidateQPS > maxQPS {
			log.VEventf(ctx, 3,
				"r%d's %.2f qps would push s%d over the max threshold (%.2f) with %.2f qps afterwards",
				replWithStats.repl.RangeID, replWithStats.qps, candidateStore, maxQPS, newCandidateQPS)
			return true
		}
	} else if newCandidateQPS > meanQPS {
		log.VEventf(ctx, 3,
			"r%d's %.2f qps would push s%d over the mean (%.2f) with %.2f qps afterwards",
			replWithStats.repl.RangeID, replWithStats.qps, candidateStore, meanQPS, newCandidateQPS)
		return true
	}

	return false
}

func storeListToMap(sl StoreList) map[roachpb.StoreID]*roachpb.StoreDescriptor {
	storeMap := make(map[roachpb.StoreID]*roachpb.StoreDescriptor)
	for i := range sl.stores {
		storeMap[sl.stores[i].StoreID] = &sl.stores[i]
	}
	return storeMap
}
