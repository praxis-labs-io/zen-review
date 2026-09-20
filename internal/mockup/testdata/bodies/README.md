# zen-review

Drew's local code review engine.

Agents write code faster than anyone can read it, and they rewrite the code you
already read. zen-review remembers which lines you inspected and carries that
record through every new generation of the changeset.

## Install

```sh
brew install praxis-labs-io/tap/zen-review
```

## Use

Run `zen-review` in a repository. It opens one changeset: the merge base with
the base branch, through the working tree, untracked files included.

Press `p` on a file to read it whole, with the changed lines still tinted.
Press `?` for the keymap.
