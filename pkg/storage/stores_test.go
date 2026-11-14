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
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/podomy/tardigrade/pkg/gossip"
	"github.com/podomy/tardigrade/pkg/roachpb"
	"github.com/podomy/tardigrade/pkg/settings/cluster"
	"github.com/podomy/tardigrade/pkg/storage/engine"
	"github.com/podomy/tardigrade/pkg/testutils"
	"github.com/podomy/tardigrade/pkg/util"
	"github.com/podomy/tardigrade/pkg/util/hlc"
	"github.com/podomy/tardigrade/pkg/util/leaktest"
	"github.com/podomy/tardigrade/pkg/util/log"
	"github.com/podomy/tardigrade/pkg/util/stop"
	"github.com/podomy/tardigrade/pkg/util/tracing"
)

func newStores(ambientCtx log.AmbientContext, clock *hlc.Clock) *Stores {
	return NewStores(ambientCtx, clock, cluster.BinaryMinimumSupportedVersion, cluster.BinaryServerVersion)
}

func TestStoresAddStore(t *testing.T) {
	defer leaktest.AfterTest(t)()
	ls := newStores(log.AmbientContext{Tracer: tracing.NewTracer()}, hlc.NewClock(hlc.UnixNano, time.Nanosecond))
	store := Store{
		Ident: &roachpb.StoreIdent{StoreID: 123},
	}
	ls.AddStore(&store)
	if !ls.HasStore(store.Ident.StoreID) {
		t.Errorf("expected local sender to contain storeID=%d", store.Ident.StoreID)
	}
	if ls.HasStore(store.Ident.StoreID + 1) {
		t.Errorf("expected local sender to not contain storeID=%d", store.Ident.StoreID+1)
	}
}

func TestStoresRemoveStore(t *testing.T) {
	defer leaktest.AfterTest(t)()
	ls := newStores(log.AmbientContext{Tracer: tracing.NewTracer()}, hlc.NewClock(hlc.UnixNano, time.Nanosecond))

	storeID := roachpb.StoreID(89)

	ls.AddStore(&Store{Ident: &roachpb.StoreIdent{StoreID: storeID}})

	ls.RemoveStore(&Store{Ident: &roachpb.StoreIdent{StoreID: storeID}})

	if ls.HasStore(storeID) {
		t.Errorf("expted local sender to remove storeID=%d", storeID)
	}
}

func TestStoresGetStoreCount(t *testing.T) {
	defer leaktest.AfterTest(t)()
	ls := newStores(log.AmbientContext{Tracer: tracing.NewTracer()}, hlc.NewClock(hlc.UnixNano, time.Nanosecond))
	if ls.GetStoreCount() != 0 {
		t.Errorf("expected 0 stores in new local sender")
	}

	expectedCount := 10
	for i := 0; i < expectedCount; i++ {
		ls.AddStore(&Store{Ident: &roachpb.StoreIdent{StoreID: roachpb.StoreID(i)}})
	}
	if count := ls.GetStoreCount(); count != expectedCount {
		t.Errorf("expected store count to be %d but was %d", expectedCount, count)
	}
}

func TestStoresVisitStores(t *testing.T) {
	defer leaktest.AfterTest(t)()
	ls := newStores(log.AmbientContext{Tracer: tracing.NewTracer()}, hlc.NewClock(hlc.UnixNano, time.Nanosecond))
	numStores := 10
	for i := 0; i < numStores; i++ {
		ls.AddStore(&Store{Ident: &roachpb.StoreIdent{StoreID: roachpb.StoreID(i)}})
	}

	visit := make([]bool, numStores)
	err := ls.VisitStores(func(s *Store) error { visit[s.Ident.StoreID] = true; return nil })
	if err != nil {
		t.Errorf("unexpected error on visit: %s", err.Error())
	}

	for i, visited := range visit {
		if !visited {
			t.Errorf("store %d was not visited", i)
		}
	}

	errBoom := errors.New("boom")
	if err := ls.VisitStores(func(s *Store) error {
		return errBoom
	}); err != errBoom {
		t.Errorf("got unexpected error %v", err)
	}
}

