package git

import (
	"context"
	"fmt"
)

// diffFlags pin what the parser sees. Each one defends against a config key a
// user or a repository is entitled to set: diff.external and a textconv filter
// return something that is not a diff, diff.noprefix and diff.context move the
// parts the parser reads, core.quotePath escapes every non-ASCII path, and an
// abbreviated index line is not the blob identity a generation anchors to.
//
// diff.submodule is the one that fails silently. Set to "log" it writes an
// embedded repository as a bare "Submodule x 000...abc" line with no
// "diff --git" header at all, so the parser sees no file and the changeset
// loses a row. "short" writes the ordinary header and one Subproject line.
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

// diffArgv is `git diff` with the flags pinned, plus whatever the caller adds.
func diffArgv(extra ...string) []string {
	argv := append([]string{"-c", "core.quotePath=false", "diff"}, diffFlags...)
	return append(argv, extra...)
}

// DiffTrees is the unified diff between two tree-ish, which is what a generation
// is measured with: the base commit against the tree just snapshotted.
//
// The head side is a tree rather than a commit deliberately. It lets the caller
// see the whole changeset, and refuse an unreviewable one, before it writes a
// commit object for it.
func (r *Repo) DiffTrees(ctx context.Context, from, to string) ([]byte, error) {
	out, err := run(ctx, r.root, diffArgv("--end-of-options", from, to, "--")...)
	if err != nil {
		return nil, fmt.Errorf("diffing %s against %s: %w", from, to, err)
	}
	return out, nil
}

// Untracked lists the paths git knows nothing about, honouring .gitignore and the
// exclude files. Paths are relative to the work tree root.
func (r *Repo) Untracked(ctx context.Context) ([]string, error) {
	out, err := run(ctx, r.root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("listing untracked files: %w", err)
	}
	return nulFields(out), nil
}
