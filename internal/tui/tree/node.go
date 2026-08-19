package tree

import (
	"path"
	"strings"

	"github.com/praxis-labs-io/zen-review/internal/review"
)

// node is one row of the tree before it is flattened: a directory holding
// others, or a file of the changeset.
type node struct {
	// name is what the row shows. A collapsed chain of directories shows all of
	// their names joined, so it is not always one path segment.
	name string

	// path is the file's path, or the directory's prefix without a trailing
	// slash. A collapsed chain takes the deepest of them.
	path string

	// chain is the directories a collapsed row stands for above its own path,
	// and is nil on everything else. Carrying a fold across a rebuild matches on
	// any of them: adding a file that branches a chain, or deleting the one that
	// branched it, moves which path the row answers to.
	chain []string

	// file is nil on a directory. It points into the changeset the model holds,
	// so the model has to outlive the tree built from it.
	file *review.File

	kids []*node
	open bool
}

func (n *node) dir() bool { return n.file == nil }

// build lays the changeset's files out under the directories that hold them.
//
// It sorts nothing. review.Derive hands back a changeset already in the order a
// file tree reads, and a sorted list groups a directory's files together and
// puts the directory ahead of the files beside it, so a node lands in the order
// the row it belongs to was first seen.
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
	return roots
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
		n.chain = append(n.chain, n.path)
		n.path = only.path
		n.kids = only.kids
	}
	for _, k := range n.kids {
		collapse(k)
	}
}

// folded collects every directory a closed row stands for, which is what a
// rebuild has to put back.
//
// The closed ones rather than the open ones, because a directory is born open
// and a rebuild that lost the set should leave the tree readable.
func folded(nodes []*node, out map[string]bool) {
	for _, n := range nodes {
		if !n.dir() {
			continue
		}
		if !n.open {
			for _, p := range n.paths() {
				out[p] = true
			}
		}
		folded(n.kids, out)
	}
}

// refold closes the rows that were closed before the rebuild. A row is closed
// when any directory it stands for was.
func refold(nodes []*node, was map[string]bool) {
	for _, n := range nodes {
		if !n.dir() {
			continue
		}
		n.open = true
		for _, p := range n.paths() {
			if was[p] {
				n.open = false
				break
			}
		}
		refold(n.kids, was)
	}
}

// paths is every directory this row stands for: the chain it collapsed, and its
// own.
func (n *node) paths() []string { return append(n.chain[:len(n.chain):len(n.chain)], n.path) }

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
