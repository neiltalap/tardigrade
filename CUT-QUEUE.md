Tardigrade cut queue (remaining medium-risk cuts)
=================================================

Backlog for the cut loop. Each entry has evidence, the design
decision it needs, and the owner question. Labels: SAFE-LOOP
(a cutter may proceed autonomously under the gate protocol),
NEEDS-DECISION (do not cut until the owner answers). Ranked
safest / most-verified first, biggest blast radius last.

Conventions: CUTS.md holds landed cuts. This file holds only
work NOT yet done. 51f7ebd (admin.proto repair + regen) and
4b1b84b (interactive harness removal) landed 2026-11-13 and
are referenced as evidence, not proposed again.


1. Jobs test files already gone (note, no action)
-------------------------------------------------
Status: SAFE-LOOP (nothing to cut; record only).

Evidence: commit 9f2147b deleted 5 files, 2,319 lines
(jobs_test, registry_test, registry_external_test,
helpers_test, main_test). pkg/jobs/ now has 6 prod files,
1,930 lines, zero test files; there is no testdata dir.
Gate (a) 2026-11-13 confirms: "pkg/jobs [no test files]".
Production jobs/ + jobspb/ untouched; jobutils and
schemachanger/backfill test doubles were skipped with refs.

Decision required: none. Keep this entry as a tombstone so
future loops do not re-propose jobs-test cuts.

Owner question: none. Close as done.


2. admin.proto follow-ups after the 51f7ebd repair
--------------------------------------------------
Status: SAFE-LOOP (verify-only; no further cuts proposed).

Evidence: 51f7ebd repaired the dangling closers d26a901
left in service Admin, dropped the dead ChartCatalog
messages + catalog import and orphan endpoint comments,
dropped the TimeSeries REST mapping (keeps the deleted
tspb gateway deleted across regens), restored the two
read-path stats protos 4618ad5 had deleted (histogram
57 lines, table_statistic 50 lines), and regenerated all
Go protobufs + gateways with the repo toolchain
(admin.pb.go 407562 -> 392686 bytes, status.pb.go also
shrunk). serverpb + tspb build clean; keys, base,
parser, roachpb, serverpb tests pass per commit message.

Decision required: none yet. Any further proto deletion
is a wire-descriptor change and re-enters NEEDS-DECISION.

Owner question: is there any named dead RPC or message
left in admin.proto / status.proto you still want gone
(RangeLogRequest/Response shells at ~L580-606 remain)?
If yes, file it as a new NEEDS-DECISION item with the
exact message names; otherwise close as done.


3. TS catalog remainder (chart_catalog + tspb shells)
-----------------------------------------------------
Status: NEEDS-DECISION (proto-tree delete needs explicit
authorization; scout-H rule still applies to new deletes).

Evidence: pkg/ts/ is down to catalog/ + tspb/ only.
chart_catalog.proto (132 lines) + chart_catalog.pb.go
have zero references outside their own directory (repo
grep hits only those two files). timeseries.proto (151
lines) now imports only gogoproto + annotations; the
REST gateway mapping is gone and stays gone across
regens. They survive as inert wire-compat shells, same
class as the RangeFeed stub. Deleting the .proto files
is a descriptor change, not a file-size win.

Decision required: authorize deleting a .proto subtree
vs keeping inert shells forever under the no-compat-
churn rule (CUTS.md DECISION 2026-11-13: regen churns
wire bytes even for unmodified inputs with protoc
3.6.0, so shells are cheap and regens are suspect).

Owner question: delete pkg/ts/catalog/chart_catalog.*
and any remaining tspb shells, or keep them pinned as
wire-compat? If delete, who re-verifies gateway regen
byte-stability before the pointer moves?


4. Gossip simulation rewrite to testcluster
-------------------------------------------
Status: NEEDS-DECISION (test-rewrite design, not a blind
delete; CUTS.md already refused the blind cut).

