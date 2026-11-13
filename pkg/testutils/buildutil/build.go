// Copyright 2015 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package buildutil

import (
	"bytes"
	"encoding/json"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pkg/errors"
)

// useModules reports whether the test binary runs inside a main module
// (go.mod found upward from the working directory, which `go test` sets
// to the package source dir). In that case dependency checks run through
// `go list` (module-aware) instead of go/build (GOPATH-only).
var useModules = func() bool {
	dir, err := os.Getwd()
	if err != nil {
		return false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}()

func init() {
	// NB: This is a hack to disable the use of go modules with
	// build.Import. This will probably break with a future version of Go, but
	// suffices until we move to using go modules. See go/build.Context.importGo
	// (https://github.com/golang/go/blob/master/src/go/build/build.go#L999) and
	// the logic to skip using `go list` if the env far "GO111MODULE" is set to
	// "off".
	//
	// When the test runs inside a main module (go.mod present), leave
	// module mode enabled so the module-aware check below works.
	if !useModules {
		_ = os.Setenv("GO111MODULE", "off")
	}
}

func short(in string) string {
	return strings.Replace(in, "github.com/podomy/tardigrade/pkg/", "./pkg/", -1)
}

// VerifyNoImports verifies that a package doesn't depend (directly or
// indirectly) on forbidden packages. The forbidden packages are specified as
// either exact matches or prefix matches.
// A match is not reported if the package that includes the forbidden package
// is listed in the whitelist.
// If GOPATH isn't set, it is an indication that the source is not available and
// the test is skipped.
func VerifyNoImports(
	t testing.TB,
	pkgPath string,
	cgo bool,
	forbiddenPkgs, forbiddenPrefixes []string,
	whitelist ...string,
) {

	// Skip test if source is not available.
	if build.Default.GOPATH == "" && !useModules {
		t.Skip("GOPATH isn't set")
	}

	if useModules {
		verifyNoImportsModules(t, pkgPath, cgo, forbiddenPkgs, forbiddenPrefixes, whitelist)
		return
	}

	buildContext := build.Default
	buildContext.CgoEnabled = cgo

	checked := make(map[string]struct{})

	var check func(string) error
	check = func(path string) error {
		pkg, err := buildContext.Import(path, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range pkg.Imports {
			for _, forbidden := range forbiddenPkgs {
				if forbidden == imp {
					whitelisted := false
					for _, w := range whitelist {
						if path == w {
							whitelisted = true
							break
						}
					}
					if !whitelisted {
						return errors.Errorf("%s imports %s, which is forbidden", short(path), short(imp))
					}
				}
				if forbidden == "c-deps" && imp == "C" && strings.HasPrefix(path, "github.com/podomy/tardigrade/pkg") {
					for _, name := range pkg.CgoFiles {
						if strings.Contains(name, "zcgo_flags") {
							return errors.Errorf("%s imports %s (%s), which is forbidden", short(path), short(imp), name)
						}
					}
				}
			}
			for _, prefix := range forbiddenPrefixes {
				if strings.HasPrefix(imp, prefix) {
					return errors.Errorf("%s imports %s which has prefix %s, which is forbidden", short(path), short(imp), prefix)
				}
			}

			// https://github.com/golang/tools/blob/master/refactor/importgraph/graph.go#L159
			if imp == "C" {
				continue // "C" is fake
			}

			importPkg, err := buildContext.Import(imp, pkg.Dir, build.FindOnly)
			if err != nil {
				// go/build does not know that gccgo's standard packages don't have
				// source, and will report an error saying that it can not find them.
				//
				// See https://github.com/golang/go/issues/16701
				// and https://github.com/golang/go/issues/23607.
				if runtime.Compiler == "gccgo" {
					continue
				}
				t.Fatal(err)
			}
			imp = importPkg.ImportPath
			if _, ok := checked[imp]; ok {
				continue
			}
			if err := check(imp); err != nil {
				return errors.Wrapf(err, "%s depends on", short(path))
			}
			checked[pkg.ImportPath] = struct{}{}
		}
		return nil
	}
	if err := check(pkgPath); err != nil {
		t.Fatal(err)
	}
}

// listNode is the subset of `go list -json` output used by the
// module-mode check.
type listNode struct {
	ImportPath string
	Imports    []string
	CgoFiles   []string
	Incomplete bool
	Error      *struct {
		Err string
	}
}

// verifyNoImportsModules implements VerifyNoImports via `go list -deps`,
// which resolves imports module-aware (no GOPATH required). The child
// inherits the test process environment with module mode forced on.
func verifyNoImportsModules(
	t testing.TB,
	pkgPath string,
	cgo bool,
	forbiddenPkgs, forbiddenPrefixes []string,
	whitelist []string,
) {
	_ = cgo // cgo file membership comes from `go list`, not this flag.
	cmd := exec.Command("go", "list", "-e", "-json", "-deps", pkgPath)
	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil && stdout.Len() == 0 {
		t.Fatalf("go list %s: %v: %s", pkgPath, err, stderr.String())
	}
	nodes := map[string]*listNode{}
	dec := json.NewDecoder(&stdout)
	for dec.More() {
		var n listNode
		if err := dec.Decode(&n); err != nil {
			t.Fatalf("decoding go list output: %v", err)
		}
		nodes[n.ImportPath] = &n
	}
	root, ok := nodes[pkgPath]
	if !ok || (root.Error != nil && root.Incomplete) {
		t.Fatalf("go list %s: %s", pkgPath, stderr.String())
	}
	// Breadth-first walk from the root so violation messages read
	// "<importer> depends on ... <violating import>", as with go/build.
	type frame struct {
		path   string
		parent *frame
	}
	chain := func(f *frame) string {
		var parts []string
		for ; f != nil; f = f.parent {
			parts = append([]string{short(f.path)}, parts...)
		}
		return strings.Join(parts, " depends on ")
	}
	visited := map[string]struct{}{}
	queue := []*frame{{path: pkgPath}}
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		if _, ok := visited[f.path]; ok {
			continue
		}
		visited[f.path] = struct{}{}
		n, ok := nodes[f.path]
		if !ok {
			t.Fatalf("%s: no go list entry for import", chain(f))
		}
		if n.Error != nil {
			t.Fatalf("%s: %s", chain(f), n.Error.Err)
		}
		for _, imp := range n.Imports {
			for _, forbidden := range forbiddenPkgs {
				if forbidden != imp {
					continue
				}
				whitelisted := false
				for _, w := range whitelist {
					if f.path == w {
						whitelisted = true
						break
					}
				}
				if !whitelisted {
					t.Fatalf("%s imports %s, which is forbidden",
						chain(f), short(imp))
				}
			}
			if forbiddenHas(forbiddenPkgs, "c-deps") && imp == "C" &&
				strings.HasPrefix(f.path, "github.com/podomy/tardigrade/pkg") {
				for _, name := range n.CgoFiles {
					if strings.Contains(name, "zcgo_flags") {
						t.Fatalf("%s imports %s (%s), which is forbidden",
							chain(f), short(imp), name)
					}
				}
			}
			for _, prefix := range forbiddenPrefixes {
				if strings.HasPrefix(imp, prefix) {
					t.Fatalf("%s imports %s which has prefix %s, which is forbidden",
						chain(f), short(imp), prefix)
				}
			}
			if imp == "C" {
				continue // "C" is fake
			}
			queue = append(queue, &frame{path: imp, parent: f})
		}
	}
}

func forbiddenHas(pkgs []string, want string) bool {
	for _, p := range pkgs {
		if p == want {
			return true
		}
	}
	return false
}
