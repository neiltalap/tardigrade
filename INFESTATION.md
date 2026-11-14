# The Infestation: What Was Hiding in a CockroachDB Fork's Dependency Tree

*Field notes from hardening a CockroachDB 19.2 fork ("tardigrade") into a
minimal core. Every number below was measured in-tree with `tokei`, `du`,
`git grep`, and per-deletion test gates — not estimated.*

---

## 1. The headline numbers

| Layer | Before | After | Cut |
|---|---|---|---|
| `vendor/` (dep submodule) | 78 MB, 169 pinned projects | 54 MB (working tree) | ~24 MB, 43 dead projects |
| `c-deps/` language runtimes | 11 upstream runtime/doc dirs | deleted | ~29 MB |
| Web assets (`html/css/js/tmpl/svg/erb/php`) | 201 files | 0 | 201 files |
| First-party languages | Go + C + C++ + protos/grammars | same | already clean |

First-party code was **never** the problem. `tokei` on the repo shows
4,680 Go files, 1,405 C files, 1,089 C++ files — and zero first-party
Ruby/Python/PHP/Java/C#/JavaScript. Every line of "shit" lived in two
places: `vendor/` (the `dep`-era flattened dependency dump) and `c-deps/`
(upstream submodule checkouts that ship every language runtime next to
the C++ sources). The remaining 198 `.py` files are all inside kept
build/test trees: `c-deps/krb5` (86), `c-deps/protobuf` third-party (45),
`c-deps/googletest` (38), `c-deps/rocksdb` (29). Zero in `pkg/`.

---

## 2. Why it got this way (three compounding causes)

**2a. `dep`-era vendoring flattens everything.** This tree predates Go
modules. `dep` resolved the full transitive closure and vendored all of
it — including test-only, doc-only, and codegen-template baggage that
never ships: `.erb` codegen templates (`pgx`), `client_model` Ruby and
Python ports, `grpc-gateway` browser samples, jemalloc's `gen_travis.py`,
`setup.py` files nobody runs. Nobody ever garbage-collected it; the pin
just rolled forward release after release.

**2b. Deleted features left their dependencies behind.** Each dead
product surface had dragged in its own SDK stack, and cutting the
feature never cut the stack:

- Cloud backup / export → AWS SDK + Azure blobs + GCP client +
  `oauth2` + `opencensus` + `gax-go` (~7 MB as a cluster).
- Acceptance harness → `docker/docker`, `distribution`,
  `go-connections`, `opencontainers` (~1 MB).
- Telemetry → Azure pipeline, DataDog, `goavro`, `boltdb`.
- Vectorized engine (deleted) → `apache/arrow` (0.8 MB).
- Admin UI / docs → protobuf's JS/C#/Java/Objective-C runtimes,
  rocksdb docs theme, krb5 Windows help files.

**2c. Submodule builds drag whole language ecosystems.** `c-deps`
exists to compile C++ (`protoc`, `rocksdb`, `jemalloc`, `snappy`,
`krb5`, `cryptopp`). Upstream ships every language runtime *beside*
the C++ sources, and a submodule pin takes all of it: protobuf's
Ruby/PHP/C#/Java/Python/Objective-C/JS (1.6–4.9 MB each), rocksdb's
Java bindings + docs (3.5 + 3.4 MB), krb5's docs + Windows tree
(1.4 + 2.7 MB). The build references none of them — verified per-dir
against every `CMakeLists.txt`, `configure.in`/`Makefile.in`, and
Makefile codegen path before cutting.

---

## 3. The confirmed kill list (cut + gated, working tree)

Each deletion was gated: zero `pkg/` importers, zero vendor-to-vendor
importers (prefix-aware, both quote forms), then `keys` + `base` test
gate, revert on failure. Zero reverts across the whole campaign.

- **Cloud/backup (~5 MB):** `cloud.google.com/go`, `google.golang.org/api`
  (later round), `appengine`, `go.opencensus.io`, `gax-go`.
- **Docker/acceptance (~1 MB):** `docker/docker`, `distribution`,
  `go-connections`, `go-units`, `opencontainers/*`, `go-ansiterm`.
- **AWS (~1.7 MB):** `aws-sdk-go` + `jmespath`. **Azure (~0.9 MB):**
  `azure-storage-blob` + `pipeline-go` (second pass finished the pair).
- **Telemetry/misc:** `go-sql-driver`, `linkedin/goavro`, `boltdb`,
  `goquery`+`cascadia`, `google/go-github`, `golang-commonmark/*`.
- **Engine leftovers:** `apache/arrow` (0.8 MB, thrift kept — the
  serialization lib is separate from the engine).
- **Fork residue:** `vitess.io/vitess` (0.8 MB) — never imported since
  the fork.
- **Codegen/web:** 2× pgx `.erb`, `grpc-gateway/examples+docs`,
  `client_model/{ruby,python,src}`, protobuf
  conformance/benchmarks/examples, jemalloc `gen_travis.py`, rocksdb
  Sandcastle helper, protobuf Java examples, one vitess `.scss`.
- **c-deps runtimes (11 dirs, ~29 MB):** protobuf
  `{ruby,php,csharp,java,python,objectivec,js}`, rocksdb `{java,docs}`,
  krb5 `{doc,windows}`.

One over-cut, caught and reverted within the hour: three
`src/.../js/well_known_types/*.js` files that `libprotoc.cmake`
embeds via a custom `js_embed` command. The mapper had checked
`add_subdirectory`/GLOB references but not custom-command `DEPENDS`.
Rule updated: `DEPENDS` counts as a build reference.

