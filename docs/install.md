# Install

## Requirements

- **git.** The tool shells out to it for everything it reads.
- **A Nerd Font**, for the tree's folder and file glyphs. Without one the tree
  draws boxes where those glyphs go. Nothing else on screen asks anything of
  your font, and the layout holds either way.
- **Go 1.26.4 or newer**, only if you are building it yourself. The released
  binaries need nothing but git.

Releases carry macOS and Linux on arm64 and amd64. Everything is pure Go, so
there is no libc to match.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/praxis-labs-io/zen-review/main/install.sh | sh
```

It downloads the binary for your platform and puts it in `~/.local/bin`. On a
platform the releases do not carry, it says so and points you at Go:

```sh
go install github.com/praxis-labs-io/zen-review/cmd/zen-review@latest
```

That builds for whatever you are on. It reports `dev` rather than a version,
being a build and not a release.

`INSTALL_DIR` overrides where the installer puts things, and `VERSION` pins a
release:

```sh
curl -fsSL https://raw.githubusercontent.com/praxis-labs-io/zen-review/main/install.sh | INSTALL_DIR=/usr/local/bin sh
curl -fsSL https://raw.githubusercontent.com/praxis-labs-io/zen-review/main/install.sh | VERSION=v0.1.0 sh
```

Every release carries a `checksums.txt` beside the archives if you want to
verify one before it runs.

From a clone, which is what you want if you intend to change anything:

```sh
git clone https://github.com/praxis-labs-io/zen-review.git
cd zen-review
make install
```

`make install` builds this tree into `~/.local/bin/zen-review`. Run it again
after every change, or you keep running the old binary.

## PATH

Both paths install to `~/.local/bin`. If it is not on your `PATH`, the installer
says so and prints the line to add:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Put it in `~/.zshrc` or `~/.bashrc` and open a new shell.

## Your first review

```sh
cd your-repo
zen-review
```

A bare invocation does three things: works out what to measure against, builds a
snapshot of the changeset, and opens the reader on the first hunk you have not
read. With no terminal to open the reader on, it prints the changeset instead,
which is what makes it usable from a script.

Nothing about the base stops it opening. It prefers `origin/HEAD`, falls back to
your local `main` or `master`, then to the branch you are stacked on, then to
`HEAD` itself with your uncommitted work as the changeset. When it had to guess,
it says so beside the ref. [The guide](guide.md#the-base) covers the ladder and
how to change the base.

State goes in `$(git rev-parse --git-common-dir)/zen-review/state.db`. A
worktree and its parent checkout share one database, and nothing lands in your
working tree.

## Upgrading

Re-run the installer, or from a clone:

```sh
git pull
make install
```

There is no update check and nothing phones home. `zen-review --version` says
what you are running, and reports `dev` on a source build: the version is
stamped in at link time, and only the release workflow stamps it.
