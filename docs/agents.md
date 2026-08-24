# Driving zen-review from an agent

The CLI answers every question the reader can, with no terminal attached. That
is what lets an agent read its own review queue and answer it.

Concepts are in [the guide](guide.md); every flag is in
[the CLI reference](cli.md). This page is the loop.

## The gate

```sh
zen-review comments --state unresolved --exit-code
```

Three codes, so a hook can tell a queue from a failure:

| Code | Means | What a hook does |
| --- | --- | --- |
| 0 | Nothing unresolved | Stop. The work is done |
| 1 | The filter matched | Keep going. There is a queue |
| 2 | The call failed | Stop and say why. This is not a queue |

A hook that reads only non-zero cannot tell an open comment from a broken git
call, and blocks forever on the second. `diff` and `grep` split answering from
failing the same way.

The listing is written either way. `--exit-code` sets the status after it, never
instead of it, so one call both reports and gates.

As a Claude Code Stop hook:

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "zen-review comments --state unresolved --exit-code"
          }
        ]
      }
    ]
  }
}
```

## Reading the queue

```sh
zen-review comments --state open --json
```

```json
{
  "comments": [
    {
      "id": "2a8359b42163",
      "path": "sum.go",
      "side": "head",
      "scope": "range",
      "start": 4,
      "end": 15,
      "state": "open",
      "body": "mean truncates. Was that intended?",
      "response": "",
      "replaced": null,
      "createdAt": "2026-08-21T20:24:06Z",
      "updatedAt": "2026-08-21T20:24:06Z"
    }
  ],
  "totals": { "comments": 1, "open": 1, "addressed": 0, "resolved": 0, "orphaned": 0, "unresolved": 1 }
}
```

`id` is what every other command takes. `side` is `head` or `base`, and a
comment on removals is anchored on the base, where the line numbers are the old
file's. `scope` is `range`, `hunk` or `file`. `replaced` carries the code an
answer replaced, once there is an answer.

The object also carries the session, the base and the generation. Narrow with
`--path <path>`, and note that on the base side of a rename the path is the old
name.

## Answering

`address` is the agent's verb.

```sh
zen-review address 2a8359b42163 --body "Changed it to return a float."
```

It stops the comment moving and records that the work was done. It does **not**
close the comment: the claim and the confirmation are different facts, and the
reader resolves it after checking. A comment is addressed once; a second
`address` is refused.

The body is optional. Half a queue is change requests where the diff is the
answer, and the other half are questions a diff does not answer at all. What is
written is what the reader confirms against, so a bare `address` on a question
leaves them with nothing to read.

For prose that should not have to survive a shell:

```sh
zen-review address 2a8359b42163 --body - <<'EOF'
Two things here. The truncation was intentional for the counter,
but mean() should not have inherited it, so that one now returns a float.
EOF
```

## Verbs that are not yours

`resolve`, `edit` and `delete` belong to the reader.

`edit` rewrites the reader's own words, and `address` writes yours. One id
naming two voices is how an agent's answer gets overwritten by a reader fixing
their own typo, so `edit` does not reach the response and `address` does not
reach the body.

## Writing against the right generation

A refresh rebuilds the changeset and moves every anchor. A write naming a
generation that is no longer the latest is refused rather than landing on code
you did not read:

```sh
zen-review status --json | jq '.generation.seq'
zen-review comment sum.go --hunk 4 --generation 1 --body "..."
```

Pass `--generation` whenever there is a gap between reading and writing. Without
it the write takes whatever generation is current.

## `--base` is refused on writes

`--base` moves the base and keeps it moved. On a write that would recompute the
changeset the write then anchors into, so `review`, `unreview`, `comment`,
`address`, `resolve`, `edit`, `delete` and `summary --set` all turn it away.
`zen-review status --base <ref>` is where it changes.

## The loop

```sh
# 1. Is there anything to do?
zen-review comments --state unresolved --exit-code || true

# 2. What is it?
zen-review comments --state open --json

# 3. Do the work, then say what you did.
zen-review address "$id" --body "..."

# 4. Rebuild so the reader sees the code against the comments.
zen-review refresh
```

Step 4 is worth doing. The reader's own `s` does it too, but a refresh you ran
means the block of replaced code under your answer is there when they look.
