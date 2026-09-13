# CLAUDE.md

## What this repo is

Drew's local code review engine, at `praxis-labs-io/zen-review` (`origin`). A review
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

## Conventions

@.claude/rules/code-quality.md

That file holds only the Go and Bubble Tea specifics. The principles and voice rules are global and load automatically; don't copy them in here, that only creates drift.

## Commands

```sh
make all              # lint (gofmt + mod-tidy + golangci-lint) + test + build
make test             # go test -race -coverprofile ./...
make lint             # includes gofmt check and go.mod tidiness
make fmt-fix          # gofmt -w .
make golden           # regenerate every golden file: diff, cli, tui/app, tui/paint
make install          # build to ~/.local/bin/zen-review
go run ./cmd/paintdemo  # look at the painter
go test ./internal/review/ -run TestName   # single test
```

Run checks directly, never through a pipe that swallows exit codes. `make lint | tail` reports success on failure.

### Lint version pin

CI pins golangci-lint to match the local brew version (`.github/workflows/ci.yml`). Keep the pin current with the local version, or CI and local runs stop agreeing.

### Git hooks

`.githooks/pre-push` is tracked and rejects pushes to `main`. `git config core.hooksPath .githooks` wires it up; the SessionStart hook does this on every session so a fresh clone is covered. Untracked `.git/hooks/` files don't survive a clone, which is why the hook lives here instead.

## The visual layer

`tui/theme` is the palette, `tui/syntax` returns Chroma tokens, and `tui/paint`
turns a diff line into a row. They hold no model, state, layout or keys.
Folding, scroll, side-by-side layout, hunk grouping, the two-sided tokenise split
and review state belong to `tui/diffpane`, and pushing any of them down makes a
second renderer.

`go run ./cmd/paintdemo` is how a rendering change is judged. It paints a canned
diff at a width where a row overflows, with a hunk header, all three line kinds,
a tab-indented line, a clipped row and a `Fill` row, through the real
derivation, so a light terminal and a dark one show different screens. A golden
file only holds it still.

### The theme

There is one theme and it is derived rather than written down. What the code has
to keep true:

- The hues are ANSI slots, so they follow the reader's palette only while they
  reach the terminal as slots. A test type-asserts them.
- **A slot may be painted and never blended**, for the reason under Styling in
  the rules. Slots 1 and 2 are queried by name because the diff tints are
  blends, and the tints are the product here. The two OSC 4 requests ride in the
  query that already runs.
- The greys are blends off the reported background, travelling toward the
  reported foreground. They only have to stay legible, which a slot cannot
  promise and a blend off a known background can, and one ratio serves a light
  terminal and a dark one.
- **A filled row is placed at a luma distance, not at a ratio.** The ratio that
  clears one palette's green leaves another's flat. `lift` solves for the
  distance between a floor and a ceiling: a pale green reaches it in a few
  percent and would read grey, and a green at the background's own weight never
  reaches it and keeps its lean.
- A selection is a lift, not a colour, so it stays neutral on a palette with a
  warm or violet foreground.
- The foreground is usable only if it is on the far side of the luma midpoint
  from the background **and** far enough from it. Those are two tests.
- `Text` is `lipgloss.NoColor{}`, which writes no sequence and tracks a terminal
  recoloured mid-session. Its `RGBA()` is black, so anything spelling a colour
  out for a third party special-cases it.
- `Background` is nil and stays nil. Every shade is derived against the
  terminal's own background, so painting it changes nothing visible and costs a
  translucent terminal its translucency.
- Nothing answered is not a guess. The three surfaces go nil rather than take a
  slot, because slot 0 is the background on many dark palettes.
- With no background reported, the text weights take `NoColor`. Slot 7 vanishes
  on a light background and slot 8 may be undeclared or equal to slot 0. Borders
  keep the grey, because a missing frame is visible and an unreadable label is
  not.
- The query runs in `app.Run` before Bubble Tea takes the tty, and drains to the
  device attributes or the terminal echoes them when raw mode ends. The reader is
  cancellable, or a timeout leaves a goroutine on the tty eating the first key.
- Chroma has no ANSI style and packs colours as RGB, so code cannot follow the
  palette. `github-dark` and `github` are paired against the background instead.

## Charm module paths

The Charm v2 line lives under `charm.land/*`, not `github.com/charmbracelet/*`. `github.com/charmbracelet/bubbletea/v2` does not resolve. Version numbers are the same across both paths.

```
charm.land/bubbletea/v2
charm.land/lipgloss/v2
charm.land/bubbles/v2
```

