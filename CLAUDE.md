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
everything it broke in one screen. A golden file only holds it still.

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

The keymap is shared with zen-octo by convention, written down in both
`CLAUDE.md` files rather than in shared code, so the two tools feel the same
without either being hostage to the other's release cycle.

```
j k g G                  movement
zz zt zb                 put the cursor mid, top or bottom of the pane
ctrl+u ctrl+d            page the diff, from either pane
h l                      tree pane / diff pane, and the columns of a split one
1 2                      the two panes, by the badge in the pane's border
} {                      the ring: next / prev hunk
space                    fold / unfold: a tree directory, a comment card
tab shift+tab            next / prev file
n N                      next / prev unreviewed hunk
] [                      next / prev unresolved comment

r                        mark hunk reviewed, advance to next unreviewed
R                        mark whole file reviewed
u U                      take either back
c                        comment on the selection, the row, the hunk or the file
v esc                    range selection for c, j/k extend. esc or v cancels
C                        session summary note
x                        resolve a comment
e D                      rewrite a comment, delete one
>                        the rest of the code a response replaced
enter                    tree: open file. comment: jump to its line
ctrl+s esc               in the composer: save, discard

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

`|` puts the two sides in two columns. A run of removals pairs against the run of
additions after it, one row each, and the shorter side draws a blank rather than
shifting up, which would put the two columns out of step for the rest of the
file. Context takes both. One gutter serves both halves, so the rule between them
sits centre.

It is what the reader asked for and not always what they get. Under a minimum of
source per column the two halves clip away more than they show, so the key
refuses and the bar says how many columns short the pane is. A terminal shrinking
under a split pane falls back to unified and keeps the answer, so widening brings
it back without a second press.

The cursor is in one column, and only that column lights. Lighting the row across
both leaves a reader on a rewritten line with nothing saying which side the next
key takes, and the rule between them stays dark or the lit block runs a cell past
its column.

`h` and `l` step into it. They already mean move left and right between things on
screen, and a split pane is one more thing to step to: `h` from the head column
goes to the base, and again to the tree; `l` walks back. The pane keeps the column
it was left in, so returning to it moves nothing, and it starts on the head. A
pane drawing one column has nowhere to step and gives the focus up on the first
press, as it always did.

`1` and `2` stay a jump, because a badge drawn in a border names the frame it is
drawn in, and there are two frames whatever the mode.

One cursor and not one per column. The rows are paired, so the two halves of a row
are the same change and stepping between them is the comparison being made. Two
cursors could point at unrelated lines and a side-switch would throw the window,
which is vimdiff's shape and vimdiff has two real buffers to hang it on.

`j` and `k` skip a row the focused column has no line on. Walking the head column
of a deletion-only block therefore steps over it, which is honest: there is no head
there to read, and `h` is where those lines live. A run reaching the end of the
file turns the cursor back rather than stranding it on a blank.

The same step off the end of a hunk turns back inside it. A column step is one
row across and not a move between hunks, and `r` takes the hunk the cursor is in,
so carrying it over the boundary would mark the hunk the reader just left.

A comment and a selection scope to the focused column, which is the whole point:
a removal with its replacement beside it can be commented on alone. A mark does
not, `r` taking the hunk and writing every side at once as it always has.

The mode lasts the run and nothing stores it. A default belongs with the reader's
other preferences, not in the session the review is kept in.

`v` scopes a comment and nothing else. The unit of review is the hunk, so `r`
over a selection marks the hunk and advances, the same press it always was. The
engine can mark lines and `review --lines` reaches it, but from the reader it
buys nothing: `n` stops on a partial hunk as well as an unread one, so a
part-marked hunk is handed back whole anyway, and re-reading four lines is
cheaper than a badge nobody can read the extent of.

A selection may cross a hunk, so `j` and `ctrl+d` keep working as they do
everywhere else. Only code fills: a heading, the blank between two hunks and a
comment card are not lines anything is written against.

`esc` is named on the status bar while a selection is up and nowhere else, the
way `x` is named in a card's own footer. It reaches one state of the program,
and that is where a reader looks for it. It answers from either pane, because
the selection stays lit while the reader walks the tree.

`c` scopes to what is under the cursor and takes the first of these it finds: a
selection, the code row the cursor is on, the hunk it is in, the file. A
selection beats the focus the way `esc` does, and the tree focused with nothing
selected is the file itself, because pointing at a file is what the tree is.

A comment anchors to the head wherever the lines it covers have one, and to the
base only when they have none, which is a selection of removals. The head is the
code the next agent rewrites; a mark writes every side at once and a comment
cannot. The box says which side it took, because base numbers read as head ones.

Side-by-side is where that rule steps aside: a row there names one column, the one
the cursor is in, so the reader picks rather than the rule. Unified has no column
to pick, so it keeps the rule.

A hunk is the block a paragraph motion moves by, so `}` and `{` step it. Vim's
own diff mode says `]c` and `[c`, and the bracket pair is spoken for. Nothing in
vim moves a whole file in one key, so `tab` is the TUI answer rather than the
editor one; the tree does the same job by hand.

`]` and `[` walk what is unresolved, the way `n` walks what is unread. A ring
that stepped through settled work is a ring nobody holds down. A resolved card
still draws, folded, and `j` still reaches it.

A comment draws as a bordered card indented to the code column, hanging under
the last line it is about. It is one stop for the cursor rather than one per
row: `j` steps onto it and the next `j` clears the whole thing.

State is a badge in its top border and focus is the border's colour, because
four states and one focus cannot share a channel. `◇` open, `◈` addressed, `◆`
resolved, `✕` orphaned, each beside its own word: the glyph is the glance and
the word is what scans down a column.

A diamond, where a hunk's badge is a circle. The two ladders sit in one column
and mean different things, so they cannot share a shape, and their colours run
opposite ways: a hunk is accent once it is done, a comment is accent while it
still wants you. Orphaned leaves the family, being a loss and not a stage.

A response draws as its own box hanging off the card on a rail, two columns in
and two columns narrower, zen-octo's reply exactly. A box says the words below it
are somebody else's; a change of weight inside one border says only that whoever
was talking trailed off. It carries no footer, having no key of its own.

Under the words it carries the code they replaced: the lines the comment was
written against, where the changeset has since taken them. The words say the work
was done and this is what a reader confirms them against, which is the same job
`response` does one level up. The before only, because the after is the diff the
card hangs in, so the two read as a pair without either being drawn twice.

It is painted as the removals it is, the diff's own `−` and `RemovedBackground`
run to the edge of the box. Muted prose was the first answer and it read as a
trailing fragment of the response rather than as code.

Whether the lines went is the translation the remap runs, not the two sides read
at the same numbers. An anchor stops moving when the comment is answered, so a
positional read calls every line inserted above it a rewrite, and the block then
shows code that is still there word for word one row down. The blob the comment
was written against is diffed against the file's blob now, the creation range
goes through `Translate`, and a range that comes back whole took nothing.

It comes out of the same act as the words, so a bare `address` grows the box to
hold it alone. Nothing is asserted before the agent answers, so an open comment
has none however far the code moved under it. A bare `address` the reader then
resolves loses its block: nothing on the row records that it passed through
addressed, and a resolved card folds by default anyway.

`anchor_blob` is the bytes and `created_start_line` is where in them, written
together at creation and left alone by every refresh that moves the anchor. A
comment that travelled before it was answered would otherwise slice its own blob
by lines it never had. One diff per pair of blobs, so a file's comments cost one
call between them rather than one each.

A file comment gets none. It names the file rather than any region of it, and the
whole of the old file is not a block. Neither does a hunk somebody marked read and
an agent then rewrote: `r` asserts nothing about the code, so there is no claim to
confirm, and `changed after review` already says it moved.

Three lines, then `>` for the rest. It is the card's size and not the response's,
which takes no keys at all. The key is named in the card's own footer and not in
the help overlay, the way `esc` is named on the status bar: it does something only
on a card holding a block, and the overlay draws to the frame's last line at
sixteen rows with no row to give.

Neither the box nor the rail ever lights. A lit border says a key reaches here,
and nothing reaches a response: no cursor stops on it, and `x`, `e`, `D` and `>`
all act on the card. Lighting it with the card would promise a stop that never
arrives. The elbow is always `╰─` and never a tee, there being one response. A
pane with no room for a second border draws the card whole and drops the box
rather than shrinking both.

A settled card folds to one row and keeps its box, and takes its response and the
block with it. Without the box it is a line of grey text in a column of diff,
which is what the diff's own notes look like, and a box still hanging off one row
says the card is open. Its footer names the direction the key goes rather than the
state it is in.

`x` is named in the lit card's own footer rather than on the status bar, because
it reaches one row on the screen and that row is where a reader looks for it. A
settled card neither offers it nor takes it: `ResolveComment` refuses a comment
already resolved, and is right to, because freezing one twice re-records an
anchor that stopped moving a generation ago. So the key has nothing to do there,
the way it has nothing to do on a row with no card. An orphan is offered it,
that being the only thing left anyone can do with a comment whose code is gone.

`e` and `D` are named there too, and nowhere else. Both reach a card in any
state: a typo in a resolved comment is still a typo, and one nobody meant to
write is a record of nothing. `D` acts at once, the capital doing the whole of
the thing the way `R` and `U` do, and a card narrow enough to drop hints drops
these two first, the overlay being where the rest of the keymap lives anyway.

An edit is the body alone, the response being the agent's words and no business
of the reader's verb. The anchor never moves either, so a comment on the wrong lines
is a delete and a new one rather than a rewrite, which would be a second remap
path with none of the translation rules behind it. The box `e` opens is the card
itself: same border, same indent, same place, holding what it said. A delete is a
real delete rather than a state, which would have to be filtered out of every
count, every ring and every export forever.

An empty body is a discard from `c`, because nothing was typed. From `e` it
writes nothing and the bar says so: wiping a box is not saving a comment, and it
is not deleting one either, which is a key of its own rather than a second
meaning for the save key.

The bar clears on the next press inside a box the way it does outside one. Every
other line there is one press long, and one that stayed up would be read as an
answer to a key pressed since.

The composer takes every key while it is up, `q` and `?` included. A note lost
to the letter `q` cannot be taken back, and one key let out is one more thing to
hold in mind while typing. `ctrl+c` is the exception, because raw mode sends no
interrupt and a box that ate it would be the one place in the program with no
way out but `esc`. `enter` is a newline in prose, so `ctrl+s` saves and `esc`
discards; nothing else on screen says so, which is why the box carries both in
its bottom border.

The box grows with what is typed into it rather than scrolling, because a box
that scrolls hides the sentence still being written. It stops at what the pane
can draw around it: its two borders, the line it hangs under, and the heading
pinned over that. Past there it scrolls, having nowhere left to grow.

The cursor in it is the terminal's own, placed by the root through the view. A
drawn one is a block we paint in a colour of ours over the character it covers,
and the reader set their cursor up already.

The box comes down when the write lands, not when the key is pressed. A save
that failed leaves it up holding the words, and so does one the reader typed
past: the only thing a local transaction can cost is what was typed into it. A
paste arrives as its own message rather than as keys, so the root routes what is
not a key press into it too.

`c` types in place and `C` types over the frame, because a comment has a line
and a session note has none. The box `c` opens is the card it is about to
become: same border, same indent, hanging under the same line, so the code it is
about is still on screen above it and the file flows below. It carries the two
keys in its own footer the way a card carries `x`, and the status bar names them
and nothing else, `?` and `q` being letters while a body is being typed.

A box nobody can see still holds every key, so where the pane has no room to
draw one it goes over the frame instead, which is where `C`'s is drawn. A
terminal shrinking under a reader mid-sentence carries the words across with it.
`c` is refused only while a reload is in git, which would land under an open box
and move the lines it was scoped to.

A failed write keeps the box and the words, except the one that committed and
could not be read back. That box comes down, because what it holds is written
and saving it again writes a second comment. It is the only write here that
cannot be retried, which is why the Source names it rather than reporting a
failure like any other.

The label says only what the card's own position cannot. A card under its line
needs no line number, because the gutter beside it has one. A range says the run
it covers, a file comment says so, and a comment the diff has no line for says
where it used to point and goes to the foot of the file. Dropping that last one
would lose the only record of what was asked.

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

The row the cursor is on carries a bar in its leading cell, in accent. The tint
alone is a change of shade a reader loses on a page of them, and the cell is one
every row already holds open, so nothing shifts as the cursor passes. The fill is
shared with a selection and the bar is not, which is what says where the next key
moves from inside one.

Side-by-side puts it at the focused column's own edge rather than the pane's, so
it marks the column as well as the row. A heading takes it at the pane edge,
belonging to neither column. A comment card does not: its border already lights,
and a second mark on one block says nothing the first did not.

`j` and `k` move a row cursor rather than the window, and a hunk heading pins to
the top row once it scrolls off, so the lines up there are never unlabelled. The
pin follows the window and not the cursor, because a heading names the lines
under it, and the next hunk's own heading pushes it out.

The pin owns the top line, so the cursor never sits there: a key that would put
it on that row opens the window one higher instead, and it lands on the second
with its heading above it. Standing the pin down for the cursor was the first
answer and it cost the heading on every paging key.

`ctrl+u` and `ctrl+d` park the cursor mid-window and let the file run past it,
rather than carrying it at whatever row it happened to be on. The ends are the
exception and the only place it moves on screen: the window stops at the first
row and the last, and the cursor goes on alone to the end of the file. Vim
carries the cursor instead, which reads fine in an editor you are typing in and
badly in a pane you are only reading.

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
