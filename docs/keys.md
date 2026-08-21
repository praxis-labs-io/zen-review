# Keys

The keymap is shared with zen-octo by convention, so the two tools feel the same
without either being hostage to the other's release cycle.

The concepts behind the review are in [the guide](guide.md), and the commands in
[the CLI reference](cli.md).

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

## Burning a review down

`n` is the one that matters. A review is a burn-down and `n` is the key held
until the count reaches zero. `r` advances after marking, so `r r r r` walks the
whole thing.

The diff pane opens focused on the first unreviewed hunk. You came here to burn a
review down, not to browse.

A hunk is the block a paragraph motion moves by, so `}` and `{` step it. Vim's
own diff mode says `]c` and `[c`, and the bracket pair is spoken for here.
Nothing in vim moves a whole file in one key, so `tab` is the TUI answer rather
than the editor one; the tree does the same job by hand.

`]` and `[` walk what is unresolved, the way `n` walks what is unread. A ring
that stepped through settled work is a ring nobody holds down. A resolved card
still draws, folded, and `j` still reaches it.

## The cursor and the window

The row the cursor is on carries a bar in its leading cell, in accent. The tint
alone is a change of shade you lose on a page of them. The fill is shared with a
selection and the bar is not, which is what says where the next key moves from
inside one. A comment card takes no bar: its border already lights, and a second
mark on one block says nothing the first did not.

`j` and `k` move a row cursor rather than the window, and a hunk heading pins to
the top row once it scrolls off, so the lines up there are never unlabelled. The
pin follows the window and not the cursor, because a heading names the lines
under it, and the next hunk's own heading pushes it out. The pin owns the top
line, so the cursor never sits there: a key that would put it on that row opens
the window one higher instead, and it lands on the second with its heading above
it.

`ctrl+u` and `ctrl+d` park the cursor mid-window and let the file run past it,
rather than carrying it at whatever row it happened to be on. The ends are the
exception and the only place it moves on screen: the window stops at the first
row and the last, and the cursor goes on alone to the end of the file. Vim
carries the cursor instead, which reads fine in an editor you are typing in and
badly in a pane you are only reading.

## Side by side

`|` puts the two sides in two columns. A run of removals pairs against the run of
additions after it, one row each, and the shorter side draws a blank rather than
shifting up, which would put the two columns out of step for the rest of the
file. Context takes both. One gutter serves both halves, so the rule between them
sits centre.

It is what you asked for and not always what you get. Under a minimum of source
per column the two halves clip away more than they show, so the key refuses and
the bar says how many columns short the pane is. A terminal shrinking under a
split pane falls back to unified and keeps the answer, so widening brings it back
without a second press.

The mode lasts the run. Nothing stores it: a default belongs with your other
preferences, not in the session the review is kept in.

### Which column you are in

The cursor is in one column, and only that column lights. Lighting the row across
both would leave you on a rewritten line with nothing saying which side the next
key takes. The bar sits at the focused column's own edge rather than the pane's,
so it marks the column as well as the row. A heading takes it at the pane edge,
belonging to neither column.

`h` and `l` step into it. They already mean move left and right between things on
screen, and a split pane is one more thing to step to: `h` from the head column
goes to the base, and again to the tree; `l` walks back. The pane keeps the
column it was left in, so returning to it moves nothing, and it starts on the
head. A pane drawing one column has nowhere to step and gives the focus up on the
first press.

`1` and `2` stay a jump, because a badge drawn in a border names the frame it is
drawn in, and there are two frames whatever the mode.

One cursor, not one per column. The rows are paired, so the two halves of a row
are the same change and stepping between them is the comparison being made. Two
cursors could point at unrelated lines and a side-switch would throw the window,
which is vimdiff's shape, and vimdiff has two real buffers to hang it on.

`j` and `k` skip a row the focused column has no line on. Walking the head column
of a deletion-only block steps over it, which is honest: there is no head there
to read, and `h` is where those lines live. A run reaching the end of the file
turns the cursor back rather than stranding it on a blank, and the same step off
the end of a hunk turns back inside it, because `r` takes the hunk the cursor is
in and carrying it over the boundary would mark the hunk you just left.

A comment and a selection scope to the focused column, which is the whole point:
a removal with its replacement beside it can be commented on alone. A mark does
not. `r` takes the hunk and writes every side at once, as it does in unified.

## Selecting

`v` scopes a comment and nothing else. The unit of review is the hunk, so `r`
over a selection marks the hunk and advances, the same press it always was. The
engine can mark lines and `review --lines` reaches it, but from the reader it
buys nothing: `n` stops on a partial hunk as well as an unread one, so a
part-marked hunk is handed back whole anyway.

A selection may cross a hunk, so `j` and `ctrl+d` keep working as they do
everywhere else. Only code fills: a heading, the blank between two hunks and a
comment card are not lines anything is written against.

`esc` is named on the status bar while a selection is up and nowhere else. It
reaches one state of the program, and that is where you look for it. It answers
from either pane, because the selection stays lit while you walk the tree.

## Commenting

`c` scopes to what is under the cursor and takes the first of these it finds: a
selection, the code row the cursor is on, the hunk it is in, the file. A
selection beats the focus the way `esc` does, and the tree focused with nothing
selected is the file itself, because pointing at a file is what the tree is.

