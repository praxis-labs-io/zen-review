package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/praxis-labs-io/zen-review/internal/config"
)

func writeConfig(t *testing.T, body string) {
	t.Helper()

	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)
	if body == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPathFollowsTheOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.DirEnv, dir)

	got, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "config.json"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestUpdateCheckIsOnUnlessTurnedOff(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "no file", body: "", want: true},
		{name: "an empty object", body: "{}", want: true},
		{name: "turned on", body: `{"update_check": true}`, want: true},
		{name: "turned off", body: `{"update_check": false}`, want: false},
		{name: "null", body: `{"update_check": null}`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeConfig(t, tt.body)

			got, err := config.Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.ChecksForUpdates() != tt.want {
				t.Errorf("ChecksForUpdates() = %v, want %v", got.ChecksForUpdates(), tt.want)
			}
		})
	}
}

func TestLoadRefusesAFileItCannotTrust(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "malformed", body: `{"update_check": `, want: "parsing"},
		{name: "a misspelt key", body: `{"update_checks": false}`, want: "update_checks"},
		{name: "the wrong type", body: `{"update_check": "no"}`, want: "update_check"},
		{name: "two values", body: `{} {}`, want: "more than one"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeConfig(t, tt.body)

			_, err := config.Load()
			if err == nil {
				t.Fatal("Load() accepted the file")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Load() error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}
