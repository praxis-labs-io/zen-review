package review

import "strings"

// byTree compares segments rather than joined paths, where zen-octo does not; zen-octo is the one to move.
func byTree(a, b string) int {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")

	for i := range min(len(as), len(bs)) {
		if as[i] == bs[i] {
			continue
		}

		dir := i < len(as)-1
		if dir != (i < len(bs)-1) {
			if dir {
				return -1
			}
			return 1
		}
		return strings.Compare(as[i], bs[i])
	}

	return len(bs) - len(as)
}
