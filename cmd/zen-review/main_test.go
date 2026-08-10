package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/zen-review/zen-review/internal/version"
)

// run drives the root command the way a shell does. SetArgs takes an empty
// slice rather than nil, because nil means "read os.Args", which under `go
// test` is the test binary's own flags.
func run(t *testing.T, args ...string) string {
	t.Helper()

	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{}, args...))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("zen-review %v: %v", args, err)
	}
	return out.String()
}

// The engine lands behind this command at M1. Until it does, a bare invocation
// says what the tool is rather than exiting silently as though it ran.
func TestABareInvocationDescribesTheToolInsteadOfRunning(t *testing.T) {
	if got := run(t); !strings.Contains(got, "Review the changes on a branch") {
		t.Errorf("output = %q, want it to say what the command is", got)
	}
}

func TestTheVersionFlagReportsTheBuildVersion(t *testing.T) {
	if got := run(t, "--version"); !strings.Contains(got, version.Version) {
		t.Errorf("version output = %q, want it to carry %q", got, version.Version)
	}
}
