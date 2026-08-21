# Code Quality (zen-review)

Go and Bubble Tea specifics for this repo. The principles live in the global
rules, which load automatically; this file only adds what the stack demands.
Don't restate the global rules here.

## Naming

- **Packages**: short, lowercase, no underscores (`git`, `diff`, `review`, `store`). One package per directory.
- **Files**: snake_case (`merge_base.go`, `reviewed_ranges.go`). Tests are `foo_test.go` beside `foo.go`.
- **Identifiers**: exported PascalCase, unexported camelCase. No stutter (`review.Session`, not `review.ReviewSession`).
- **Constants**: Go style (`MixedCaps`), not SCREAMING_SNAKE.

## Go specifics

- No `any` or `interface{}` where a concrete type or a small interface works. Accept interfaces, return structs.
- Wrap errors with `%w` and context: `fmt.Errorf("resolving the base for %s: %w", branch, err)`. Never discard an error silently.
- No naked returns in anything longer than a few lines.
- Table-driven tests for anything with more than two cases.

## Bubble Tea

- **All model mutation happens in `Update`.** A goroutine never touches the model. Anything asynchronous returns a `tea.Cmd` that delivers a typed message, and `Update` applies it. This is the invariant that keeps `-race` quiet.
- One message type per outcome, named for what happened (`generationBuiltMsg`, `remapFailedMsg`). No shared bag-of-fields message reused across call sites.
- **A command outlives the program.** Bubble Tea does not wait for one it started, so `Run` returns with a git call still going. Whatever the command borrowed is released after that call finishes, never after `Run` does.
- `View` is pure. It reads the model and returns a string, it never fetches, mutates, or starts anything.
- Sub-models own their own keymaps and return commands upward. The root model routes; it doesn't reach into a child's fields.

## Layers

These boundaries are the architecture. Breaking one is a review-stopper, not a nit.

The test for any decision: **could the CLI answer this question without the TUI
running?** If no, the logic is in the wrong package.

- **`internal/git` is plumbing only.** It runs git and returns bytes and structs, never opinions. Nothing above it shells out to git.
- **`internal/diff` turns unified diff text into files, hunks and lines, and knows nothing about review.** It never imports `review`. A parsed diff stays a parsed diff.
- **`internal/store` is the only package importing `database/sql`.** Everything else goes through `review`.
- **`internal/review` is the engine, and the TUI holds none of it.** Every state change is a call into `review`, and `cli` calls the same functions. A behaviour reachable by key and not by subcommand is in the wrong place.
- **`internal/tui/*` packages never import each other sideways.** Shared widgets live in `internal/tui/comp`.
- **The visual layer stops at the TUI.** `tui/diffpane` and `tui/comp` import `tui/theme`, `tui/syntax` and `tui/paint`. Nothing below `tui/` imports any of the three.
- **`tui/paint` imports `tui/theme` and `tui/syntax`, and neither of those imports anything of ours.** `go list -deps ./internal/tui/theme ./internal/tui/syntax` naming anything of ours but those two means the boundary has already broken.

## State changes

Every state change goes through `review`, and the TUI renders what `review`
returned rather than its own guess at what the write did.

These writes are a local SQLite transaction: it committed or it failed, and
there is no round trip worth painting over. A failure raises a toast and leaves
the state alone. zen-octo applies a write optimistically and reverts on error
because its writes cross a network; copying that here would add a revert branch
guarding a window that does not exist.

What does need care is the refresh: a new generation moves every anchor at once,
and nothing moves on screen until the key is pressed.

## Styling

Everything styles from the `theme.Theme` it was handed, never a hardcoded
Lipgloss color and never a Lipgloss default. A new color that isn't in
`theme.Theme` means the struct needs a field.

A nil color field means "leave the terminal's own showing". Guard for it rather
than passing it to Lipgloss, or a transparent background stops being
transparent.

Diff rows come from `paint`, and every exported function there is pure: same
arguments, same string. A second line painter anywhere else is the drift the
package exists to prevent.

## Tests

- Tests ship in the same PR as the logic, never a follow-up.
- Test through the real interface: drive key messages and assert rendered frames, not model fields. A test that only reads internal state stays green while the thing it renders is broken.
- **`internal/git` runs against real temporary repos, no mocks.** What is under test is whether git is being called correctly. `internal/testrepo` builds them, and every package wanting one uses it rather than growing its own.
- **Remapping gets a case per way it goes bad**, over fixture repos: a line inserted above a reviewed hunk, a reviewed line deleted, a region rewritten wholesale, a rename, a delete, a rebase to identical content, a base change, a base force-push that loses the fork point.
- `diff` gets golden files on unified diff text. `cli` gets golden JSON.
- TUI render tests run at widths where something actually overflows, which is the only width that proves anything.
- **`paint`'s goldens keep their escapes where the frame goldens are stripped.** What a painted row gets wrong is the colour and the fill, so `cat` one to read it. A case earns a file by being a way the painter goes wrong, not by being another example of it going right.

## File size

Keep files focused. A file too big to review in one sitting is doing too much;
split it before it gets there rather than after.