func TestStoresGetReplicaForRangeID(t *testing.T) {
	defer leaktest.AfterTest(t)()
	ctx := context.Background()
	stopper := stop.NewStopper()
	defer stopper.Stop(ctx)

	clock := hlc.NewClock(hlc.UnixNano, time.Nanosecond)

	ls := newStores(log.AmbientContext{}, clock)
	numStores := 10
	for i := 1; i <= numStores; i++ {
		storeID := roachpb.StoreID(i)
		rangeID := roachpb.RangeID(i)
		replicaID := roachpb.ReplicaID(1)

		memEngine := engine.NewInMem(roachpb.Attributes{}, 1<<20)
		stopper.AddCloser(memEngine)

		cfg := TestStoreConfig(clock)
		cfg.Transport = NewDummyRaftTransport(cfg.Settings)

		store := NewStore(ctx, cfg, memEngine, &roachpb.NodeDescriptor{NodeID: 1})
		// Fake-set an ident. This is usually read from the engine on store.Start()
		// which we're not even going to call.
		store.Ident = &roachpb.StoreIdent{StoreID: storeID}
		ls.AddStore(store)

		desc := &roachpb.RangeDescriptor{
			RangeID:  rangeID,
			StartKey: roachpb.RKey("a"),
			EndKey:   roachpb.RKey("b"),
			InternalReplicas: []roachpb.ReplicaDescriptor{
				{
					StoreID:   storeID,
					ReplicaID: replicaID,
					NodeID:    1,
				},
			},
		}

		replica, err := NewReplica(desc, store, 0)
		if err != nil {
			t.Fatalf("unexpected error when creating replica: %+v", err)
		}
		err2 := store.AddReplica(replica)
		if err2 != nil {
			t.Fatalf("unexpected error when adding replica: %v", err2)
		}
	}

	// Test the case where the replica we're looking for exists.
	rangeID1 := roachpb.RangeID(5)
	replica1, err1 := ls.GetReplicaForRangeID(rangeID1)
	if replica1 == nil {
		t.Fatal("expected replica to be found; was nil")
	}
	if err1 != nil {
		t.Fatalf("expected err to be nil; was %v", err1)
	}
	if replica1.RangeID != rangeID1 {
		t.Fatalf("expected replica's range id to be %v; got %v", rangeID1, replica1.RangeID)
	}

	// Test the case where the replica we're looking for doesn't exist.
	rangeID2 := roachpb.RangeID(1000)
	replica2, err2 := ls.GetReplicaForRangeID(rangeID2)
	if replica2 != nil {
		t.Fatalf("expected replica to be nil; was %v", replica2)
	}
	expectedError := roachpb.NewRangeNotFoundError(rangeID2, 0)
	if err2.Error() != expectedError.Error() {
		t.Fatalf("expected err to be %v; was %v", expectedError, err2)
	}
}

func TestStoresGetStore(t *testing.T) {
	defer leaktest.AfterTest(t)()
	ls := newStores(log.AmbientContext{Tracer: tracing.NewTracer()}, hlc.NewClock(hlc.UnixNano, time.Nanosecond))
	store := Store{Ident: &roachpb.StoreIdent{StoreID: 1}}
	replica := roachpb.ReplicaDescriptor{StoreID: store.Ident.StoreID}
	s, pErr := ls.GetStore(replica.StoreID)
	if s != nil || pErr == nil {
		t.Errorf("expected no stores in new local sender")
	}

	ls.AddStore(&store)
	s, pErr = ls.GetStore(replica.StoreID)
	if s == nil {
		t.Errorf("expected store")
	} else if s.Ident.StoreID != store.Ident.StoreID {
		t.Errorf("expected storeID to be %d but was %d",
			s.Ident.StoreID, store.Ident.StoreID)
	} else if pErr != nil {
		t.Errorf("expected no error, instead had err=%s", pErr)
	}
}

