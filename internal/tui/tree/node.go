package tree

import (
	"path"
	"strings"

	"github.com/praxis-labs-io/zen-review/internal/review"
)

type node struct {
	name string

	path string

	// chain is kept because a rebuild that branches the collapsed chain moves its path.
	chain []string

	file *review.File

	kids []*node
	open bool
}

func (n *node) dir() bool { return n.file == nil }

// build sorts nothing: review.Derive already returns files in file-tree order.
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

// folded records closed directories rather than open ones, so a lost set leaves the tree readable.
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

func (n *node) paths() []string { return append(n.chain[:len(n.chain):len(n.chain)], n.path) }

type row struct {
	depth int
	n     *node
}

func flatten(nodes []*node, depth int, out []row) []row {
	for _, n := range nodes {
		out = append(out, row{depth: depth, n: n})
		if n.dir() && n.open {
			out = flatten(n.kids, depth+1, out)
		}
	}
	return out
}
