// Package version carries build metadata stamped in at link time.
package version

// Values are overwritten by GoReleaser via -ldflags. The defaults are what a
// plain `go build` produces.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