var storeIDAlloc roachpb.StoreID

// createStores creates a slice of count stores.
func createStores(count int, t *testing.T) (*hlc.ManualClock, []*Store, *Stores, *stop.Stopper) {
	stopper := stop.NewStopper()
	manual := hlc.NewManualClock(123)
	cfg := TestStoreConfig(hlc.NewClock(manual.UnixNano, time.Nanosecond))
	ls := newStores(log.AmbientContext{Tracer: tracing.NewTracer()}, cfg.Clock)

	// Create two stores with ranges we care about.
	stores := []*Store{}
	for i := 0; i < count; i++ {
		cfg.Transport = NewDummyRaftTransport(cfg.Settings)
		eng := engine.NewInMem(roachpb.Attributes{}, 1<<20)
		stopper.AddCloser(eng)
		s := NewStore(context.TODO(), cfg, eng, &roachpb.NodeDescriptor{NodeID: 1})
		storeIDAlloc++
		s.Ident = &roachpb.StoreIdent{StoreID: storeIDAlloc}
		stores = append(stores, s)
	}

	return manual, stores, ls, stopper
}

// TestStoresGossipStorage verifies reading and writing of bootstrap info.
func TestStoresGossipStorage(t *testing.T) {
	defer leaktest.AfterTest(t)()
	manual, stores, ls, stopper := createStores(2, t)
	defer stopper.Stop(context.TODO())
	ls.AddStore(stores[0])

	// Verify initial read is empty.
	var bi gossip.BootstrapInfo
	if err := ls.ReadBootstrapInfo(&bi); err != nil {
		t.Fatal(err)
	}
	if len(bi.Addresses) != 0 {
		t.Errorf("expected empty bootstrap info: %+v", bi)
	}

	// Add a fake address and write.
	manual.Increment(1)
	bi.Addresses = append(bi.Addresses, util.MakeUnresolvedAddr("tcp", "127.0.0.1:8001"))
	if err := ls.WriteBootstrapInfo(&bi); err != nil {
		t.Fatal(err)
	}

	// Verify on read.
	manual.Increment(1)
	var newBI gossip.BootstrapInfo
	if err := ls.ReadBootstrapInfo(&newBI); err != nil {
		t.Fatal(err)
	}
	if len(newBI.Addresses) != 1 {
		t.Errorf("expected single bootstrap info address: %+v", newBI)
	}

	// Add another store and verify it has bootstrap info written.
	ls.AddStore(stores[1])

	// Create a new stores object to verify read.
	ls2 := newStores(log.AmbientContext{Tracer: tracing.NewTracer()}, ls.clock)
	ls2.AddStore(stores[1])
	var verifyBI gossip.BootstrapInfo
	if err := ls2.ReadBootstrapInfo(&verifyBI); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bi, verifyBI) {
		t.Errorf("bootstrap info %+v not equal to expected %+v", verifyBI, bi)
	}
}

