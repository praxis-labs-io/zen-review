# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

Drew's local code review engine, at `zen-review/zen-review` (`origin`). A review
engine with a TUI attached, not a diff viewer with review features bolted on.

It answers one question a diff viewer cannot: which of these machine-generated
changes have I personally inspected, and are they still the ones I inspected.
Agents write code faster than anyone can read it and rewrite code you already
read, so reviewed state is stored as line ranges and translated through every
new generation of the changeset.

Bare `zen-review` opens one changeset: `merge-base(base, HEAD)` through the
working tree, untracked files included. There is no `--staged` and no
`--working-tree`; both would be a second answer to "what am I looking at".

**`main` is the product branch.** Feature work flows ticket → branch → PR on `origin`.

Two things skip the PR and commit straight to `main`:

- Genuinely trivial tweaks. A typo, a one-liner.
- **Doc-only changes with no code.** Markdown, comments, `CLAUDE.md`, rules files. A PR for prose is ceremony.

A tracked pre-push hook rejects pushes to `main`, so an agent commits these and Drew pushes them. Don't reach for `--no-verify`.

The installed binary is built from here to `~/.local/bin/zen-review`; **rebuild after changes or Drew keeps running the old code**:

```sh
make install
```

Anything published under Drew's name (PR bodies, issues, README) must be shown to him word-for-word before pushing. His voice: terse, considerate, stoic, no strong adverbs, no em-dashes.

## Conventions

@.claude/rules/code-quality.md

That file holds only the Go and Bubble Tea specifics. The principles and voice rules are global and load automatically; don't copy them in here, that only creates drift.

## Commands

```sh
make all              # lint (gofmt + mod-tidy + golangci-lint) + test + build
make test             # go test -race -coverprofile ./...
make lint             # includes gofmt check and go.mod tidiness
make fmt-fix          # gofmt -w .
make golden           # regenerate the golden files in internal/diff and internal/cli
make install          # build to ~/.local/bin/zen-review
go test ./internal/review/ -run TestName   # single test
```

Run checks directly, never through a pipe that swallows exit codes. `make lint | tail` reports success on failure.

### Lint version pin

CI pins golangci-lint to match the local brew version (`.github/workflows/ci.yml`). Keep the pin current with the local version, or CI and local runs stop agreeing.

### Git hooks

`.githooks/pre-push` is tracked and rejects pushes to `main`. `git config core.hooksPath .githooks` wires it up; the SessionStart hook does this on every session so a fresh clone is covered. Untracked `.git/hooks/` files don't survive a clone, which is why the hook lives here instead.

## zen-kit

The visual layer is a separate module, `github.com/zen-kit/zen-kit`: `theme`,
`syntax`, and the `paint` diff-line painter. zen-octo paints from the same one.

A rendering change that both tools want belongs there. A change only this tool
wants belongs in `tui/diffpane`. zen-kit holds no model, no state, no layout and
no keys, and pushing any of those into it is how it stops being reusable.

It is pre-1.0 and both consumers are ours, so a breaking change there is a bump
and two import fixes, not a deprecation cycle. `go run ./cmd/kitdemo` in that
repo is how a theme change is judged.

## Charm module paths

The Charm v2 line lives under `charm.land/*`, not `github.com/charmbracelet/*`. `github.com/charmbracelet/bubbletea/v2` does not resolve. Version numbers are the same across both paths.

```
charm.land/bubbletea/v2
charm.land/lipgloss/v2
charm.land/bubbles/v2
charm.land/glamour/v2
```

`github.com/charmbracelet/fang` (v1 line) keeps its github path and pulls an older beta of `charm.land/lipgloss/v2`. Requiring v2.0.5 directly upgrades past it; there is no two-lipgloss problem as long as nothing imports the github v2 path.

## Project Management

