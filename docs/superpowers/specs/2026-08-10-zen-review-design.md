# zen-review

A review engine with a TUI attached. Not a diff viewer with review features
bolted on.

The test for every decision below: could the CLI answer this question without
the TUI running? If no, the logic is in the wrong package.

## The problem

Agents write code faster than anyone can read it, and they rewrite code you
already read. Halfway through a session you cannot answer the only question
that matters: which of these machine-generated changes have I personally
inspected, and are they still the ones I inspected?

A diff viewer does not answer that. It shows you the current state and forgets
everything you did last time.

## Scope

Four projects. Each gets its own spec, its own repo, its own tickets. This
document covers the second one.

**zen-kit** (`github.com/zen-kit/zen-kit`) comes first because zen-review
depends on it. It holds the visual layer only: `theme`, `syntax`, and a
diff-line painter covering the gutter, marker, background, token painting, tab
expansion and width clipping. The painter is a pure function over a line. No
model, no state, no layout, no keys.

`theme` and `syntax` start as copies of zen-octo's, and zen-kit proves itself
here rather than there. zen-octo swaps its own copies out under ZNO-43, on its
own schedule. M0 has its own spec.

Behaviour is deliberately not shared. Each tool owns its renderer, layout,
folding and scroll. Keys are a convention written down in both `CLAUDE.md`
files, not shared code, so the two tools feel the same without either being
hostage to the other's release cycle.

**zen-review v0.1** (`github.com/zen-review/zen-review`) is this spec.

**The ZenTerm rip-out** deletes the native Swift viewer, roughly 5.3k lines,
and replaces it with a keybinding that opens zen-review in a pane. Its own
spec, after zen-review proves out. The open problem there is `DiffSendTarget`:
today a finished comment is pasted into a chosen pane with submit or queue
semantics, and a TUI running inside a pane has no equivalent yet.

**zen-octo composition** is v0.3 and stays hypothetical until the other three
land.

## Packages

```
cmd/zen-review/         cobra + fang, same shape as zen-octo

internal/
  git/                  plumbing only. merge-base, rev-list, diff,
                        snapshot tree, commit-tree, update-ref. Returns
                        bytes and structs, never opinions.
  diff/                 unified diff text -> File / Hunk / Line.
                        Knows nothing about review.
  review/               the engine. Sessions, generations, review state,
                        comments, remapping. Depends on git + diff + store.
  store/                SQLite and migrations. Nothing above it imports
                        database/sql.
  cli/                  the review subcommands. A thin shell over review/.
  tui/
    app/                root model, focus, keys
    tree/               file tree pane
    diffpane/           unified, side-by-side, preview
    compose/            comment composer
    comp/               shared widgets, status bar, toast
```

`tui/diffpane` and `tui/comp` import zen-kit for `theme`, `syntax` and the
line painter. Nothing below `tui/` imports zen-kit at all.

Two rules hold the shape. The TUI contains no review logic: every state change
is a call into `review/`, and `cli/` calls the same functions. And `diff/`
never imports `review/`, so a parsed diff stays a parsed diff.

Note that zen-octo has a package called `store` holding in-memory fetch state.
Same name, different job.

## The changeset

Bare `zen-review` opens one changeset: `merge-base(base, HEAD)` through the
working tree, untracked files included.

Whether the agent committed is an accident of its behaviour, not something a
reviewer should have to think about. A file committed and then edited again
shows as one set of hunks.

There is no `--staged` and no `--working-tree`. Both would be a second answer
to "what am I looking at".

### Sessions

A session is one repo plus one branch. Bare `zen-review` on branch `ZEN-301`
opens or resumes that session. Come back three days later, same branch, same
state.

- An explicit range (`zen-review HEAD~3..HEAD`) gets its own session, keyed by
  the range.
- Detached HEAD keys on the sha.

### Base resolution

In order:

1. `--base <ref>`, or a range argument.
2. Whatever the session already has. The base is set once per branch and
   sticks for the life of the session.
