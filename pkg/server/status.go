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
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/pkg/errors"
	"github.com/podomy/tardigrade/pkg/base"
	"github.com/podomy/tardigrade/pkg/gossip"
	"github.com/podomy/tardigrade/pkg/internal/client"
	"github.com/podomy/tardigrade/pkg/keys"
	"github.com/podomy/tardigrade/pkg/roachpb"
	"github.com/podomy/tardigrade/pkg/rpc"
	"github.com/podomy/tardigrade/pkg/security"
	"github.com/podomy/tardigrade/pkg/server/debug"
	"github.com/podomy/tardigrade/pkg/server/serverpb"
	"github.com/podomy/tardigrade/pkg/server/status/statuspb"
	"github.com/podomy/tardigrade/pkg/settings/cluster"
	"github.com/podomy/tardigrade/pkg/sql"
	"github.com/podomy/tardigrade/pkg/storage"
	"github.com/podomy/tardigrade/pkg/storage/storagepb"
	"github.com/podomy/tardigrade/pkg/util/contextutil"
	"github.com/podomy/tardigrade/pkg/util/httputil"
	"github.com/podomy/tardigrade/pkg/util/log"
	"github.com/podomy/tardigrade/pkg/util/stop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	// statusPrefix is the root of the cluster statistics and metrics API.
	statusPrefix = "/_status/"

	// statusVars exposes prometheus metrics for monitoring consumption.
	statusVars = statusPrefix + "vars"

	// maxConcurrentRequests is the maximum number of RPC fan-out requests
	// that will be made at any point of time.
	maxConcurrentRequests = 100

	// omittedKeyStr is the string returned in place of a key when keys aren't
	// permitted in responses.
	omittedKeyStr = "omitted (due to the 'server.remote_debugging.mode' setting)"
)

var (
	// Pattern for local used when determining the node ID.
	localRE = regexp.MustCompile(`(?i)local`)

	// Error used to convey that remote debugging is needs to be enabled for an
	// endpoint to be usable.
	remoteDebuggingErr = grpcstatus.Error(
		codes.PermissionDenied, "not allowed (due to the 'server.remote_debugging.mode' setting)")
)

type metricMarshaler interface {
	json.Marshaler
	PrintAsText(io.Writer) error
}

func propagateGatewayMetadata(ctx context.Context) context.Context {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		return metadata.NewOutgoingContext(ctx, md)
	}
	return ctx
}

// A statusServer provides a RESTful status API.
type statusServer struct {
	log.AmbientContext

	st              *cluster.Settings
	cfg             *base.Config
	admin           *adminServer
	db              *client.DB
	gossip          *gossip.Gossip
	metricSource    metricMarshaler
	nodeLiveness    *storage.NodeLiveness
	storePool       *storage.StorePool
	rpcCtx          *rpc.Context
	stores          *storage.Stores
	stopper         *stop.Stopper
	sessionRegistry *sql.SessionRegistry
	si              systemInfoOnce
}

// newStatusServer allocates and returns a statusServer.
func newStatusServer(
	ambient log.AmbientContext,
	st *cluster.Settings,
	cfg *base.Config,
	adminServer *adminServer,
	db *client.DB,
	gossip *gossip.Gossip,
	metricSource metricMarshaler,
	nodeLiveness *storage.NodeLiveness,
	storePool *storage.StorePool,
	rpcCtx *rpc.Context,
	stores *storage.Stores,
	stopper *stop.Stopper,
	sessionRegistry *sql.SessionRegistry,
) *statusServer {
	ambient.AddLogTag("status", nil)
	server := &statusServer{
		AmbientContext:  ambient,
		st:              st,
		cfg:             cfg,
		admin:           adminServer,
		db:              db,
		gossip:          gossip,
		metricSource:    metricSource,
		nodeLiveness:    nodeLiveness,
		storePool:       storePool,
		rpcCtx:          rpcCtx,
		stores:          stores,
		stopper:         stopper,
		sessionRegistry: sessionRegistry,
	}

	return server
}

// RegisterService registers the GRPC service.
func (s *statusServer) RegisterService(g *grpc.Server) {
	serverpb.RegisterStatusServer(g, s)
}

// RegisterGateway starts the gateway (i.e. reverse
// proxy) that proxies HTTP requests to the appropriate gRPC endpoints.
func (s *statusServer) RegisterGateway(
	ctx context.Context, mux *gwruntime.ServeMux, conn *grpc.ClientConn,
) error {
	ctx = s.AnnotateCtx(ctx)
	return serverpb.RegisterStatusHandler(ctx, mux, conn)
}

