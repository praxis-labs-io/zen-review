package review

import "strings"

// byTree orders two paths the way a file tree reads: at the first segment where
// they differ, a directory sorts above a file, and two of the same kind sort by
// byte.
//
// Git hands back the order it walked the index in, which drops a root file above
// every directory holding the rest of the changeset. One ordering comes out of
// the engine so the printed table and the tree pane cannot disagree about what
// is first.
//
// Byte order needs no case of its own for a dotted name: "." sorts below every
// letter, so .github lands above cmd and .gitignore above CLAUDE.md. A rule
// spelling that out would only change the answer for a name starting below ".",
// where it would put .gitignore above -report.txt for no reason anyone asked
// for.
//
// Comparing segments rather than whole paths is what lets a directory's own name
// place it. "z/deep.go" against "z-x.go" is z before z-x; comparing the joined
// strings would answer z-x first, on "/" sorting below "-".
func byTree(a, b string) int {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")

	for i := range min(len(as), len(bs)) {
		if as[i] == bs[i] {
			continue
		}

		// Whichever path still has a segment below this one is a directory here.
		dir := i < len(as)-1
		if dir != (i < len(bs)-1) {
			if dir {
				return -1
			}
			return 1
		}
		return strings.Compare(as[i], bs[i])
	}

	// One path is the other's prefix, which is a file turning into the directory
	// that took its name: converting src/foo into src/foo/bar.go deletes the file
	// and adds the directory in one changeset. The longer path is the directory,
	// so it goes first, the same as everywhere above.
	return len(bs) - len(as)
}
