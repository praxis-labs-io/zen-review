package tree

import (
	"path"
	"slices"
	"strings"

	"github.com/zen-review/zen-review/internal/review"
)

// node is one row of the tree before it is flattened: a directory holding
// others, or a file of the changeset.
type node struct {
	// name is what the row shows. A collapsed chain of directories shows all of
	// their names joined, so it is not always one path segment.
	name string

	// path is the file's path, or the directory's prefix without a trailing
	// slash.
	path string

	// file is nil on a directory. It points into the changeset the model holds,
	// so the model has to outlive the tree built from it.
	file *review.File

	kids []*node
	open bool
}

func (n *node) dir() bool { return n.file == nil }

// build lays the changeset's files out under the directories that hold them,
// and sorts every level the way a file tree reads.
func build(files []review.File) []*node {
	var roots []*node
	dirs := make(map[string]*node)

	for i := range files {
		f := &files[i]
		dir, name := path.Split(f.Diff.Path)
		parent := ensure(&roots, dirs, strings.TrimSuffix(dir, "/"))

		leaf := &node{name: name, path: f.Diff.Path, file: f}
		if parent == nil {
			roots = append(roots, leaf)
			continue
		}
		parent.kids = append(parent.kids, leaf)
	}

	for _, n := range roots {
		collapse(n)
	}

	// After collapsing, because a merged chain is sorted under the name it
	// shows rather than the first segment of it.
	order(roots)
	return roots
}

// order sorts one level and everything under it: directories before files, and
// inside each group the dot-prefixed names before the rest.
//
// Git's order is the order it walked the index in, which drops a root file
// above every directory that holds the rest of the changeset. The sort is by
// byte, so a name starting in upper case sorts above one in lower, which is
// what puts CLAUDE.md over go.mod.
func order(nodes []*node) {
	slices.SortFunc(nodes, func(a, b *node) int {
		if a.dir() != b.dir() {
			return boolCmp(b.dir(), a.dir())
		}
		if da, db := dotted(a.name), dotted(b.name); da != db {
			return boolCmp(db, da)
		}
		return strings.Compare(a.name, b.name)
	})

	for _, n := range nodes {
		order(n.kids)
	}
}

func dotted(name string) bool { return strings.HasPrefix(name, ".") }

// boolCmp orders false before true, so the caller passes the flag it wants
// last on the left.
func boolCmp(a, b bool) int {
	switch {
	case a == b:
		return 0
	case b:
		return -1
	default:
		return 1
	}
}

// ensure is the directory node for a prefix, creating it and every directory
// above it. It returns nil for a file sitting at the repository root, which is
// what path.Dir spells "." on the way up.
func ensure(roots *[]*node, dirs map[string]*node, dir string) *node {
	if dir == "" || dir == "." {
		return nil
	}
	if n, ok := dirs[dir]; ok {
		return n
	}

	n := &node{name: path.Base(dir), path: dir, open: true}
	dirs[dir] = n

	parent := ensure(roots, dirs, path.Dir(strings.TrimSuffix(dir, "/")))
	if parent == nil {
		*roots = append(*roots, n)
		return n
	}
	parent.kids = append(parent.kids, n)
	return n
}

// collapse merges a directory holding one directory and nothing else into it,
// so a chain nothing branches off costs one row instead of four.
func collapse(n *node) {
	for len(n.kids) == 1 && n.kids[0].dir() {
		only := n.kids[0]
		n.name += "/" + only.name
		n.path = only.path
		n.kids = only.kids
	}
	for _, k := range n.kids {
		collapse(k)
	}
}

// row is a node and how deep it sits, which is everything the view needs.
type row struct {
	depth int
	n     *node
}

// flatten is the rows a closed directory's children do not appear in.
func flatten(nodes []*node, depth int, out []row) []row {
	for _, n := range nodes {
		out = append(out, row{depth: depth, n: n})
		if n.dir() && n.open {
			out = flatten(n.kids, depth+1, out)
		}
	}
	return out
}