Work is tracked in Linear: Praxis Labs workspace, reached through the `linear-zen-review` MCP server declared in `.mcp.json`. This repo's tickets are the **Zen Review** team (key `ZNR`, tickets `ZNR-###`). zen-kit's are the **Zen Kit** team (key `ZNK`). Address projects and statuses **by name, never a UUID**; ids don't survive workspace moves.

Zen Kit has no projects and no tickets yet, and everything filed against zen-kit so far is a `ZNR`. Give the next one a `ZNK` and its own bucket; leave the old ones where they are rather than renumbering links that already point at them.

The bucket names are shared with other teams, so `save_issue` resolving a bare project name can land on another team's copy and fail the call. Pass the Zen Review project id in that one argument when it does.

### Projects

Zen Review's five long-running buckets plus the current epic. Every ticket belongs to exactly one:

- **Polish & Bugs**: bugs and rough edges in surfaces that already ship. The dogfood inbox.
- **Feature Backlog**: net-new capabilities. Ideas live here until promoted.
- **Performance and Code-Quality**: improves the code, no user-visible change.
- **Website**: the public site, its copy, its SEO.
- **Release & Distribution**: how the binary gets from `main` to a user and stays current.
- **Zen Review v0.1**: the current epic, M0 through M6. An epic is a Linear Project, never a tracking issue. When it closes, follow-ups move to the matching bucket.

### Tickets

- Every ticket gets the team, exactly one project, a priority, and a status. No orphans.
- Create tickets as we go; never dump a full backlog up front.
- PR-sized scoping: 1 ticket = 1 branch = 1 PR as the rule of thumb. A ticket spanning both repos gets one PR in each.
- Keep descriptions lean: clear title, short goal and scope. No boilerplate acceptance criteria.
- Use Linear's generated branch name (`gitBranchName` from the MCP), never an invented one.
- Reference the ticket id in commits and the PR title/body so Linear auto-links.
- Status ladder: agent drives Backlog → Todo → In Progress. The GitHub integration owns In Review and Done; never write those by hand.

### Shipping

Feature-complete work ships via the global `ship-feature` skill: `make all` green, push, draft PR, Copilot + `/code-review`, triage with no tech debt, push then mark ready as separate actions. Manual invocation only.

**There is no copy of it in this repo.** Drew's global skills are a symlink into drucial-dots and load in every repo, so a copy here only shadows the real one and drifts behind it, which is what the copy this repo used to carry did. Edit the skill at its source.

### Specs

`docs/superpowers/specs/` holds the design docs that shaped a milestone. `docs/` otherwise describes only what is true today. Durable context lives in Linear project descriptions and tickets.

## Architecture

`cmd/zen-review` is the entrypoint (fang over cobra). Everything else lives in `internal/`.

```
internal/
  git/         plumbing only. Returns bytes and structs, never opinions.
  diff/        unified diff text -> files, hunks, lines. Knows nothing of review.
  review/      the engine. Sessions, generations, review state, comments, remapping.
  store/       SQLite and migrations. Nothing above it imports database/sql.
  cli/         the review subcommands. A thin shell over review/.
  tui/         app, tree, diffpane, compose, comp.
  testrepo/    real git repos for tests. Test-only, imports nothing of ours.
  testchangeset/  changesets for the render tests. Test-only, no git, no database.
  golden/      the golden-file compare. Test-only, and owns the -update flag.
```

The boundaries are in `.claude/rules/code-quality.md` and breaking one is a review-stopper. The short version: the CLI has to be able to answer any question the TUI can.

Note that zen-octo has a `store` package holding in-memory fetch state. Same name, different job: this one is the database.

`Session.Files` and `Derive` both hand the files back in the order a file tree
reads: directories above the files beside them, by byte within each group. Git's
order is the order it walked the index in, and one ordering from the engine is
what keeps the printed table and the tree pane from disagreeing about what is
first. Nothing above `review` sorts.

### Sessions and generations

A session is one repo plus one branch, resumable days later. A generation is a
snapshot of the whole changeset written into git as a real commit under
`refs/zen-review/sessions/<id>`, so a comment always knows the exact bytes it was
about and `git gc` cannot take them.

