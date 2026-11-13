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
the pinned module-cache paths. c-deps/ carries no nested
go.mod: no c-deps submodule holds imported Go packages, so
the module walk never enters it.

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

Status: fast loop green in default vendor mode (keys,
base, encoding, uuid, caller, roachpb incl.
TestNoLinkForbidden, parser, lex, jobs) plus all seven
opt suites (idxconstraint, memo, norm, optbuilder,
testcat, xform, exprgen) and reduce; memo, execbuilder,
sql, cliflags, serverpb, tspb build clean; full-repo
go build ./... 195 packages ok, 1 cgo-jemalloc package
needs the builder image (same as GOPATH mode).
Remaining red, all verified pre-existing on
tardigrade-main and unrelated to modules: roachpb
TestMakePriority (probabilistic win-ratio flake, passes
on retry), three lint-pass harnesses (analysistest GOPATH
overlay unresolvable under this toolchain; hash pass
panics inside 2019 x/tools), cgo test binaries.
Fixes made along the way, all committed on this branch:
datadriven pinned to the locked May revision (etcd pulls
an August one whose golden format differs), MERGE-hint
now errors honestly at build time (was silently
hash-fallbacking after the executor cut).
