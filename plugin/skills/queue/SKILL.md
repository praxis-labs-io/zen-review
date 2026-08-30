---
name: queue
description: Work the review queue zen-review keeps for this repository — read the comments written against the code, answer each one with what was done, and rebuild so the reader sees the change under the answer. Use when asked to address review comments, work the review queue, or see what a reviewer left; after changing code that is under review; or when a zen-review Stop hook hands over a queue.
---

# The review queue

zen-review is a review engine for this repository. A person reads the changes,
writes comments against particular lines, and those comments are the queue. This
skill is how you work it down.

Everything here is the CLI. There is no other interface, and every read takes
`--json`.

## Is there anything to do

```sh
zen-review comments --state open --json
```

`open` is the half that is yours: comments nobody has answered yet. The other
states are not.

- **addressed** — you already answered it. The reader confirms it, not you.
- **resolved** — the reader closed it. Finished.
- **orphaned** — the file it was written against left the changeset.

`--state unresolved` is every state but `resolved`. It answers whether the
review is finished, which is the reader's question. It is not your queue, and
treating it as one loops forever: you cannot reach `resolved` from here.

## Answer each one

```sh
zen-review address <id> --body "Changed it to return a float."
```

`address` is your verb. It records that the work was done and stops the comment
moving. It does **not** close the comment — the claim and the confirmation are
different facts.

Write a body. Half a queue is change requests where the diff is the answer, and
the other half are questions a diff does not answer at all. What you write is
what the reader checks against, so a bare `address` on a question leaves them
nothing to read. A comment is addressed once; a second `address` is refused.

For prose that should not have to survive a shell:

```sh
zen-review address <id> --body - <<'EOF'
Two things here. The truncation was intentional for the counter,
but mean() should not have inherited it, so that one now returns a float.
EOF
```

Answer a comment you disagree with by saying so in the body. An `address` is
what you did and why, not agreement.

## Rebuild

```sh
zen-review refresh
```

Run it once the answers are in. It builds a new generation, which is what puts
the block of replaced code under your answer when the reader looks. Their own
`s` does it too, but a refresh you ran means it is there when they arrive.

## Verbs that are not yours

- **`resolve`** closes a comment. The reader confirms your work; you do not
  confirm it for them.
- **`edit`** rewrites the reader's own words. `address` writes yours. One id
  naming two voices is how an answer gets overwritten by somebody fixing a typo.
- **`delete`** removes a comment outright.
- **`review` and `unreview`** mark code as inspected. This is the one thing the
  tool exists to record, and it means a person read it. Never mark your own work
  reviewed.

## Two rules that will bite

**A write names a generation.** If the reader refreshed while you were working,
your write is refused rather than landing against a diff that moved. Run
`zen-review refresh`, re-read the queue, and write again. Nothing is lost.

**`--base` is refused on writes.** It sticks to the session rather than applying
to one invocation, so moving it would recompute the changeset the write then
anchors into. `zen-review status --base <ref>` is where a base changes, and that
is the reader's call.

## Exit codes

`0` answered and nothing matched, `1` answered and the filter matched, `2`
failed. Only `comments --exit-code` reaches 1. A `2` is a broken call, not an
empty queue — read what it said rather than treating it as done.

Full reference: `zen-review <command> --help`, and `docs/agents.md` in the
zen-review repository.
