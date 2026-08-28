# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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
turns a diff line into a row. They hold no model, no state, no layout and no
keys, and every exported function is pure: same arguments, same string.

Pushing any of those in is how they stop being the visual layer and start being
a second renderer. Folding, scroll, side-by-side layout, hunk grouping, the
two-sided tokenise split and review state belong to `tui/diffpane`.

`go run ./cmd/paintdemo` is how a rendering change is judged. It paints a canned
diff at a width where a row overflows: a hunk header, all three line kinds, a
tab-indented line, a clipped row and a `Fill` row, so a theme change shows
everything it broke in one screen. It runs the real derivation, so a light
terminal and a dark one show different screens. A golden file only holds it
still.

### The theme

There is one theme and it is derived rather than written down. What the code has
to keep true:

- The hues are ANSI slots, so they are whatever the reader's terminal maps them
  to. That is the whole feature, and it survives only as long as a slot reaches
  the terminal as a slot: flattened to RGB it stops following the palette, which
  is why a test type-asserts them.
- **A slot may be painted and must never be blended.** `RGBA()` on a slot is its
  canonical value and never what the terminal mapped it to, so blending one
  washes a row in xterm's dark system red rather than in the red beside it in
  the marker column. This is why slots 1 and 2 are asked for by name: the diff
  tints are blends, and the tints are the product here. zen-octo writes the same
  query off as not worth it for a tint, which is right for a PR reader where the
  tints are decoration. The two OSC 4 requests ride in the write and the read
  that already run, so the cost is a longer query string and nothing else.
- The greys are blends off the reported background for the same reason in
  reverse. They are structural and only have to stay legible, which a slot
  cannot promise and a blend off a known background is by construction. Ratios
  travel toward the reported foreground, which serves a light terminal and a
  dark one without a second table.
- **A filled row is placed at a luma distance, not at a ratio.** A ratio toward
  a hue the reader chose is not a fixed step: the same fraction that clears one
  palette's green leaves the row flat against another's. `lift` solves for the
  distance instead, held between a floor and a ceiling, because the two ends
  fail in opposite directions — a pale green reaches the distance in a few
  percent and a few percent of a colour reads grey, while a green sitting at the
  background's own weight never reaches it at all and keeps its lean instead.
- A selection is a lift and not a colour, so it travels neutrally. Along the
  shade axis it took the foreground's tint, which on a palette with a warm or a
  violet foreground is a colour the reader never chose.
- The foreground is only usable if it is on the far side of the luma midpoint
  from the background **and** far enough from it. Far and opposite are two
  different tests, and a pair failing either is no direction to travel in.
- `Text` is `lipgloss.NoColor{}`, which writes no sequence at all, so it tracks
  a terminal the reader recolours mid-session. Its `RGBA()` is black, so
  anything spelling a colour out for a third party has to special-case it.
- `Background` is nil and stays nil. The background every shade was derived
  against is the terminal's own, so painting it would change nothing a reader
  can see and would cost a translucent terminal its translucency.
- Nothing answered is not the same as a guess. The three surfaces go nil rather
  than take a slot: slot 0 is the background on a great many dark palettes, so a
  selection painted in it is invisible exactly where it was needed. Borders are
  drawn runes rather than fills and still take one.
- The query runs in `app.Run`, before Bubble Tea takes the tty, and it drains to
  the device attributes. Stopping at the last colour leaves them buffered, and
  the terminal echoes them the moment raw mode ends. The reader is a cancellable
  one, or the timeout leaves a goroutine parked on the tty eating the first key.
- Chroma has no ANSI style and `chroma.Colour` is packed RGB, so code is the one
  thing that cannot follow the palette. `github-dark` and `github` are paired
  against the background instead.

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
  golden/      the golden-file compare. Test-only, and owns the -update flag.