3. Auto-detect, as a proposal, never a silent guess.

Auto-detect prefers the remote-tracking default branch (`origin/HEAD`, usually
`origin/main`) over local `main`. In an agentic workflow local `main` is never
checked out and goes stale within a day.

Before accepting it, check whether any other local branch tip sits on HEAD's
first-parent chain above the detected base. If one does the branch is stacked,
and zen-review refuses rather than guessing, naming the candidates nearest
first. Plain ancestry is not enough: a branch merged with `--no-ff` is an
ancestor of HEAD and not of the base, and measuring from it gives a changeset
nobody asked for.

v0.1 refuses with one line saying to pass `--base`. The picker on `b` lands in
M5 and reads the same candidate list.

No `git config` key for this in v0.1. Session stickiness covers the same
ground and a second place to look is a second place to be wrong.

### When the base moves

The base is recomputed on every refresh.

- **Parent branch gains commits, no rebase.** `merge-base` stays at the old
  fork point. The changeset does not move.
- **Rebase onto the new parent.** The base jumps forward and commits get
  absorbed. Reviewed state is anchored to head-side file content, so a rebase
  that leaves the resulting file byte-identical leaves the blob shas matching
  and there is nothing to remap. Review survives a rebase intact.
- **Base changed by hand.** New generation, diff, remap. A file that leaves the
  changeset keeps its comments, marked out of scope, and they return if the
  base moves again.
- **Base force-pushed and the fork point is gone.** No clean answer. zen-review
  says so, keeps the comments, and asks for a new base. The old snapshot is
  still readable because the generation ref holds it.

## Generations

A generation is a snapshot of the whole changeset at one moment, written into
git as a real commit under `refs/zen-review/sessions/<id>`.

```
base commit (merge-base)
      |
      +-- gen 1   opened Tuesday 14:20, 11 files
      |
      +-- gen 2   reloaded Tuesday 16:05, 12 files
      |
      +-- gen 3   reloaded Wednesday 09:30, 12 files
```

Each generation's tree is the whole work tree, not only the changed files, so
`git diff <base> <generation>` is exactly the changeset. Everything written
anchors to the generation it was written against, so a comment always knows the
exact bytes it was about. The ref chain keeps those blobs alive through
`git gc`, and the review history is walkable with plain `git log`.

The first generation's parent is the base commit, which keeps the base
reachable even if the branch it came from is later rewritten. Every later one
hangs off the previous generation, plus the base again whenever the base moved.

A changeset over 5,000 files refuses rather than builds. It names the directory
holding the most of them and says to gitignore it or measure from a nearer
base. The count runs against the tree before the commit is written, so a
refusal leaves nothing behind.

Reloading is one operation: build generation N+1, `git diff` each file's blob
from N to N+1, translate everything through the result.

### Refresh

zen-review watches for changes and the status bar picks up a marker:
`3 files changed · s to reload`. Nothing moves until the key is pressed. Scroll
position, cursor and the focused hunk all stay put.

Auto-applying a refresh would move the page under the reader mid-sentence,
which every scroll lesson in zen-octo says not to do. A formatter running on
save would reshuffle the view while a comment is being written.

## Review state

**Stored as line ranges, not hunk indices.**

Storing "hunk 3 of session.go is reviewed" breaks the moment the agent inserts
twenty lines above it: hunk 3 is now different code wearing the same label.

Line ranges translate through a blob diff exactly. A range whose lines were
rewritten fails to translate and disappears. So:

- A hunk reads as reviewed when every line it introduces sits inside a
  surviving reviewed range.
- It returns to unreviewed the moment it does not.

One mechanism, no special cases, and `changed_after_review` falls out of it
rather than being tracked separately.

Deletion-only hunks have no head-side lines, so they anchor to base-side ranges
instead, which live in the base blob and translate through base changes.

Files carry three states in the tree, all derived from the hunks under them and
never set directly. The glyphs below and in the mockup are ASCII stand-ins; the
real ones come from the same codicon set zen-octo already uses.

