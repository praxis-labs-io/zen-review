# zen-review

Which of these machine-generated changes have I inspected, and are they still
the ones I inspected?

A diff viewer cannot answer that. Agents write code faster than anyone can read
it, and they rewrite code you have already read, so a mark on hunk 3 stops
meaning anything the moment twenty lines land above it.

zen-review is a review engine with a TUI attached. What you have read is stored
as line ranges and carried through every new version of the changeset. A range
that survives moves with its code; a range the rewrite destroyed comes back
unread, and the file says it changed after you reviewed it.

```
base        main (fc8b758)  ·  no remote
generation  1
session     refs/zen-review/sessions/5817549abf0a013f

M  main.go  reviewed    1 of 1  +2 -1
     head  7  reviewed
M  sum.go   unreviewed  0 of 1  +10 -3
     head  4  unreviewed

2 files, 1 of 2 reviewed
```

Bare `zen-review` opens one changeset: the merge base with the base branch,
through the working tree, untracked files included. There is no `--staged` and
no `--working-tree`, because both would be a second answer to what am I looking
at.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/praxis-labs-io/zen-review/main/install.sh | sh
```

Downloads the binary for macOS or Linux, on arm64 or amd64. Windows takes the
`.zip` off the
[releases page](https://github.com/praxis-labs-io/zen-review/releases), the
installer being a POSIX script. On anything else:

```sh
go install github.com/praxis-labs-io/zen-review/cmd/zen-review@latest
```

Or from a clone, which is what you want if you intend to change anything:

```sh
git clone https://github.com/praxis-labs-io/zen-review.git
cd zen-review
make install
```

Both put the binary in `~/.local/bin`. Set `INSTALL_DIR` to put it somewhere
else. [docs/install.md](docs/install.md) has the requirements, the PATH setup
and how to upgrade.

## A first review

```sh
cd your-repo
zen-review
```

It measures against your base branch, builds a snapshot of the changeset, and
opens on the first hunk you have not read.

`n` is the key that matters. A review is a burn-down and `n` walks to the next
unreviewed hunk. `r` marks the one you are on and advances, so `r r r r` walks
the whole thing. `c` writes a comment, `|` splits the diff into two columns, and
`?` lists every key.

Come back tomorrow and it is where you left it. Let an agent rewrite half of it
first and the review is still where you left it, minus the parts that moved.

## Documentation

- [Guide](docs/guide.md) — the base, sessions and generations, what survives a
  rewrite, how comments travel
- [CLI reference](docs/cli.md) — every command, flag and exit code
- [Keys](docs/keys.md) — the keymap and what each key does
- [Install](docs/install.md) — requirements, PATH, upgrading
- [Agents](docs/agents.md) — driving the tool from a hook or a script
- [Contributing](docs/CONTRIBUTING.md) — building, testing and the layer
  boundaries

## License

MIT. See [LICENSE](LICENSE).
