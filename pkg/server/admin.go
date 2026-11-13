// Copyright 2014 The Cockroach Authors.
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
	"context"
	"sort"

	"github.com/cockroachdb/errors"
	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/podomy/tardigrade/pkg/internal/client"
	"github.com/podomy/tardigrade/pkg/keys"
	"github.com/podomy/tardigrade/pkg/roachpb"
	"github.com/podomy/tardigrade/pkg/security"
	"github.com/podomy/tardigrade/pkg/server/serverpb"
	"github.com/podomy/tardigrade/pkg/sql"
	"github.com/podomy/tardigrade/pkg/sql/sem/tree"
	"github.com/podomy/tardigrade/pkg/storage/storagepb"
	"github.com/podomy/tardigrade/pkg/util/envutil"
	"github.com/podomy/tardigrade/pkg/util/log"
	"github.com/podomy/tardigrade/pkg/util/mon"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// adminPrefix is the prefix for RESTful endpoints used to provide an
	// administrative interface to the cockroach cluster.
	adminPrefix = "/_admin/v1/"
)

// apiServerMessage is the standard body for all HTTP 500 responses.
var errAdminAPIError = status.Errorf(codes.Internal, "An internal server error "+
	"has occurred. Please check your CockroachDB logs for more details.")

// A adminServer provides a RESTful HTTP API to administration of
// the cockroach cluster.
type adminServer struct {
	server     *Server
	memMonitor mon.BytesMonitor
	memMetrics *sql.MemoryMetrics
}

// noteworthyAdminMemoryUsageBytes is the minimum size tracked by the
// admin SQL pool before the pool start explicitly logging overall
// usage growth in the log.
var noteworthyAdminMemoryUsageBytes = envutil.EnvOrDefaultInt64("COCKROACH_NOTEWORTHY_ADMIN_MEMORY_USAGE", 100*1024)

// newAdminServer allocates and returns a new REST server for
// administrative APIs.
func newAdminServer(s *Server) *adminServer {
	server := &adminServer{server: s, memMetrics: &s.adminMemMetrics}
	// TODO(knz): We do not limit memory usage by admin operations
	// yet. Is this wise?
	server.memMonitor = mon.MakeUnlimitedMonitor(
		context.Background(),
		"admin",
		mon.MemoryResource,
		nil,
		nil,
		noteworthyAdminMemoryUsageBytes,
		s.ClusterSettings(),
	)
	return server
}

// RegisterService registers the GRPC service.
func (s *adminServer) RegisterService(g *grpc.Server) {
	serverpb.RegisterAdminServer(g, s)
}

// RegisterGateway starts the gateway (i.e. reverse proxy) that proxies HTTP requests
// to the appropriate gRPC endpoints.
func (s *adminServer) RegisterGateway(
	ctx context.Context, mux *gwruntime.ServeMux, conn *grpc.ClientConn,
) error {
	return serverpb.RegisterAdminHandler(ctx, mux, conn)
}

// serverError logs the provided error and returns an error that should be returned by
// the RPC endpoint method.
func (s *adminServer) serverError(err error) error {
	log.ErrorfDepth(context.TODO(), 1, "%s", err)
	return errAdminAPIError
}

// AllMetricMetadata returns all metrics' metadata.

// Databases is an endpoint that returns a list of databases.

// DatabaseDetails is an endpoint that returns grants and a list of table names
// for the specified database.

// TableDetails is an endpoint that returns columns, indices, and other
// relevant details for the specified table.

// generateTableSpan generates a table's key span.
//
// NOTE: this doesn't make sense for interleaved (children) table. As of
// 03/2018, callers around here use it anyway.

// TableStats is an endpoint that returns disk usage and replication statistics
// for the specified table.

// NonTableStats is an endpoint that returns disk usage and replication
// statistics for non-table parts of the system.

// Users returns a list of users, stripped of any passwords.

// Events is an endpoint that returns the latest event log entries, with the following
// optional URL parameters:
//
// type=STRING  returns events with this type (e.g. "create_table")
// targetID=INT returns events for that have this targetID

// make a best-effort attempt at redacting the setting value.

// RangeLog is an endpoint that returns the latest range log entries.

// getUIData returns the values and timestamps for the given UI keys. Keys
// that are not found will not be returned.

// makeUIKey combines username and key to form a lookup key in
// system.ui.
// The username is combined to ensure that different users
// can use different customizations.

