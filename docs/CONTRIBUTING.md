# Contributing

## Setup

```sh
git clone https://github.com/praxis-labs-io/zen-review.git
cd zen-review
git config core.hooksPath .githooks
make install
```

The hook wiring matters. `.githooks/pre-push` is tracked and rejects pushes to
`main`; untracked `.git/hooks/` files do not survive a clone, which is why it
lives in the repo. Do not reach for `--no-verify`.

`make install` builds this tree into `~/.local/bin/zen-review`. Run it after
every change or you keep testing the old binary.

## The checks

`make all` is the gate. It runs lint, then tests, then the build.

| Command | Does |
| --- | --- |
| `make all` | lint, test, build. What has to be green |
| `make test` | `go test -race -coverprofile ./...` |
| `make lint` | gofmt, go.mod tidiness, golangci-lint |
| `make fmt-fix` | `gofmt -w .` |
| `make golden` | Regenerate every golden file |
| `make coverage` | Coverage report from the last test run |
| `go run ./cmd/paintdemo` | Paint a canned diff and exit |

Run them directly, never through a pipe that swallows the exit code.
`make lint | tail` reports success on failure.

CI pins golangci-lint to match the local brew version, in
`.github/workflows/ci.yml`. Keep the pin current or CI and local runs stop
agreeing.

`go run ./cmd/paintdemo` is how a rendering change is judged. It paints a canned
diff at a width where a row overflows, so a theme change shows everything it
broke in one screen.

## Layout

```
cmd/zen-review    the entrypoint (fang over cobra)
cmd/paintdemo     paints a canned diff and exits

internal/
  git/            plumbing only. Bytes and structs, never opinions
  diff/           unified diff text -> files, hunks, lines
  review/         the engine. Sessions, generations, state, comments, remapping
  store/          SQLite and migrations
  cli/            the subcommands. A thin shell over review/
  tui/            app, tree, diffpane, compose, comp
  tui/theme/      the palette
  tui/syntax/     Chroma tokens, not rendered text
  tui/paint/      the diff-line painter. Pure functions
```

## Boundaries

These are the architecture, and breaking one is a review-stopper rather than a
nit. The full set is in
[`.claude/rules/code-quality.md`](../.claude/rules/code-quality.md).

The test that settles most questions: **could the CLI answer this without the
TUI running?** If not, the logic is in the wrong package.

- `internal/git` runs git and returns bytes. Nothing above it shells out.
- `internal/diff` never imports `review`.
- `internal/store` is the only package importing `database/sql`.
- `internal/review` is the engine, and the TUI holds none of it. A behaviour
  reachable by key and not by subcommand is in the wrong place.
- `internal/tui/*` packages never import each other sideways. Shared widgets go
  in `internal/tui/comp`.
- Nothing below `tui/` imports `theme`, `syntax` or `paint`.

Everything styles from the `theme.Theme` it was handed. A colour that is not in
the struct means the struct needs a field, not a hardcoded Lipgloss value.

## Tests

Tests ship in the same commit as the behaviour they verify, never a follow-up.

- Test through the real interface. Drive key messages and assert rendered
  frames, not model fields: a test that only reads internal state stays green
  while the thing it renders is broken.
- `internal/git` runs against real temporary repos, no mocks. `internal/testrepo`
  builds them.
- Remapping gets a case per way it goes bad: a line inserted above a reviewed
  hunk, a reviewed line deleted, a region rewritten wholesale, a rename, a
  delete, a rebase to identical content, a base change, a force-push that loses
  the fork point.
- `diff` gets golden files on unified diff text, `cli` gets golden JSON.
- Render tests run at widths where something overflows, which is the only width
  that proves anything.
- `paint`'s goldens keep their escapes where the frame goldens are stripped.
  `cat` one to read it.

## Commits and pull requests

- Atomic and single-purpose. Implementation, cleanup and unrelated refactors are
  separate commits.
- Never commit a known-broken intermediate state.
- Terse messages describing intent.
- Feature work goes ticket, branch, PR. `main` is the product branch.
- Doc-only changes and genuinely trivial fixes skip the PR and go straight to
  `main`. A PR for prose is ceremony.

## Docs that describe the code

Every user-facing surface has a document that describes it, and a change to the
surface makes that document wrong until somebody moves it. This is the map, and
it is read at merge time and again before a release:

| Changed | Read |
| --- | --- |
| `internal/cli/**` | [`cli.md`](cli.md), [`agents.md`](agents.md) |
| `internal/tui/**` | [`keys.md`](keys.md) |
| `internal/review/**`, `internal/store/**` | [`guide.md`](guide.md) |
| `internal/git/**`, `internal/diff/**` | [`guide.md`](guide.md) |
| `install.sh`, `Makefile`, `.github/workflows/**` | [`install.md`](install.md), [`README.md`](../README.md) |
| `.claude/rules/**`, the test conventions | this file |

`git diff --name-only <ref>..HEAD` gives the left column, so the set of documents
to check is derived rather than remembered.

A change nothing on this map covers is one of two things: a doc gap to fill, or
work no user sees. Say which rather than leaving it unanswered.

## Version numbers

Semver, and pre-1.0 while the shape can still move:

- **Minor** carries anything a user would notice. A new key, a changed default, a
  command that answers differently.
- **Patch** carries fixes and everything internal.
- **Major** waits for 1.0.

A published tag is permanent. It cannot be renumbered, and a release cut under
the wrong number stays wrong, so the version is confirmed before the tag is
pushed rather than inferred from the range.

Releases are cut with the `release` skill, which curates
`docs/release-notes/vX.Y.Z.md` and hands the tag command over. The notes file has
to be on `main` before the tag is cut: the workflow reads it out of the tagged
commit.

Agent-facing conventions, the project's own history and the reasoning behind the
design live in [`CLAUDE.md`](../CLAUDE.md).
