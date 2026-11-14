// Copyright 2017 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package cluster

import "github.com/podomy/tardigrade/pkg/roachpb"

// VersionKey is a unique identifier for a version of CockroachDB.
type VersionKey int

// Version constants.
//
// To add a version:
//   - Add it at the end of this block.
//   - Add it at the end of the `Versions` block below.
//   - For major or minor versions, bump BinaryMinimumSupportedVersion. For
//     example, if introducing the `1.4` release, bump it from `1.2` to `1.3`.
//
// To delete a version.
//   - Remove its associated runtime checks.
//   - If the version is not the latest one, delete the constant, comment out
//     its stanza, and say "Removed." above the versionsSingleton.
//
//go:generate stringer -type=VersionKey
const (
	_ VersionKey = iota - 1 // want first named one to start at zero
	Version2_1
	VersionUnreplicatedRaftTruncatedState // see versionsSingleton for details
	VersionSideloadedStorageNoReplicaID   // see versionsSingleton for details
	Version19_1
	VersionStart19_2
	VersionQueryTxnTimestamp
	VersionStickyBit
	VersionParallelCommits
	VersionGenerationComparable
	VersionLearnerReplicas
	VersionTopLevelForeignKeys
	VersionAtomicChangeReplicasTrigger
	VersionAtomicChangeReplicas
	VersionTableDescModificationTimeFromMVCC
	VersionPartitionedBackup
	Version19_2

	// Add new versions here (step one of two).

)

