package review

import "testing"

func TestCrowdedNamesTheDirectoryWorthIgnoring(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []string
		dir   string
		n     int
	}{
		{
			name:  "nothing but root files, so there is nothing to name",
			files: []string{"a.txt", "b.txt"},
		},
		{
			name:  "the busiest of two directories",
			files: []string{"src/a.go", "vendor/x.go", "vendor/y.go", "vendor/z.go"},
			dir:   "vendor",
			n:     3,
		},
		{
			name:  "a tree spread over leaves reports its root",
			files: []string{"node_modules/a/i.js", "node_modules/b/i.js", "node_modules/c/i.js", "src/main.go"},
			dir:   "node_modules",
			n:     3,
		},
		{
			name:  "a tie with a child goes to the parent",
			files: []string{"build/out/one.o", "build/out/two.o"},
			dir:   "build",
			n:     2,
		},
		{
			name:  "a tie between siblings of one length goes to the first by name",
			files: []string{"zeta/one.txt", "beta/one.txt"},
			dir:   "beta",
			n:     1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, n := crowded(tc.files)
			if dir != tc.dir || n != tc.n {
				t.Errorf("crowded = %q, %d, want %q, %d", dir, n, tc.dir, tc.n)
			}
		})
	}
}
