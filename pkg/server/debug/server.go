// Copyright 2016 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package debug

import (
	"context"
	"expvar"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/podomy/tardigrade/pkg/settings"
	"github.com/podomy/tardigrade/pkg/settings/cluster"
	"github.com/podomy/tardigrade/pkg/util/stop"
	"github.com/pkg/errors"
	"github.com/rcrowley/go-metrics"
	"github.com/rcrowley/go-metrics/exp"
	"google.golang.org/grpc/metadata"
)

// RemoteMode controls who can access confidential /debug/* endpoints.
type RemoteMode string

const (
	// RemoteOff disallows access to confidential /debug/* endpoints.
	RemoteOff RemoteMode = "off"
	// RemoteLocal allows only host-local access to confidential /debug/* endpoints.
	RemoteLocal RemoteMode = "local"
	// RemoteAny allows all access to confidential /debug/* endpoints.
	RemoteAny RemoteMode = "any"
)

// Endpoint is the entry point under which the debug tools are housed.
const Endpoint = "/debug/"

// DebugRemote controls which clients are allowed to access certain
// confidential debug pages, such as those served under the /debug/ prefix.
var DebugRemote = settings.RegisterValidatedStringSetting(
	"server.remote_debugging.mode",
	"set to enable remote debugging, localhost-only or disable (any, local, off)",
	"local",
	func(sv *settings.Values, s string) error {
		switch RemoteMode(strings.ToLower(s)) {
		case RemoteOff, RemoteLocal, RemoteAny:
			return nil
		default:
			return errors.Errorf("invalid mode: '%s'", s)
		}
	},
)

// Server serves the /debug/* family of tools.
//
// The web interface (admin UI, pprof web UI, goroutine HTML UI, log spy UI,
// request-trace UI, landing redirect) has been removed. Only machine-oriented
// raw endpoints remain: standard pprof, expvar/metrics, and the stopper task
// list.
type Server struct {
	st  *cluster.Settings
	mux *http.ServeMux
}

// NewServer sets up a debug server.
func NewServer(st *cluster.Settings) *Server {
	mux := http.NewServeMux()

	// Cribbed straight from pprof's `init()` method. See:
	// https://golang.org/src/net/http/pprof/pprof.go
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	// This registers a superset of the variables exposed through the
	// /debug/vars endpoint onto the /debug/metrics endpoint. It includes all
	// expvars registered globally and all metrics registered on the
	// DefaultRegistry.
	mux.Handle("/debug/metrics", exp.ExpHandler(metrics.DefaultRegistry))
	// Also register /debug/vars (even though /debug/metrics is better).
	mux.Handle("/debug/vars", expvar.Handler())

	// Register the stopper endpoint, which lists all active tasks.
	mux.HandleFunc("/debug/stopper", stop.HandleDebug)

	return &Server{
		st:  st,
		mux: mux,
	}
}

// ServeHTTP serves various tools under the /debug endpoint. It restricts access
// according to the `server.remote_debugging.mode` cluster variable.
func (ds *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if authed := ds.authRequest(r); !authed {
		http.Error(w, "not allowed (due to the 'server.remote_debugging.mode' setting)",
			http.StatusForbidden)
		return
	}

	handler, _ := ds.mux.Handler(r)
	handler.ServeHTTP(w, r)
}

// authRequest restricts access to /debug/*.
func (ds *Server) authRequest(r *http.Request) bool {
	return authRequest(r.RemoteAddr, ds.st)
}

// authRequest restricts access according to the DebugRemote setting.
func authRequest(remoteAddr string, st *cluster.Settings) bool {
	switch RemoteMode(strings.ToLower(DebugRemote.Get(&st.SV))) {
	case RemoteAny:
		return true
	case RemoteLocal:
		return isLocalhost(remoteAddr)
	default:
		return false
	}
}

// isLocalhost returns true if the remoteAddr represents a client talking to
// us via localhost.
func isLocalhost(remoteAddr string) bool {
	// RemoteAddr is commonly in the form "IP" or "IP:port".
	// If it is in the form "IP:port", split off the port.
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

// GatewayRemoteAllowed returns whether a request that has been passed through
// the grpc gateway should be allowed accessed to privileged debugging
// information. Because this function assumes the presence of a context field
// populated by the grpc gateway, it's not applicable for other uses.
func GatewayRemoteAllowed(ctx context.Context, st *cluster.Settings) bool {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		// This should only happen for direct grpc connections, which are allowed.
		return true
	}
	peerAddr, ok := md["x-forwarded-for"]
	if !ok || len(peerAddr) == 0 {
		// This should only happen for direct grpc connections, which are allowed.
		return true
	}

	return authRequest(peerAddr[0], st)
}
