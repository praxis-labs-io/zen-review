# zen-kit and the M0 scaffold

Milestone M0 of Zen Review v0.1. Tracked as ZNR-1.

## The problem

zen-review renders diffs. zen-octo already renders diffs, and does it well: a
Chroma colorizer that returns tokens instead of rendered text, a theme struct
that every style reads from, and a line painter that puts a tint behind a
changed row without the escape sequences tearing holes in it. Writing that
twice means two things that drift.

zen-kit holds the visual layer both tools paint with. It is the only piece
zen-review takes from zen-octo, and it is deliberately small.

## What zen-kit is

Three packages and a demo.

```
github.com/zen-kit/zen-kit        go 1.26

theme/        palettes. Lifted from zen-octo unchanged.
syntax/       Chroma tokens. Lifted out of zen-octo's comp/.
paint/        the diff-line painter. The new part.
cmd/kitdemo/  paints a canned diff and exits.
```

`syntax` leaves `comp` on the way out. `comp` is zen-octo's widget bag, and a
colorizer is not a widget. It also pulls Chroma in, which `theme` has no reason
to carry.

The package rename forces the no-stutter fixes: `comp.NewSyntax` becomes
`syntax.New`, `comp.SyntaxNames` becomes `syntax.Names`.

## What zen-kit is not

No model, no state, no layout, no keys. The painter is a pure function over a
line. Folding, scroll, side-by-side layout, hunk grouping, the two-sided
tokenise split, and review state all stay with the caller.

Behaviour is not shared. Each tool owns its renderer. The keymaps match by
convention, written down in both `CLAUDE.md` files, so the two feel the same
without either being hostage to the other's release cycle.

## paint

```go
package paint

type Kind int

const (
	Context Kind = iota
	Added
	Removed
)

// Line is one row ready to paint. Old and New are line numbers; 0 means
// that side has none.
type Line struct {
	Kind     Kind
	Old, New int
	Tokens   []syntax.Token
	Fill     color.Color // beats the kind tint. nil uses it.
}

type Painter struct {
	Theme    theme.Theme
	TabWidth int // 0 means 4
}

func (p Painter) Line(l Line, gutter, width int) string
func (p Painter) HunkHeader(text string, gutter, width int) string

func Gutter(widest int) int
func Clip(s string, width int, mark lipgloss.Style) string
```

`Painter.Line` writes the two line numbers, the marker, and the tokens over a
tint of the kind's color. It paints cell by cell and pads out to the full
width, because every styled run ends in a reset that clears the background with
it: a joined line wrapped in a background style afterwards carries it only as
far as the first token. A context line skips the fill, having no background to
run out of. Anything wider than the pane is clipped rather than wrapped, since
a wrapped line puts its tail under the gutter and every row below it out of
step.

`Gutter` returns the line-number column width for a file, minimum two. Callers
pass it back into `Line` and `HunkHeader` so both agree.

`Clip` always marks the cut. A caller that wants content left alone when it
fits checks the width first. This matches zen-octo's `comp.Clip` exactly, and
carrying the semantics over means the tree, the status bar and every other
truncating row work the same in both tools.

### Fill

`Fill` is the one addition to what zen-octo does today. A cursor row, an active
range selection, and a reviewed-hunk tint all have to beat the added/removed
background. One nil-able field now costs less than a breaking change at M4,
when the diffpane needs all three.

## The demo

`cmd/kitdemo` paints a fixed Go-source diff to stdout and exits. It is the
proof that survives M0, because zen-review has no TUI until M4 and golden files
read wrong on the page and right in a terminal.

It also becomes the thing to look at when a theme changes.

## Testing

`theme` and `syntax` bring their tests with them, ported as-is against the new
import paths.

`paint` gets golden files, one per case:

```
line_added        line_removed       line_context
tabs              clipped            fill_override
one_sided         wide_gutter
```

`-update` regenerates them. `wide_gutter` earns its place: `Gutter` and the
number formatter disagreeing is the bug that misaligns every row in a
thousand-line file, and it does not show up at three digits.

zen-octo has no golden-file convention anywhere. This introduces one. For ANSI
it pays for itself, since asserting escape sequences inline is unreadable.

## The scaffold

zen-octo is the template. The same set in zen-kit and zen-review:

```
go.mod  .gitignore  LICENSE (MIT, 2026 Drew White)
Makefile                       help/test/lint/build/install/all
.github/workflows/ci.yml       lint + test -race + build
.githooks/pre-push             rejects pushes to main
.mcp.json                      linear-zen-review
CLAUDE.md
.claude/  settings.json, hooks/session-start.sh,
          rules/code-quality.md, skills/ship-feature/
```

Two deltas. zen-kit's CI drops the six-way GOOS/GOARCH matrix, because a
library only needs `go build ./...`. And both repos point `.mcp.json` at
`linear-zen-review`, because zen-kit's tickets live in ZNR and a Linear team of
its own buys nothing while zen-review is its only consumer.

`golangci-lint` pins to v2.12.2 in both, matching zen-octo and the local brew
version. Local runs and CI disagreeing about what passes is worse than either
being wrong.

### Rules files

The three diverge.

zen-review's `.claude/rules/code-quality.md` is currently a verbatim copy of
zen-octo's, still headed `# Code Quality (zen-octo)`, and its Layers section
names `internal/gh` and `internal/store` as the boundaries. Rewrite it against
the boundaries this project's spec already defines: `git` and `diff` know
nothing about review, `store` is the only package importing `database/sql`, the
TUI holds no review logic and calls into `review/` for every state change.

zen-kit's is thinner. No Bubble Tea section, since `paint` is pure functions
with no model. One layer rule: `paint` imports `theme` and `syntax`, and
neither of them imports anything of ours.

zen-review's `CLAUDE.md` does not exist yet. It gets the keymap table, so the
convention lives somewhere both tools can point at.

## Build order

```
1  scaffold both repos
2  theme/       125 lines + 68 of tests
3  syntax/      110 lines + 151 of tests, plus the rename
4  paint/       written fresh against 2 and 3
5  cmd/kitdemo
6  tag v0.1.0
```

Steps 2 and 3 are mechanical. Step 4 is the only real work.

Each step gates on `gofmt -l`, `golangci-lint run`, and `go test -race ./...`.
Step 5 gets eyeballed in a terminal.

## zen-octo adopts later

zen-octo keeps its copies through M0. ZNO-43 swaps them out on its own
schedule, and zen-review does not wait for it or own it.

Two copies coexist in the meantime. That is the accepted cost of building
zen-kit clean and proving it here rather than shaping it around zen-octo's
existing call sites.

## Out of scope for M0

- Any zen-octo change.
- A neutral diff model. `paint.Line` is a painter argument, not a diff type.
- Side-by-side layout. The painter renders one row; arranging two columns of
  them is the caller's job, at M6.
- Additional themes. Rose Pine Moon is the only one, as it is today.
- Publishing a versioned API contract. zen-kit stays pre-1.0 and both consumers
  are ours, so a breaking change is a bump and two import fixes.

## Open items

- Whether `syntax.Syntax` keeps its name. It mirrors `theme.Theme`, which reads
  fine, but a second look at M4 costs nothing while the module is pre-1.0.
- Where zen-kit's spec lives once its repo exists. It sits in zen-review's docs
  now because ZNR-1 tracks it.