// splitUIKey is the inverse of makeUIKey.
// The caller must ensure that the value was produced by makeUIKey.

// SetUIData is an endpoint that stores the given key/value pairs in the
// system.ui table. See GetUIData for more details on semantics.

// GetUIData returns data associated with the given keys, which was stored
// earlier through SetUIData.
//
// The stored values are meant to be opaque to the server. In the rare case that
// the server code needs to call this method, it should only read from keys that
// have the prefix `serverUIDataKeyPrefix`.

// Settings returns settings associated with the given keys.

// Cluster returns cluster metadata.

// Health returns liveness for the node target of the request.
//
// See the docstring for HealthRequest for more details about
// what this function precisely reports.
//
// Note: Health is non-privileged and non-authenticated and thus
// must not report privileged information.
func (s *adminServer) Health(
	ctx context.Context, req *serverpb.HealthRequest,
) (*serverpb.HealthResponse, error) {

	resp := &serverpb.HealthResponse{}
	// If Ready is not set, the client doesn't want to know whether this node is
	// ready to receive client traffic.
	if !req.Ready {
		return resp, nil
	}

	if err := s.checkReadinessForHealthCheck(); err != nil {
		return nil, err
	}
	return resp, nil
}

func (s *adminServer) checkReadinessForHealthCheck() error {
	serveMode := s.server.grpc.mode.get()
	switch serveMode {
	case modeInitializing:
		return status.Error(codes.Unavailable, "node is waiting for cluster initialization")
	case modeDraining:
		// grpc.mode is set to modeDraining when the Drain(DrainMode_CLIENT) has
		// been called (client connections are to be drained).
		return status.Errorf(codes.Unavailable, "node is shutting down")
	case modeOperational:
		break
	default:
		return s.serverError(errors.Newf("unknown mode: %v", serveMode))
	}

	// TODO(knz): update this code when progress is made on
	// https://github.com/cockroachdb/cockroach/issues/45123
	l, err := s.server.nodeLiveness.GetLiveness(s.server.NodeID())
	if err != nil {
		return s.serverError(err)
	}
	clock := s.server.clock
	ls := l.LivenessStatus(
		clock.PhysicalTime(),
		0, /* threshold */
		clock.MaxOffset(),
	)
	if isHealthy := ls == storagepb.NodeLivenessStatus_LIVE; !isHealthy {
		return status.Errorf(codes.Unavailable, "node is not healthy")
	}
	if l.Draining {
		// l.Draining indicates that the node is draining leases.
		// This is set when Drain(DrainMode_LEASES) is called.
		// It's possible that l.Draining is set without
		// grpc.mode being modeDraining, if a RPC client
		// has requested DrainMode_LEASES but not DrainMode_CLIENT.
		return status.Errorf(codes.Unavailable, "node is shutting down")
	}

	return nil
}

// Liveness returns the liveness state of all nodes on the cluster
// known to gossip. To reach all nodes in the cluster, consider
// using (statusServer).NodesWithLiveness instead.
func (s *adminServer) Liveness(
	context.Context, *serverpb.LivenessRequest,
) (*serverpb.LivenessResponse, error) {
	livenesses := s.server.nodeLiveness.GetLivenesses()
	statusMap := s.server.nodeLiveness.GetLivenessStatusMap()

	return &serverpb.LivenessResponse{
		Livenesses: livenesses,
		Statuses:   statusMap,
	}, nil
}

// QueryPlan returns a JSON representation of a distsql physical query
// plan.

