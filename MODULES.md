Tardigrade module migration (spike branch: modules-spike)
=========================================================

Why: the repo is a pre-modules GOPATH/dep tree. Every Go tool
demands $GOPATH/src/github.com/podomy/tardigrade, vendor/ only
resolves there, and the Makefile hard-errors outside GOPATH.
Editors (gopls) cannot work with that layout. The migration
lets plain go build and go test run from the repo root and
lets editors open the repo directly.

How the graph was pinned: Gopkg.lock holds 169 projects.
A resolver script mapped each locked revision to an exact
module version through the proxy (168 of 169; the dead
go-github pin has no reachable commit and is not imported).
go.mod carries those pins plus replace directives for the
five Cockroach forks (gogoproto, raven-go, vitess, yaml,
teamcity) and one pin for etcd's bogus x/net pseudo-version
(its go.mod records a midnight timestamp that never existed;
pinned to the same commit with the correct timestamp).

No go mod tidy: tidy chases test-dependencies-of-imported-
packages (lightstep tests want gomega, pgx tests want fake)
to latest and upgrades twenty modules into 2024/2025,
breaking the faithful 2019 graph (protobuf v1.5.0 against
runtime v1.36 is the observed break). -mod=mod resolves
only what the build imports, and the pins hold (only
minor era-internal MVS bumps remain). Do not run tidy on
this go.mod. If a new import is added, pin it with
go list -m repo@sha and append the require by hand.

Fences: vendor/ is deleted from git entirely (no in-repo
tree, no modules.txt). Nothing resolves through ./vendor
anymore: plain go commands default to -mod=readonly against
the module cache, and the buildmod/testmod make targets pass
-mod=mod explicitly; both work with no other flags. The
Makefile no longer references vendor/ except in these notes:
bootstrap installs pinned tools via go install pkg@version,
the go-libedit cgo override is dropped (it compiles from the
module cache with its own directives), upload-coverage uses
the bootstrapped binaries, and proto include roots point at
the pinned module-cache paths. c-deps/ carries a nested
go.mod stub so the main module walk stops before the C++
submodules (the protobuf fork ships Go examples whose
imports would otherwise escape the pinned graph, and its
.cc files break the go tool).

Source fixes for non-GOPATH layout (both auto-detect, both
keep the old path when no go.mod is present):
- pkg/util/caller: the call resolver walked up six dirs
  from its own file and panicked unless it landed on src,
  vendor, or pkg/mod. It now anchors on the go.mod
  directory when found, yielding identical stripped paths.
- pkg/testutils/buildutil: VerifyNoImports forced
  GO111MODULE=off and used go/build (GOPATH-only). It now
  shells to go list -deps with module mode forced on when
  go.mod is present, with the same forbidden/whitelist
  semantics and depends-on chains in messages.

Makefile: the GOPATH hard error and the dep-era tool
installs apply only without go.mod. With go.mod, bootstrap
installs the same pinned tools via go install pkg@version
(dep and go-bindata skipped: legacy and dead UI pipeline).
New targets: buildmod (all pure-Go packages) and testmod
(the gated fast loop). The builder-image flow is untouched.

Editor: open /home/neil/tardigrade directly. No GOPATH,
no shim, no bind mount. Full cluster/cgo work still needs
the builder image (jemalloc, libroach headers).

Status (vendor-free, go 1.26): vendor/ deleted from git;
plain commands default to -mod=readonly, buildmod/testmod
pass -mod=mod; everything resolves from the module cache
after one `go mod download`. go directive bumped
1.17 -> 1.26: full `go build ./pkg/...` green (zero
language-semantics fallout), fast gate green (keys,
base, encoding, uuid, caller, roachpb, parser, lex,
jobs) plus opt suites (idxconstraint, memo, norm,
optbuilder, xform, exprgen) and reduce/reducesql.
The twelve parser/lex/optgen outputs are committed, so
no make codegen step precedes the build; each file was
verified byte-reproducible from the pinned toolchain
(goyacc/stringer May-24, graph goimports run in-tree).
cgo: the four static c-deps (jemalloc, snappy, protobuf,
rocksdb built via repo make in cockroachdb/builder:
20191029-105705 with CC=cc; libroach via direct cmake,
its Makefile rule's protoc regen skipped since the
protos are tracked) are installed to /usr/local/lib on
this machine. `go build -mod=mod ./pkg/cmd/cockroach-oss`
links; the binary runs `version` and passes a
single-node SQL smoke test. Fresh-clone proof: clean
clone + `go mod download` + `go build -mod=mod
./pkg/cmd/cockroach-oss` succeeds with no vendor/,
and package tests run. build.yml CI now runs this
two-stage flow (builder image for C++, setup-go 1.26
for the Go build + smoke).
Remaining red, all verified pre-existing on
tardigrade-main and unrelated to modules: roachpb
TestMakePriority (probabilistic win-ratio flake, passes
on retry), three lint-pass harnesses (analysistest GOPATH
overlay unresolvable under this toolchain; hash pass
panics inside 2019 x/tools). googletest sources had to
be force-checked-out (empty worktree, shared git dir).
Next steps: in-builder go1.12 can no longer build Go
code (no -mod=mod, module re-resolution fails), so the
builder image is C++-only now; any flow that invoked
`make buildoss` inside it must use the host toolchain.
Rebase onto tardigrade-main bf1983b (43 commits of
enterprise purges and deadcode cuts; no new Go imports,
so no go.mod pin additions were needed): 7 spike commits
replayed, 4 conflicts needing judgment. (1) build.go:
main deleted VerifyTransitiveWhitelist as dead code, so
the merge keeps main's file plus only the module-mode
machinery (useModules/listNode/verifyNoImportsModules).
(2) optbuilder/join.go: both lines converged on the same
honest MERGE-hint panic; kept main's wording. (3)
Makefile upload-coverage: main cut
build/upload-coverage.sh, so main's disabled-stub recipe
stands (spike's script call would reference a deleted
file). (4) Makefile bootstrap: dropped the GOPATH ifeq
entirely (spike side). New red inherited from main, kept
per the source-truth rule, follow-up for the owning
loop: memo TestStats and exprgen TestExprGen fail with
"unknown operator or function MakeLookupJoin /
MinPhysProps / ProjectionItem" because main's deadcode
wave removed those customFuncs methods from
optgen/exprgen/custom_funcs.go while leaving the
testdata references; the harness resolves names by
reflection (expr_gen.go), so this is red on main by
construction. Everything else in the gate stays green.
Fixes made along the way, all committed on this branch:
datadriven pinned to the locked May revision (etcd pulls
an August one whose golden format differs), MERGE-hint
now errors honestly at build time (was silently
hash-fallbacking after the executor cut).
