# Tardigrade core cuts

What was removed from the CockroachDB fork to leave a solid core
distributed SQL database, and why each cut is safe. One entry per cut:
what it was, why it went, what we lose, why the core is unaffected.

Core kept: Raft per range, serializable transactions, basic KV, splits,
PG wire compat for core DDL/DML, leases, HLC, gossip-based membership,
local + distributed row execution through the cost-based optimizer.

## SQL statements (grammar)

BACKUP / RESTORE / SHOW BACKUP, IMPORT / EXPORT, CHANGEFEED, SCRUB,
SHOW FINGERPRINTS, REPLICA TRACE, table PARTITIONING statements,
CREATE / DROP / GRANT / REVOKE ROLE, EXPERIMENTAL AUDIT, window functions.
Ten statement families cut from `sql.y` with zero grammar conflicts on
every regen, plus their opt operators, exec/distsql processors,
planner and tree wiring, help text, and logic/pretty/opt testdata.

They were bulk-IO, CDC, enterprise-gated, or analytic extras, each with
its own processor zoo and testdata tail. What we lose: backup files,
bulk ingest, change feeds, declarative partitioning, role-based access
control beyond users and privilege grants, window analytics. Core DDL,
DML, transactions, users, `SHOW ROLES`, privilege GRANT/REVOKE, views,
sequences, and COPY are untouched, so everyday SQL is unaffected.

## Web interface

`pkg/ui/` (browser bundle stub and asset handler), the `/` index handler,
debug web consoles (`pprofui`, `goroutineui`, log spy, request-trace pages,
`/debug/` landing redirect), `newAuthenticationMuxAllowAnonymous`, and the
`webui:` startup line. `/` now returns 404.

A browser UI is an app shipped inside the database binary. It pulled in
asset embedding, session-cookie templating into HTML, and a parallel auth
path just to serve JavaScript. What we lose: point-and-click admin graphs.
The machine APIs stay (`/_admin`, `/_status`, `/ts`, `/health`, `/login`,
`/logout`, raw `pprof`, `vars`/`metrics`, `stopper`), so orchestration,
health checks, and debuggability are unaffected.

## Telemetry and phone-home

Telemetry system, endpoint counters, feature-usage tables (kept as an
always-empty compat stub), updater/phone-home, cloudinfo, AWS SDK import,
encryption hooks. Tests asserting on counters were deleted, not faked;
two lint rules enforcing telemetry call conventions went with them.

Usage reporting is vendor observability, not database function. What we
lose: upstream gets no reports about this binary. Query behavior is
unchanged; the only visible difference is empty `feature_usage` output.

## Vectorized execution engine

`pkg/sql/colexec/`, `pkg/col/`, `pkg/sql/colflow/`, `pkg/sql/colencoding/`,
`explain_vec.go` (about 48k lines over 204 files, most of it `*.eg.go`
type-combination codegen). The `vectorize` settings remain as inert stubs
defaulting to off; `EXPLAIN (VEC)` returns an honest error.

It was genuinely fast: column batches of about 1024 rows instead of
row-at-a-time volcano iteration, so tight loops over contiguous arrays,
fewer branches and virtual calls, roughly 2-4x on scan-heavy aggregations
and sorts. Real speed, not marketing. But the structure is the problem,
not the idea: one operator per file times two engines, plus generated
copies per type combination. Two engines implementing identical SQL
semantics in lockstep forever is duplication debt. As a migration tactic
the parallel engine was defensible (isolation, per-operator rollout, flag
fallback); as an end state it is bloat. What we lose: raw analytic scan
speed. Every plan still executes correctly through the row engine, local
and distributed. If speed is ever needed back, the right structure is one
narrow columnar fast lane (scan, filter, project, hash aggregate) sharing
the row engine's semantics, not a second engine.

## JSON builtins (bulk cut, narrow restore)

Thirty-plus `json_*` / `jsonb_*` builtins removed (builders, extractors,
setters, `typeof`, `each`, `object_keys`, existence operators and their
opt `JsonExists` ops, norm rules, and fold whitelist entries). Four were
restored with evidence, not sentiment: `json_agg`, `jsonb_agg`,
`jsonb_pretty`, and `json[b]_array_elements`, because the decommission
test inspects range logs through them and plan reproduction renders
statistics through them. The `#-` operator desugar keeps
`json_remove_path`.

Most of these are application conveniences that Postgres happens to ship.
What we lose: JSON document manipulation in SQL. JSONB storage and casts
stay, so documents still store and compare; only the manipulation
function library shrinks to what core introspection actually calls.

## ClosedTS, RangeFeed, follower reads

`closedts/` and `rangefeed/` trees, `dist_sender_rangefeed.go`, follower
timestamp builtins and routing. The `RangeFeed` RPC keeps an honest stub
that returns "no longer supported" so the dispatch chain still compiles
and misconfigured callers get a clean error instead of a hang.

These serve follower (non-leaseholder) reads and external change
consumers. Cutting them removes a whole timestamp-tracking subsystem and
its tests. What we lose: stale-but-fast replica reads and streaming
change consumers. Leaseholder reads and writes, the actual consistency
core, are untouched.