Evidence: pkg/gossip/simulation/network.go is imported
by exactly 3 test files: pkg/gossip/convergence_test.go
(NewNetwork at lines 63/96, SimulateNetwork at 101),
pkg/gossip/storage_test.go, and
pkg/kv/dist_sender_test.go (line 30 import, line 1286
NewNetwork, line 1311 SimulateNetwork, with an in-tree
TODO(spencer) to remove this use). Gossip itself stays;
only the simulator is at issue. Removal without
rewrites orphans convergence coverage and the one
dist_sender multi-node test.

Decision required: approve the rewrite target
(testcluster-based multi-node tests) and accept that
timing-sensitive convergence assertions become
deterministic testcluster assertions, possibly weaker
on partition behavior.

Owner question: which convergence properties must the
rewritten tests still prove (equilibrium reach, refused
connections count, storage gossip propagation), and is
dropping simulated-partition coverage acceptable?


5. Optimizer xform / norm thinning
----------------------------------
Status: NEEDS-DECISION (plan-shape changes need corpus
re-baseline approval).

Evidence: pkg/sql/opt/xform/ 12 files, norm/ 24 files,
30 TODO markers. Coster TODOs assume distributed costs
(local in-memory sort, per-ON-condition hash-join setup)
that no longer describe the engine; custom_funcs TODOs
mark unbuilt index-join and semi/anti shapes. Any rule
deletion or cost-constant change re-plans queries and
invalidates opt/execbuilder/logictest goldens, same
class as the merge-join wrong-results incident (fixed
at three levels in 99ccf0d).

Decision required: define the thinning boundary
(dead-rule + cost-comment cleanup only, vs dropping
live exploration branches) and authorize the golden
re-baseline run that follows.

Owner question: is a golden-rewrite pass for xform /
norm approved, and which rule families are explicitly
off-limits (join ordering, decorrelation, folding)?


6. CLI dump.go removal vs COPY
------------------------------
Status: NEEDS-DECISION (user-visible surface).

Evidence: pkg/cli/dump.go is 737 lines (runDump,
getDumpMetadata) plus dump_test.go (12KB). The tree
already keeps a COPY path: pkg/sql/copy.go +
copy_in_test.go, referenced from conn_executor,
plan_opt, and cli/sql_util.go. dump is a client-side
table/database dumper; COPY covers bulk movement in
SQL. Removing dump changes documented CLI behavior;
keeping it keeps a second export path alive.

Decision required: choose one export story (COPY only,
or COPY + dump) and accept the help-text / docs delta.

Owner question: is `cockroach dump` part of the kept
operator story, or is COPY the only supported bulk-out
path? If dump goes, who signs off on the help + docs
removal?


7. Jobs production collapse (sync-DDL design)
---------------------------------------------
Status: NEEDS-DECISION (semantic change, not a cleanup).

Evidence: pkg/jobs/ production code is 6 files, 1,930
lines; registry.go alone is 855 lines (StartJob async
start, Start poll loop, maybeAdoptJob, AdoptInterval
30s default, TestingResumerCreationKnobs, Resumer
interface). Live callers span server.go, testserver.go,
exec_util.go, truncate / drop_table / drop_database,
and delegate/show_jobs.go. Collapsing to synchronous
DDL means schema changes run inline with no jobs table
polling, no adoption on liveness failure, no resume.

Decision required: a sync-DDL design (error paths for
interrupted schema changes, what replaces SHOW JOBS
for DDL, retention of the jobs table vs full drop).

Owner question: is synchronous DDL with no background
adoption acceptable for crash-during-DDL, and what
should SHOW JOBS display after the collapse?


8. Distsql local-only (drop distributed flowinfra)
---------------------------------------------------
Status: NEEDS-DECISION (largest runtime blast radius in
this queue).

Evidence: 76 files reference distsql. The distributed
layer is pkg/sql/distsql/ (server.go, inbound, drain
tests) plus pkg/sql/flowinfra/ (5,835 lines: registry
544, outbox 471, inbound 245, scheduler 179, stream
codecs, stats.pb.go). Local row execution stays either
way. Going local-only deletes cross-node flow setup,
outbox/inbound transport, and the drain protocol, and
re-plans every distributed query to local execution.

Decision required: confirm single-node / local-only
execution is the product (no multi-node SQL fan-out),
plus a plan for the distsql_physical_planner and
backfill/CTAS/join runners that assume remote flows.

