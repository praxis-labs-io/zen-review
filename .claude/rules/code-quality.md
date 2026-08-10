# Code Quality (zen-octo)

Go and Bubble Tea specifics for this repo. The principles live in the global
rules, which load automatically; this file only adds what the stack demands.
Don't restate the global rules here.

## Naming

- **Packages**: short, lowercase, no underscores (`gh`, `store`, `theme`). One package per directory.
- **Files**: snake_case (`review_threads.go`, `check_runs.go`). Tests are `foo_test.go` beside `foo.go`.
- **Identifiers**: exported PascalCase, unexported camelCase. No stutter (`config.Load`, not `config.LoadConfig`).
- **Constants**: Go style (`MixedCaps`), not SCREAMING_SNAKE.

## Go specifics

- No `any` or `interface{}` where a concrete type or a small interface works. Accept interfaces, return structs.
- Wrap errors with `%w` and context: `fmt.Errorf("fetching review threads: %w", err)`. Never discard an error silently.
- No naked returns in anything longer than a few lines.
- Table-driven tests for anything with more than two cases.

## Bubble Tea

- **All model mutation happens in `Update`.** A goroutine never touches the model. Anything asynchronous returns a `tea.Cmd` that delivers a typed message, and `Update` applies it. This is the invariant that keeps `-race` quiet.
- One message type per outcome, named for what happened (`prsFetchedMsg`, `mergeFailedMsg`). No shared bag-of-fields message reused across call sites.
- `View` is pure. It reads the model and returns a string, it never fetches, mutates, or starts anything.
- Sub-models own their own keymaps and return commands upward. The root model routes; it doesn't reach into a child's fields.

## Layers

These boundaries are the architecture. Breaking one is a review-stopper, not a nit.

- **`internal/gh` is the only package that touches the network.** It returns domain types, never raw GraphQL structs, so everything above it tests against a fake.
- **`internal/store` owns fetched state and refresh timing.** Views read from it, they never fetch.
- **`internal/tui/*` packages never import each other sideways.** Shared widgets live in `internal/tui/comp`.

## Writes

Every write is optimistic: apply locally, toast, reconcile on response, revert on error. A write path without its revert branch is incomplete, not "the happy path first".

## Styling

Everything styles from the active theme (`internal/tui/theme`), never a hardcoded Lipgloss color and never a Lipgloss default. A new color that isn't in the theme struct means the theme struct needs a field.

## Tests

- Tests ship in the same PR as the logic, never a follow-up.
- Test through the real interface: drive key messages and assert rendered frames, not model fields. A test that only reads internal state stays green while the thing it renders is broken.

## File size

Keep files focused. A file too big to review in one sitting is doing too much; split it before it gets there rather than after.
