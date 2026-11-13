// Copyright 2017 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package sql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/podomy/tardigrade/pkg/roachpb"
	"github.com/podomy/tardigrade/pkg/settings"
	"github.com/podomy/tardigrade/pkg/settings/cluster"
	"github.com/podomy/tardigrade/pkg/sql/parser"
	"github.com/podomy/tardigrade/pkg/sql/sem/tree"
	"github.com/podomy/tardigrade/pkg/util/log"
	"github.com/podomy/tardigrade/pkg/util/syncutil"
	"github.com/podomy/tardigrade/pkg/util/timeutil"
)

type stmtKey struct {
	stmt        string
	failed      bool
	distSQLUsed bool
	optUsed     bool
	implicitTxn bool
}

// appStats holds per-application statistics.
type appStats struct {
	syncutil.Mutex

	st    *cluster.Settings
	stmts map[stmtKey]*stmtStats
	txns  transactionStats
}

// stmtStats holds per-statement statistics.
type stmtStats struct {
	syncutil.Mutex

	data roachpb.StatementStatistics
}

// transactionStats holds per-application transaction statistics.
type transactionStats struct {
	mu struct {
		syncutil.Mutex
		roachpb.TxnStats
	}
}

// stmtStatsEnable determines whether to collect per-statement
// statistics.
var stmtStatsEnable = settings.RegisterBoolSetting(
	"sql.metrics.statement_details.enabled", "collect per-statement query statistics", true,
)

// txnStatsEnable determines whether to collect per-application transaction
// statistics.
var txnStatsEnable = settings.RegisterBoolSetting(
	"sql.metrics.transaction_details.enabled", "collect per-application transaction statistics", true,
)

// sqlStatsCollectionLatencyThreshold specifies the minimum amount of time
// consumed by a SQL statement before it is collected for statistics reporting.
var sqlStatsCollectionLatencyThreshold = settings.RegisterDurationSetting(
	"sql.metrics.statement_details.threshold",
	"minimum execution time to cause statistics to be collected",
	0,
)

var dumpStmtStatsToLogBeforeReset = settings.RegisterBoolSetting(
	"sql.metrics.statement_details.dump_to_logs",
	"dump collected statement statistics to node logs when periodically cleared",
	false,
)

var sampleLogicalPlans = settings.RegisterBoolSetting(
	"sql.metrics.statement_details.plan_collection.enabled",
	"periodically save a logical plan for each fingerprint",
	true,
)

var logicalPlanCollectionPeriod = settings.RegisterNonNegativeDurationSetting(
	"sql.metrics.statement_details.plan_collection.period",
	"the time until a new logical plan is collected",
	5*time.Minute,
)

func (s stmtKey) String() string {
	return s.flags() + s.stmt
}

func (s stmtKey) flags() string {
	var b bytes.Buffer
	if s.failed {
		b.WriteByte('!')
	}
	if s.distSQLUsed {
		b.WriteByte('+')
	}
	if !s.optUsed {
		b.WriteByte('-')
	}
	return b.String()
}

// recordStatement saves per-statement statistics.
//
// samplePlanDescription can be nil, as these are only sampled periodically per unique fingerprint.
func (a *appStats) recordStatement(
	stmt *Statement,
	samplePlanDescription *roachpb.ExplainTreePlanNode,
	distSQLUsed bool,
	optUsed bool,
	implicitTxn bool,
	automaticRetryCount int,
	numRows int,
	err error,
	parseLat, planLat, runLat, svcLat, ovhLat float64,
	bytesRead, rowsRead int64,
) {
	if !stmtStatsEnable.Get(&a.st.SV) {
		return
	}

	if t := sqlStatsCollectionLatencyThreshold.Get(&a.st.SV); t > 0 && t.Seconds() >= svcLat {
		return
	}

	// Get the statistics object.
	s := a.getStatsForStmt(stmt, distSQLUsed, optUsed, implicitTxn, err, true /* createIfNonexistent */)

	// Collect the per-statement statistics.
	s.Lock()
	s.data.Count++
	if err != nil {
		s.data.SensitiveInfo.LastErr = err.Error()
	}
	// Only update MostRecentPlanDescription if we sampled a new PlanDescription.
	if samplePlanDescription != nil {
		s.data.SensitiveInfo.MostRecentPlanDescription = *samplePlanDescription
		s.data.SensitiveInfo.MostRecentPlanTimestamp = timeutil.Now()
	}
	if automaticRetryCount == 0 {
		s.data.FirstAttemptCount++
	} else if int64(automaticRetryCount) > s.data.MaxRetries {
		s.data.MaxRetries = int64(automaticRetryCount)
	}
	s.data.NumRows.Record(s.data.Count, float64(numRows))
	s.data.ParseLat.Record(s.data.Count, parseLat)
	s.data.PlanLat.Record(s.data.Count, planLat)
	s.data.RunLat.Record(s.data.Count, runLat)
	s.data.ServiceLat.Record(s.data.Count, svcLat)
	s.data.OverheadLat.Record(s.data.Count, ovhLat)
	s.data.BytesRead = bytesRead
	s.data.RowsRead = rowsRead
	s.Unlock()
}

// getStatsForStmt retrieves the per-stmt stat object.
func (a *appStats) getStatsForStmt(
	stmt *Statement,
	distSQLUsed bool,
	optimizerUsed bool,
	implicitTxn bool,
	err error,
	createIfNonexistent bool,
) *stmtStats {
	// Extend the statement key with various characteristics, so
	// that we use separate buckets for the different situations.
	key := stmtKey{failed: err != nil, distSQLUsed: distSQLUsed, optUsed: optimizerUsed, implicitTxn: implicitTxn}
	if stmt.AnonymizedStr != "" {
		// Use the cached anonymized string.
		key.stmt = stmt.AnonymizedStr
	} else {
		key.stmt = anonymizeStmt(stmt.AST)
	}

	return a.getStatsForStmtWithKey(key, createIfNonexistent)
}