A refresh moves the ref before it writes the row, and swaps against the ref's
own previous value rather than the last `commit_sha` stored. Two instances
refreshing one session both build, one wins the swap, and the loser writes
nothing. The other order lets both write rows and leaves the ref pointing at one
of them.

The row is written in a transaction that reads what it carries from inside
itself, and every write naming a generation asserts from inside its own that the
generation is still the latest. A mark, a comment or a state change committed
while a refresh is in flight therefore moves forward with it or is refused,
never accepted and lost. All the git work is done before that transaction opens,
which it can be because nothing the translation needs is a row.

The refresh takes the same assertion. A swap can succeed on a ref read after
somebody else moved it, and a refresh carrying out of a generation that is no
longer the tip would drop every write made against the one in between. So it
refuses, and reports the lost race the swap would have.

Reviewed state is line ranges, never hunk indices: an agent inserting twenty
lines above hunk 3 leaves different code wearing the same label. A refresh
translates them through one diff of the two generation trees, and a range that
fails to translate disappears.

`changed after review` is the refresh writing down what it took, on
`gen_files.cut`. It cannot be read back off the coverage: a range the
translation cut and a range somebody unmarked leave the same coverage behind,
and only the refresh ran the translation. The record follows a rename through
the same diff the ranges do, and stands until the file reads reviewed again or
an unmark settles it, because a refresh only runs when something moved.

A base-side range fails to translate when upstream rewrote the lines whose
removal somebody read, which is the same fact on the other side, and it is
recorded under the name the changeset lists the file by. What a base move does
that is not a cut is widen the scope, and that leaves every stored range
translating cleanly.

Deletion-only hunks have no head-side lines and anchor to base-side ranges.

A comment moves while it is open, and stops the moment it is addressed, resolved
or orphaned. Its row then stays at the generation it stopped at and records where
the anchor was, so nothing has to know which generation a frozen comment is
pinned to in order to say where it lived. Only an open comment orphans: one
already addressed or resolved that loses its anchor was acted on, and the rewrite
that destroyed it is the acting.

The anchor translation is deliberately more forgiving than the one reviewed
ranges take. A comment on ten lines is about a region, and an agent rewriting a
line in the middle of it is usually the comment being acted on rather than the
comment being lost, so the anchor clamps to what survived where a range would be
cut into the pieces either side. A file comment is the exception and takes the
range rule: it names the file, so it follows a rename and is lost when the bytes
move. `anchor_blob` is written once, at creation, and is the bytes the comment
was about at the generation `created_generation_id` names.

### Storage

`$(git rev-parse --git-common-dir)/zen-review/state.db`, so a worktree and its
parent checkout share one database and a throwaway worktree does not take the
review with it. Nothing lands in the working tree.

`modernc.org/sqlite`, pure Go: the cgo driver is faster but puts a C toolchain in
the path of every cross-compile and CI runner, for a few thousand rows. WAL with
a busy timeout so two instances on one repo do not deadlock.

`.git` not writable is a startup error, not a degraded mode where the review
silently is not saved.

## Keys

The keymap is shared with zen-octo by convention, written down in both
`CLAUDE.md` files rather than in shared code, so the two tools feel the same
without either being hostage to the other's release cycle. zen-kit holds no keys.

```
j k g G                  movement
ctrl+u ctrl+d            page the diff, from either pane
h l                      tree pane / diff pane
1 2                      the same two, by the badge in the pane's border
} {                      the ring: next / prev hunk
space                    fold / unfold
tab shift+tab            next / prev file
n N                      next / prev unreviewed hunk
] [                      next / prev comment

r                        mark hunk reviewed, advance to next unreviewed
R                        mark whole file reviewed
u U                      take either back
c                        comment on the selection
v                        range selection, j/k extend
C                        session summary note
x                        resolve a comment
enter                    tree: open file. comment: jump to its line

p                        full-file preview
|                        unified / side-by-side
/                        filter the tree
b                        change the base
s                        reload
? q ctrl+c               help, quit, quit from anywhere
```