## Roles, audit, partitioning descriptors (residue)

Role statement wiring, `EXPERIMENTAL AUDIT` mode, `PARTITION BY`
descriptors beyond inert compat fields. `PartitionBy` struct fields and
job payload shells stay where the opt interface or wire format would
otherwise churn, but no statement can produce them.

Half-removed features are worse than present ones: dead descriptors that
no statement fills yet every reader must handle. What we lose: nothing
executable; these paths were already unreachable.

## Storage background queues (no-op, not delete)

Merge queue (`shouldQueue` always false, `process` nil), TS maintenance
queue (deleted with its registration), consistency-checker queue
(diagnostic only), store-rebalancer worker (`Start` returns immediately).
Structs, settings, and metrics wiring stay so callers compile unchanged;
manual `AdminMerge` still works.

Background optimizers that rearrange data for you: auto-merging empty
ranges, pruning old time series, rebalancing on load. What we lose:
the cluster no longer tidies itself on these axes; splits still fire,
replication still repairs, snapshots still catch followers up. An
operator can merge ranges by hand; nothing about consensus or
durability changes.

## CLI and tooling

`bench`, `sqlfmt`, `encryption-key` commands; `zip`, `demo`, `gen`,
`examples`, `haproxy`, `systembench`, `syncbench`, `debug_merge_logs`,
`debug_check_store`, `debug_synctest`; `heapprofiler`, `goroutinedumper`,
`diagnosticspb`; `pkg/cmd/roachtest`, `roachprod`, `docgen`,
`cmp-protocol`, and sibling one-shot tools; `pkg/workload/*` generators;
`pkg/internal/sqlsmith/*`; `docs/*`, `monitoring/*`, `cloud/*`
manifests; driver harnesses, sim binaries, acceptance harness.

Benchmarks, load generators, cloud provisioners, doc publishers, and
fuzzers are a vendor's go-to-market kit, not the database. What we lose:
no in-repo way to run TPCC, publish docs, or provision cloud clusters.
`cockroach-oss` and `cockroach-short` build clean; every deleted tool's
tests went with it.

## Debug remainder (slim, not gone)

Kept raw `pprof`, `vars`/`metrics`, and the `stopper` task list behind
the existing auth gate. Removed the HTML around them. Memory-stats
sampling and the runtime stat sampler stay because the status recorder
feeds on them.

## Statistics: collection removed, read path kept

`create_stats.go`, `automatic_stats.go`, `row_sampling.go`,
`delete_stats.go`, the sampler and sample-aggregator processors,
`distsql_plan_stats.go`, the refresher wiring in `server.go` and the
mutation hooks in DML paths are gone. `CREATE STATISTICS` parses but
returns an honest "not supported" error; background auto-collection no
longer exists, so `distsql_automatic_stats` testdata went with it.

Kept: the read path (`stats_cache.go`, `json.go`, histogram types,
`TableStatistic` protos, `new_stat.go`) plus `SHOW STATISTICS` /
`SHOW HISTOGRAM` / `ALTER TABLE ... INJECT STATISTICS`. The optimizer
test corpus injects statistics by the thousand; deleting the read path
would gut coverage without removing any runtime cost, since collection
was the expensive part. The planner falls back to default selectivity
when no statistics exist, so real queries are unaffected.

## Small correctness-boundary removals

Savepoint-restart escape hatch, legacy OID remap, follower-timestamp
builtin, `KVOption` helper, `WindowerStats` proto, changefeed chart
catalog section, `LicenseType` var (zero callers). Each was a hatch,
shim, or dashboard for something already cut. What we lose: escape
routes around guarantees we want to actually hold.

## Migrations: manager replaced with fresh bootstrap

`pkg/sqlmigrations/*` deleted. It existed to upgrade old clusters; a new
system never has one. `server.go` now calls `bootstrapFreshCluster` on
first boot only, which replicates the needed effects idempotently
(root/admin membership, version and settings keys, default databases).
`TestSelfBootstrap` passes. The `SQLMigrationManager` knob and all its
test blocks went with the package.

## Replication reports: tables and reporter removed

`pkg/storage/reports/*` and the `system.replication_stats`,
`replication_constraint_stats`, and `replication_critical_localities`
tables are gone, with their migrations, settings, logictest rows, and
zip fixtures. The reporter only fed allocation-report UIs that no longer
exist. Replication itself (queues, allocator, snapshots) is untouched;
only the commentary about it went away.

## Time series: subsystem removed

`pkg/ts/` is deleted except `tspb/` and `catalog/chart_catalog` pure
types (still referenced by generated `admin.pb.go`/`chart_catalog.pb.go`
descriptors). `server.go` no longer constructs, serves, or polls the TS
DB; `/ts/` falls through to 404; `debug tsdump`, the TS test knobs, and
`TestMetricsRecording` went with it. The recorder keeps serving live
metrics (`vars`, status summaries); only historical storage is gone.
One real find during removal: `keys.PrettyPrintTimeseriesKey` was a hook
assigned by the deleted package, and replica init pretty-prints range
bounds over the still-present TS keyspace, so it panicked on boot. The
printer now falls back to raw output when the hook is nil.

