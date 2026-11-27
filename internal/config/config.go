// Package config reads the reader's settings from ~/.zen-review/config.json, or the XDG copy when there is one.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DirEnv names the environment variable that overrides the config directory.
const DirEnv = "ZEN_REVIEW_CONFIG_DIR"

const (
	dirName    = ".zen-review"
	xdgDirName = "zen-review"
	xdgHomeEnv = "XDG_CONFIG_HOME"
	fileName   = "config.json"

	maxTabWidth = 16
)

// Config is the file's settings. A key the file leaves out, or names badly, keeps its zero value.
type Config struct {
	UpdateCheck *bool

	// SyntaxTheme names a Chroma style. Empty pairs one against the background.
	SyntaxTheme string

	// Background, Foreground, AddedHue and RemovedHue outrank what the terminal reported.
	Background string
	Foreground string
	AddedHue   string
	RemovedHue string

	PaintBackground bool

	// Theme maps token names to colors, applied after the derivation.
	Theme map[string]string

	Split bool

	// TabWidth is 0 when the file names none.
	TabWidth int

	FoldSettled *bool
}

// ChecksForUpdates reports updateCheck, which is on when the key is absent.
func (c Config) ChecksForUpdates() bool {
	return c.UpdateCheck == nil || *c.UpdateCheck
}

// FoldsSettled reports foldSettled, which is on when the key is absent.
func (c Config) FoldsSettled() bool {
	return c.FoldSettled == nil || *c.FoldSettled
}

const legacyUpdateCheck = "update_check"

var fields = map[string]func(*Config, json.RawMessage) error{
	"updateCheck":     func(c *Config, raw json.RawMessage) error { return json.Unmarshal(raw, &c.UpdateCheck) },
	"syntaxTheme":     func(c *Config, raw json.RawMessage) error { return json.Unmarshal(raw, &c.SyntaxTheme) },
	"background":      func(c *Config, raw json.RawMessage) error { return json.Unmarshal(raw, &c.Background) },
	"foreground":      func(c *Config, raw json.RawMessage) error { return json.Unmarshal(raw, &c.Foreground) },
	"addedHue":        func(c *Config, raw json.RawMessage) error { return json.Unmarshal(raw, &c.AddedHue) },
	"removedHue":      func(c *Config, raw json.RawMessage) error { return json.Unmarshal(raw, &c.RemovedHue) },
	"paintBackground": func(c *Config, raw json.RawMessage) error { return json.Unmarshal(raw, &c.PaintBackground) },
	"theme":           func(c *Config, raw json.RawMessage) error { return json.Unmarshal(raw, &c.Theme) },
	"split":           func(c *Config, raw json.RawMessage) error { return json.Unmarshal(raw, &c.Split) },
	"tabWidth":        decodeTabWidth,
	"foldSettled":     func(c *Config, raw json.RawMessage) error { return json.Unmarshal(raw, &c.FoldSettled) },
}

func decodeTabWidth(c *Config, raw json.RawMessage) error {
	var n *int
	if err := json.Unmarshal(raw, &n); err != nil {
		return err
	}
	if n == nil {
		return nil
	}
	if *n < 1 || *n > maxTabWidth {
		return fmt.Errorf("%d is out of range, want 1 to %d", *n, maxTabWidth)
	}
	c.TabWidth = *n
	return nil
}

// Dir returns the directory zen-review keeps its own files in, DirEnv when set and ~/.zen-review otherwise.
func Dir() (string, error) {
	if override := os.Getenv(DirEnv); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving the home directory: %w", err)
	}
	return filepath.Join(home, dirName), nil
}

// Path returns the config file: under DirEnv when set, else the XDG one when it can be read, else under Dir.
func Path() (string, error) {
	if override := os.Getenv(DirEnv); override != "" {
		return filepath.Join(override, fileName), nil
	}

	xdg, err := xdgPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(xdg); err == nil {
		return xdg, nil
	}

	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// xdgPath ignores a relative XDG_CONFIG_HOME, as the XDG spec requires.
func xdgPath() (string, error) {
	base := os.Getenv(xdgHomeEnv)
	if !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving the home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, xdgDirName, fileName), nil
}

// Load reads the config file, answering the defaults when there is none. Every key that
// reads cleanly is kept; the error names each one that did not, and the file when it
// does not parse at all.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}

	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var keys map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&keys); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("parsing %s: more than one JSON value", path)
	}

	cfg, problems := decode(keys)
	if len(problems) > 0 {
		return cfg, fmt.Errorf("%s: %s", path, strings.Join(problems, "; "))
	}
	return cfg, nil
}

func decode(keys map[string]json.RawMessage) (Config, []string) {
	var cfg Config
	var problems []string

	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)

	for _, key := range names {
		if key == legacyUpdateCheck {
			continue
		}
		set, ok := fields[key]
		if !ok {
			problems = append(problems, fmt.Sprintf("unknown key %q", key))
			continue
		}
		next := cfg
		if err := set(&next, keys[key]); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", key, err))
			continue
		}
		cfg = next
	}

	if raw, ok := keys[legacyUpdateCheck]; ok {
		if _, named := keys["updateCheck"]; !named {
			problems = append(problems, legacyUpdateCheck+" is now updateCheck, and the old name stops working in the next release")
			next := cfg
			if err := fields["updateCheck"](&next, raw); err != nil {
				problems = append(problems, fmt.Sprintf("%s: %v", legacyUpdateCheck, err))
			} else {
				cfg = next
			}
		}
	}
	return cfg, problems
}
