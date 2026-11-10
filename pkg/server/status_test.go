// Copyright 2015 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package server

import (
	"bytes"
	"context"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	"github.com/podomy/tardigrade/pkg/base"
	"github.com/podomy/tardigrade/pkg/rpc"
	"github.com/podomy/tardigrade/pkg/security"
	"github.com/podomy/tardigrade/pkg/server/serverpb"
	"github.com/podomy/tardigrade/pkg/server/status/statuspb"
	"github.com/podomy/tardigrade/pkg/testutils"
	"github.com/podomy/tardigrade/pkg/testutils/serverutils"
	"github.com/podomy/tardigrade/pkg/util/leaktest"
	"github.com/podomy/tardigrade/pkg/util/log"
	"github.com/podomy/tardigrade/pkg/util/protoutil"
)

func getStatusJSONProto(
	ts serverutils.TestServerInterface, path string, response protoutil.Message,
) error {
	return serverutils.GetJSONProto(ts, statusPrefix+path, response)
}

func getStatusJSONProtoWithAdminOption(
	ts serverutils.TestServerInterface, path string, response protoutil.Message, isAdmin bool,
) error {
	return serverutils.GetJSONProtoWithAdminOption(ts, statusPrefix+path, response, isAdmin)
}

// TestStatusLocalStacks verifies that goroutine stack traces are available
// via the /_status/stacks/local endpoint.

// TestStatusJson verifies that status endpoints return expected Json results.
// The content type of the responses is always httputil.JSONContentType.
func TestStatusJson(t *testing.T) {
	defer leaktest.AfterTest(t)()
	s, _, _ := serverutils.StartServer(t, base.TestServerArgs{})
	defer s.Stopper().Stop(context.TODO())

	var nodes serverpb.NodesResponse
	testutils.SucceedsSoon(t, func() error {
		if err := getStatusJSONProto(s, "nodes", &nodes); err != nil {
			t.Fatal(err)
		}

		if len(nodes.Nodes) == 0 {
			return errors.Errorf("expected non-empty node list, got: %v", nodes)
		}
		return nil
	})
}

// TestStatusGossipJson ensures that the output response for the full gossip
// info contains the required fields.

// TestStatusEngineStatsJson ensures that the output response for the engine
// stats contains the required fields.

// startServer will start a server with a short scan interval, wait for
// the scan to complete, and return the server. The caller is
// responsible for stopping the server.
func startServer(t *testing.T) *TestServer {
	tsI, _, kvDB := serverutils.StartServer(t, base.TestServerArgs{
		StoreSpecs: []base.StoreSpec{
			base.DefaultTestStoreSpec,
			base.DefaultTestStoreSpec,
			base.DefaultTestStoreSpec,
		},
	})

	ts := tsI.(*TestServer)

	// Make sure the range is spun up with an arbitrary read command. We do not
	// expect a specific response.
	if _, err := kvDB.Get(context.TODO(), "a"); err != nil {
		t.Fatal(err)
	}

	// Make sure the node status is available. This is done by forcing stores to
	// publish their status, synchronizing to the event feed with a canary
	// event, and then forcing the server to write summaries immediately.
	if err := ts.node.computePeriodicMetrics(context.TODO(), 0); err != nil {
		t.Fatalf("error publishing store statuses: %s", err)
	}

	if err := ts.WriteSummaries(); err != nil {
		t.Fatalf("error writing summaries: %s", err)
	}

	return ts
}

func newRPCTestContext(ts *TestServer, cfg *base.Config) *rpc.Context {
	rpcContext := rpc.NewContext(
		log.AmbientContext{Tracer: ts.ClusterSettings().Tracer}, cfg, ts.Clock(), ts.Stopper(),
		&ts.ClusterSettings().Version)
	// Ensure that the RPC client context validates the server cluster ID.
	// This ensures that a test where the server is restarted will not let
	// its test RPC client talk to a server started by an unrelated concurrent test.
	rpcContext.ClusterID.Set(context.TODO(), ts.ClusterID())
	return rpcContext
}

// TestStatusGetFiles tests the GetFiles endpoint.

// TestStatusLocalLogs checks to ensure that local/logfiles,
// local/logfiles/{filename} and local/log function
// correctly.