func (s *statusServer) parseNodeID(nodeIDParam string) (roachpb.NodeID, bool, error) {
	// No parameter provided or set to local.
	if len(nodeIDParam) == 0 || localRE.MatchString(nodeIDParam) {
		return s.gossip.NodeID.Get(), true, nil
	}

	id, err := strconv.ParseInt(nodeIDParam, 0, 32)
	if err != nil {
		return 0, false, errors.Wrap(err, "node id could not be parsed")
	}
	nodeID := roachpb.NodeID(id)
	return nodeID, nodeID == s.gossip.NodeID.Get(), nil
}

func (s *statusServer) dialNode(
	ctx context.Context, nodeID roachpb.NodeID,
) (serverpb.StatusClient, error) {
	addr, err := s.gossip.GetNodeIDAddress(nodeID)
	if err != nil {
		return nil, err
	}
	conn, err := s.rpcCtx.GRPCDialNode(addr.String(), nodeID,
		rpc.DefaultClass).Connect(ctx)
	if err != nil {
		return nil, err
	}
	return serverpb.NewStatusClient(conn), nil
}

// Gossip returns gossip network status.
func (s *statusServer) Gossip(
	ctx context.Context, req *serverpb.GossipRequest,
) (*gossip.InfoStatus, error) {
	if _, err := s.admin.requireAdminUser(ctx); err != nil {
		return nil, err
	}
	if !debug.GatewayRemoteAllowed(ctx, s.st) {
		return nil, remoteDebuggingErr
	}

	ctx = propagateGatewayMetadata(ctx)
	ctx = s.AnnotateCtx(ctx)
	nodeID, local, err := s.parseNodeID(req.NodeId)
	if err != nil {
		return nil, grpcstatus.Errorf(codes.InvalidArgument, err.Error())
	}

	if local {
		infoStatus := s.gossip.GetInfoStatus()
		return &infoStatus, nil
	}
	status, err := s.dialNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	return status.Gossip(ctx, req)
}

// Allocator returns simulated allocator info for the ranges on the given node.

// AllocatorRange returns simulated allocator info for the requested range.

// Certificates returns the x509 certificates.

func formatCertNames(p pkix.Name) string {
	return fmt.Sprintf("CommonName=%s, Organization=%s", p.CommonName, strings.Join(p.Organization, ","))
}

func extractCertFields(contents []byte, details *serverpb.CertificateDetails) error {
	certs, err := security.PEMContentsToX509(contents)
	if err != nil {
		return err
	}

	for _, c := range certs {
		addresses := c.DNSNames
		for _, ip := range c.IPAddresses {
			addresses = append(addresses, ip.String())
		}

		extKeyUsage := make([]string, len(c.ExtKeyUsage))
		for i, eku := range c.ExtKeyUsage {
			extKeyUsage[i] = security.ExtKeyUsageToString(eku)
		}

		var pubKeyInfo string
		if rsaPub, ok := c.PublicKey.(*rsa.PublicKey); ok {
			pubKeyInfo = fmt.Sprintf("%d bit RSA", rsaPub.N.BitLen())
		} else if ecdsaPub, ok := c.PublicKey.(*ecdsa.PublicKey); ok {
			pubKeyInfo = fmt.Sprintf("%d bit ECDSA", ecdsaPub.Params().BitSize)
		} else {
			// go's x509 library does not support other types (so far).
			pubKeyInfo = fmt.Sprintf("unknown key type %T", c.PublicKey)
		}

		details.Fields = append(details.Fields, serverpb.CertificateDetails_Fields{
			Issuer:             formatCertNames(c.Issuer),
			Subject:            formatCertNames(c.Subject),
			ValidFrom:          c.NotBefore.UnixNano(),
			ValidUntil:         c.NotAfter.UnixNano(),
			Addresses:          addresses,
			SignatureAlgorithm: c.SignatureAlgorithm.String(),
			PublicKey:          pubKeyInfo,
			KeyUsage:           security.KeyUsageToString(c.KeyUsage),
			ExtendedKeyUsage:   extKeyUsage,
		})
	}
	return nil
}

// Details returns node details.

// GetFiles returns a list of files of type defined in the request.

