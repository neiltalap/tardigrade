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
