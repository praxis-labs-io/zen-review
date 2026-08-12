// Package golden compares test output against a recorded file, and rewrites the
// file instead when -update is passed.
//
// It is test-only and imported by test files alone, so it never reaches the
// binary. The flag is registered here rather than once per suite, which is what
// lets `make golden` regenerate every golden in the repo under one name.
//
// The flag only exists in a test binary that links this package, so name the
// packages in that target rather than passing -update to ./... , which fails in
// every package that does not.
package golden

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "regenerate the golden files")

// root is the package directory the test binary started in, captured before any
// test can move away from it.
//
// A test that drives a command from inside a temporary repository has to chdir,
// and t.Chdir does not put it back until that test ends. Resolving testdata
// against the working directory would then write the golden into the fixture and
// delete it along with the fixture, and the run would pass while recording
// nothing.
var root = startDir()

func startDir() string {
	wd, err := os.Getwd()
	if err != nil {
		panic("golden: reading the working directory: " + err.Error())
	}
	return wd
}

// Compare checks got against testdata/<name>.golden and fails the test when
// they differ, or writes got there and returns when -update is set.
//
// The path is under the calling package's own testdata. The caller owns the
// bytes: anything that has to be normalised out of them, a sha or a timestamp,
// is normalised before this sees them.
func Compare(t *testing.T, name string, got []byte) {
	t.Helper()

	dir := filepath.Join(root, "testdata")
	path := filepath.Join(dir, name+".golden")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v (run: make golden)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("%s changed.\n got %s\nwant %s", name, got, want)
	}
}