Owner question: is dropping multi-node execution an
approved product direction, or must remote flows stay
even though the cluster story is shrinking?


9. Vendor submodule removal (pin vs spike vendoring)
-----------------------------------------------------
Status: NEEDS-DECISION (supply-chain + build-graph call).

Evidence: .gitmodules pins vendor to
cockroachdb/vendored at f000e78d. Main-tree vendor/
is 78M today with only 15 working-tree deletions
visible; the wave-4/5 claims (1,651 files, 78M to 53M)
are not present in the current tree, so earlier
working-tree cuts were reset and the pointer was never
bumped (upstream remote). NEVER-list chains (sarama via
zipkin collector-kafka, DataDog, thrift, logrus) forbid
whole-project rm -rf; only dep-prune with BFS checks
plus per-cluster go build is safe (batch 2026-11-11
lesson). modules-spike instead deletes the submodule
pointer and materializes 36M via go mod vendor.

Decision required: keep the dep-submodule pin (with
narrow prune overrides) or adopt spike-style in-repo
vendoring owned by go.mod.

Owner question: who owns the vendored graph going
forward, and is an in-repo vendor/ tree (reviewable
diffs, no submodule) worth the one-time 36M churn?


10. modules-spike merge (go.mod on main)
-----------------------------------------
Status: NEEDS-DECISION (breaks builder CI without
bootstrap rework; do not merge silently).

Evidence: origin/modules-spike adds go.mod (353 pins,
go 1.17), go.sum (508), MODULES.md, and a module-aware
Makefile (buildmod/testmod, bootstrap via pinned go
install, skips legacy dep + dead go-bindata). Main has
no go.mod. MODULES.md warns: never run go mod tidy (it
chases test-deps-of-deps to 2024/2025 and breaks the
faithful 2019 graph; observed break protobuf v1.5.0
against runtime v1.36). The branch also carries source
fixes (caller path anchoring, buildutil go list mode)
and pins (datadriven May, honest MERGE-hint error).
Builder CI (bin/.bootstrap, GOPATH hard error, cgo
jemalloc/libroach headers) assumes the dep layout.

Decision required: a bootstrap rework plan so builder
CI works with and without go.mod (dual-mode Makefile
already sketches this), plus ownership of pin updates.

Owner question: approve merging go.mod onto main with
the dual-mode Makefile, accepting that builder CI must
be reworked and `go mod tidy` stays forever banned?
If yes, which branch absorbs the editor-only commits
(d1255e4, 4b1b84b) first?


11. Loop-coord vendor wave L1: google orphans (flatbuffers/go-cmp/go-querystring/pprof)
----------------------------------------------------------------------------------------
Status: SAFE-LOOP (cut in gated batch L1; B5 complete, no claim conflicts).

Evidence: precise import-path grep for each of the four
paths returns zero hits repo-wide outside the dirs
themselves (pkg/, pkg/cmd/, vendor/, Makefile, build/,
scripts/ all screened 2026-11-13). btree (same org)
stays: pkg/cli + storage import it. E3 cut flatbuffers
+ go-cmp 2026-11-13 with the same evidence; the tree
was reset by a restart and both are back, still
zero-ref. Sizes: flatbuffers 120K, go-cmp 240K,
go-querystring 24K, pprof 784K (~1.2M total).

Decision required: none. Gated keys+base per batch;
revert on FAIL.

Owner question: none.


12. Loop-coord vendor wave L2: debug/doc libs (ianlancetaylor x2, BurntSushi/toml, blackfriday, md2man)
-------------------------------------------------------------------------------------------------------
Status: SAFE-LOOP (cut in gated batch L2).