`github.com/charmbracelet/fang` (v1 line) keeps its github path and pulls an older beta of `charm.land/lipgloss/v2`. Requiring v2.0.5 directly upgrades past it; there is no two-lipgloss problem as long as nothing imports the github v2 path.

## Project Management

Work is tracked in Linear: Praxis Labs workspace, reached through the `linear-zen-review` MCP server declared in `.mcp.json`. Every ticket is the **Zen Review** team (key `ZNR`, tickets `ZNR-###`). Address projects and statuses **by name, never a UUID**; ids don't survive workspace moves.

The bucket names are shared with other teams, so `save_issue` resolving a bare project name can land on another team's copy and fail the call. Pass the Zen Review project id in that one argument when it does.

### Projects

Zen Review's five long-running buckets. Every ticket belongs to exactly one:

- **Polish & Bugs**: bugs and rough edges in surfaces that already ship. The dogfood inbox.
- **Feature Backlog**: net-new capabilities. Ideas live here until promoted.
- **Performance and Code-Quality**: improves the code, no user-visible change.
- **Website**: the public site, its copy, its SEO.
- **Release & Distribution**: how the binary gets from `main` to a user and stays current.

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

**There is no copy of it in this repo.**

### Specs

`docs/specs/` holds the design docs that shaped a milestone. `docs/` otherwise describes only what is true today. Durable context lives in Linear project descriptions and tickets. Specs are deleted as a part of branch cleanup.

## Architecture

`cmd/zen-review` is the entrypoint (fang over cobra). `cmd/paintdemo` paints a
canned diff and exits. Everything else lives in `internal/`.

```
internal/
  git/         plumbing only. Returns bytes and structs, never opinions.
  diff/        unified diff text -> files, hunks, lines. Knows nothing of review.
  review/      the engine. Sessions, generations, review state, comments, remapping.
  store/       SQLite and migrations. Nothing above it imports database/sql.
  cli/         the review subcommands. A thin shell over review/.
  tui/         app, tree, diffpane, compose, comp.
  tui/theme/   the palette. Every style reads from one.
  tui/syntax/  Chroma tokens, not rendered text.
  tui/paint/   the diff-line painter. Pure functions.
  testrepo/    real git repos for tests. Test-only, imports nothing of ours.
  testchangeset/  changesets for the render tests. Test-only, no git, no database.
  tui/testtheme/  the surface the render tests derive from. Test-only.
  golden/      the golden-file compare. Test-only, and owns the -update flag.
  plugin/      drives the shipped hook script. Test-only, no Go under test.
  version/     the version stamped in at build time.
```

The boundaries are in `.claude/rules/code-quality.md` and breaking one is a review-stopper. The short version: the CLI has to be able to answer any question the TUI can.

User-facing behaviour lives in `docs/`: [the guide](docs/guide.md) for the base, sessions, generations and comments, and [the CLI reference](docs/cli.md) for every command and flag. What follows here is what the code has to keep true, not what a reader sees. Change one and the other says the wrong thing.

`Session.Files` and `Derive` both return files in the order a file tree reads,
directories above the files beside them and by byte within each group, so the
printed table and the tree pane agree about what is first. Nothing above
`review` sorts.

### The base

- Detection always reaches the bottom of the ladder. The only startup that fails
  is a directory that is not a repository.
- The rung above bounds the candidate walk. On a trunk not called `main` or
  `master` it walks the whole first-parent chain, and on a default branch it does
  not run at all, because a tip left on that branch's own history would hide
  every commit since.
- `Candidates` is every local branch, not a first-parent walk, which hid a base
  that had been merged in. The only remote rows are the session's base and
  `origin/HEAD`, and nothing with nothing behind HEAD is offered.
- A fallback writes no ref and never clears the stored one, or one mistyped
  `--base` would cost the session every range measured from its base. The tag on
  `Base.Fallback` stands until the ref resolves.
- `SetBase` is the one call. `--base` and the `b` box both go through it, so a
  tag, a sha and `HEAD~5` behave the way a flag does.
- The tag is a standing fact, not a notice, so it takes no status bar and no key
  clears it. The reason sits right of the ref, so a narrow pane clips the reason
  and keeps the ref.

### Sessions and generations

- A refresh moves the ref before it writes the row, swapping against the ref's
  own previous value rather than the last stored `commit_sha`. The other order
  lets two instances both write rows.
- Every write naming a generation asserts inside its own transaction that the
  generation is still the latest, the refresh included. A write landing
  mid-refresh moves forward with it or is refused, never accepted and lost.
- All the git work runs before that transaction opens, which works because
  nothing the translation needs is a row.
- A row that does not land takes its commit back off the ref, past a cancel too.
  A reader quitting mid-refresh is commoner than two instances racing.