A comment anchors to the head wherever the lines it covers have one, and to the
base only when they have none, which is a selection of removals. The head is the
code the next agent rewrites; a mark writes every side at once and a comment
cannot. The box says which side it took, because base numbers read as head ones.

Side by side is where that rule steps aside: a row there names one column, the
one the cursor is in, so you pick rather than the rule. Unified has no column to
pick, so it keeps the rule.

The label says only what the card's own position cannot. A card under its line
needs no line number, because the gutter beside it has one. A range says the run
it covers, a file comment says so, and a comment the diff has no line for says
where it used to point and goes to the foot of the file.

## The card

A comment draws as a bordered card indented to the code column, hanging under the
last line it is about. It is one stop for the cursor rather than one per row: `j`
steps onto it and the next `j` clears the whole thing.

State is a badge in its top border and focus is the border's colour, because four
states and one focus cannot share a channel.

| Badge | State |
| --- | --- |
| `◇` | open |
| `◈` | addressed |
| `◆` | resolved |
| `✕` | orphaned |

Each sits beside its own word: the glyph is the glance and the word is what scans
down a column. A diamond, where a hunk's badge is a circle. The two ladders sit
in one column and mean different things, so they cannot share a shape, and their
colours run opposite ways: a hunk is accent once it is done, a comment is accent
while it still wants you. Orphaned leaves the family, being a loss and not a
stage.

A settled card folds to one row and keeps its box, and takes its response and the
replaced code with it. Its footer names the direction the key goes rather than
the state it is in.

`x`, `e` and `D` are named in the lit card's own footer and nowhere else, because
they reach one row on the screen and that row is where you look for them. `e` and
`D` reach a card in any state: a typo in a resolved comment is still a typo, and
one nobody meant to write is a record of nothing. `D` acts at once, the capital
doing the whole of the thing the way `R` and `U` do. A card narrow enough to drop
hints drops those two first, the help overlay being where the rest of the keymap
lives.

A settled card neither offers `x` nor takes it: freezing a comment twice
re-records an anchor that stopped moving a generation ago. An orphan is offered
it, that being the only thing left anyone can do with a comment whose code is
gone.

An edit is the body alone. The response is the agent's words and no business of
your verb, and the anchor never moves, so a comment on the wrong lines is a `D`
and a new one. The box `e` opens is the card itself: same border, same indent,
same place, holding what it said.

## The response

A response draws as its own box hanging off the card on a rail, two columns in
and two columns narrower. A box says the words below it are somebody else's; a
change of weight inside one border says only that whoever was talking trailed
off. It carries no footer, having no key of its own, and neither the box nor the
rail ever lights: nothing reaches a response, so lighting it would promise a stop
that never arrives.

Under the words it carries the code they replaced: the lines the comment was
written against, where the changeset has since taken them. The words say the work
was done and this is what you confirm them against. The before only, because the
after is the diff the card hangs in, so the two read as a pair without either
being drawn twice. It is painted as the removals it is, the diff's own `−` and
removed background run to the edge of the box.

Three lines, then `>` for the rest.

Whether the lines went is a translation and not the two sides read at the same
numbers. An anchor stops moving once the comment is answered, so a positional
read would call every line inserted above it a rewrite.

Some comments have no block. A bare `address` grows the box to hold the code
alone. An open comment has none however far the code moved under it, nothing
being asserted before the agent answers. A file comment gets none, because it
names the file rather than any region of it and the whole of the old file is not
a block. Neither does a hunk you marked read and an agent then rewrote: `r`
asserts nothing about the code, so there is no claim to confirm, and
`changed after review` already says it moved.

## The composer

`c` types in place and `C` types over the frame, because a comment has a line and
a session note has none. The box `c` opens is the card it is about to become:
same border, same indent, hanging under the same line, so the code it is about is
still on screen above it and the file flows below.

`enter` is a newline in prose, so `ctrl+s` saves and `esc` discards. Nothing else
on screen says so, which is why the box carries both in its bottom border.

The box takes every key while it is up, `q` and `?` included. A note lost to the
letter `q` cannot be taken back, and one key let out is one more thing to hold in
mind while typing. `ctrl+c` is the exception, because raw mode sends no interrupt
and a box that ate it would be the one place in the program with no way out but
`esc`.

It grows with what is typed into it rather than scrolling, because a box that
scrolls hides the sentence still being written. It stops at what the pane can
draw around it: its two borders, the line it hangs under, and the heading pinned
over that. Past there it scrolls, having nowhere left to grow. Where the pane has
no room to draw one at all it goes over the frame instead, which is where `C`'s
is drawn, so a terminal shrinking under you mid-sentence carries the words across
with it.

The box comes down when the write lands, not when the key is pressed. A save that
failed leaves it up holding the words, and so does one you typed past.

An empty body is a discard from `c`, because nothing was typed. From `e` it
writes nothing and the bar says so: wiping a box is not saving a comment, and it
is not deleting one either, which is `D` rather than a second meaning for the
save key.

`c` is refused only while a reload is in git, which would land under an open box
and move the lines it was scoped to.