---

## 4. The five bullshit deps (verified, queued for the blade)

Surveyed with direct importer evidence, approved for cutting:

1. **`vitess.io/vitess` — 996 KB.** Zero `pkg/` refs, zero vendor
   importers. An earlier cutter tried a typo'd path and skipped it;
   the real dir survived.
2. **Azure pair — 748 KB.** `azure-storage-blob-go` has zero
   importers; `azure-pipeline-go`'s 26 importers are all intra-Azure.
3. **Cloud cluster — ~7 MB.** `cloud.google.com/go` (4 MB) plus
   `googleapis/api`, `x/oauth2`, `opencensus` (416 KB),
   `nlopes/slack`. All pkg-zero; `oauth2`'s only code importers sit
   inside the `api` cluster; `slack` has zero importers anywhere. The
   remaining `cloud.google.com` hits are `go.mod`/`go.sum`/README
   metadata, not code.
4. **`sirupsen/logrus` — 160 KB.** Zero code refs repo-wide (one
   `Gopkg.lock` manifest line, two pgx doc mentions).
   `pkg/util/log` does not import it. An old NEVER-list entry
   claimed "heavily used" — the tree contradicts it.
5. **Tracing dead-ends — `Shopify/sarama`, `DataDog/zstd`,
   `apache/thrift`, zipkin kafka/scribe collectors.** `pkg/` imports
   only the zipkin/lightstep *root* packages
   (`pkg/util/tracing/tracer.go:29`, `shadow.go:27-29`); the kafka
   collector (sole sarama importer, sole zstd importer) and the
   scribe collector (sole thrift importer) are never imported, and
   lightstep itself is thrift-clean.

Deliberately **kept**: `hon­nef.co` (staticcheck), `client9/misspell`
(lint tools — projects, not linked code), `spf13` (23 pkg refs),
`etcd` (67), `gogo`/`cockroachdb` (core), `opentracing-go` and the
zipkin/lightstep roots (actually imported).

---

## 5. What stays and why (the load-bearing core)

- `gogo/protobuf` fork, `grpc` + `golang/protobuf` + `genproto`,
  etcd raft, `pebble`, `cockroachdb/{errors,logtags,…}`, prometheus
  client, `pgx`/`lib/pq`, the live half of the tracing chain,
  `cobra`/`pflag`, `apd`, `go-cmp`.
- Tool binaries (never linked, needed to build): `misspell`,
  `crlfmt`, `errcheck`, `dupl`, `golint`, `goyacc`, `stringer`,
  `shadow`, `staticcheck`, `dep`, `go-bindata`, `benchstat`.
- `c-deps` keeps (with exact config-line evidence): krb5
  `appl`/`clients` (unix `SUBDIRS`), rocksdb `tools`
  (`WITH_TOOLS`), `googletest` (libroach `add_subdirectory`), `snappy`
  (engine links it, `pkg/storage/engine/rocksdb.go:54`).
- Inert wire-compat shells: `chart_catalog` protos, `MetricsRequest`
  message, RangeFeed stubs — descriptor churn costs more than bytes.

---

## 6. Collateral: the bugs the cuts exposed

Cutting hard found real breakage, all fixed and green:

- **`admin.proto` blocked CI** — a prior trim left dangling closers
  in `service Admin`; every `protoc` regen failed. Repaired, dead
  ChartCatalog/messages dropped, full regen re-run.
- **`MERGE` join hint silently degraded to hash join** after the merge
  executor's removal (`pkg/sql/opt/optbuilder/join.go:54`). Restored
  the explicit `FeatureNotSupported` panic; `TestBuilder/join` green.
- **Lint harness vs go1.26** — vendored `x/tools/go/packages`
  assumed `SizesFor` returns `*types.StdSizes`; on go1.26 it returns
  `*types.gcSizes`. Two-line harness fix (`types.Sizes` passthrough).
  Plus stale `cockroachdb/cockroach` import paths in two analysistest
  fixtures (fork-rename miss).
- **`TestMakePriority` flake** — 20%-tolerance statistical assertion
  on an auto-seeded `math/rand` source; fails ~1/8 unseeded runs.
  Deterministic seed, documented.
- **Trim consequence, still open** — the API trim deleted
  `Status.Metrics`; `pkg/testutils/testcluster` still calls it.
  Fix is test-only (point at kept `Nodes` RPC), held until the
  native libs exist to gate it.

Test state: fast loop (`keys`, `base`, `encoding`, `uuid`, `roachpb`,
`parser`, `lex`, `jobs`, `optbuilder`, all 4 lint passes) green;
full tree 157/196 green with 39 packages awaiting the `libroach`
native build (environmental: `ld: cannot find -lroach/-lprotobuf/
-lrocksdb/-lsnappy`) — the C++ build was in progress at time of
writing, not waived.

---

## 7. What remains (the ranked queue)

`CUT-QUEUE.md` holds 10 entries: 2 done, 8 needing owner decisions —
ts catalog shells, gossip-sim rewrite to testcluster, optimizer
xform/norm thinning (28,644 lines max), `cockroach dump` vs COPY,
jobs production collapse to sync DDL, distsql local-only (largest
blast radius), vendor pin vs in-repo vendoring, and the
`modules-spike` merge (in-repo 36 MB vendor + `go.mod`, changes CI
semantics — proof branch only until approved).

*Total naive deletable if the whole queue is approved: ~44k prod
lines + ~18 MB vendor. Real number is smaller — four entries are
rewrites, not deletions.*