- `edit` and `delete` name no generation. Words are true at every generation, and
  a delete leaves no anchor to go stale.
- Reviewed state is line ranges, never hunk indices. Deletion-only hunks anchor
  to base-side ranges.
- `changed after review` is on `gen_files.cut` and cannot be read off the
  coverage, because a range the translation cut and a range somebody unmarked
  leave the same coverage. It follows a rename through the same diff the ranges
  do.
- The response is on `comments.response` and lands in the same swap as the
  state. Every other transition passes no response rather than reading and
  rewriting it, and a refresh carries neither.
- `--body` is the words of the thing the command names, so an invocation reused
  against the wrong verb is refused rather than written to the wrong half.
  `edit` does not reach the response, because the tool has no identity to
  enforce one voice with.
- A frozen comment's row stays at the generation it stopped at and records where
  the anchor was. Only an unfrozen comment orphans, and an orphan still takes
  `address`, since the anchor usually went because the comment was acted on.
- A comment's anchor clamps to what survived, where a reviewed range is cut into
  the pieces either side, and a file comment goes only when the file does. A mark
  says somebody read these bytes and an edit voids it; a comment says something
  about the file and an edit is usually what it asked for. `Anchor` and `Ranges`
  split on that.
- `anchor_blob`, `created_generation_id`, `created_start_line` and
  `created_end_line` are written once and never moved, or a comment that
  travelled would slice its blob by lines it never had. One diff per pair of
  blobs covers every comment on a file.
- A refresh race is tested by putting one refresh inside another at a chosen
  point, not by hoping the scheduler lines them up. `export_test.go` exposes
  `DuringRefresh`, which fires after the latest generation is read, and
  `AfterSwap`, which fires between the ref moving and the row being written.

### Storage

`$(git rev-parse --git-common-dir)/zen-review/state.db`, so a worktree and its
parent checkout share one database and nothing lands in the working tree.
`plugin/hooks/unresolved.sh` tests for that exact path to decide whether a
review was ever opened here, because resolving a session creates it. Moving one
moves the other.

`modernc.org/sqlite`, pure Go, so no C toolchain in the path of a cross-compile
or CI. WAL with a busy timeout so two instances on one repo do not deadlock.
`.git` not writable is a startup error, never a mode where the review silently
is not saved.

## Keys

The keymap is shared with zen-octo by convention. Every key is in
[docs/keys.md](docs/keys.md), and zen-octo keeps its own copy. What the code has
to keep true:

- The heading pin follows the window, not the cursor, because a heading names the
  lines under it. The pin owns the top line, so a key that would put the cursor
  there opens the window one higher.
- The cursor bar sits in a leading cell every row holds open, so nothing shifts
  as it passes. A selection shares the fill but not the bar, which marks where
  the next key moves from.
- A comment card is one stop for the cursor, not one per row. One taller than the
  window scrolls under the cursor before the cursor leaves it, and `reveal` keeps
  its end on screen rather than its first row, or `k` onto it from below skips
  the middle.
- A hunk comment is its own scope, not a range that happens to match a hunk,
  because a match flips the moment the hunk grows. Its card draws under the
  heading of the hunk holding its first line. Rows written as `range` before the
  scope existed stay `range`.
- `was` on a card's label is for numbers naming no code the pane could draw, an
  orphan or a comment frozen at another generation. It keys off the anchor, not
  off whether the layout found a row, because a comment written outside a hunk
  has no row while its line is still there.
- One cursor in side-by-side, never one per column, or a side-switch could throw
  the window.
- The mode `|` sets lasts the run and nothing stores it. `p` stores nothing and
  lasts only the file, because left on it would put hundreds of unchanged rows
  between the hunks of every file after it. `preview` names that file, and
  arriving at another clears it. The two compose: a filled-in line is a context
  line and takes both columns.
- `p` is the one rendering key that reads the repository. `Session.Body` hands
  the bytes up and the pane caches them per path for the generation, keyed to its
  own field because a reload blanks the pane's generation.
- The lines `p` fills in are synthetic `diff.Context` lines through the row
  builder the hunks use, so selection, the split pairing and the painter need no
  second path. They belong to no hunk, so no heading pins over them and `r` finds
  nothing to mark.
- A hunk keeps the blank row above and below it. Without the one below, nothing
  says where a change stops, and `r` refusing on the line past it reads as a
  fault.
- `r` refuses on a line in no hunk but not on a card in none. The ring puts a
  reader on such a card, so the mark falls back to the stop it came from.
- A mode change that moves a row by a hundred lines opens the window on the hunk
  rather than revealing the row, or the hunk lands on the bottom row.
