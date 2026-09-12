// Package golden compares test output against a recorded file, rewriting it under -update.
package golden

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "regenerate the golden files")

// root is captured at init, since a test's t.Chdir would otherwise put testdata in its fixture.
var root = startDir()

func startDir() string {
	wd, err := os.Getwd()
	if err != nil {
		panic("golden: reading the working directory: " + err.Error())
	}
	return wd
}

// Compare fails t when got differs from testdata/<name>.golden, or writes got there under -update.
// The caller normalises anything unstable, such as a sha, out of got first.
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