```
o   unreviewed
v   reviewed
~   changed after review
```

## Comments

A comment anchors to a generation, a file, a side, and a line range. Scope is
one of `line`, `range`, `file`, or `session`.

```
open ---------> resolved      you closed it
  |
  +-----------> addressed     the agent claims it handled this
  |                  |
  |                  +------> resolved   you confirmed
  |
  +-----------> orphaned      remap lost the anchor
```

The agent can never reach `resolved` on its own. It marks `addressed` and the
comment sits in the queue looking like a claim, which is what it is.

An orphaned comment keeps its text and its last known location and shows in the
tree under the file it used to live in. A rewrite never silently swallows
something you said.

## Storage

`$(git rev-parse --git-common-dir)/zen-review/state.db`.

The common dir means a worktree and its parent checkout share one database.
Review a branch in a throwaway worktree, delete the worktree, the review
survives. Nothing lands in the working tree, so there is no gitignore entry and
nothing to commit by accident.

If `.git` is not writable, zen-review says so and exits at startup. It does not
run in a degraded mode where the review silently is not saved. One check covers
both the database and the ref writes, since they live in the same place.

**Driver: `modernc.org/sqlite`, pure Go, no cgo.** The cgo driver is faster and
smaller but puts a C toolchain in the path of every cross-compile and every CI
runner. The workload here is a few thousand rows.

WAL with a busy timeout, so two instances on one repo do not deadlock. They can
still disagree about what is on screen, and the reload indicator is what
surfaces that.

### Schema

```
sessions          id, repo_path, kind, branch, range_spec,
                  base_ref, summary, created_at, updated_at

generations       id, session_id, seq, base_sha, head_sha,
                  commit_sha, created_at

gen_files         generation_id, path, old_path, status,
                  base_blob, head_blob

reviewed_ranges   session_id, generation_id, path, side,
                  start_line, end_line, created_at

comments          id, session_id, generation_id, path, side,
                  start_line, end_line, scope, body, state,
                  anchor_blob, last_path, last_line,
                  created_at, updated_at
```

Migrations are numbered SQL files embedded with `embed.FS`, applied in a
transaction against `user_version`.

There is no `hunks` table. Hunks are derived from the diff at render time and
nothing about them persists. That falls out of storing review state as ranges,
and it is what keeps the schema this small.

`repo_path` is the resolved git common dir, so every worktree agrees on which
repo it is in.

## The CLI

```
zen-review                        open on the current branch
zen-review --base <ref>           set the base, sticks to the session
zen-review <range>                explicit range, its own session

zen-review status                 base, generation, counts
zen-review files                  per-file state
zen-review comments               filter by --state, --path
zen-review summary                the session-level note
zen-review export                 markdown, for pasting into a chat
zen-review address <id>           the agent claims it handled one
zen-review resolve <id>           you confirm
zen-review refresh                new generation, no TUI
```

Every subcommand takes `--json`. Human-readable output otherwise, because these
get run by hand while debugging.

`comments` takes `--exit-code`, which exits non-zero when the filter matches
anything. That turns a Claude Code Stop hook into three lines: if there are
open comments, the agent does not get to stop. It is the cheapest version of
the loop and it works before any MCP server exists.

`export` is the other bridge. Until the ZenTerm rip-out provides something
better, `zen-review export | pbcopy` is how a review reaches an agent, and it
is honest about being a stopgap.

## The TUI

Two panes and a status bar. Tree left at 32 columns, diff right. No tabs: a PR
has conversation and checks and commits, and a local review has one thing to
look at.

