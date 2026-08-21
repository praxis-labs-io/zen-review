# Guide

zen-review answers one question a diff viewer cannot: which of these changes
have you inspected, and are they still the ones you inspected.

Agents write code faster than anyone can read it, and they rewrite code you have
already read. So a review here is not a mark on a hunk. It is a set of line
ranges carried through every new version of the changeset, and a range the
rewrite destroyed is a range that comes back unread.

The commands are in [the CLI reference](cli.md), the reader's keys in
[the keymap](keys.md), and how to install it in
[install](install.md).

## One changeset

Bare `zen-review` opens one changeset: the merge base with the base branch,
through the working tree, untracked files included. With no terminal to open the
reader on, it prints the changeset instead.

There is no `--staged` and no `--working-tree`. Both would be a second answer to
what am I looking at.

## The base

Nothing about the base stops you opening the tool. The only startup that fails is
a directory that is not a repository, because there is nothing there to open.

### How it is chosen

With nothing recorded and nothing passed, detection walks a ladder and always
reaches the bottom of it:

1. `origin/HEAD`, the remote's default branch.
2. The nearest local branch behind HEAD. Usually `main` or `master`, or the
   branch under this one when HEAD is stacked. Never the branch HEAD is on.
3. `HEAD`. The changeset is whatever has not been committed.
4. The empty tree, when there is no commit under HEAD. Every file reads as new.

A ref that stops resolving and one that loses its fork point are both a rung to
step off.

The walk for a branch underneath does not run on a default branch. A tip left
behind on that branch's own history is a branch nobody deleted, and measuring
from it would hide every commit since.

### When it is not what you asked for

A base nobody asked for wears a tag beside the ref, in the facts at the top of
the reader and on the CLI's own base line. The tag names what the base is, since
the row carrying it already says which ref it is.

| Tag | The base is |
| --- | --- |
| `no remote` | A local branch, there being no `origin/HEAD` |
| `uncommitted` | `HEAD`, so the changeset is your uncommitted work |
| `stacked` | The local branch this one sits on top of |
| `not <ref>` | Something else, because `<ref>` does not resolve or shares no history with HEAD |
| `<ref> gone` | Something else, because `origin/HEAD` names a branch that has been renamed or deleted |

A base measured from the empty tree wears none. Its name is the whole answer.

The tag is a standing fact and not a notice, so it does not take the status bar
and no key clears it.

### Changing it

`zen-review status --base <ref>` writes the base the session keeps, and `b` in
the reader does the same from a list. Nothing else takes `--base`: passing it to
a write would move the base and keep it moved, which is not what that call was
about.

A detected base is never written down, and it never clears the ref already
stored. It is a guess, so keeping it would hold after the repository moved past
it, and clearing what you chose would lose what to go back to. One mistyped
`--base` would cost the session its base and every range measured from it. The
stored ref stands, and the tag stands with it until the ref resolves again.

`b` lists every local branch, nearest first, plus two remote rows: the base the
session measures from, and whatever `origin/HEAD` names. Every other remote
shares a merge base with HEAD, so keeping them is hundreds of rows on a real
checkout. What the list leaves out, the box takes by name, so a tag, a sha,
`HEAD~5` and every other remote stay reachable by typing. A branch with nothing
behind HEAD is not offered, because measuring from it gives a review with every
commit gone.

## Sessions and generations

A session is one repo plus one branch, resumable days later.

A generation is a snapshot of the whole changeset, written into git as a real
commit under `refs/zen-review/sessions/<id>`. A comment always knows the exact
bytes it was about, and `git gc` cannot take them.

`zen-review refresh` builds one, and a bare `zen-review` builds one before it
opens the reader. Nothing is built when the changeset has not moved, and nothing
moves on screen until you press the key: a refresh landing under you mid-sentence
would reshuffle the page you are reading.

Two instances refreshing one session both build, and one of them writes. The
loser writes nothing and says so, so run it again.

## What survives a rewrite

Reviewed state is line ranges, never hunk indices. An agent inserting twenty
lines above hunk 3 leaves different code wearing the same label.

A refresh translates every stored range through one diff of the two generation
trees. A range that survives moves with its code. A range that fails to translate
disappears, and those lines read unreviewed again.

`changed after review` on a file is the refresh writing down what it took. It
stands until the file reads reviewed again, or until `unreview` settles it:
taking lines back by hand makes the coverage yours.

Deletion-only hunks have no head-side lines, so they anchor to base-side ranges.
A base-side range fails to translate when upstream rewrote the lines whose
removal you read. Moving the base further back only widens the scope, and leaves
every stored range translating cleanly.

## Comments

A comment is anchored to lines, to a hunk, or to the file itself, and it travels
with its code the way a reviewed range does. The anchor is more forgiving: a
comment on ten lines is about a region, so it clamps to whatever survived, where
a reviewed range would be cut into the pieces either side. A file comment is the
exception. It names the file, so it follows a rename and is lost when the bytes
move.

Four states:

| State | Means |
| --- | --- |
| `open` | Waiting on somebody. The only state that still moves |
| `addressed` | An agent says the work is done, and may have said how |
| `resolved` | You closed it |
| `orphaned` | The code it was about is gone |

`unresolved` is not a state a comment is ever in. It is the filter word for the
three that are not `resolved`.

A comment moves while it is open and stops the moment it is addressed, resolved
or orphaned. Only an open comment orphans: one already answered that loses its
anchor was acted on, and the rewrite that destroyed it is the acting.

### Answering and closing

`address` is the agent's verb. It says the work was done and it does not close
the comment, because the claim and the confirmation are different facts and a
queue letting one stand for the other is worth nothing. `resolve` is yours, and
it closes anything not already closed, an orphan included.

`address --body` carries the words that back the claim. They are optional: half a
queue is change requests where the diff is the answer, and the other half are
questions a diff does not answer at all. What the words are confirmed against is
the code they replaced, which the reader draws under the answer and
`comments --json` carries.

`edit` rewrites what the comment says and nothing else. It does not reach the
answer: one id naming two voices is how an agent's words get rewritten by a
reader fixing their own typo. The anchor does not move either, so a comment on
the wrong lines is a `delete` and a new one.

`delete` is a real delete. A comment nobody meant to write is a record of
nothing, and a state for it would have to be filtered out of every count and
every ring forever.

## Where the state is kept

`$(git rev-parse --git-common-dir)/zen-review/state.db`. A worktree and its
parent checkout share one database, so a throwaway worktree does not take the
review with it, and nothing lands in the working tree.

It is SQLite in WAL with a busy timeout, so two instances on one repo do not
deadlock. A `.git` that is not writable is a startup error rather than a degraded
mode where the review is not saved.