func (a *appStats) getStatsForStmtWithKey(key stmtKey, createIfNonexistent bool) *stmtStats {
	a.Lock()
	// Retrieve the per-statement statistic object, and create it if it
	// doesn't exist yet.
	s, ok := a.stmts[key]
	if !ok && createIfNonexistent {
		s = &stmtStats{}
		a.stmts[key] = s
	}
	a.Unlock()
	return s
}

func anonymizeStmt(ast tree.Statement) string {
	return tree.AsStringWithFlags(ast, tree.FmtHideConstants)
}

func (s *transactionStats) getStats() (
	txnCount int64,
	txnTimeAvg float64,
	txnTimeVar float64,
	committedCount int64,
	implicitCount int64,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	txnCount = s.mu.TxnCount
	txnTimeAvg = s.mu.TxnTimeSec.Mean
	txnTimeVar = s.mu.TxnTimeSec.GetVariance(txnCount)
	committedCount = s.mu.CommittedCount
	implicitCount = s.mu.ImplicitCount
	return txnCount, txnTimeAvg, txnTimeVar, committedCount, implicitCount
}

func (s *transactionStats) recordTransaction(txnTimeSec float64, ev txnEvent, implicit bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mu.TxnCount++
	s.mu.TxnTimeSec.Record(s.mu.TxnCount, txnTimeSec)
	if ev == txnCommit {
		s.mu.CommittedCount++
	}
	if implicit {
		s.mu.ImplicitCount++
	}
}

func (a *appStats) recordTransaction(txnTimeSec float64, ev txnEvent, implicit bool) {
	if !txnStatsEnable.Get(&a.st.SV) {
		return
	}
	a.txns.recordTransaction(txnTimeSec, ev, implicit)
}

// sqlStats carries per-application statistics for all applications.
type sqlStats struct {
	syncutil.Mutex

	st *cluster.Settings
	// lastReset is the time at which the app containers were reset.
	lastReset time.Time
	// apps is the container for all the per-application statistics objects.
	apps map[string]*appStats
}

func (s *sqlStats) getStatsForApplication(appName string) *appStats {
	s.Lock()
	defer s.Unlock()
	if a, ok := s.apps[appName]; ok {
		return a
	}
	a := &appStats{
		st:    s.st,
		stmts: make(map[stmtKey]*stmtStats),
	}
	s.apps[appName] = a
	return a
}

// resetStats clears all the stored per-app and per-statement
// statistics.
func (s *sqlStats) resetStats(ctx context.Context) {
	// Note: we do not clear the entire s.apps map here. We would need
	// to do so to prevent problems with a runaway client running `SET
	// APPLICATION_NAME=...` with a different name every time.  However,
	// any ongoing open client session at the time of the reset has
	// cached a pointer to its appStats struct and would thus continue
	// to report its stats in an object now invisible to the other tools
	// (virtual table, marshaling, etc.). It's a judgement call, but
	// for now we prefer to see more data and thus not clear the map, at
	// the risk of seeing the map grow unboundedly with the number of
	// different application_names seen so far.

	s.Lock()
	// Clear the per-apps maps manually,
	// because any SQL session currently open has cached the
	// pointer to its appStats object and will continue to
	// accumulate data using that until it closes (or changes its
	// application_name).
	for appName, a := range s.apps {
		a.Lock()

		// Save the existing data to logs.
		// TODO(knz/dt): instead of dumping the stats to the log, save
		// them in a SQL table so they can be inspected by the DBA and/or
		// the UI.
		if dumpStmtStatsToLogBeforeReset.Get(&a.st.SV) {
			dumpStmtStats(ctx, appName, a.stmts)
		}

		// Clear the map, to release the memory; make the new map somewhat already
		// large for the likely future workload.
		a.stmts = make(map[stmtKey]*stmtStats, len(a.stmts)/2)
		a.Unlock()
	}
	s.lastReset = timeutil.Now()
	s.Unlock()
}

// Save the existing data for an application to the info log.
func dumpStmtStats(ctx context.Context, appName string, stats map[stmtKey]*stmtStats) {
	if len(stats) == 0 {
		return
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Statistics for %q:\n", appName)
	for key, s := range stats {
		s.Lock()
		json, err := json.Marshal(s.data)
		s.Unlock()
		if err != nil {
			log.Errorf(ctx, "error while marshaling stats for %q // %q: %v", appName, key.String(), err)
			continue
		}
		fmt.Fprintf(&buf, "%q: %s\n", key.String(), json)
	}
	log.Info(ctx, buf.String())
}

func scrubStmtStatKey(vt VirtualTabler, key string) (string, bool) {
	// Re-parse the statement to obtain its AST.
	stmt, err := parser.ParseOne(key)
	if err != nil {
		return "", false
	}

	// Re-format to remove most names.
	f := tree.NewFmtCtx(tree.FmtAnonymize)

	reformatFn := func(ctx *tree.FmtCtx, tn *tree.TableName) {
		virtual, err := vt.getVirtualTableEntry(tn)
		if err != nil || virtual.desc == nil {
			ctx.WriteByte('_')
			return
		}
		// Virtual table: we want to keep the name; however
		// we need to scrub the database name prefix.
		newTn := *tn
		newTn.CatalogName = "_"

		ctx.WithFlags(tree.FmtParsable, func() {
			ctx.WithReformatTableNames(nil, func() {
				ctx.FormatNode(&newTn)
			})
		})
	}
	f.SetReformatTableNames(reformatFn)
	f.FormatNode(stmt.AST)
	return f.CloseAndGetString(), true
}
