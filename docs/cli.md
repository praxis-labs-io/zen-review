# CLI reference

Every question the reader can answer, the CLI answers with no terminal attached.
`zen-review <command> --help` prints the short form of this page.

The concepts behind it are in [the guide](guide.md), and the reader's keys in
[the keymap](keys.md).

```sh
zen-review [--base <ref>] [--json]
zen-review <command> [flags]
```

A bare invocation builds a generation and opens the reader, or prints the
changeset when there is no terminal to open it on.

## Global flags

| Flag | What it does |
| --- | --- |
| `--base <ref>` | The ref to measure the changeset from. Detected when unset, and kept by the session until another is passed. Reads take it; writes refuse it, because the move would stick and it would recompute the changeset the write then anchors into. `zen-review status --base <ref>` is where it changes |
| `--json` | Write the answer as JSON. `export` writes markdown and refuses it |
| `--version` | Print the version |
| `-h, --help` | Print help for the command |

## Exit codes

| Code | Means |
| --- | --- |
| 0 | Answered, and nothing matched |
| 1 | Answered, and the filter matched. Only `comments --exit-code` reaches it |
| 2 | Failed |

`diff` and `grep` split answering from failing the same way. A hook reading only
non-zero cannot tell an open comment from a broken git call.

## Naming part of a file

`review`, `unreview` and `comment` all take a path and one of three ways of
naming what in it to act on. Passing two is refused rather than ranked, because a
call passing two meant one and the tool cannot tell which.

| Flag | Names |
| --- | --- |
| `--hunk <line>` | The hunk that introduces this line first, which is what `files` prints beside it |
| `--lines <A-B>` | A run of lines, or a single `A` |
| `--all` / `--file` | The file itself. `--all` on a mark, `--file` on a comment |
| `--side head\|base` | Which blob the lines are measured on. Defaults to `head`, and is refused under `--all` and `--file`, which take every side the file has |

`review --lines` clips what you named to the lines the file's hunks hold. A range
reaching past every anchor records nothing you can see, and it does not stay
harmless: it carries into each new generation and the first hunk to land inside
it reads as read with nobody having read it.

`comment --lines` takes the range as typed and refuses lines no hunk holds. A
comment is what somebody said, and moving it onto lines they did not pick says
something else. A range spanning two hunks stays one comment about both.

## Reading

### `zen-review status`

Report the session without building a generation. It writes nothing and does not
move the session ref, but it does read the working tree to answer whether
anything has moved since the last generation was built.

This is where `--base` changes.

```sh
zen-review status
zen-review status --base origin/main
```

### `zen-review files`

Report the changeset with what has been reviewed on it: a row per file, and a row
per hunk under it named by the side and line `review` takes.

It builds nothing and reads the generation already recorded, so run `refresh`
first.

### `zen-review comments`

List the comments written against this session, live and frozen, by file and then
down the file.

| Flag | What it does |
| --- | --- |
| `--state <state>` | Only comments in this state: `open`, `addressed`, `resolved`, `orphaned`, or `unresolved` for every one of those but `resolved` |
| `--path <path>` | Only comments recorded under this path, which on the base side of a rename is the old name |
| `--exit-code` | Leave a status of 1 when the filter matched anything |

The listing is written either way. `--exit-code` sets the status after it, never
instead of it.

```sh
zen-review comments --state unresolved
zen-review comments --state open --exit-code   # 1 while anything is open
```

### `zen-review export`

Write the review as markdown, for pasting somewhere else: the note, how much has
been read, and every comment still waiting on somebody, grouped by file. Open,
addressed and orphaned, never resolved.

Locations and bodies, never the code they are about, which whoever reads the
paste has in front of them anyway.

## Writing

### `zen-review refresh`

Build a generation from the working tree. Nothing is built when the changeset has
not moved.

Two instances refreshing one session both build and one of them writes. The loser
writes nothing and says so, so run it again.

### `zen-review review <path>` and `zen-review unreview <path>`

Record part of a file as reviewed, or take it back out.

Marking a hunk marks every side it touches, because the lines it removes are not
lines it has. `unreview` cuts a recorded range where the two overlap rather than
dropping it whole, and it settles any `changed after review` the last refresh
recorded against the file.

| Flag | What it does |
| --- | --- |
| `--hunk <line>`, `--lines <A-B>`, `--all` | What to mark. See [naming part of a file](#naming-part-of-a-file) |
| `--side head\|base` | Which blob the lines are measured on |
| `--generation <n>` | Refuse unless this is the generation the mark lands on |

```sh
zen-review review internal/review/base.go --hunk 42
zen-review review internal/review/base.go --all
zen-review unreview internal/review/base.go --lines 40-58
```

### `zen-review comment <path>`

Write a comment against part of the changeset.

| Flag | What it does |
| --- | --- |
| `--hunk <line>`, `--lines <A-B>`, `--file` | What to comment on. See [naming part of a file](#naming-part-of-a-file) |
| `--side head\|base` | Which blob the lines are measured on |
| `--body <text>` | What the comment says, or `-` to read it from stdin |
| `--generation <n>` | Refuse unless this is the generation the comment lands on |

A body is required, and one with nothing in it is refused rather than stored.
Trailing whitespace goes, because a heredoc ends in a newline and a comment does
not. Leading whitespace stays.

```sh
zen-review comment internal/store/db.go --hunk 88 --body "This drops the busy timeout."
zen-review comment internal/store/db.go --file --body - <<'EOF'
Two things here want to be two files.
EOF
```

### `zen-review address <id>`

Record that a comment has been handled, and say how. This is the agent's verb. It
stops the comment moving and says the work was done; it does not close it.

| Flag | What it does |
| --- | --- |
| `--body <text>` | What the answer says, or `-` to read it from stdin |

The answer is optional and is what the reader confirms against. Half a queue is
change requests where the diff is the answer, and the other half are questions a
diff does not answer at all.

A comment is addressed once. A second `address` is refused.

### `zen-review resolve <id>`

Close a comment. This is the reader's verb, and it closes anything not already
closed, an orphan included: the code it was about is gone, and saying that
settles it is the reader's call.

### `zen-review edit <id>`

Rewrite what a comment says. The body and nothing else.

| Flag | What it does |
| --- | --- |
| `--body <text>` | What the comment says, or `-` to read it from stdin |

The anchor never moves, so a comment on the wrong lines is a `delete` and a new
one. An answer is the agent's words and this verb does not reach them.

### `zen-review delete <id>`

Delete a comment. It goes rather than settling into a state. The row that went is
what this prints, so `--json` hands back what it removed.

### `zen-review summary`

Read or write the note about the whole review. One note per session, replaced
each time it is written.

| Flag | What it does |
| --- | --- |
| `--set <text>` | Write this as the note, or `-` to read it from stdin. An empty one clears it |

It is what `export` opens with, and the place a conclusion that is not about any
one file goes.

## JSON

`--json` is on every command that answers with the changeset or with comments. A
write hands back what it wrote in the shape the listing prints, so the id the
next command takes comes out of the call that created it.

```sh
id=$(zen-review comment main.go --hunk 12 --body "..." --json | jq -r '.comments[0].id')
zen-review address "$id" --body "Renamed it."
```
