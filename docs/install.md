# Install

## Requirements

- **git.** The tool shells out to it for everything it reads.
- **A Nerd Font**, for the tree's folder and file glyphs. Without one the tree
  draws boxes where those glyphs go. Nothing else on screen asks anything of
  your font, and the layout holds either way.
- **Go 1.26.4 or newer**, only if you are building it yourself. The released
  binaries need nothing but git.

Releases carry macOS and Linux on arm64 and amd64, and Windows on amd64.
Everything is pure Go, so there is no libc to match.

## Install

macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/praxis-labs-io/zen-review/main/install.sh | sh
```

Windows:

```powershell
irm https://raw.githubusercontent.com/praxis-labs-io/zen-review/main/install.ps1 | iex
```

Both download the binary for your machine, check it against the `checksums.txt`
the release publishes, and install nothing that doesn't match. `install.sh` puts
it in `~/.local/bin` and `install.ps1` in `%LOCALAPPDATA%\Programs\zen-review`.
On a platform no release carries, either one says so and points you at Go:

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

```powershell
$env:VERSION = 'v0.1.0'; irm https://raw.githubusercontent.com/praxis-labs-io/zen-review/main/install.ps1 | iex; Remove-Item Env:VERSION
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

`install.sh` and `make install` both put the binary in `~/.local/bin`. If it's
not on your `PATH`, the installer says so and prints the line to add:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Put it in `~/.zshrc` or `~/.bashrc` and open a new shell.

On Windows:

```powershell
[Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ";$env:LOCALAPPDATA\Programs\zen-review", 'User')
```

Then open a new terminal. Neither installer edits `PATH` for you.

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

```sh
zen-review update
```

It asks GitHub for the latest release and stops if you already have it.
Otherwise it runs the same installer as above into the directory the running
binary is in, so the new one replaces it in place. If the lookup fails it says
so and installs anyway. It refuses a binary renamed from `zen-review`, since the
installer would write a new one beside it and leave the old one running.

`zen-review --version` says what you're running. A build from a clone reports
`dev`, because only the release workflow stamps a version, and
`zen-review update` replaces it with the latest release. To stay on your own
build, upgrade from the clone instead:

```sh
git pull
make install
```

### The launch check

When the reader opens, zen-review asks the same endpoint whether a newer release
is out. When one is, the status bar names it and `zen-review update` until
something else has to be said there. The answer is kept for a day in
`~/.zen-review/update-check.json`, so it's at most one request a day. The request
carries no token, only the running version in its user agent, and a failed one
shows nothing.

Only the reader asks. `status`, `comments` and every other command, and a bare
`zen-review` with no terminal, never touch the network, so an agent or a hook
running them doesn't either. A `dev` build never asks.

To turn it off, put this in `~/.zen-review/config.json`:

```json
{ "update_check": false }
```

`ZEN_REVIEW_CONFIG_DIR` moves that directory. A key the file doesn't know, or a
file that doesn't parse, is named on the status bar when the reader opens, and
the check stays off until it's fixed. `zen-review update` checks regardless,
since running it is the ask.