// checkFilePattern checks if a pattern is acceptable for the GetFiles call.
// Only patterns to match filenames are acceptable, not more general paths.
func checkFilePattern(pattern string) error {
	if strings.Contains(pattern, string(os.PathSeparator)) {
		return errors.New("invalid pattern: cannot have path seperators")
	}
	return nil
}

// LogFilesList returns a list of available log files.

// LogFile returns a single log file.

// parseInt64WithDefault attempts to parse the passed in string. If an empty
// string is supplied or parsing results in an error the default value is
// returned.  If an error does occur during parsing, the error is returned as
// well.
func parseInt64WithDefault(s string, defaultValue int64) (int64, error) {
	if len(s) == 0 {
		return defaultValue, nil
	}
	result, err := strconv.ParseInt(s, 10, 0)
	if err != nil {
		return defaultValue, err
	}
	return result, nil
}

// Logs returns the log entries parsed from the log files stored on
// the server. Log entries are returned in reverse chronological order. The
// following options are available:
// * "starttime" query parameter filters the log entries to only ones that
//   occurred on or after the "starttime". Defaults to a day ago.
// * "endtime" query parameter filters the log entries to only ones that
//   occurred before on on the "endtime". Defaults to the current time.
// * "pattern" query parameter filters the log entries by the provided regexp
//   pattern if it exists. Defaults to nil.
// * "max" query parameter is the hard limit of the number of returned log
//   entries. Defaults to defaultMaxLogEntries.
// To filter the log messages to only retrieve messages from a given level,
// use a pattern that excludes all messages at the undesired levels.
// (e.g. "^[^IW]" to only get errors, fatals and panics). An exclusive
// pattern is better because panics and some other errors do not use
// a prefix character.

// TODO(tschottdorf): significant overlap with /debug/pprof/goroutine, except
// that this one allows querying by NodeID.
//
// Stacks returns goroutine stack traces.

// TODO(tschottdorf): significant overlap with /debug/pprof/heap, except that
// this one allows querying by NodeID.
//
// Profile returns a heap profile.

// Nodes returns all node statuses.
func (s *statusServer) Nodes(
	ctx context.Context, req *serverpb.NodesRequest,
) (*serverpb.NodesResponse, error) {
	ctx = propagateGatewayMetadata(ctx)
	ctx = s.AnnotateCtx(ctx)
	startKey := keys.StatusNodePrefix
	endKey := startKey.PrefixEnd()

	b := &client.Batch{}
	b.Scan(startKey, endKey)
	if err := s.db.Run(ctx, b); err != nil {
		log.Error(ctx, err)
		return nil, grpcstatus.Errorf(codes.Internal, err.Error())
	}
	rows := b.Results[0].Rows

	resp := serverpb.NodesResponse{
		Nodes: make([]statuspb.NodeStatus, len(rows)),
	}
	for i, row := range rows {
		if err := row.ValueProto(&resp.Nodes[i]); err != nil {
			log.Error(ctx, err)
			return nil, grpcstatus.Errorf(codes.Internal, err.Error())
		}
	}
	return &resp, nil
}

// NodesWithLiveness returns all node statuses and their known
// liveness information according to gossip. Any known-dead, known-decommissioned
// nodes (= removed nodes) are excluded. Nodes for which there is
// no gossip information will have a liveness status set to UNKNOWN.
func (s *statusServer) NodesWithLiveness(
	ctx context.Context,
) (map[roachpb.NodeID]NodeStatusWithLiveness, error) {
	nodes, err := s.Nodes(ctx, nil)
	if err != nil {
		return nil, err
	}
	statusMap := s.nodeLiveness.GetLivenessStatusMap()
	ret := make(map[roachpb.NodeID]NodeStatusWithLiveness)
	for _, node := range nodes.Nodes {
		nodeID := node.Desc.NodeID
		livenessStatus := statusMap[nodeID]
		if livenessStatus == storagepb.NodeLivenessStatus_DECOMMISSIONED {
			// Skip over removed nodes.
			continue
		}
		ret[nodeID] = NodeStatusWithLiveness{
			NodeStatus:     node,
			LivenessStatus: livenessStatus,
		}
	}
	return ret, nil
}

// NodeStatusWithLiveness combines a NodeStatus with a NodeLivenessStatus.
type NodeStatusWithLiveness struct {
	statuspb.NodeStatus
	LivenessStatus storagepb.NodeLivenessStatus
}

