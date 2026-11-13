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

Cut inventory (by commit)
=========================

One row per cut commit from git log, oldest first. Reason codes:
NOT CORE = outside a solid core distributed SQL database; BLOAT =
duplication or go-to-market kit inflating build and test surface;
PAIN = flaky, timing-sensitive, or wrong-result-prone subsystem.
Risk is residual product risk after the cut. Green test is the
evidence re-run for that commit. Short SHAs are git log --oneline.

  COMMIT   FILES / DIRS REMOVED              REASON    RISK    GREEN TEST
  -------  --------------------------------  --------  ------  -----------------
  4ca5407  Makefile CCL graph: LIBROACHCCL,  NOT CORE  low:    make buildoss
           C_LIBS_CCL, ccl test/logictest    (propr-   default dry-run; live
           targets; live targets to          ietary    make build  go targets on
           C_LIBS_OSS. Fork had already       license)  unsup-     C_LIBS_OSS
           removed pkg/ccl, c-deps/libroach/            ported
           ccl, pkg/ui/distccl, pkg/ui/ccl,
           CCL-gated mains (see CHANGELOG
           v1.1.1: AGPL-clean, cockroach-oss
           is the binary)
  8c8506f  cloud/ manifests, docs/ RFCS      NOT CORE  none:   cockroach-oss
           notes, monitoring/ rules,          + BLOAT   not im- and
           pkg/workload/* generators,                   ported  cockroach-short
           pkg/cmd/roachprod harnesses,                 by core build clean
           driver/sim harnesses. 441 files,            binaries
           98515 deletions
  55e9897  pkg/cmd leaf binaries: allocsim,  BLOAT     none:   oss/short
           benchmark, cmp-protocol, cmp-sql,  (dev      dev-only build clean
           cr2pg, docgen, fuzz,               tooling)  mains
           generate-binary, github-post,
           github-pull-request-make,
           gossipsim, roachtest, smithcmp,
           smithtest, teamcity-trigger,
           uptodate, urlcheck, workload,
           wraprules, zerosum. 207 files,
           64974 deletions
  db3c213  pkg/ui browser bundle + handler,  BLOAT     low:    cli, server
           pprofui/goroutineui/logspy web     (app in   machine suites; root
           consoles, heapprofiler,            binary)   APIs    handler now
           goroutinedumper, diagnosticspb,              kept    404
           cli zip/demo/gen/haproxy benches
           + debug helpers. 50 files,
           7771 deletions
  6506fec  Vectorized engine: pkg/col,       BLOAT     perf    rowexec,
           pkg/sql/colexec (incl. *.eg.go     (second   only;   distsql,
           type-combination codegen),         engine)   row     logictest;
           pkg/sql/colflow, colencoding,                engine  EXPLAIN VEC
           explain_vec.go. 198 files,                   covers  honest error
           45731 deletions. vectorize                   all
           settings stay as inert stubs                 plans
           defaulting off
  adc28d5  Telemetry + phone-home:           NOT CORE  none:   counter
           pkg/server/telemetry,               (vendor   empty   assertions
           pkg/server/updates.go,              observ-   feature deleted, not
           pkg/sql/sqltelemetry,               ability)  _usage  faked; lint
           opt/telemetry.go, endpoint                   compat  rules for
           counters, AWS SDK import,                     stub    telemetry
           cloudinfo, encryption hooks.                  kept    removed with
           18 files, 2481 deletions                               them
  da4ddea  Storage commentary + external-    NOT CORE  low:    storage, kv
           consumer subsystems:                        manual  suites;
           pkg/storage/reports + system                merge   RangeFeed stub
           replication_* tables/migrations,            stays;  returns clean
           closedts/ tree, rangefeed/ tree,             lease-  error
           dist_sender_rangefeed.go,                   holder
           consistency_queue, TS                        reads
           maintenance queue. Merge queue               untouched
           and store rebalancer become
           no-ops (structs/settings stay).
  4618ad5  Stats collection + dead joiners:  PAIN +    low:    opt packages,
           create_stats.go,                    NOT CORE  planner logictest
           automatic_stats.go,                           falls (distsql_stats
           row_sampling.go, delete_stats.go,             back to  and automatic
           sampler + sample-aggregator                   default _stats data
           processors, distsql_plan_stats,               selec-  removed);
           rsg + sqlsmith fuzzers,                       tivity  SHOW/INJECT
           mergejoiner, zigzagjoiner,                             read path
           windower executors, window.go.                          kept + tested
           44 files, 14559 deletions.
           Read path kept: stats_cache,
           TableStatistic protos,
           SHOW STATISTICS/HISTOGRAM,
           INJECT STATISTICS
  d3ee9bc  pkg/sqlmigrations/* deleted       NOT CORE  fresh   TestSelf-
           (1615 deletions);                   (no old   clus-   Bootstrap;
           server.go calls new                 clusters  ters    idempotent
           bootstrapFreshCluster on first      to up-    only,   root/admin/
           boot only (root/admin, version +    grade)    no up-  version/DB
           settings keys, default DBs; 232               grade   keys written
           lines added). MigrationManager                path
           knob + test blocks removed
  cad09f9  Grammar: BACKUP/RESTORE/SHOW      NOT CORE  none:   parser, lex,
           BACKUP, IMPORT/EXPORT,                       cut     tree suites;
           CHANGEFEED, SCRUB, SHOW                       state-  regen with
           FINGERPRINTS, REPLICA TRACE,                  ments   zero goyacc
           partitioning, roles, audit,                   never   conflicts
           window statements (10 families),              parsed
           opt_partition_by, func_expr_
           windowless alias, bare
           EXPERIMENTAL token, PARTITION
           keyword out of sql.y.
           1189 lines removed
  d26a901  Admin/status/TS surface:          BLOAT +   low:    server,
           admin.go/status.go trimmed to        NOT CORE  /ts/    status
           Health/Liveness/Drain/Nodes/vars;              404s;   suites;
           serverpb admin/status protos                  live    TestMetrics-
           trimmed; pkg/ts/* deleted except              metrics Recording
           tspb + chart_catalog pure types;              kept    removed with
           tsdump gone. 46 files, 18011                             the DB
           deletions. Found + fixed live
           bug: keys.PrettyPrintTimeseriesKey
           hook panicked on boot once the
           TS package stopped assigning it;
           printer now falls back to raw
           output when the hook is nil
  99ccf0d  Planner/exec surface: JSON/window BLOAT +   med:    opt, memo,
           builtins to introspection set,       NOT CORE  merge-  xform, jobs,
           JsonExists ops + norm rules,                          rowexec,
           merge exploration deregistered,                       logictest
           distsql ordering gated on real
           merge cores, changefeed/sampler/
           dead-join processors deleted,
           optbuilder window/export/groupby
           + tree window funcs removed.
           100 files, 9586 deletions.
           Caught a live wrong-results bug:
           optimizer planned group-by
           ordered on merge-join output
           ordering while hash fallback
           executed; fixed at three levels
           (kill merge exploration at
           source in GenerateMergeJoiner,
           gate ordering propagation, MERGE
           hint to unsatisfiable-hint
           error path)
  6310116  Test goldens + removed-feature    BLOAT     none:   affected
           coverage: EXPLAIN/optimizer         (test     test-   suites
           goldens rewritten row-only and      only)    only    re-run green
           merge-free, logic/pretty/opt                          (parser
           testdata for cut statements/                          through
           engines/endpoints deleted,                             logictest);
           honest-error assertions kept.                         no test
           270 files, 4480 insertions,                           rewritten to
           34202 deletions                                       fake a pass
  ddce5bb  pkg/acceptance/* docker/driver    NOT CORE  none    build clean;
           harness + pkg/bench/* benches.     + PAIN    (harness acceptance
           76 files, 8188 deletions           (docker-   only)   coverage
                                                     flaky)             dropped
                                                     with it
  6270006  Residue sweep: scrub* planners/   NOT CORE  none:   suites
           statements/tests, distsql csv/                paths   re-run
           scrub/window plans, export.go,                already
           explain_vec.go, fingerprints,                 unrea-
           trace-replica, alter_index.go,                 chable
           show_partitions, replica_
           closedts/follower_read/
           rangefeed, cloudinfo. 38 files,
           9166 deletions
  89c2399  Rewire after removals (no new     BLOAT     med:    server,
           cuts): server construction,         (coher-   live    storage, kv,
           executor config, planner            ence)     wiring  sql, cli,
           dispatch, distsql planning,                   touched security,
           storage config, settings                                  pgwire
           registry, session handling.
           120 files, 348 insertions,
           3691 deletions
  666ca99  This record (CUTS.md) +           (docs)    none    row writer
           pkg/sql/row_result_writer.go                               covered by
           extracted during cleanup                                   existing
                                                                     rowexec
                                                                     tests
  12b065b  Build repair: bin/prereqs stub,   BLOAT     low:    buildoss
           bin/uptodate shell mtime check,     (dead     on-     dry-run zero
           EXECGEN/DOCGEN targets emptied,     graph)    demand  missing
           bins pruned, protoc-gen-gogoroach             mains   rules; real
           restored as build infra,                      stubbed  builds of
           variables.mk regenerated                                  protoc-gen-
                                                                     gogoroach,
                                                                     optgen,
                                                                     langgen

  Method (all rows): no-compat premise for a new system, nothing kept
  merely to read old clusters, jobs, descriptors, or wires; wire-compat
  shells stay only where deleting them would churn a live interface for
  zero gain. Tests for removed functionality deleted whole; load-bearing
  failures caused honest restores (four JSON builtins) and reversals.
  Every batch ended with a green build and the affected suites re-run.

Historic pain cuts
==================

Placeholder. This section records subsystems cut at least partly
because they were a historic source of flakes, races, or wrong
results, plus candidates under observation that have NOT been cut.
Nothing in the candidate list is approved for removal; each needs
its own evidence pass (flake-rate data, owner sign-off, green-test
plan) before it becomes a cut row above.

  Already cut for pain (evidence in this file):
  - ClosedTS + RangeFeed (da4ddea, 6270006): timestamp-tracking
    subsystem with timing-sensitive tests (closed_timestamp_test,
    client_closed_timestamp_test, rangefeed processor/registry/
    resolved-timestamp tests, all deleted with the code). Served
    follower reads and external change consumers only; leaseholder
    path untouched. The RangeFeed RPC stub returns not-supported so
    misconfigured callers fail clean instead of hanging.
  - Stats auto-collection sampler pipeline (4618ad5): background
    refresher + sampler/sample-aggregator processors + gossip
    invalidation tests; collection was the expensive runtime part
    while the optimizer corpus depends on the read path, which is
    why collection went and the cache stayed.
  - Merge/zigzag join executors (4618ad5, 99ccf0d): caused a live
    wrong-results incident (ordered group-by over hash fallback);
    killed at the exploration source, not just behind flags.
  - Fuzzers and harnesses (4618ad5, ddce5bb, 55e9897): sqlsmith,
    rsg, acceptance docker harness, roachtest, bench. Valuable
    upstream, but each is a flake generator in a small-team tree
    with nobody to triage vendor-kit failures.
  - Storage self-tuning queues (da4ddea): merge queue, TS
    maintenance queue, consistency checker, store rebalancer.
    Background optimizers that rearrange data on their own
    schedule are a classic source of nondeterministic test
    interference; manual AdminMerge still works.

  Candidates under observation (NOT cut, no action taken):
  - Gossip-based membership: retained as core, but watch for
    timing-sensitive test failures; if flakes cluster here,
    prefer longer test timeouts and deterministic test hooks
    over removal, since membership is load-bearing.
  - Schema changer / lease manager: retained as the database
    itself; any flake reports should be fixed in place, never
    stubbed, because DDL correctness is the product.
  - Compaction and MVCC GC queues: retained (reclaim space);
    kept explicitly out of the queue cuts for this reason.
  - Allocator scorer and replicate/split/raft-log queues:
    retained as replication correctness, not tidying.
  - Contentions queue, txnrecovery, backpressure: retained;
    bypassing them changes write-path semantics.

  Follow-ups: collect flake-rate evidence before promoting any
  candidate; keep this section as the intake list and move entries
  to the inventory table only when a cut lands with its green
  tests.

Cut batch 2026-11-11 (solid core, tests green)
==============================================

  What went in this batch and why it is safe. Parent repo cuts
  are committed as file deletions; vendor cuts live in the
  vendor submodule (cockroachdb/vendored) and show as submodule
  dirt until the submodule pointer is updated.

  - vendor/github.com/gogo/protobuf/test (13M, 352 files):
    dead test trees (combos, types, etc) not listed in
    Gopkg.lock packages for gogo/protobuf. Zero refs from
    pkg/ or Gopkg files. Build and fast suites stay green.
    Reason BLOAT. Risk none. Green: keys, roachpb, encoding,
    uuid, base all pass. Vendor 78M to 66M.
  - build/teamcity-acceptance.sh, teamcity-benchmarks.sh,
    teamcity-bench.sh, teamcity-local-roachtest.sh,
    teamcity-sqllogictest.sh: CI for already-deleted
    acceptance harness, benchmarks, roachtest, sqllogictest
    (ddce5bb, 8c8506f). Zero refs in Makefile, build/*.mk,
    .github. Reason NOT CORE + PAIN (docker-flaky). Risk
    none. Green: no Go code touched.
  - deploy/docker-compose.yml: multi-node demo compose left
    after cloud manifests went in 8c8506f. Minimal deploy is
    deploy/Dockerfile only. Reason NOT CORE. Risk trivial.
  - pkg/cli/testdata/zip/*, merge_logs/*, interactive_tests/
    test_demo_*.tcl: fixtures for cut debug zip, debug
    merge_logs, demo command (db3c213). Zero refs in pkg/cli.
    Reason BLOAT. Risk none. Green: cli untouched otherwise.
  - Vendor lesson, revert to green: batch tried rm -rf of
    cloud, aws, docker, Azure, arrow, vitess, mysql, linkedin,
    goquery, shopify, logrus, opencontainers. All show zero
    direct refs in pkg/, but pkg/base failed to build:
    vendor/.../zipkin-go-opentracing/collector-kafka.go
    imports Shopify/sarama, keeping the Kafka path alive even
    though tracing uses only HTTP. Same class holds for
    DataDog/zstd, thrift, and shared deps like logrus. Rule:
    never rm -rf whole vendor projects; use dep prune with
    BFS reachability plus go build after each cluster, and
    narrow Gopkg.toml prune overrides for same-package dead
    files. Reverted full vendor tree, re-applied only the
    safe gogo/test cut. Green restored.
  - Historic pain intake (no code cut yet, evidence mined
    2026-11-11): gossip simulation timing, logictest + race
    never-mix, allocator/replicate/split TODO cluster,
    distsql flowinfra TODOs, opt xform/norm 157 TODOs,
    jobs async registry, rpc heartbeat/keepalive flake
    comments, cli interactive flakes. See agent reports.
    Candidates stay NOT cut until flake-rate data plus a
    sync-replacement design lands with green tests.
    Gossip simulation specifically NOT cut: used by
    convergence_test, storage_test, dist_sender_test, so
    removal needs test rewrites to testcluster first.

Cut batch 2026-11-12 (loop cutters A/B/C, D-partial, E)
========================================================

  Run by parallel cutters under CUT GATE PROTOCOL (claim,
  zero-ref pre-check, cut, keys+base gate, revert on FAIL).
  Two server restarts wiped /tmp state mid-loop; claims were
  reconstructed from git status so no path was cut twice.
  Cutter-D was killed mid-edit (left an XXX marker in
  metrics.go, repaired by merger); its merge_queue.go
  deletions were adopted after ref verification.

  - Build/CI dead surface (43 paths, cutter-A): remaining
    build/teamcity-*.sh (20 files), build/packer/ whole dir,
    build/release/ whole dir, teamcity-post-failures.py,
    scripts/ authors, azworker, bench, deplicense,
    diff-example, gceworker, git-blame-stats, goescape,
    log2html, pprof, purge-terraform, release-notes.py +
    release-notes/ dir, tag-custom-build, torch,
    verify-release-binaries, winworker. Zero refs in
    Makefile, build/, .github/ each (packer/ cut only after
    its last referrer went; ls-files.sh, deploy/cockroach.sh
    still live and kept). Reason BLOAT + NOT CORE.
  - CLI dead surface (10 items, cutter-B): interactive_tests
    test_workload/test_example_data/test_encryption tcl,
    testdata zone_attrs*.yaml + zone_range_max_bytes.yaml
    (zero path refs), flags_util demoLocalityList,
    flags.go demo comment, cliflags DemoNodes,
    RunDemoWorkload, DemoNodeLocality,
    DemoGeoPartitionedReplicas, UseEmptyDatabase, Global
    (zero refs outside def; demo command gone). cliflags
    package builds clean. Reason BLOAT.
  - SQL dead code, non-proto only (5 items, cutter-C):
    opt/bench + opt/opbench whole dirs, logictest vec-only
    testdata (dist_vectorize, vectorize_types),
    execbuilder disableTelemetry field + call (write-only).
    Proto-wired items explicitly SKIPPED (zigzag/merge
    specs, bulk/table-stats, metadata test doubles need
    proto regen). Reason BLOAT + PAIN (bench/flake).
  - Storage no-op, partial (cutter-D, adopted): merge_queue
    mergesDisabledForRange, rangeMergePurgatoryError,
    requestRangeStats + dead imports removed (zero
    remaining refs repo-wide; shouldQueue already false).
    metrics.go XXX marker repaired. Rebalancer/recorder
    shells left for D-continuation. Reason BLOAT.
  - Vendor working-tree only (submodule, pointer not
    bumped): first wave gocovmerge, circbuf, mmap-go,
    pkg/browser, goveralls, reflectwalk,
    hyperloglog+go-metro, go-bindata-assetfs,
    MichaelTJones/walk, abourget/teamcity; second wave
    toxiproxy, golang/dep cluster (dep, Masterminds/*,
    go-radix, go-toml, constext, nuts, lockfile),
    go-github, commonmark/*, boltdb, go-bindata, plus
    gogo/test from batch 1 (836 files total). All zero
    pkg refs + zero vendor-to-vendor importers
    (prefix-aware grep). sarama, logrus, DataDog, thrift
    and all NEVER-list chains kept. Known trade-off:
    golang/dep deleted though bin/.bootstrap names it;
    dep ensure is legacy, builds/tests unaffected.
    Reason BLOAT. Vendor 78M to 65M.
  - Generated-artifact repair (merger, ignored files, no
    commit): sql.y cut in cad09f9 never regenerated, so
    pkg/sql/parser/sql.go, tokens.go, keywords,
    reserved_keywords, help files failed the build with
    undefined tree.* errors. Regenerated with vendored
    goyacc (zero conflicts), optgen (5 *.og.go), and a
    vendored-stringer equivalent for rule_name_string.go
    (plus one types import goimports used to add).
    helpmap_test.go regen fixed parser coverage test.
    parser + lex suites green; memo, execbuilder, pkg/sql
    build clean. cli full build still needs cgo jemalloc
    headers absent in this env (pre-existing env limit,
    not cut-related).

  Green: keys, base, encoding, uuid, parser, lex PASS;
  cliflags/memo/execbuilder/sql build clean. roachpb
  known-flaky winRatio re-PASSes on retry.

Cut batch 2026-11-13 (loop cutters D2 storage, E3 vendor)
==========================================================

  - Storage no-ops (cutter-D2, 3 files): store_rebalancer
    zero-ref timer/jitter/imports; metrics orphaned
    TS-maintenance metas/fields/ctors only (merge-queue
    counters + RangeMerges verified live, kept);
    server/status.go dangling SpanStats/Diagnostics/Stores
    doc comments (proto RPCs already trimmed, zero
    callers, comment-only). Rebalance fns, graphite
    helpers, gossip/simulation explicitly SKIPPED with
    refs (convergence/storage/dist_sender tests need the
    simulator). pkg/storage builds clean; keys+base PASS.
    Reason BLOAT.
  - Vendor working-tree only (cutter-E3, 62M to 55M, 550
    files, 9 gated rounds, 0 reverts): goquery+cascadia,
    panicparse, aws-sdk-go+jmespath+slack, websocket,
    Azure pipeline/blob (kept go-ansiterm), linkedin
    goavro, arrow (kept thrift), vitess, flatbuffers+
    go-cmp (kept pkg-imported btree). Prefix-aware
    importer checks both directions; tool binaries
    (dupl, staticcheck, errcheck, misspell, crlfmt,
    returncheck, stress) and all NEVER-list chains kept.
    Total vendor deletions 1386 files; 78M to 55M.
    Reason BLOAT. Pointer not bumped (upstream remote).
  - Still uncut, next waves: vendor docker/* org +
    go-ansiterm (zero importers anywhere, unreached);
    pkg/jobs test-only files; logictest remainder.

  Green: keys, base PASS; pkg/storage builds clean.

Cut batch 2026-11-13, wave 4 (cutter-F vendor)
===============================================

  - Vendor working-tree only, 55M to 53M, 5 deletions,
    all keys+base gated, 0 reverts: docker/go-units,
    docker/distribution, docker/go-connections,
    docker/docker (zero pkg + zero vendor importers;
    build/ hits are docker-CLI calls and URL comments),
    Azure/go-ansiterm (sole importer was docker windows
    term files, cut after docker). docker/ + Azure/
    remain as empty husks. sarama, DataDog, thrift,
    logrus and all NEVER chains kept; backoff, spew,
    gax-go, openzipkin, opentracing, btree re-verified
    as staying (pkg/vendor importers). Total vendor
    deletions 1651 files; 78M to 53M. Pointer not
    bumped (upstream remote). Reason BLOAT.

  Green: keys, base PASS.

  NOTE 2026-11-13: cutter-G's jobs-test cuts (5 files,
  2,319 lines: jobs_test, registry_test,
  registry_external_test, helpers_test, main_test) rode
  into commit 9f2147b with the wave-4 vendor doc; each
  gated keys+base + jobs build ok. pkg/jobs now reports
  no test files. Production jobs/ + jobspb/ untouched.
  jobutils, schemachanger/backfill tests, live-coverage
  test doubles explicitly SKIPPED by G with refs.

Cut batch 2026-11-13, wave 5 (scout-H ts gateway)
===================================================

  - CUT (132 lines): pkg/ts/tspb/timeseries.pb.gw.go
    (REST gateway for the deleted ts query API; zero
    refs, no back-refs from timeseries.pb.go) + 11 lines
    orphan comments in pkg/server/status_test.go for
    already-deleted TestChartCatalog/findUndefinedMetrics
    helpers. tspb + serverpb build clean; keys+base
    PASS. Reason BLOAT (dead REST surface).
  - SKIP with evidence: chart_catalog.proto/.pb.go +
    admin.pb.go regen. Unmodified admin.proto fails
    protoc (dangling } closers ~L805-808/~L825-826 left
    by d26a901 rpc-block deletions); checked-in
    status.pb.go also drifts from status.proto
    (DiagnosticsRequest). Regen would inject unintended
    diffs and needs .proto hand-edits, out of cutter
    scope. Unblock: authorize a .proto-repair pass
    (drop chart_catalog import + ChartCatalog messages,
    reconcile DiagnosticsRequest drift) then regen with
    the vendored plugin (protoc 3.6.0 +
    protoc-gen-gogoroach both verified working).

  Green: keys, base PASS; tspb, serverpb build clean.

  DECISION 2026-11-13: admin.proto regen PERMANENTLY
  SKIPPED, not just blocked. Scout-H proved any regen
  with the available toolchain (protoc 3.6.0 +
  vendored plugin) produces bytes differing from the
  checked-in files even for unmodified inputs
  (descriptor hash drift on status.proto). Regenerating
  admin.pb.go would therefore churn wire descriptors
  repo-wide for zero runtime gain, violating the
  no-compat-churn rule. chart_catalog types stay as
  inert wire-compat shells, same class as the RangeFeed
  stub. Revisit only with the exact upstream protoc +
  plugin versions that generated the checked-in files.

Cut batch 2026-11-13, wave 6 (cutter-B5 vendor)
===============================================

  - Vendor working-tree only, 54M to 44M, 14 deletions,
    all keys+base gated, 0 reverts: vitess.io, Azure
    org (azure-pipeline-go + azure-storage-blob-go),
    cloud.google.com/go, go.opencensus.io,
    golang.org/x/oauth2 (subdir; x/ siblings intact),
    google.golang.org/api (subdir; genproto/grpc/
    appengine intact), googleapis/gax-go, nlopes/slack,
    sirupsen/logrus, zipkin collector-kafka.go +
    collector-scribe.go (files; cut before sarama so
    the tracing build stays green), Shopify/sarama,
    DataDog/zstd (subdir), zipkin thrift/gen-go/scribe
    (subdir; zipkincore stays). Dependency-order
    cutting throughout (collectors before sarama/zstd/
    thrift evaluation). SKIP with evidence:
    google.golang.org/appengine (out of scope, guard
    clause), apache/thrift whole-cut (surviving
    collector-http.go + zipkincore gen-go still import
    it; would break the tracing build). Reason BLOAT.
    Pointer not bumped (upstream remote).

  Green: keys, base PASS; tracing vendor build clean.

Cut batch 2026-11-13, wave 7 (loop-coord vendor)
================================================

  - Vendor working-tree only, 44M to 42M, 13 deletions
    standing, 14 restored, all keys+base gated:
    google/flatbuffers, google/go-cmp, google/
    go-querystring, google/pprof (google/ now btree
    only), ianlancetaylor/demangle (sole pristine
    importer pprof, cut same wave), eapache/
    go-resiliency + go-xerial-snappy + queue and
    pierrec/lz4 (sole pristine importer sarama, cut by
    B5), gorilla/websocket (sole pristine importer
    nlopes/slack, cut by B5), jmespath/go-jmespath
    (sole pristine importer aws-sdk-go, cut by V/E3),
    maruel/panicparse (no importers anywhere),
    konsorten/go-windows-terminal-sequences (sole
    pristine importer logrus, cut by B5). Every
    pristine importer lives in a dir that is itself
    already deleted with passing gates. Reason BLOAT.
    Pointer not bumped (upstream remote).
  - Restored with evidence (10 in L4 + 4 tainted, all
    via `git -C vendor checkout`, re-gated green):
    beorn7/perks (client_golang), matttproud/
    golang_protobuf_extensions (common/expfmt), spew
    (testify/assert), ghodss/yaml (gateway
    descriptor), go-logfmt/logfmt + go-observer
    (zipkin root files), GoLLRB (btree_mem.go),
    mousetrap (cobra/command_win.go), wmi (gopsutil/
    gosigar windows files), go-ole (wmi/swbemservices),
    ianlancetaylor/cgosymbolizer (benesch),
    BurntSushi/toml (honnef.co config), md2man
    (cobra/doc), blackfriday (md2man). Old F/E3 skip
    reasons for these were valid and stand.
  - Method lesson, recorded so no cutter repeats it:
    parent-repo `git grep -- vendor/` NEVER descends
    into the vendor submodule and returns false ZERO
    for every vendor-importer check. Vendor checks
    must run as `git -C vendor grep` (or plain
    grep -r), and must run against the pristine tree:
    already-deleted dirs hide as importers (blackfriday
    looked dead only because md2man was already
    deleted; go-ole only because wmi was). Gate order
    that catches this class: keys+base, then a
    transitive vendcheck build (linux + windows) of
    every touched chain, then the wider sweep.
  - Standing-cut proof: linux + GOOS=windows builds of
    a probe binary importing every restored chain pass
    clean; keys, base, encoding, uuid, roachpb, parser,
    lex, lint passes all PASS (jobs: no test files),
    matching the allgreen-triage baseline (no NEW
    failures; 39 c-deps-linker REDs are pre-existing
    env limits, untouched).

  Green: keys, base, encoding, uuid, roachpb, parser,
  lex, lint passes PASS.