```

The boundaries are in `.claude/rules/code-quality.md` and breaking one is a review-stopper. The short version: the CLI has to be able to answer any question the TUI can.

User-facing behaviour lives in `docs/`: [the guide](docs/guide.md) for the base, sessions, generations and comments, and [the CLI reference](docs/cli.md) for every command and flag. What follows here is what the code has to keep true, not what a reader sees. Change one and the other says the wrong thing.

`Session.Files` and `Derive` both hand the files back in the order a file tree
reads: directories above the files beside them, by byte within each group. Git's
order is the order it walked the index in, and one ordering from the engine is
what keeps the printed table and the tree pane from disagreeing about what is
first. Nothing above `review` sorts.

### The base

How a base is chosen, what the fallback tags mean and how a reader changes one
is in [docs/guide.md](docs/guide.md). What the code has to keep true:

- Detection always reaches the bottom of the ladder. The only startup that fails
  is a directory that is not a repository. A refusal a reader cannot act on from
  inside the tool sends them to the shell to guess.
- The rung above bounds the candidate walk. On a trunk called anything but `main`
  or `master` there is no rung above HEAD to bound it with, so it walks the whole
  first-parent chain instead, and on a default branch it does not run at all: a
  tip left behind on that branch's own history would hide every commit since.
- `Candidates` is every local branch, not a first-parent walk, which hid a base
  that had been merged in. Two remote rows earn their place, the session's base
  and `origin/HEAD`; every other remote shares a merge base with HEAD and would
  be hundreds of rows. Nothing with nothing behind HEAD is offered.
- A fallback writes no ref and never clears the one stored. It is a guess, and
  one mistyped `--base` would otherwise cost the session its base and every range
  measured from it. The tag on `Base.Fallback` stands until the ref resolves.
- `SetBase` is the one call. `--base` and the raw string typed into the `b` box
  both go through it, so a tag, a sha and `HEAD~5` reach the same place a flag
  does.
- The tag is a standing fact and not a notice, so it takes no status bar and no
  key clears it. The reason sits right of the ref, so a narrow pane's clip eats
  the reason and leaves the ref whole.

### Sessions and generations

What a session and a generation are, what survives a rewrite and how a comment
travels is in [docs/guide.md](docs/guide.md). What the code has to keep true:

- A refresh moves the ref before it writes the row, and swaps against the ref's
  own previous value rather than the last `commit_sha` stored. The other order
  lets two instances both write rows and leaves the ref pointing at one of them.
- Every write naming a generation asserts from inside its own transaction that
  the generation is still the latest, the refresh included. A mark, a comment or
  a state change landing while a refresh is in flight moves forward with it or is
  refused, never accepted and lost.
- All the git work is done before that transaction opens, which it can be because
  nothing the translation needs is a row.
- The two gates are on different things, so a row that does not land takes its
  commit back off the ref. The loser leaves nothing rather than a generation
  commit no row names, and that runs past a cancel: a reader quitting mid-refresh
  is commoner than two instances racing.
- `edit` and `delete` name no generation. Words are true at every generation, and
  a delete leaves no anchor to go stale.
- Reviewed state is line ranges, never hunk indices. Deletion-only hunks have no
  head-side lines and anchor to base-side ranges.
- `changed after review` is on `gen_files.cut` and cannot be read back off the
  coverage: a range the translation cut and a range somebody unmarked leave the
  same coverage behind, and only the refresh ran the translation. It follows a
  rename through the same diff the ranges do.
- The response is on `comments.response` and lands in the same swap as the state.
  Every other transition passes no response rather than reading one and writing
  it back, which would clobber a write that landed in between. A refresh carries
  neither: they are words, not an anchor.
- `--body` is the words of the thing the command names, on every command that
  takes one, so an invocation reused against the wrong verb is a refusal rather
  than a write to the wrong half. `edit` does not reach the response: the tool
  has no identity to enforce one voice with, so it refuses to make the wrong
  write easy.
- A frozen comment's row stays at the generation it stopped at and records where
  the anchor was, so nothing has to know which generation it is pinned to in
  order to say where it lived. Only an open comment orphans.
- A comment's anchor translation is more forgiving than a reviewed range's: it
  clamps to what survived, where a range is cut into the pieces either side. A
  file comment is the exception and takes the range rule.
- `anchor_blob`, `created_generation_id`, `created_start_line` and
  `created_end_line` are written once at creation and never moved. A comment that
  travelled before it was answered would otherwise slice its own blob by lines it
  never had. One diff per pair of blobs, so a file's comments cost one call
  between them rather than one each.

### Storage

`$(git rev-parse --git-common-dir)/zen-review/state.db`, so a worktree and its
parent checkout share one database. Nothing lands in the working tree.

`modernc.org/sqlite`, pure Go: the cgo driver is faster but puts a C toolchain in
the path of every cross-compile and CI runner, for a few thousand rows. WAL with
a busy timeout so two instances on one repo do not deadlock.

`.git` not writable is a startup error, not a degraded mode where the review
silently is not saved.

## Keys

The keymap is shared with zen-octo by convention, so the two tools feel the same
without either being hostage to the other's release cycle. Every key and what it
does is in [docs/keys.md](docs/keys.md), and zen-octo keeps its own copy. What
follows is what the code has to keep true.

- The heading pin follows the window and not the cursor, because a heading names
  the lines under it. The pin owns the top line, so the cursor never sits there:
  a key that would put it on that row opens the window one higher instead.
  Standing the pin down for the cursor was the first answer and it cost the
  heading on every paging key.
- The cursor bar sits in a leading cell every row already holds open, so nothing
  shifts as the cursor passes. The fill is shared with a selection and the bar is
  not, which is what says where the next key moves from inside one.
- A comment card is one stop for the cursor, not one per row.
- One cursor in side-by-side, never one per column. Two could point at unrelated
  lines and a side-switch would throw the window.
- The mode `|` sets lasts the run and nothing stores it. A default belongs with
  the reader's other preferences, not in the session the review is kept in.
- The composer takes every key while it is up, `ctrl+c` excepted: raw mode sends
  no interrupt and a box that ate it would be the one place in the program with
  no way out but `esc`. A paste arrives as its own message rather than as keys,
  so the root routes what is not a key press into it too.
- The cursor in the composer is the terminal's own, placed by the root through
  the view. A drawn one paints a colour of ours over the character it covers, and
  the reader set their cursor up already.
- The box comes down when the write lands, not when the key is pressed. The one
  exception is the write that committed and could not be read back: that box
  comes down, because saving it again writes a second comment, and it is why the
  Source names that failure rather than reporting it like any other.
- The status bar clears on the next press inside a box the way it does outside
  one.
- `x`, `e`, `D` and `>` are named in the card's own footer and never in the help
  overlay, which draws to the frame's last line at sixteen rows with no row to
  give. `esc` is named on the status bar while a selection is up.
- `ResolveComment` refuses a comment already resolved, so a settled card neither
  offers `x` nor takes it. Freezing one twice re-records an anchor that stopped
  moving a generation ago.
- Neither the response box nor its rail ever lights. The elbow is always `╰─` and
  never a tee, there being one response, and a pane with no room for a second
  border draws the card whole and drops the box rather than shrinking both.
- The replaced block is the translation the remap runs, not the two sides read at
  the same numbers. The blob the comment was written against is diffed against
  the file's blob now, the creation range goes through `Translate`, and a range
  that comes back whole took nothing.
- A delete is a real delete rather than a state, which would have to be filtered
  out of every count, every ring and every export forever.

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

Each of these looks like working code and produces a broken frame. The first
group is why `tui/paint` and `tui/syntax` exist rather than every pane rolling
its own; the rest belong to the panes above them.

- **Every styled cell ends in a full SGR reset**, which clears the background along with the foreground. A row background has to be set per cell; wrapping a joined row paints only the first one, and the tint stops at the first token.
- **A row with a background has to be padded to the full width.** Otherwise the tint ends where the code does and the block reads as ragged. A row with no background needs no padding, which is the only reason a context line is cheaper.
- **`Style.Width` wraps before it clips.** Truncating to a column width means clipping explicitly first, or one long line of code becomes two rows.
- **Soft wrap and a line-number gutter cannot both be on.** One long line folds onto a second row, and every line under it is then one further out of step with the number beside it. Clip instead, and only ever measure at a width where something overflows.
- **A lexer carries state across lines.** Highlighting line by line comes apart on the first multi-line string. Tokenise the whole file, and tokenise the two sides of a diff separately, or the lexer reads a file holding both halves of every change. `syntax.Lines` takes a whole body for this reason; splitting a diff into two bodies is the caller's job.
- **A raw tab is a variable number of cells.** One anywhere in a line puts every column after it out of step with the line above. `paint` expands them.
- **Chroma's terminal formatter is unusable here.** It renders its own escapes, resets included. `syntax` returns tokens so the caller keeps control of the row.
- **A Chroma style carries a background.** Taking it paints over the terminal's, which is what keeps a transparent one transparent. Read the foreground only.
- **A viewport offset is a line, and a row is not.** Once rows are two lines, scroll arithmetic that lands on the row it wants opens the window on that row's second line with its title cut off above. Round the offset up to the next item boundary, and size the viewport to a whole number of rows or the end-of-list clamp lands back between two lines.
- **`viewport.EnsureVisible` is not a scroll-to-cursor.** It acts only once the line is already outside the window, then puts it on the top row. Move the offset by hand.
- **The shortest scroll onto the screen is the wrong one.** A key that lands on a block is taking the reader somewhere, so put the block at the top row, and leave it alone when it already fits on screen whole. A cursor moving a row at a time is the exception: the reader is already looking at the row, and the window is what fell behind.
- **A key cannot aim at a block through the scroll offset.** A pane that fits its content has no offset to move, so a key reading its target off the top row acts on the first block whatever the reader does. Give the pane a cursor or do not give it the key.
- **A block that answers the line above it cannot go to the top row.** A comment hangs under the code it was written against, so topping the card scrolls that code away. Bring its last row on screen and scroll no further up than the line it answers. That is two clamps and no magic number, and it degrades honestly: a card taller than the window keeps the line rather than the card. The line it answers is the last of the lines it covers and not the first, because a comment on a whole hunk anchors at the top of the hunk, and a scroll holding out for that leaves the card off the bottom of any window shallower than the hunk. A box being typed in is where that costs the most: off the window it still holds every key that would scroll to it.
- **A block whose height moves with the width breaks a row index.** A card's body wraps, so a resize renumbers every row after it and a stored cursor lands on something else. The pane remembers what the cursor was on, not which row: a comment id, or a sequence over the rows a width cannot move.
- **A pane clips overflow silently.** A row wider than the pane loses its trailing columns mid-cell with no ellipsis, and a width test still passes. The row has to fit before the pane sees it.
- **A glyph is only one cell if lipgloss and the terminal agree it is.** The tree's folders and its file marker are Nerd Font, which is the one thing on screen that asks anything of the terminal's font. Measure a new one with `lipgloss.Width` before using it: a two-cell glyph puts every row after it out of step, where a font missing a one-cell glyph only draws a box.
- **A stripped golden cannot see a colour.** The tree's cursor is a filled background and nothing else, so the frame is identical whether `j` moved or not. Anything said only in colour needs an assertion against the theme value beside the golden.
- **A newline in a body is a break and not a soft wrap.** `comp.Wrap` folds each line on its own and joins none of them. It used to join a paragraph before folding, so a body hard-wrapped elsewhere reflowed to the width in hand, and the price was that every break somebody typed into the box was drawn away on the card that came back.
- **Nothing moves on a refresh until the key is pressed.** A formatter running on save would otherwise reshuffle the page while a comment is being written.