```
+------------------------+---------------------------------------------+
| CHANGES                | src/auth/session.go                          |
|                        |                                              |
| v src                  | @@ -42,7 +42,11 @@                           |
|   v auth               |  func validateSession(...) {                 |
|     ~ session.go   2   | -    return oldThing()                       |
|     o token.go         | +    if session.Expired() {                  |
|   > api                | +        return ErrExpired                   |
|                        | +    }                                       |
| v README.md            | +    return newThing()                       |
|                        |                                              |
| 3 / 11 reviewed        | HUNK 2/4 - UNREVIEWED - 1 COMMENT            |
+------------------------+---------------------------------------------+
| tab next  r reviewed  c comment  s reload  ?               3 changed |
+----------------------------------------------------------------------+
```

### Navigation

`tab` / `shift+tab` are the ring, and hunks are the stops. Same model as
zen-octo's conversation tab: stops keyed by identity rather than position, the
page scrolls to keep the focused one in sight, and the tree selection follows
so you always know where you are. A review is a sequence of hunks, so the ring
is the review.

One deliberate difference from zen-octo. The conversation opens unfocused,
because the reader came to read. zen-review opens focused on the first
unreviewed hunk, because you came to burn a review down. `r` advances to the
next unreviewed hunk after marking, so `r r r r` walks the whole thing.

### Keys

```
j k g G ctrl+u ctrl+d    movement
h l                      tree pane / diff pane
tab shift+tab            the ring: next / prev hunk
space                    fold / unfold
} {                      next / prev file
n N                      next / prev unreviewed hunk
] [                      next / prev comment

r                        mark hunk reviewed, advance to next unreviewed
R                        mark whole file reviewed
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
until the count reaches zero.

Divergences from zen-octo, all deliberate:

- `space` folds, replacing `o`. zen-octo adopts this too.
- `]` / `[` are tabs in zen-octo and comments here. zen-octo has tabs and
  zen-review never will.
- `r` is reply in zen-octo and mark-reviewed here. Neither tool has the other's
  concept.
- `v` is jump-to-diff in zen-octo, scoped to the conversation, and range
  selection here. They do not collide.

### View modes

Unified is the default. `|` toggles side-by-side. `p` toggles full-file
preview, which shows the whole file with changes highlighted in place, for when
three lines of context are not enough to judge a hunk. Preview and side-by-side
are orthogonal.

Word-level intra-line diff is out of v0.1.

## Failure

- **Not a repo, or no git on PATH.** One plain line, exit 1.
- **Empty changeset.** An empty state, not an error.
- **Binary files, symlinks, submodules, mode changes.** Listed in the tree as a
  one-line status. Can be marked reviewed. Cannot be commented on.
- **Large files.** A diff past a few thousand lines opens folded with its size
  shown. Syntax highlighting switches off above a threshold, because Chroma on
  a 20k-line file will stall a keystroke.
- **`.git` not writable.** Says so, exits at startup.

## Testing

`internal/review` is the whole game, and it is exactly the code that can be
silently wrong while the screen looks perfect. Remapping gets table tests over
real fixture repos, one case per way it goes bad:

```
line inserted above a reviewed hunk
reviewed line deleted outright
reviewed region rewritten wholesale
file renamed
file deleted
branch rebased, head content identical
base branch changed
base force-pushed, fork point gone
comment anchor survives
comment anchor orphans
deletion-only hunk, base-side anchor
```

`internal/git` runs against real temporary repos with no mocks, because the
thing under test is whether git is being called correctly.

`internal/diff` gets golden tests on unified diff text.

`cli` gets golden JSON.

The TUI gets render tests at widths where something actually overflows, which
is the only width that proves anything.

## Out of scope for v0.1

- MCP server
- Claude Code skill or hook, beyond what `--exit-code` enables
- Codex SDK integration
- GitHub, GitLab, PR discovery
- AI-generated review summaries
- Word-level intra-line diff
- Staging or committing from the viewer
- Any ZenTerm wire protocol

## Open items

- zen-kit's release cadence past v0.1.0, which M0 tags. It stays pre-1.0 while
  both consumers are ours, so a breaking change is a bump and two import fixes.
- Whether `zen-review export` output has a stable format worth documenting, or
  stays deliberately loose until an agent consumes it in anger.
