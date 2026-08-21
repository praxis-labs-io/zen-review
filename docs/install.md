# Install

## Requirements

- **Go 1.26.4 or newer.** There is no release artifact yet, so every install
  path builds from source.
- **git.** The tool shells out to it for everything it reads.
- **A Nerd Font**, for the tree's folder and file glyphs. Without one the tree
  draws boxes where those glyphs go. Nothing else on screen asks anything of
  your font, and the layout holds either way.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/praxis-labs-io/zen-review/main/install.sh | sh
```

It clones into a temp directory, builds, and puts the binary in `~/.local/bin`.
`INSTALL_DIR` overrides where it lands:

```sh
curl -fsSL https://raw.githubusercontent.com/praxis-labs-io/zen-review/main/install.sh | INSTALL_DIR=/usr/local/bin sh
```

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

There is no update check and nothing phones home. `zen-review --version`
reports `dev` on a source build; the version is stamped in at link time and
nothing stamps it yet.