// versionsSingleton lists all historical versions here in chronological order,
// with comments describing what backwards-incompatible features were
// introduced.
//
// A roachpb.Version has the colloquial form MAJOR.MINOR[.PATCH][-UNSTABLE],
// where the PATCH and UNSTABLE components can be omitted if zero. Keep in mind
// that a version with an unstable component, like 1.1-2, represents a version
// that was developed AFTER v1.1 was released and is not slated for release
// until the next stable version (either 1.2-0 or 2.0-0). Patch releases, like
// 1.1.2, do not have associated migrations.
//
// NB: The version upgrade process requires the versions as seen by a cluster to
// be monotonic. Once we've added 1.1-0,  we can't slot in 1.0-4 because
// clusters already running 1.1-0 won't migrate through the new 1.0-4 version.
// Such clusters would need to be wiped. As a result, do not bump the major or
// minor version until we are absolutely sure that no new migrations will need
// to be added (i.e., when cutting the final release candidate).
var versionsSingleton = keyedVersions([]keyedVersion{
	{
		// Version2_1 is CockroachDB v2.1. It's used for all v2.1.x patch releases.
		Key:     Version2_1,
		Version: roachpb.Version{Major: 2, Minor: 1},
	},
	{
		// VersionUnreplicatedRaftTruncatedState is https://github.com/cockroachdb/cockroach/pull/34660.
		// When active, it moves the truncated state into unreplicated keyspace
		// on log truncations.
		//
		// The migration works as follows:
		//
		// 1. at any log position, the replicas of a Range either use the new
		// (unreplicated) key or the old one, and exactly one of them exists.
		//
		// 2. When a log truncation evaluates under the new cluster version,
		// it initiates the migration by deleting the old key. Under the old cluster
		// version, it behaves like today, updating the replicated truncated state.
		//
		// 3. The deletion signals new code downstream of Raft and triggers a write
		// to the new, unreplicated, key (atomic with the deletion of the old key).
		//
		// 4. Future log truncations don't write any replicated data any more, but
		// (like before) send along the TruncatedState which is written downstream
		// of Raft atomically with the deletion of the log entries. This actually
		// uses the same code as 3.
		// What's new is that the truncated state needs to be verified before
		// replacing a previous one. If replicas disagree about their truncated
		// state, it's possible for replica X at FirstIndex=100 to apply a
		// truncated state update that sets FirstIndex to, say, 50 (proposed by a
		// replica with a "longer" historical log). In that case, the truncated
		// state update must be ignored (this is straightforward downstream-of-Raft
		// code).
		//
		// 5. When a split trigger evaluates, it seeds the RHS with the legacy
		// key iff the LHS uses the legacy key, and the unreplicated key otherwise.
		// This makes sure that the invariant that all replicas agree on the
		// state of the migration is upheld.
		//
		// 6. When a snapshot is applied, the receiver is told whether the snapshot
		// contains a legacy key. If not, it writes the truncated state (which is
		// part of the snapshot metadata) in its unreplicated version. Otherwise
		// it doesn't have to do anything (the range will migrate later).
		//
		// The following diagram visualizes the above. Note that it abuses sequence
		// diagrams to get a nice layout; the vertical lines belonging to NewState
		// and OldState don't imply any particular ordering of operations.
		//
		// ┌────────┐                            ┌────────┐
		// │OldState│                            │NewState│
		// └───┬────┘                            └───┬────┘
		//     │                        Bootstrap under old version
		//     │ <─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─
		//     │                                     │
		//     │                                     │     Bootstrap under new version
		//     │                                     │ <─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─
		//     │                                     │
		//     │─ ─ ┐
		//     │    | Log truncation under old version
		//     │< ─ ┘
		//     │                                     │
		//     │─ ─ ┐                                │
		//     │    | Snapshot                       │
		//     │< ─ ┘                                │
		//     │                                     │
		//     │                                     │─ ─ ┐
		//     │                                     │    | Snapshot
		//     │                                     │< ─ ┘
		//     │                                     │
		//     │   Log truncation under new version  │
		//     │ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─>│
		//     │                                     │
		//     │                                     │─ ─ ┐
		//     │                                     │    | Log truncation under new version
		//     │                                     │< ─ ┘
		//     │                                     │
		//     │                                     │─ ─ ┐
		//     │                                     │    | Log truncation under old version
		//     │                                     │< ─ ┘ (necessarily running new binary)
		//
		// Source: http://www.plantuml.com/plantuml/uml/ and the following input:
		//
		// @startuml
		// scale 600 width
		//
		// OldState <--] : Bootstrap under old version
		// NewState <--] : Bootstrap under new version
		// OldState --> OldState : Log truncation under old version
		// OldState --> OldState : Snapshot
		// NewState --> NewState : Snapshot
		// OldState --> NewState : Log truncation under new version
		// NewState --> NewState : Log truncation under new version
		// NewState --> NewState : Log truncation under old version\n(necessarily running new binary)
		// @enduml
		Key:     VersionUnreplicatedRaftTruncatedState,
		Version: roachpb.Version{Major: 2, Minor: 1, Unstable: 6},
	},
	{
		// VersionSideloadedStorageNoReplicaID is https://github.com/cockroachdb/cockroach/pull/35035.
		//
		// It moves from a sideloaded directory naming scheme of
		// <rangeID>.<replicaID> to one that only depends on the rangeID. The
		// migration itself happens in storage.newDiskSideloadStorage, via
		// (*Replica).setReplicaIDRaftMuLockedMuLocked and is thus expected to
		// have completed when cluster version has been bumped and the cluster
		// restarted at least once post the bump (as calls to NewReplica then
		// carry out the migration for all replicas). We can thus safely remove
		// support for the legacy directory scheme in 2020.1 or we do it in
		// 2019.2 but have to require that all nodes be restarted at least once
		// while running 2019.1. It is straightforward to detect whether legacy
		// directories exist, so by adding code to 2020.1 to error out in this
		// case we can make removal in 2019.2 safe.
		Key:     VersionSideloadedStorageNoReplicaID,
		Version: roachpb.Version{Major: 2, Minor: 1, Unstable: 9},
	},
	{
		// Version19_1 is CockroachDB v19.1. It's used for all v19.1.x patch releases.
		Key:     Version19_1,
		Version: roachpb.Version{Major: 19, Minor: 1},
	},
	{
		// Version19_2_Start demarcates work towards CockroachDB v19.2.
		Key:     VersionStart19_2,
		Version: roachpb.Version{Major: 19, Minor: 1, Unstable: 1},
	},
	{
		// VersionQueryTxnTimestamp is https://github.com/cockroachdb/cockroach/pull/36307.
		Key:     VersionQueryTxnTimestamp,
		Version: roachpb.Version{Major: 19, Minor: 1, Unstable: 2},
	},
	{
		// VersionStickyBit is https://github.com/cockroachdb/cockroach/pull/37506.
		Key:     VersionStickyBit,
		Version: roachpb.Version{Major: 19, Minor: 1, Unstable: 3},
	},
	{
		// VersionParallelCommits is https://github.com/cockroachdb/cockroach/pull/37777.
		Key:     VersionParallelCommits,
		Version: roachpb.Version{Major: 19, Minor: 1, Unstable: 4},
	},
	{
		// VersionGenerationComparable is https://github.com/cockroachdb/cockroach/pull/38334.
		Key:     VersionGenerationComparable,
		Version: roachpb.Version{Major: 19, Minor: 1, Unstable: 5},
	},
	{
		// VersionLearnerReplicas is https://github.com/cockroachdb/cockroach/pull/38149.
		Key:     VersionLearnerReplicas,
		Version: roachpb.Version{Major: 19, Minor: 1, Unstable: 6},
	},
	{
		// VersionTopLevelForeignKeys is https://github.com/cockroachdb/cockroach/pull/39173.
		//
		// It represents an upgrade to the table descriptor format in which foreign
		// key references are pulled out of the index descriptors where they
		// originally were kept, and rewritten into a top level field on the index's
		// parent table descriptors. During a mixed-version state, the database will
		// write old-style table descriptors at all system boundaries, but upgrade
		// all old-style table descriptors into the new format upon read. Once the
		// upgrade is finalized, the database will write the upgraded format, but
		// continue to upgrade old-style descriptors on-demand.
		//
		// This version is also used for the new foreign key schema changes which
		// are run in the schema changer, requiring new types of mutations on the
		// table descriptor. The same version is used for both of these changes
		// because the changes are intertwined, and it slightly simplifies some of
		// the logic to assume that either neither or both sets of changes can be
		// active.
		Key:     VersionTopLevelForeignKeys,
		Version: roachpb.Version{Major: 19, Minor: 1, Unstable: 7},
	},
	{
		// VersionAtomicChangeReplicasTrigger is https://github.com/cockroachdb/cockroach/pull/39485.
		//
		// It enables use of updated fields in ChangeReplicasTrigger that will
		// support atomic replication changes.
		Key:     VersionAtomicChangeReplicasTrigger,
		Version: roachpb.Version{Major: 19, Minor: 1, Unstable: 8},
	},
	{
		// VersionAtomicChangeReplicas is https://github.com/cockroachdb/cockroach/pull/39936.
		//
		// It provides an implementation of (*Replica).ChangeReplicas that uses
		// atomic replication changes. The corresponding cluster setting
		// 'kv.atomic_replication_changes.enabled' provides a killswitch (i.e.
		// no atomic replication changes will be scheduled when it is set to
		// 'false').
		Key:     VersionAtomicChangeReplicas,
		Version: roachpb.Version{Major: 19, Minor: 1, Unstable: 9},
	},
	{
		// VersionTableDescModificationTimeFromMVCC is https://github.com/cockroachdb/cockroach/pull/40581
		//
		// It represents an upgrade to the table descriptor format in which
		// CreateAsOfTime and ModifiedTime are set to zero when new versions of
		// table descriptors are written. This removes the need to fix the commit
		// timestamp for transactions which update table descriptors. The value
		// is then populated by the reading client with the MVCC timestamp of the
		// row which contained the serialized table descriptor.
		Key:     VersionTableDescModificationTimeFromMVCC,
		Version: roachpb.Version{Major: 19, Minor: 1, Unstable: 10},
	},
	{
		// VersionPartitionedBackup is https://github.com/cockroachdb/cockroach/pull/39250.
		Key:     VersionPartitionedBackup,
		Version: roachpb.Version{Major: 19, Minor: 1, Unstable: 11},
	},
	{
		// Version19_2 is CockroachDB v19.2. It's used for all v19.2.x patch releases.
		Key:     Version19_2,
		Version: roachpb.Version{Major: 19, Minor: 2},
	},

	// Add new versions here (step two of two).

})

var (
	// BinaryMinimumSupportedVersion is the earliest version of data supported by
	// this binary. If this binary is started using a store marked with an older
	// version than BinaryMinimumSupportedVersion, then the binary will exit with
	// an error.
	BinaryMinimumSupportedVersion = VersionByKey(Version19_1)

	// BinaryServerVersion is the version of this binary.
	//
	// This is the version that a new cluster will use when created.
	BinaryServerVersion = versionsSingleton[len(versionsSingleton)-1].Version
)

// VersionByKey returns the roachpb.Version for a given key.
// It is a fatal error to use an invalid key.
func VersionByKey(key VersionKey) roachpb.Version {
	return versionsSingleton.MustByKey(key)
}
