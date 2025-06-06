// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package bench

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/tools/gopls/internal/protocol"
	. "golang.org/x/tools/gopls/internal/test/integration"
	"golang.org/x/tools/gopls/internal/test/integration/fake"
)

// Use a global edit counter as bench function may execute multiple times, and
// we want to avoid cache hits. Use time.Now to also avoid cache hits from the
// shared file cache.
var editID int64 = time.Now().UnixNano()

type changeTest struct {
	repo    string // repo identifier + optional disambiguating ".foo" suffix
	file    string
	canSave bool
}

var didChangeTests = []changeTest{
	{"google-cloud-go", "internal/annotate.go", true},
	{"istio", "pkg/fuzz/util.go", true},
	{"kubernetes", "pkg/controller/lookup_cache.go", true},
	{"kubernetes.types", "staging/src/k8s.io/api/core/v1/types.go", true}, // results in 25K file batch!
	{"kuma", "api/generic/insights.go", true},
	{"oracle", "dataintegration/data_type.go", false}, // diagnoseSave fails because this package is generated
	{"pkgsite", "internal/frontend/server.go", true},
	{"starlark", "starlark/eval.go", true},
	{"tools", "internal/lsp/cache/snapshot.go", true},
}

// BenchmarkDidChange benchmarks modifications of a single file by making
// synthetic modifications in a comment. It controls pacing by waiting for the
// server to actually start processing the didChange notification before
// proceeding. Notably it does not wait for diagnostics to complete.
func BenchmarkDidChange(b *testing.B) {
	for _, test := range didChangeTests {
		b.Run(test.repo, func(b *testing.B) {
			env := getRepo(b, test.repo).sharedEnv(b)
			env.OpenFile(test.file)
			defer closeBuffer(b, env, test.file)

			// Insert the text we'll be modifying at the top of the file.
			edit := makeEditFunc(env, test.file)

			if stopAndRecord := startProfileIfSupported(b, env, qualifiedName(test.repo, "didchange")); stopAndRecord != nil {
				defer stopAndRecord()
			}
			b.ResetTimer()

			for b.Loop() {
				edit()
				env.Await(env.StartedChange())
			}
		})
	}
}

// makeEditFunc prepares the given file for incremental editing, by inserting a
// placeholder comment that will be overwritten with a new unique value by each
// call to the resulting function. While makeEditFunc awaits gopls to finish
// processing the initial edit, the callback for incremental edits does not
// await any gopls state.
//
// This is used for benchmarks that must repeatedly invalidate a file's
// contents.
//
// TODO(rfindley): use this throughout.
func makeEditFunc(env *Env, file string) func() {
	// Insert the text we'll be modifying at the top of the file.
	env.EditBuffer(file, protocol.TextEdit{NewText: "// __TEST_PLACEHOLDER_0__\n"})
	env.AfterChange()

	return func() {
		edits := atomic.AddInt64(&editID, 1)
		env.EditBuffer(file, protocol.TextEdit{
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 1, Character: 0},
			},
			// Increment the placeholder text, to ensure cache misses.
			NewText: fmt.Sprintf("// __TEST_PLACEHOLDER_%d__\n", edits),
		})
	}
}

func BenchmarkDiagnoseChange(b *testing.B) {
	for _, test := range didChangeTests {
		runChangeDiagnosticsBenchmark(b, test, false, "diagnoseChange")
	}
}

// TODO(rfindley): add a benchmark for with a metadata-affecting change, when
// this matters.
func BenchmarkDiagnoseSave(b *testing.B) {
	for _, test := range didChangeTests {
		runChangeDiagnosticsBenchmark(b, test, true, "diagnoseSave")
	}
}

// runChangeDiagnosticsBenchmark runs a benchmark to edit the test file and
// await the resulting diagnostics pass. If save is set, the file is also saved.
func runChangeDiagnosticsBenchmark(b *testing.B, test changeTest, save bool, operation string) {
	b.Run(test.repo, func(b *testing.B) {
		if !test.canSave {
			b.Skipf("skipping as %s cannot be saved", test.file)
		}
		sharedEnv := getRepo(b, test.repo).sharedEnv(b)
		config := fake.EditorConfig{
			Env: map[string]string{
				"GOPATH": sharedEnv.Sandbox.GOPATH(),
			},
			Settings: map[string]any{
				"diagnosticsDelay": "0s",
			},
		}
		// Use a new env to avoid the diagnostic delay: we want to measure how
		// long it takes to produce the diagnostics.
		env := getRepo(b, test.repo).newEnv(b, config, operation, false)
		defer env.Close()
		env.OpenFile(test.file)
		// Insert the text we'll be modifying at the top of the file.
		env.EditBuffer(test.file, protocol.TextEdit{NewText: "// __TEST_PLACEHOLDER_0__\n"})
		if save {
			env.SaveBuffer(test.file)
		}
		env.AfterChange()
		b.ResetTimer()

		// We must use an extra subtest layer here, so that we only set up the
		// shared env once (otherwise we pay additional overhead and the profiling
		// flags don't work).
		b.Run("diagnose", func(b *testing.B) {
			if stopAndRecord := startProfileIfSupported(b, env, qualifiedName(test.repo, operation)); stopAndRecord != nil {
				defer stopAndRecord()
			}
			for b.Loop() {
				edits := atomic.AddInt64(&editID, 1)
				env.EditBuffer(test.file, protocol.TextEdit{
					Range: protocol.Range{
						Start: protocol.Position{Line: 0, Character: 0},
						End:   protocol.Position{Line: 1, Character: 0},
					},
					// Increment the placeholder text, to ensure cache misses.
					NewText: fmt.Sprintf("// __TEST_PLACEHOLDER_%d__\n", edits),
				})
				if save {
					env.SaveBuffer(test.file)
				}
				env.AfterChange()
			}
		})
	})
}