Evidence: zero import-path refs repo-wide (same screen
as 11) for ianlancetaylor/cgosymbolizer (216K),
ianlancetaylor/demangle (144K), BurntSushi/toml
(164K), russross/blackfriday (168K),
cpuguy83/go-md2man (24K). E3's old blocks re-checked
and overturned with current-tree evidence: honnef.co
(staticcheck, kept) does NOT import BurntSushi/toml;
benesch/cgosymbolizer (live, pkg/cli) does NOT import
ianlancetaylor/* (different org, different package);
this cobra tree does NOT import md2man, so blackfriday's
only in-tree chain (md2man) is itself dead. md2man is
not built by bin/.bootstrap and is absent from the
hard tool-skip list, so it is cut as a dead lib, not
as a tool. (~0.7M total.)

CORRECTION 2026-11-13 (loop-coord): the vendor-side
half of this screen used parent-repo `git grep`,
which does NOT descend into the vendor submodule, so
every vendor-importer check returned a false ZERO.
L4's gate caught 4 live chains; a correct re-check
(`git -C vendor grep`) plus a pristine-HEAD pass
overturned most of this entry: cgosymbolizer IS
imported by benesch/cgosymbolizer, BurntSushi/toml IS
imported by honnef.co config, md2man IS imported by
cobra/doc (which itself imports blackfriday, so
blackfriday is live too). All four were restored via
`git -C vendor checkout`, re-gated green. ONLY
ianlancetaylor/demangle (144K) stands cut: its sole
pristine importer is google/pprof, cut in wave L1.
Lesson: vendor-importer checks must run inside the
submodule; and re-verify AFTER deleting nothing
(deleted dirs hide as importers).

Decision required: none. Gated keys+base per batch.

Owner question: none.


13. Loop-coord vendor wave L3: dead sarama/websocket chains (eapache x3, pierrec/lz4, gorilla, jmespath, maruel)
----------------------------------------------------------------------------------------------------------------
Status: SAFE-LOOP (cut in gated batch L3).

Evidence: zero import-path refs repo-wide (same screen
as 11) for eapache/go-resiliency (20K),
eapache/go-xerial-snappy (28K), eapache/queue (24K),
pierrec/lz4 (92K), gorilla/websocket (160K),
jmespath/go-jmespath (104K), maruel/panicparse (68K).
E3 cut gorilla/websocket, jmespath (+aws), and
panicparse with the same evidence before the reset;
B5's sarama cut (2026-11-13, wave 6) removed the last
live importer of the eapache/pierrec chain. E3's
"eapache+pierrec (sarama)" skip reason is therefore
spent. (~0.5M total.)

Decision required: none. Gated keys+base per batch.

Owner question: none.


14. Loop-coord vendor wave L4: prometheus/win-only leftovers (beorn7, matttproud, spew, ghodss, logfmt, go-observer, GoLLRB, konsorten, mousetrap, wmi, go-ole)
-------------------------------------------------------------------------------------------------------------------------------------------------------------
Status: SAFE-LOOP (cut in gated batch L4).

Evidence: zero import-path refs repo-wide (same screen
as 11) for beorn7/perks (28K), matttproud/
golang_protobuf_extensions (44K), davecgh/go-spew
(100K), ghodss/yaml (48K), go-logfmt/logfmt (64K),
opentracing-contrib/go-observer (28K), petar/GoLLRB
(44K), konsorten/go-windows-terminal-sequences (20K),
inconshreveable/mousetrap (24K), StackExchange/wmi
(36K), go-ole/go-ole (284K). Stale skip reasons
overturned: F skipped spew (importers were sarama +
testify; B5 cut sarama and this testify tree never
imported spew) and go-observer (importer was zipkin;
zipkin root files no longer reference it); E3's
konsorten (logrus, cut by B5), mousetrap (cobra, this
tree never imports), wmi/go-ole (gopsutil, never
imports), ghodss (grpc-gateway, never imports),
logfmt/go-observer (zipkin chain, never imports)
blocks all re-verified empty. prometheus/common +
client_golang stay (pkg/util/metric imports them; they
do not import beorn7/matttproud). (~0.7M total.)

CORRECTION 2026-11-13 (loop-coord): same false-ZERO
method bug as entry 12 (parent-repo grep misses the
submodule). Correct re-check: beorn7 IS imported by
client_golang/summary.go, matttproud by common/expfmt,
spew by testify/assert, ghodss/yaml by the gateway
descriptor package, logfmt + go-observer by the zipkin
root files, GoLLRB by btree/btree_mem.go, mousetrap by
cobra/command_win.go, wmi by gopsutil/gosigar windows
files, and go-ole by wmi itself (found only after
restoring wmi; deleted dirs hide as importers). Ten
paths restored, re-gated green, plus linux + windows
vendcheck builds of every restored chain. ONLY
konsorten/go-windows-terminal-sequences (20K) stands
cut: its sole pristine importer is logrus, cut by B5.
The F/E3 skip reasons for the other ten were valid.

Decision required: none. Gated keys+base per batch.

Owner question: none.


15. staticcheck U1000 dead symbols in prod files (function-level surgery)
-------------------------------------------------------------------------
Status: NEEDS-DECISION (prod-file edits exceed the cutter
keys+base-only gate protocol; each cluster needs its
affected-package suite green, not just keys+base).

Evidence: survey-unused.log (modern staticcheck 2026.2.2,
vendored 2019 binary cannot run under go1.26) reports 64
U1000 findings across pkg/, triaged to 60 real dead
symbols: pkg/server admin.go 22 + status 14, pkg/sql 17
(rowexec stream merger/accumulator, rebalance leftovers,
dead wrappers/fields), pkg/storage 5, rpc/nodedialer 2.
Every finding verified zero-ref repo-wide (incl. tests),
no build tags, no generated code, no interface
assertions. 3 test-only helpers ignored; conn_executor
notifyStatsRefresherOfNewTables retained as intentional
no-op stub (do not cut). cutter-DEAD is executing the
admin.go 22-symbol cluster in flight (claimed, builds
clean); status/rowexec/singles remain.

Decision required: approve surgical prod-file deletion
cluster by cluster with per-package suite gates (server,
status, rowexec, storage, nodedialer suites), accepting
that any cluster whose suite reddens is reverted whole;
confirm the conn_executor stub stays.

Owner question: is function-level dead-code deletion in
live packages approved at all, and which clusters are
explicitly off-limits (e.g. anything touching
txn/commit paths vs pure admin/status commentary)?


16. rocksdb examples/ (10 files, 1339 lines, 68K)
-------------------------------------------------
Status: SAFE-LOOP (cut in gated batch; zero build refs).

Evidence (verified 2026-11-14): c-deps/rocksdb/examples/
(column_families/compact_files/c_simple/options_file/simple/
transaction/optimistic_transaction/multi_processes examples +
own Makefile) has zero refs in CMakeLists.txt, src.mk, and the
top-level Makefile (grep for "example" minus prose hits returns
only env_hdfs.cc, which is unrelated). Our build drives rocksdb
via cmake (Makefile EXTRA_XCMAKE_FLAGS), never via the examples/
Makefile. Gate: scratch cmake configure exit 0 with the
Makefile's exact flags (same as wave-10 gate). Split into 2-3
commits (~400 lines each) to respect the small-batch rule.
NOTE: hdfs/ (env_hdfs.h) SKIPPED - env_hdfs.cc IS in the cmake
source list (CMakeLists.txt:542); only examples/ is dead.

Decision required: none. Revert on configure FAIL.

Owner question: none.


17. c-deps/protobuf docs + protoc-artifacts + root tests.sh
-----------------------------------------------------------
Status: SAFE-LOOP (verify refs file-by-file, then cut in one
gated batch).

Evidence (verified 2026-11-14): c-deps/protobuf/docs/ (24K:
performance.md, third_party.md) and protoc-artifacts/ (28K:
release-binary build scripts) have zero refs in cmake/
CMakeLists.txt, the root CMakeLists.txt, and build/ (only hit
for "tests.sh" in-tree is objectivec/Tests distribution list
in Makefile.am:497, an already-deleted dir). Root tests.sh
(20K) is the upstream CI driver, never invoked by our build.
Sizes are small (docs+artifacts ~52K + tests.sh); batch with
any adjacent protobuf orphans found during the per-file screen.
Gate: scratch cmake configure exit 0 (protobuf invocation).

Decision required: none. Revert on configure FAIL.

Owner question: none.


18. Method-grep hazard list (analyzed 2026-11-14, DO NOT CUT without interface audit)
--------------------------------------------------------------------------------------
Status: NEEDS-DECISION only insofar as each needs an interface-
satisfaction proof; default is SKIP (all verified live or
generated). Recorded so future loops do not re-propose them.

Evidence: filesystem grep over pkg/ INCLUDING gitignored
generated files (optgen *.og.go, goyacc sql.go are untracked;
parent-repo `git grep` misses them) plus interface-decl search:
IntersectionCols (called from ignored explorer.og.go:732),
NextRowWithErrors (execinfra RowFetcher iface), forRF (copysets
Reader iface), getSlotIdx + GetCPUCombinedPercentNorm (own
package/settings + rowexec iface decls), Decompress/InitialState/
GetEnvStats/CommitTimestampFixed/ToSlice/WritableIndexCount/
RunParams/DrainMeta/IsVectorized/GetDatums (interface-required),
Marshal/Unmarshal YAML + String-style methods (reflection/fmt),
gateway Register*FromEndpoint (generated .gw.go), Fuzz* (gofuzz
build tag), bindata embedded.go helpers (DO NOT EDIT source),
rocksDBLog/prettyPrintKey (cgo //export), StartJob /
NewFakeNodeLiveness (jobs entry 7), sdnotify (live callers),
ObjectIter (json.JSON iface), notifyStatsRefresherOfNewTables
(intentional no-op stub, entry 15).

Decision required: any future cut from this list must cite the
interface declaration it was checked against and the filesystem
(not git) grep that cleared ignored generated files.

Owner question: none; close as reference.


19. server_systemlog_gc.go (212 lines, range-log GC worker) - FOR SIBLING, NOT THIS LOOP
------------------------------------------------------------------------------------------
Status: NEEDS-DECISION (lives in sibling-owned pkg/server; recorded, not cut).

Evidence (verified 2026-11-14): gcSystemLog + startSystemLogsGC
(methods on *Server) have zero callers repo-wide (filesystem
grep, both forms); the TS-maintenance queue registration that
drove them went in da4ddea. RangeLog RPC shells
(node.go/stores.go/roachpb honest-error stub) and
storagepb/state.proto rangefeed_registrations stay (wire-compat).
The worker file itself is the only dead part.

Decision required: sibling (RED owner) confirms the GC worker is
unwired and cuts or re-wires it; a non-owner cut would touch
pkg/server against the work split.

Owner question: re-wire startSystemLogsGC into server.go or
delete the file? If deleting, drop rangeLogTTL setting + its
testdata/docs refs in the same batch.


20. Loop-M leftovers (verified, out of scope for this loop)
------------------------------------------------------------
Status: SAFE-LOOP for c-deps (working-tree-only, pointer never
bumped - no parent commit possible); SKIP for the rest.

Evidence (verified 2026-11-14):
- c-deps/rocksdb/examples/ (10 files, 1339 lines): zero refs in
  CMakeLists/src.mk/Makefile (only hit is libedit's unrelated
  --disable-examples). Gate: scratch cmake configure exit 0.
  Split 3-4 commits (~300 lines each).
- c-deps/protobuf docs/ + protoc-artifacts/ + root tests.sh:
  zero cmake/build refs (only hit is an objectivec dist-list
  line for an already-deleted dir). Small batch.
- pkg/storage/copysets protos (copysets.proto + .pb.go): package
  has zero importers and the Go stub is cut (loop-M B5), but
  removing the messages is a descriptor change - needs the same
  authorization as entry 3 shells.
- pkg/util/log/testshout (47 lines): zero importers, BUT it is
  the only coverage for log.Shout (kept functionality).
  Deleting it loses coverage for zero product gain - SKIP,
  do not re-propose.
- pkg/jobs/metrics.go remainder (Metrics struct + MetricStruct +
  empty InitHooks): live via registry.metrics + server.go:465
  MetricsStruct() call. SKIP unless entry 7 collapse lands.

Decision required: none for c-deps (record-only until someone
owns submodule pointer bumps); explicit owner sign-off for the
proto delete.

Owner question: none.