// TestNodeStatusResponse verifies that node status returns the expected
// results.
func TestNodeStatusResponse(t *testing.T) {
	defer leaktest.AfterTest(t)()
	s := startServer(t)
	defer s.Stopper().Stop(context.TODO())

	// First fetch all the node statuses.
	wrapper := serverpb.NodesResponse{}
	if err := getStatusJSONProto(s, "nodes", &wrapper); err != nil {
		t.Fatal(err)
	}
	nodeStatuses := wrapper.Nodes

	if len(nodeStatuses) != 1 {
		t.Errorf("too many node statuses returned - expected:1 actual:%d", len(nodeStatuses))
	}
	if !proto.Equal(&s.node.Descriptor, &nodeStatuses[0].Desc) {
		t.Errorf("node status descriptors are not equal\nexpected:%+v\nactual:%+v\n", s.node.Descriptor, nodeStatuses[0].Desc)
	}

	// Now fetch each one individually. Loop through the nodeStatuses to use the
	// ids only.
	for _, oldNodeStatus := range nodeStatuses {
		nodeStatus := statuspb.NodeStatus{}
		if err := getStatusJSONProto(s, "nodes/"+oldNodeStatus.Desc.NodeID.String(), &nodeStatus); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(&s.node.Descriptor, &nodeStatus.Desc) {
			t.Errorf("node status descriptors are not equal\nexpected:%+v\nactual:%+v\n", s.node.Descriptor, nodeStatus.Desc)
		}
	}
}

// TestMetricsEndpoint retrieves the metrics endpoint, which is currently only
// used for development purposes. The metrics within the response are verified
// in other tests.

// TestMetricsMetadata ensures that the server's recorder return metrics and
// that each metric has a Name, Help, Unit, and DisplayUnit defined.
func TestMetricsMetadata(t *testing.T) {
	defer leaktest.AfterTest(t)()
	s := startServer(t)
	defer s.Stopper().Stop(context.TODO())

	metricsMetadata := s.recorder.GetMetricsMetadata()

	if len(metricsMetadata) < 200 {
		t.Fatal("s.recorder.GetMetricsMetadata() failed sanity check; didn't return enough metrics.")
	}

	for _, v := range metricsMetadata {
		if v.Name == "" {
			t.Fatal("metric missing name.")
		}
		if v.Help == "" {
			t.Fatalf("%s missing Help.", v.Name)
		}
		if v.Measurement == "" {
			t.Fatalf("%s missing Measurement.", v.Name)
		}
		if v.Unit == 0 {
			t.Fatalf("%s missing Unit.", v.Name)
		}
	}
}

// TestChartCatalog ensures that the server successfully generates the chart catalog.

// findUndefinedMetrics finds metrics listed in pkg/ts/catalog/chart_catalog.go
// that are not defined. This is most likely caused by a metric being removed.

// deleteSeenMetrics removes all metrics in a section from the metricMetadata map.

// TestChartCatalogMetric ensures that all metrics are included in at least one
// chart, and that every metric included in a chart is still part of the metrics
// registry.

// TestStatusVars verifies that prometheus metrics are available via the
// /_status/vars endpoint.
func TestStatusVars(t *testing.T) {
	defer leaktest.AfterTest(t)()
	s, _, _ := serverutils.StartServer(t, base.TestServerArgs{})
	defer s.Stopper().Stop(context.TODO())

	if body, err := getText(s, s.AdminURL()+statusPrefix+"vars"); err != nil {
		t.Fatal(err)
	} else if !bytes.Contains(body, []byte("# TYPE sql_bytesout counter\nsql_bytesout")) {
		t.Errorf("expected sql_bytesout, got: %s", body)
	}
}

func TestNodesGRPCResponse(t *testing.T) {
	defer leaktest.AfterTest(t)()
	ts := startServer(t)
	defer ts.Stopper().Stop(context.TODO())

	rootConfig := testutils.NewTestBaseContext(security.RootUser)
	rpcContext := newRPCTestContext(ts, rootConfig)
	var request serverpb.NodesRequest

	url := ts.ServingRPCAddr()
	nodeID := ts.NodeID()
	conn, err := rpcContext.GRPCDialNode(url, nodeID, rpc.DefaultClass).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	client := serverpb.NewStatusClient(conn)

	response, err := client.Nodes(context.Background(), &request)
	if err != nil {
		t.Fatal(err)
	}

	if a, e := len(response.Nodes), 1; a != e {
		t.Errorf("expected %d node(s), found %d", e, a)
	}
}
