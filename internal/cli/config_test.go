package cli_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/config"
)

func TestConfigPathPrintsTheFileTheReaderReads(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "config.json")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"plain", []string{"config", "path"}, want + "\n"},
		{"json", []string{"config", "path", "--json"}, `{"path":"` + want + `"}` + "\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(config.DirEnv, dir)
			f := newFixture(t)

			out, _, err := f.run(tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if out != tt.want {
				t.Errorf("stdout = %q, want %q", out, tt.want)
			}
		})
	}
}

func TestConfigPathRefusesABase(t *testing.T) {
	f := newFixture(t)

	_, _, err := f.run("config", "path", "--base", "main")
	if err == nil || !strings.Contains(err.Error(), "--base") {
		t.Errorf("err = %v, want a refusal naming --base", err)
	}
}