// TestStoresGossipStorageReadLatest verifies that the latest
// bootstrap info from multiple stores is returned on Read.
func TestStoresGossipStorageReadLatest(t *testing.T) {
	defer leaktest.AfterTest(t)()
	manual, stores, ls, stopper := createStores(2, t)
	defer stopper.Stop(context.TODO())
	ls.AddStore(stores[0])

	// Add a fake address and write.
	var bi gossip.BootstrapInfo
	bi.Addresses = append(bi.Addresses, util.MakeUnresolvedAddr("tcp", "127.0.0.1:8001"))
	if err := ls.WriteBootstrapInfo(&bi); err != nil {
		t.Fatal(err)
	}

	// Now remove store 0 and add store 1.
	ls.RemoveStore(stores[0])
	ls.AddStore(stores[1])

	// Increment clock, add another address and write.
	manual.Increment(1)
	bi.Addresses = append(bi.Addresses, util.MakeUnresolvedAddr("tcp", "127.0.0.1:8002"))
	if err := ls.WriteBootstrapInfo(&bi); err != nil {
		t.Fatal(err)
	}

	// Create a new stores object to freshly read. Should get latest
	// version from store 1.
	manual.Increment(1)
	ls2 := newStores(log.AmbientContext{Tracer: tracing.NewTracer()}, ls.clock)
	ls2.AddStore(stores[0])
	ls2.AddStore(stores[1])
	var verifyBI gossip.BootstrapInfo
	if err := ls2.ReadBootstrapInfo(&verifyBI); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bi, verifyBI) {
		t.Errorf("bootstrap info %+v not equal to expected %+v", verifyBI, bi)
	}

	// Verify that stores[0], which had old info, was updated with
	// latest bootstrap info during the read.
	ls3 := newStores(log.AmbientContext{Tracer: tracing.NewTracer()}, ls.clock)
	ls3.AddStore(stores[0])
	verifyBI.Reset()
	if err := ls3.ReadBootstrapInfo(&verifyBI); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bi, verifyBI) {
		t.Errorf("bootstrap info %+v not equal to expected %+v", verifyBI, bi)
	}
}

// TestStoresClusterVersionIncompatible verifies an error occurs when
// setting up the cluster version from stores that are incompatible with the
// running binary.
func TestStoresClusterVersionIncompatible(t *testing.T) {
	defer leaktest.AfterTest(t)()

	ctx := context.Background()

	vOneDashOne := roachpb.Version{Major: 1, Unstable: 1}
	vOne := roachpb.Version{Major: 1}

	type testFn func(*cluster.ClusterVersion, *Stores) string
	for name, setter := range map[string]testFn{
		"StoreTooNew": func(cv *cluster.ClusterVersion, ls *Stores) string {
			// This is what the running node requires from its stores.
			ls.minSupportedVersion = vOne
			// This is what the node is running.
			ls.serverVersion = vOneDashOne
			// Version is way too high for this node.
			cv.Version = roachpb.Version{Major: 9}
			return `cockroach version v1\.0-1 is incompatible with data in store <no-attributes>=<in-mem>; use version v9\.0 or later`
		},
		"StoreTooOldVersion": func(cv *cluster.ClusterVersion, ls *Stores) string {
			// This is what the running node requires from its stores.
			ls.minSupportedVersion = roachpb.Version{Major: 5}
			// This is what the node is running.
			ls.serverVersion = roachpb.Version{Major: 9}
			// Version is way too low.
			cv.Version = roachpb.Version{Major: 4}
			return `store <no-attributes>=<in-mem>, last used with cockroach version v4\.0, is too old for running version v9\.0 \(which requires data from v5\.0 or later\)`
		},
		"StoreTooOldMinVersion": func(cv *cluster.ClusterVersion, ls *Stores) string {
			// Like the previous test case, but this time cv.MinimumVersion is the culprit.
			ls.minSupportedVersion = roachpb.Version{Major: 5}
			ls.serverVersion = roachpb.Version{Major: 9}
			cv.Version = roachpb.Version{Major: 4}
			return `store <no-attributes>=<in-mem>, last used with cockroach version v4\.0, is too old for running version v9\.0 \(which requires data from v5\.0 or later\)`
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, stores, ls, stopper := createStores(1, t)
			defer stopper.Stop(ctx)
			ls.AddStore(stores[0])
			// Configure versions and write.
			var cv cluster.ClusterVersion
			expErr := setter(&cv, ls)
			if err := ls.WriteClusterVersion(ctx, cv); err != nil {
				t.Fatal(err)
			}
			if _, err := ls.SynthesizeClusterVersion(ctx); !testutils.IsError(err, expErr) {
				t.Fatalf("unexpected error: %+v", err)
			}
		})
	}
}
