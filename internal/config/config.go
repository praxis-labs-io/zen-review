// Package config reads the user's settings from ~/.zen-review/config.json.
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
)

// DirEnv names the environment variable that overrides the config directory.
const DirEnv = "ZEN_REVIEW_CONFIG_DIR"

const (
	dirName  = ".zen-review"
	fileName = "config.json"
)

type Config struct {
	UpdateCheck *bool `json:"update_check"`
}

// ChecksForUpdates reports update_check, which is on when the key is absent.
func (c Config) ChecksForUpdates() bool {
	return c.UpdateCheck == nil || *c.UpdateCheck
}

// Dir returns the config directory, DirEnv when set and ~/.zen-review otherwise.
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

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load reads the config file, answering the defaults when there is none.
// A file that does not parse, or names a key this build does not know, is an error.
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

	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("parsing %s: more than one JSON value", path)
	}
	return cfg, nil
}
