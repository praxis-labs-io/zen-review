package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	installScriptURL = "https://raw.githubusercontent.com/praxis-labs-io/zen-review/main/install.sh"

	installScriptWindowsURL = "https://raw.githubusercontent.com/praxis-labs-io/zen-review/main/install.ps1"

	DevVersion = devVersion

	maxScriptBytes = 1 << 20

	scriptTimeout = 30 * time.Second
)

type installRunner func(ctx context.Context, script, dir string, out io.Writer) error

// InstallOptions requires only Dir.
type InstallOptions struct {
	Dir string
	Out io.Writer

	scriptURL string
	runner    installRunner
}

// Install runs the platform's published installer with INSTALL_DIR set to opts.Dir.
func Install(ctx context.Context, opts InstallOptions) error {
	if opts.Dir == "" {
		return errors.New("install directory is empty")
	}

	script, err := fetchInstallScript(ctx, opts)
	if err != nil {
		return err
	}

	path, cleanup, err := stageScript(runtime.GOOS, script)
	if err != nil {
		return err
	}
	defer cleanup()

	run := opts.runner
	if run == nil {
		run = runInstallScript
	}

	return run(ctx, path, opts.Dir, opts.Out)
}

func scriptURLFor(goos string) string {
	if goos == "windows" {
		return installScriptWindowsURL
	}
	return installScriptURL
}

func fetchInstallScript(ctx context.Context, opts InstallOptions) ([]byte, error) {
	endpoint := opts.scriptURL
	if endpoint == "" {
		endpoint = scriptURLFor(runtime.GOOS)
	}
	client := &http.Client{Timeout: scriptTimeout}

	ctx, cancel := context.WithTimeout(ctx, scriptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building the installer request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading the installer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the installer download answered %s", resp.Status)
	}

	script, err := io.ReadAll(io.LimitReader(resp.Body, maxScriptBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading the installer: %w", err)
	}
	if len(script) == 0 {
		return nil, errors.New("the installer download was empty")
	}
	if len(script) > maxScriptBytes {
		return nil, fmt.Errorf("the installer is larger than %d bytes", maxScriptBytes)
	}

	return script, nil
}

// PowerShell refuses -File on a path that is not .ps1, so the extension matters.
func stageScript(goos string, script []byte) (string, func(), error) {
	pattern := "zen-review-install-*.sh"
	if goos == "windows" {
		pattern = "zen-review-install-*.ps1"
	}

	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, fmt.Errorf("staging the installer: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }

	if _, err := file.Write(script); err != nil {
		_ = file.Close()
		cleanup()
		return "", nil, fmt.Errorf("staging the installer: %w", err)
	}
	if err := file.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("staging the installer: %w", err)
	}

	return path, cleanup, nil
}

func installerArgs(goos, script string) (string, []string) {
	if goos == "windows" {
		return "powershell", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script}
	}
	return "sh", []string{script}
}

func runInstallScript(ctx context.Context, script, dir string, out io.Writer) error {
	name, args := installerArgs(runtime.GOOS, script)

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "INSTALL_DIR="+dir, "VERSION=")
	cmd.Stdout = out
	cmd.Stderr = out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running the installer: %w", err)
	}

	return nil
}

// InstallDir returns the running binary's directory, symlinks resolved.
// Errors when the binary isn't named what the installer writes, since the install would land beside it.
func InstallDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolving the running binary: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolving the running binary: %w", err)
	}

	return installDirFor(resolved, runtime.GOOS)
}

func installDirFor(binary, goos string) (string, error) {
	want, got := "zen-review", filepath.Base(binary)
	matches := got == want
	if goos == "windows" {
		want += ".exe"
		matches = strings.EqualFold(got, want)
	}
	if !matches {
		return "", fmt.Errorf("the installer writes %s, not %s", want, got)
	}
	return filepath.Dir(binary), nil
}