- The composer takes every key while it is up, `ctrl+c` excepted, because raw
  mode sends no interrupt. A paste arrives as its own message, so the root routes
  what is not a key press into it too.
- The composer's cursor is the terminal's own, placed by the root through the
  view. A drawn one paints over the character it covers.
- The box comes down when the write lands, not when the key is pressed. A write
  that committed and could not be read back closes it anyway, because saving
  again writes a second comment, which is why the Source names that failure.
- The status bar clears on the next press inside a box as it does outside one.
- `>` is named in the card's own footer and never in the help overlay, which has
  no row to give at 16 rows. `x`, `e` and `D` are named in both. `esc` is named on
  the status bar while a selection is up.
- `ResolveComment` refuses a comment already resolved, so a settled card neither
  offers `x` nor takes it.
- Neither the response box nor its rail lights. The elbow is always `╰─`, there
  being one response, and a pane with no room for a second border drops the box
  rather than shrinking both.
- The replaced block is the translation the remap runs, not the two sides read at
  the same numbers. The comment's blob is diffed against the file's blob now, the
  creation range goes through `Translate`, and a range that comes back whole took
  nothing.
- A delete is a real delete, not a state every count, ring and export would have
  to filter out.

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

The codes are in [docs/cli.md](docs/cli.md#exit-codes). `cmd/zen-review` maps
them through `cli.ExitCode`, and `cli.Quiet` keeps the matched status from being
printed as an error.

## Rendering traps

Each of these looks like working code and produces a broken frame. The first
group is why `tui/paint` and `tui/syntax` exist; the rest belong to the panes
above them.

- **Every styled cell ends in a full SGR reset**, which clears the background too. Set a row background per cell; wrapping a joined row tints only up to the first token.
- **A row with a background has to be padded to the full width**, or the tint ends where the code does. A row with no background needs no padding.
- **`Style.Width` wraps before it clips.** Clip explicitly first, or one long line of code becomes two rows.
- **Soft wrap and a line-number gutter cannot both be on.** A folded line puts every line under it out of step with its number. Clip instead, and measure at a width where something overflows.
- **A lexer carries state across lines.** Tokenise whole bodies, and the two sides of a diff separately. `syntax.Lines` takes a whole body; splitting a diff into two is the caller's job.
- **A raw tab is a variable number of cells.** One anywhere in a line puts every column after it out of step with the line above. `paint` expands them.
- **Chroma's terminal formatter is unusable here.** It renders its own escapes, resets included. `syntax` returns tokens so the caller keeps control of the row.
- **A Chroma style carries a background.** Taking it paints over the terminal's. Read the foreground only.
- **A viewport offset is a line, and a row is not.** With two-line rows, landing on a row can open the window on its second line. Round the offset up to the next item boundary, and size the viewport to a whole number of rows.
- **`viewport.EnsureVisible` is not a scroll-to-cursor.** It acts only once the line is already outside the window, then puts it on the top row. Move the offset by hand.
- **The shortest scroll onto the screen is the wrong one.** A key that lands on a block puts it at the top row, and leaves it alone when it already fits whole. A cursor moving a row at a time is the exception, since the window is what fell behind.
- **A key cannot aim at a block through the scroll offset.** A pane that fits its content has no offset, so a key reading its target off the top row always acts on the first block. Give the pane a cursor or do not give it the key.
- **A block that answers the line above it cannot go to the top row.** Bring a comment card's last row on screen and scroll no further up than the line it answers, which is the last of the lines it covers. A card taller than the window keeps the line rather than the card.
- **A block whose height moves with the width breaks a row index.** A resize renumbers every row after a wrapped card. Remember what the cursor was on, a comment id or a sequence a width cannot move, not which row.
- **A pane clips overflow silently.** A row wider than the pane loses its trailing columns mid-cell with no ellipsis, and a width test still passes. The row has to fit before the pane sees it.
- **A glyph is one cell only if lipgloss and the terminal agree.** The tree's Nerd Font folders and file marker are the one thing on screen that asks the terminal's font for anything. Measure a new one with `lipgloss.Width`, because a two-cell glyph shifts every row after it.
- **A stripped golden cannot see a colour.** The tree's cursor is a filled background and nothing else, so the frame is identical whether `j` moved or not. Anything said only in colour needs an assertion against the theme value beside the golden.
- **A newline in a body is a break, not a soft wrap.** `comp.Wrap` folds each line on its own and joins none of them, so a break typed into the box survives onto the card.
- **Nothing moves on a refresh until the key is pressed.** A formatter running on save would otherwise reshuffle the page while a comment is being written.