## Grammar compat stubs removed

`opt_partition_by`, the `func_expr_windowless` alias, the bare
`EXPERIMENTAL` token, and the `PARTITION` keyword are out of `sql.y`,
regenerated with zero goyacc conflicts. `PARTITION BY ...` now fails to
parse instead of parsing to a stub that produced nil. Parser and lexer
suites green.

## Merge and zigzag join executors removed, planning defaults off

`mergejoiner.go` and `zigzagjoiner.go` are deleted; unknown specs fail
honestly in `NewProcessor`. The planner still can emit both shapes, but
`sql.distsql.merge_joins.enabled` and `sql.defaults.zigzag_join.enabled`
now default to false, so no executable plan contains them (explicit
opt-ins error at execution). FK validation and interleaved-join tests
that executed merge plans now run hash plans; EXPLAIN-only goldens were
rewritten.

This cut caught a real wrong-results bug live: the optimizer plans
`group-by (ordered)` relying on merge-join output ordering, and the
hash-join fallback executed under that promise, returning bool-grouped
rows for an ORDER BY query. Fixed at three levels: merge exploration
killed at its source in `GenerateMergeJoins` (norm-created joins bypass
`JoinFlags`, so flag-setting alone was insufficient), distsql output
ordering propagation gated on an actual merge core, and the `MERGE` hint
left to the pre-existing unsatisfiable-hint error path (a planner panic
there would crash sessions and silently veto test-file rewrites).

## Deliberately kept (cut requests refused with evidence)

- `pkg/ts/*` time-series DB: status recorder writes it; needs a stub-first
  plan, not a blind delete.
- `pkg/storage/reports/*`: populates real `system.replication_*` tables
  with migrations and logic-test coverage; orphaning the schema is a
  behavior change, not a cleanup.
- `bulk/*` and `replica_sideload*`: the snapshot path depends on them;
  replication catch-up outranks code size.
- Snapshot, replica-GC, replicate, split, raft-log, and MVCC GC queues:
  these are replication correctness, not tidying.
- `contention_queue`, `txnrecovery`, backpressure: write-path
  serialization and commit resolution; bypassing them changes semantics.
- `allocator_scorer`, compactor, copysets: scoring is scale-out;
  compaction reclaims space.
- `pg_builtins`, `generator_builtins` load-bearing entries, `scrub`
  error helpers, `control_jobs`, `querycache`, `sql/tests`: imported by
  live code or dozens of tests.
- `opt/` planner, gossip mesh, `tscache`/`spanlatch`/`txnwait`,
  node liveness, schema changer and leases, parser, `pgwire`, types,
  descriptors, KV span refresher and heartbeater, `start`/`init`/`cert`/
  `sql`/`quit` CLIs: the database itself.
- Wire-compat shells (`jobspb` payloads, `RangeFeed` error stub,
  diagnostics shape, inert partition fields): breaking the wire to save
  lines is vandalism, not trimming.

## Method

No-compat premise (new system): nothing is kept merely to read old
clusters, old job rows beyond harmless literals, old descriptors, or old
wires. The migration manager, compat grammar stubs, and version-gated
paths are cut on sight; wire-compat shells named below stay only where
deleting them would churn a live interface for zero gain.

No test was ever rewritten to fake a pass. Tests for removed
functionality were deleted whole; load-bearing failures caused honest
restores (four JSON functions) and reversals (load-lease-rebalance and
merge defaults, after bisect against pristine HEAD proved the
joint-config removal depends on them). Every batch ended with a green
build and the affected suites re-run: parser, lex, tree, opt packages,
roachpb, settings, keys, jobs, sql, storage, kv, server, cli, security,
pgwire, colexec (while it lived), rowexec, and logictest.

## Build system

`make buildoss` (the CI job in `.github/workflows/build.yml`, run in
the 20191029 builder image) needed repair after the cuts:

- `bin/prereqs`: sources deleted; replaced with a stub that emits no
  extra make dependencies.
- `bin/uptodate`: sources deleted; replaced with a shell equivalent
  that exits 0 when the target is newer than every file under the
  source dirs, else 1 to trigger a rebuild.
- `EXECGEN_TARGETS`: emptied (colexec and pkg/col deleted); the
  colexec `find` cleanups are guarded by `test -d`.
- `DOCGEN_TARGETS`: emptied (docs and docgen deleted); `build` no
  longer requires the settings HTML page since the `gen` command is
  gone (`buildoss` never needed it).
- `bins` pruned to tools with surviving sources; removed on-demand
  binaries (`workload`, `docgen`, `execgen`, `roachtest`, `fuzz`, …)
  fail with an honest "removed for core" error instead of "No rule".
- `pkg/cmd/protoc-gen-gogoroach` restored: it is build
  infrastructure (protoc plugin over vendored gogo), required for
  protobuf regeneration on fresh checkouts, not product bloat.
- `build/variables.mk` regenerated (drops `C_LIBS_CCL` /
  `LIBROACHCCL`, already gone from the Makefile).