`n` is the one that matters. A review is a burn-down and `n` is the key held
until the count reaches zero. `r` advances after marking, so `r r r r` walks the
whole thing.

A hunk is the block a paragraph motion moves by, so `}` and `{` step it. Vim's
own diff mode says `]c` and `[c`, and the bracket pair is spoken for. Nothing in
vim moves a whole file in one key, so `tab` is the TUI answer rather than the
editor one; the tree does the same job by hand.

Six divergences from zen-octo, all deliberate:

- `space` folds, replacing `o`. zen-octo adopts this too.
- `tab` / `shift+tab` are the tab strip in zen-octo and next / previous file here. Same reason as `]` and `[`: zen-review will never have tabs.
- `ctrl+u` / `ctrl+d` page the diff from either pane. zen-octo pages whichever pane has focus, its rail included. Walking the tree here is how a reader gets to a file, and reading it is what they came for.
- `]` / `[` are tabs in zen-octo and comments here. zen-octo has tabs and zen-review never will.
- `r` is reply in zen-octo and mark-reviewed here, and `u` / `U` take a mark back. Neither tool has the other's concept.
- `v` is jump-to-diff in zen-octo, scoped to the conversation, and range selection here. They do not collide.

The diff pane opens focused on the first unreviewed hunk. zen-octo's
conversation opens unfocused because the reader came to read; you came here to
burn a review down.

## Exit codes

`diff` and `grep` split answering from failing, and so does this: **0** answered
and nothing matched, **1** answered and the filter matched, **2** failed. Only
`comments --exit-code` reaches 1, which is what makes a Stop hook able to tell an
open comment from a broken git call. `cmd/zen-review` maps them through
`cli.ExitCode`, and `cli.Quiet` is what keeps the matched status from being
printed as an error.

## Rendering traps

zen-kit's `CLAUDE.md` holds the painter's traps: per-cell backgrounds, clipping
before wrapping, tokenising a side whole. These are this repo's own.

- **A viewport offset is a line, and a row is not.** Once rows are two lines, scroll arithmetic that lands on the row it wants opens the window on that row's second line with its title cut off above. Round the offset up to the next item boundary, and size the viewport to a whole number of rows or the end-of-list clamp lands back between two lines.
- **`viewport.EnsureVisible` is not a scroll-to-cursor.** It acts only once the line is already outside the window, then puts it on the top row. Move the offset by hand.
- **The shortest scroll onto the screen is the wrong one.** A key that lands on a block is taking the reader somewhere, so put the block at the top row, and leave it alone when it already fits on screen whole. A cursor moving a row at a time is the exception: the reader is already looking at the row, and the window is what fell behind.
- **A key cannot aim at a block through the scroll offset.** A pane that fits its content has no offset to move, so a key reading its target off the top row acts on the first block whatever the reader does. Give the pane a cursor or do not give it the key.
- **A block that answers the line above it cannot go to the top row.** A comment hangs under the code it was written against, so topping the card scrolls that code away. Open a few lines above it, never above the file's heading.
- **A pane clips overflow silently.** A row wider than the pane loses its trailing columns mid-cell with no ellipsis, and a width test still passes. The row has to fit before the pane sees it.
- **A glyph is only one cell if lipgloss and the terminal agree it is.** The tree's folders and its file marker are Nerd Font, which is the one thing on screen that asks anything of the terminal's font. Measure a new one with `lipgloss.Width` before using it: a two-cell glyph puts every row after it out of step, where a font missing a one-cell glyph only draws a box.
- **A stripped golden cannot see a colour.** The tree's cursor is a filled background and nothing else, so the frame is identical whether `j` moved or not. Anything said only in colour needs an assertion against the theme value beside the golden.
- **Nothing moves on a refresh until the key is pressed.** A formatter running on save would otherwise reshuffle the page while a comment is being written.
