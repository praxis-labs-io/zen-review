package git

import (
	"context"
	"fmt"
)

// diffFlags override every diff config key that changes what the parser reads, diff.submodule=log included.
var diffFlags = []string{
	"--no-color",
	"--no-ext-diff",
	"--no-textconv",
	"--find-renames",
	"--full-index",
	"--unified=3",
	"--src-prefix=a/",
	"--dst-prefix=b/",
	"--submodule=short",
}

func diffArgv(extra ...string) []string {
	argv := append([]string{"-c", "core.quotePath=false", "diff"}, diffFlags...)
	return append(argv, extra...)
}

func (r *Repo) DiffTrees(ctx context.Context, from, to string) ([]byte, error) {
	out, err := run(ctx, r.root, diffArgv("--end-of-options", from, to, "--")...)
	if err != nil {
		return nil, fmt.Errorf("diffing %s against %s: %w", from, to, err)
	}
	return out, nil
}

// RemapDiff is DiffTrees with no context and rename detection past diff.renameLimit.
func (r *Repo) RemapDiff(ctx context.Context, from, to string) ([]byte, error) {
	out, err := run(ctx, r.root, diffArgv("--unified=0", "-l0", "--end-of-options", from, to, "--")...)
	if err != nil {
		return nil, fmt.Errorf("diffing %s against %s for the remap: %w", from, to, err)
	}
	return out, nil
}

// Untracked lists non-ignored untracked paths, relative to the work tree root.
func (r *Repo) Untracked(ctx context.Context) ([]string, error) {
	out, err := run(ctx, r.root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("listing untracked files: %w", err)
	}
	return nulFields(out), nil
}
