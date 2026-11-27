package config_test

import (
	"os"
	"path/filepath"
	"reflect"
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

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPathPrefersTheOverrideThenAnXDGFileThenHome(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	override := t.TempDir()

	tests := []struct {
		name     string
		override string
		xdgHome  string
		exists   string
		want     string
	}{
		{"the override", override, xdg, filepath.Join(xdg, "zen-review", "config.json"), filepath.Join(override, "config.json")},
		{"an XDG file", "", xdg, filepath.Join(xdg, "zen-review", "config.json"), filepath.Join(xdg, "zen-review", "config.json")},
		{"an XDG home with no file", "", xdg, "", filepath.Join(home, ".zen-review", "config.json")},
		{"~/.config when XDG is unset", "", "", filepath.Join(home, ".config", "zen-review", "config.json"),
			filepath.Join(home, ".config", "zen-review", "config.json")},
		{"home with nothing else", "", "", "", filepath.Join(home, ".zen-review", "config.json")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", home)
			t.Setenv(config.DirEnv, tt.override)
			t.Setenv("XDG_CONFIG_HOME", tt.xdgHome)
			if tt.exists != "" {
				touch(t, tt.exists)
				t.Cleanup(func() { _ = os.Remove(tt.exists) })
			}

			got, err := config.Path()
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("Path() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEveryKeyReads(t *testing.T) {
	writeConfig(t, `{
		"updateCheck": false,
		"syntaxTheme": "dracula",
		"background": "#0a0d14",
		"foreground": "#dce3ee",
		"addedHue": "#6ee6b8",
		"removedHue": "#f2707a",
		"paintBackground": true,
		"theme": {"accent": "#6ee6b8", "error": "1"},
		"split": true,
		"tabWidth": 8,
		"foldSettled": false
	}`)

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	off := false
	want := config.Config{
		UpdateCheck: &off, SyntaxTheme: "dracula",
		Background: "#0a0d14", Foreground: "#dce3ee", AddedHue: "#6ee6b8", RemovedHue: "#f2707a",
		PaintBackground: true,
		Theme:           map[string]string{"accent": "#6ee6b8", "error": "1"},
		Split:           true, TabWidth: 8, FoldSettled: &off,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestTheSwitchesDefaultOn(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		updates      bool
		foldsSettled bool
	}{
		{"no file", "", true, true},
		{"an empty object", "{}", true, true},
		{"turned off", `{"updateCheck": false, "foldSettled": false}`, false, false},
		{"null", `{"updateCheck": null, "foldSettled": null}`, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeConfig(t, tt.body)

			got, err := config.Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.ChecksForUpdates() != tt.updates {
				t.Errorf("ChecksForUpdates() = %v, want %v", got.ChecksForUpdates(), tt.updates)
			}
			if got.FoldsSettled() != tt.foldsSettled {
				t.Errorf("FoldsSettled() = %v, want %v", got.FoldsSettled(), tt.foldsSettled)
			}
		})
	}
}

func TestABadKeyKeepsTheRest(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"a misspelt key", `{"split": true, "spilt": true}`, "spilt"},
		{"the wrong type", `{"split": true, "tabWidth": "8"}`, "tabWidth"},
		{"a tab width of zero", `{"split": true, "tabWidth": 0}`, "tabWidth"},
		{"a tab width too wide", `{"split": true, "tabWidth": 17}`, "tabWidth"},
		{"a theme that is not a map", `{"split": true, "theme": "zen-ink"}`, "theme"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeConfig(t, tt.body)

			got, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want it to name %q", err, tt.want)
			}
			if !got.Split {
				t.Error("Split = false, want the good key kept beside the bad one")
			}
			if got.TabWidth != 0 || got.Theme != nil {
				t.Errorf("TabWidth = %d, Theme = %v, want the bad key left at its default", got.TabWidth, got.Theme)
			}
		})
	}
}

func TestAFileThatDoesNotParseIsTheDefaults(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"malformed", `{"split": `, "parsing"},
		{"two values", `{} {}`, "more than one"},
		{"not an object", `[]`, "parsing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeConfig(t, tt.body)

			got, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want it to mention %q", err, tt.want)
			}
			if !reflect.DeepEqual(got, config.Config{}) {
				t.Errorf("Load() = %+v, want the defaults", got)
			}
		})
	}
}

func TestTheOldUpdateCheckNameStillReadsAndSaysSo(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		updates bool
	}{
		{"alone", `{"update_check": false}`, false},
		{"beside the new name", `{"update_check": false, "updateCheck": true}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeConfig(t, tt.body)

			got, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), "updateCheck") {
				t.Errorf("Load() error = %v, want it to name the new key", err)
			}
			if got.ChecksForUpdates() != tt.updates {
				t.Errorf("ChecksForUpdates() = %v, want %v", got.ChecksForUpdates(), tt.updates)
			}
		})
	}
}