// DecommissionStatus returns the DecommissionStatus for all or the given nodes.
func (s *adminServer) DecommissionStatus(
	ctx context.Context, req *serverpb.DecommissionStatusRequest,
) (*serverpb.DecommissionStatusResponse, error) {
	// Get the number of replicas on each node. We *may* not need all of them,
	// but that would be more complicated than seems worth it right now.
	ns, err := s.server.status.Nodes(ctx, &serverpb.NodesRequest{})
	if err != nil {
		return nil, errors.Wrap(err, "loading node statuses")
	}

	nodeIDs := req.NodeIDs
	// If no nodeIDs given, use all nodes.
	if len(nodeIDs) == 0 {
		for _, status := range ns.Nodes {
			nodeIDs = append(nodeIDs, status.Desc.NodeID)
		}
	}

	// Compute the replica counts for the target nodes only. This map doubles as
	// a lookup table to check whether we care about a given node.
	var replicaCounts map[roachpb.NodeID]int64
	if err := s.server.db.Txn(ctx, func(ctx context.Context, txn *client.Txn) error {
		const pageSize = 10000
		replicaCounts = make(map[roachpb.NodeID]int64)
		for _, nodeID := range nodeIDs {
			replicaCounts[nodeID] = 0
		}
		return txn.Iterate(ctx, keys.MetaMin, keys.MetaMax, pageSize,
			func(rows []client.KeyValue) error {
				rangeDesc := roachpb.RangeDescriptor{}
				for _, row := range rows {
					if err := row.ValueProto(&rangeDesc); err != nil {
						return errors.Wrapf(err, "%s: unable to unmarshal range descriptor", row.Key)
					}
					for _, r := range rangeDesc.Replicas().All() {
						if _, ok := replicaCounts[r.NodeID]; ok {
							replicaCounts[r.NodeID]++
						}
					}
				}
				return nil
			})
	}); err != nil {
		return nil, err
	}

	var res serverpb.DecommissionStatusResponse

	for nodeID := range replicaCounts {
		l, err := s.server.nodeLiveness.GetLiveness(nodeID)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to get liveness for %d", nodeID)
		}
		nodeResp := serverpb.DecommissionStatusResponse_Status{
			NodeID:          l.NodeID,
			ReplicaCount:    replicaCounts[l.NodeID],
			Decommissioning: l.Decommissioning,
			Draining:        l.Draining,
		}
		if l.IsLive(s.server.clock.Now(), s.server.clock.MaxOffset()) {
			nodeResp.IsLive = true
		}

		res.Status = append(res.Status, nodeResp)
	}

	sort.Slice(res.Status, func(i, j int) bool {
		return res.Status[i].NodeID < res.Status[j].NodeID
	})

	return &res, nil
}

// Decommission sets the decommission flag to the specified value on the specified node(s).
func (s *adminServer) Decommission(
	ctx context.Context, req *serverpb.DecommissionRequest,
) (*serverpb.DecommissionStatusResponse, error) {
	nodeIDs := req.NodeIDs
	if nodeIDs == nil {
		// If no NodeIDs are specified, decommission the current node. This is
		// used by `quit --decommission`.
		nodeIDs = []roachpb.NodeID{s.server.NodeID()}
	}

	// Mark the target nodes as decommissioning. They'll find out as they
	// heartbeat their liveness.
	if err := s.server.Decommission(ctx, req.Decommissioning, nodeIDs); err != nil {
		return nil, err
	}
	return s.DecommissionStatus(ctx, &serverpb.DecommissionStatusRequest{NodeIDs: nodeIDs})
}

// DataDistribution returns a count of replicas on each node for each table.

// EnqueueRange runs the specified range through the specified queue, returning
// the detailed trace and error information from doing so.

// enqueueRangeLocal checks whether the local node has a replica for the
// requested range that can be run through the queue, running it through the
// queue and returning trace/error information if so. If not, returns an empty
// response.

func (s *adminServer) requireAdminUser(ctx context.Context) (userName string, err error) {
	userName, isAdmin, err := s.getUserAndRole(ctx)
	if err != nil {
		return "", err
	}
	if !isAdmin {
		return "", errInsufficientPrivilege
	}
	return userName, nil
}

func (s *adminServer) getUserAndRole(
	ctx context.Context,
) (userName string, isAdmin bool, err error) {
	userName, err = userFromContext(ctx)
	if err != nil {
		return "", false, err
	}
	isAdmin, err = s.hasAdminRole(ctx, userName)
	return userName, isAdmin, err
}

func (s *adminServer) hasAdminRole(ctx context.Context, sessionUser string) (bool, error) {
	if sessionUser == security.RootUser {
		// Shortcut.
		return true, nil
	}
	rows, cols, err := s.server.internalExecutor.QueryWithUser(
		ctx, "check-is-admin",
		nil /* txn */, sessionUser, "SELECT crdb_internal.is_admin()")
	if err != nil {
		return false, err
	}
	if len(rows) != 1 || len(cols) != 1 {
		return false, errors.AssertionFailedf("hasAdminRole: expected 1 row, got %d", len(rows))
	}
	dbDatum, ok := tree.AsDBool(rows[0][0])
	if !ok {
		return false, errors.AssertionFailedf("hasAdminRole: expected bool, got %T", rows[0][0])
	}
	return bool(dbDatum), nil
}

var errInsufficientPrivilege = errors.New("this operation requires admin privilege")