// handleNodeStatus handles GET requests for a single node's status.
func (s *statusServer) Node(
	ctx context.Context, req *serverpb.NodeRequest,
) (*statuspb.NodeStatus, error) {
	ctx = propagateGatewayMetadata(ctx)
	ctx = s.AnnotateCtx(ctx)
	nodeID, _, err := s.parseNodeID(req.NodeId)
	if err != nil {
		return nil, grpcstatus.Errorf(codes.InvalidArgument, err.Error())
	}

	key := keys.NodeStatusKey(nodeID)
	b := &client.Batch{}
	b.Get(key)
	if err := s.db.Run(ctx, b); err != nil {
		log.Error(ctx, err)
		return nil, grpcstatus.Errorf(codes.Internal, err.Error())
	}

	var nodeStatus statuspb.NodeStatus
	if err := b.Results[0].Rows[0].ValueProto(&nodeStatus); err != nil {
		err = errors.Errorf("could not unmarshal NodeStatus from %s: %s", key, err)
		log.Error(ctx, err)
		return nil, grpcstatus.Errorf(codes.Internal, err.Error())
	}
	return &nodeStatus, nil
}

// Metrics return metrics information for the server specified.

// RaftDebug returns raft debug information for all known nodes.

func (s *statusServer) handleVars(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(httputil.ContentTypeHeader, httputil.PlaintextContentType)
	err := s.metricSource.PrintAsText(w)
	if err != nil {
		log.Error(r.Context(), err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Ranges returns range info for the specified node.

// HotRanges returns the hottest ranges on each store on the requested node(s).

// Range returns rangeInfos for all nodes in the cluster about a specific
// range. It also returns the range history for that range as well.

// ListLocalSessions returns a list of SQL sessions on this node.
func (s *statusServer) ListLocalSessions(
	ctx context.Context, req *serverpb.ListSessionsRequest,
) (*serverpb.ListSessionsResponse, error) {
	ctx = propagateGatewayMetadata(ctx)
	if !debug.GatewayRemoteAllowed(ctx, s.st) {
		return nil, remoteDebuggingErr
	}

	sessionUser, isAdmin, err := s.admin.getUserAndRole(ctx)
	if err != nil {
		return nil, err
	}

	if !isAdmin {
		// For non-superusers, requests with an empty username is
		// implicitly a request for the client's own sessions.
		if req.Username == "" {
			req.Username = sessionUser
		}

		// Non-superusers are not allowed to query sessions others than their own.
		if sessionUser != req.Username {
			return nil, grpcstatus.Errorf(
				codes.PermissionDenied,
				"client user %q does not have permission to view sessions from user %q",
				sessionUser, req.Username)
		}
	}

	// The empty username means "all sessions".
	showAll := req.Username == ""

	registry := s.sessionRegistry
	sessions := registry.SerializeAll()
	userSessions := make([]serverpb.Session, 0, len(sessions))

	for _, session := range sessions {
		if req.Username != session.Username && !showAll {
			continue
		}

		session.NodeID = s.gossip.NodeID.Get()
		userSessions = append(userSessions, session)
	}

	return &serverpb.ListSessionsResponse{Sessions: userSessions}, nil
}

// iterateNodes iterates nodeFn over all non-removed nodes concurrently.
// It then calls nodeResponse for every valid result of nodeFn, and
// nodeError on every error result.
func (s *statusServer) iterateNodes(
	ctx context.Context,
	errorCtx string,
	dialFn func(ctx context.Context, nodeID roachpb.NodeID) (interface{}, error),
	nodeFn func(ctx context.Context, client interface{}, nodeID roachpb.NodeID) (interface{}, error),
	responseFn func(nodeID roachpb.NodeID, resp interface{}),
	errorFn func(nodeID roachpb.NodeID, nodeFnError error),
) error {
	nodeStatuses, err := s.NodesWithLiveness(ctx)
	if err != nil {
		return err
	}

	// channels for responses and errors.
	type nodeResponse struct {
		nodeID   roachpb.NodeID
		response interface{}
		err      error
	}

	numNodes := len(nodeStatuses)
	responseChan := make(chan nodeResponse, numNodes)

	nodeQuery := func(ctx context.Context, nodeID roachpb.NodeID) {
		var client interface{}
		err := contextutil.RunWithTimeout(ctx, "dial node", base.NetworkTimeout, func(ctx context.Context) error {
			var err error
			client, err = dialFn(ctx, nodeID)
			return err
		})
		if err != nil {
			err = errors.Wrapf(err, "failed to dial into node %d (%s)",
				nodeID, nodeStatuses[nodeID].LivenessStatus)
			responseChan <- nodeResponse{nodeID: nodeID, err: err}
			return
		}

		res, err := nodeFn(ctx, client, nodeID)
		if err != nil {
			err = errors.Wrapf(err, "error requesting %s from node %d (%s)",
				errorCtx, nodeID, nodeStatuses[nodeID].LivenessStatus)
		}
		responseChan <- nodeResponse{nodeID: nodeID, response: res, err: err}
	}

	// Issue the requests concurrently.
	sem := make(chan struct{}, maxConcurrentRequests)
	for nodeID := range nodeStatuses {
		nodeID := nodeID // needed to ensure the closure below captures a copy.
		if err := s.stopper.RunLimitedAsyncTask(
			ctx, fmt.Sprintf("server.statusServer: requesting %s", errorCtx),
			sem, true, /* wait */
			func(ctx context.Context) { nodeQuery(ctx, nodeID) },
		); err != nil {
			return err
		}
	}

	var resultErr error
	for numNodes > 0 {
		select {
		case res := <-responseChan:
			if res.err != nil {
				errorFn(res.nodeID, res.err)
			} else {
				responseFn(res.nodeID, res.response)
			}
		case <-ctx.Done():
			resultErr = errors.Errorf("request of %s canceled before completion", errorCtx)
		}
		numNodes--
	}
	return resultErr
}

// ListSessions returns a list of SQL sessions on all nodes in the cluster.
func (s *statusServer) ListSessions(
	ctx context.Context, req *serverpb.ListSessionsRequest,
) (*serverpb.ListSessionsResponse, error) {
	if !debug.GatewayRemoteAllowed(ctx, s.st) {
		return nil, remoteDebuggingErr
	}

	ctx = propagateGatewayMetadata(ctx)
	ctx = s.AnnotateCtx(ctx)

	response := &serverpb.ListSessionsResponse{
		Sessions: make([]serverpb.Session, 0),
		Errors:   make([]serverpb.ListSessionsError, 0),
	}

	dialFn := func(ctx context.Context, nodeID roachpb.NodeID) (interface{}, error) {
		client, err := s.dialNode(ctx, nodeID)
		return client, err
	}
	nodeFn := func(ctx context.Context, client interface{}, _ roachpb.NodeID) (interface{}, error) {
		status := client.(serverpb.StatusClient)
		return status.ListLocalSessions(ctx, req)
	}
	responseFn := func(_ roachpb.NodeID, nodeResp interface{}) {
		sessions := nodeResp.(*serverpb.ListSessionsResponse)
		response.Sessions = append(response.Sessions, sessions.Sessions...)
	}
	errorFn := func(nodeID roachpb.NodeID, err error) {
		errResponse := serverpb.ListSessionsError{NodeID: nodeID, Message: err.Error()}
		response.Errors = append(response.Errors, errResponse)
	}

	if err := s.iterateNodes(ctx, "session list", dialFn, nodeFn, responseFn, errorFn); err != nil {
		err := serverpb.ListSessionsError{Message: err.Error()}
		response.Errors = append(response.Errors, err)
	}
	return response, nil
}

// CancelSession responds to a session cancellation request by canceling the
// target session's associated context.
func (s *statusServer) CancelSession(
	ctx context.Context, req *serverpb.CancelSessionRequest,
) (*serverpb.CancelSessionResponse, error) {
	sessionUser, isAdmin, err := s.admin.getUserAndRole(ctx)
	if err != nil {
		return nil, err
	}

	if !isAdmin && sessionUser != req.Username {
		// A user can only cancel their own sessions.
		return nil, errInsufficientPrivilege
	}

	ctx = s.AnnotateCtx(ctx)
	nodeID, local, err := s.parseNodeID(req.NodeId)

	if err != nil {
		return nil, grpcstatus.Errorf(codes.InvalidArgument, err.Error())
	}

	if !local {
		status, err := s.dialNode(ctx, nodeID)
		if err != nil {
			return nil, err
		}
		return status.CancelSession(ctx, req)
	}

	output := &serverpb.CancelSessionResponse{}
	canceled, err := s.sessionRegistry.CancelSession(req.SessionID, req.Username)

	if err != nil {
		output.Error = err.Error()
	}

	output.Canceled = canceled
	return output, nil
}

// CancelQuery responds to a query cancellation request, and cancels
// the target query's associated context and sets a cancellation flag.
func (s *statusServer) CancelQuery(
	ctx context.Context, req *serverpb.CancelQueryRequest,
) (*serverpb.CancelQueryResponse, error) {
	sessionUser, isAdmin, err := s.admin.getUserAndRole(ctx)
	if err != nil {
		return nil, err
	}

	if !isAdmin && sessionUser != req.Username {
		// A user can only cancel their own queries.
		return nil, errInsufficientPrivilege
	}

	ctx = propagateGatewayMetadata(ctx)
	ctx = s.AnnotateCtx(ctx)
	nodeID, local, err := s.parseNodeID(req.NodeId)

	if err != nil {
		return nil, grpcstatus.Errorf(codes.InvalidArgument, err.Error())
	}

	if !local {
		status, err := s.dialNode(ctx, nodeID)
		if err != nil {
			return nil, err
		}
		return status.CancelQuery(ctx, req)
	}

	output := &serverpb.CancelQueryResponse{}
	canceled, err := s.sessionRegistry.CancelQuery(req.QueryID, req.Username)

	if err != nil {
		output.Error = err.Error()
	}

	output.Canceled = canceled
	return output, nil
}

// jsonWrapper provides a wrapper on any slice data type being
// marshaled to JSON. This prevents a security vulnerability
// where a phishing attack can trick a user's browser into
// requesting a document from Cockroach as an executable script,
// allowing the contents of the fetched document to be treated
// as executable javascript. More details here:
// http://haacked.com/archive/2009/06/25/json-hijacking.aspx/
type jsonWrapper struct {
	Data interface{} `json:"d"`
}

// marshalToJSON marshals the given value into nicely indented JSON. If the
// value is an array or slice it is wrapped in jsonWrapper and then marshaled.
func marshalToJSON(value interface{}) ([]byte, error) {
	switch reflect.ValueOf(value).Kind() {
	case reflect.Array, reflect.Slice:
		value = jsonWrapper{Data: value}
	}
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, errors.Errorf("unable to marshal %+v to json: %s", value, err)
	}
	return body, nil
}

// marshalJSONResponse converts an arbitrary value into a JSONResponse protobuf
// that can be sent via grpc.
func marshalJSONResponse(value interface{}) (*serverpb.JSONResponse, error) {
	data, err := marshalToJSON(value)
	if err != nil {
		return nil, err
	}
	return &serverpb.JSONResponse{Data: data}, nil
}

func userFromContext(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		// If the incoming context has metadata but no attached web session user,
		// it's a gRPC / internal SQL connection which has root on the cluster.
		return security.RootUser, nil
	}
	usernames, ok := md[webSessionUserKeyStr]
	if !ok {
		// If the incoming context has metadata but no attached web session user,
		// it's a gRPC / internal SQL connection which has root on the cluster.
		return security.RootUser, nil
	}
	if len(usernames) != 1 {
		log.Warningf(ctx, "context's incoming metadata contains unexpected number of usernames: %+v ", md)
		return "", fmt.Errorf(
			"context's incoming metadata contains unexpected number of usernames: %+v ", md)
	}
	return usernames[0], nil
}

type systemInfoOnce struct {
	once sync.Once
	info serverpb.SystemInfo
}

func (si *systemInfoOnce) systemInfo(ctx context.Context) serverpb.SystemInfo {
	// We only want to attempt to populate the si.info once. If an error occurs
	// it is logged but the corresponding field in the returned struct is just
	// left empty as there isn't anything to do with an error from this function.
	si.once.Do(func() {
		// Don't use CommandContext because uname ought to not run for too long and
		// if the status request were canceled, we don't want future requests to
		// get blank information because we bailed out of this Once.
		cmd := exec.Command("uname", "-a")
		var errBuf bytes.Buffer
		cmd.Stderr = &errBuf
		output, err := cmd.Output()
		if err != nil {
			log.Warningf(ctx, "failed to get system information: %v\nstderr: %v",
				err, errBuf.String())
			return
		}
		si.info.SystemInfo = string(bytes.TrimSpace(output))
		cmd = exec.Command("uname", "-r")
		errBuf.Reset()
		cmd.Stderr = &errBuf
		output, err = cmd.Output()
		if err != nil {
			log.Warningf(ctx, "failed to get kernel information: %v\nstderr: %v",
				err, errBuf.String())
			return
		}
		si.info.KernelInfo = string(bytes.TrimSpace(output))
	})
	return si.info
}
