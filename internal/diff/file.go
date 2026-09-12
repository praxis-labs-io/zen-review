// Package diff parses git's unified diff text into files, hunks and lines; it knows nothing of review.
package diff

// Kind is a string so a parsed diff reads as words in a golden file.
type Kind string

const (
	Context Kind = "context"
	Added   Kind = "added"
	Removed Kind = "removed"
)

type Status string

const (
	FileAdded    Status = "added"
	FileModified Status = "modified"
	FileDeleted  Status = "deleted"
	FileRenamed  Status = "renamed"
	FileCopied   Status = "copied"
)

type Line struct {
	Kind Kind `json:"kind"`

	// Old and New are zero on the side the line does not belong to.
	Old int `json:"old,omitempty"`
	New int `json:"new,omitempty"`

	// Text excludes the diff marker.
	Text string `json:"text"`

	// NoEOL marks a "\ No newline at end of file" after this line.
	NoEOL bool `json:"noEol,omitempty"`
}

type Hunk struct {
	// Header is the @@ line verbatim, section heading included.
	Header string `json:"header"`

	OldStart int `json:"oldStart"`
	OldLines int `json:"oldLines"`
	NewStart int `json:"newStart"`
	NewLines int `json:"newLines"`

	Lines []Line `json:"lines"`
}

type File struct {
	Path string `json:"path"`

	// OldPath is set on a rename or a copy. A deleted file keeps its path in Path.
	OldPath string `json:"oldPath,omitempty"`

	Status Status `json:"status"`

	OldMode string `json:"oldMode,omitempty"`
	NewMode string `json:"newMode,omitempty"`

	// OldBlob and NewBlob are full shas, empty on the side the file does not exist on.
	OldBlob string `json:"oldBlob,omitempty"`
	NewBlob string `json:"newBlob,omitempty"`

	Binary bool `json:"binary,omitempty"`

	// Omitted says why there are no hunks, and is empty when the hunks are the whole story.
	Omitted string `json:"omitted,omitempty"`

	Hunks []Hunk `json:"hunks,omitempty"`

	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}

// BasePath is the file's name on the base side: OldPath on a rename or a copy, Path otherwise.
func (f File) BasePath() string {
	if f.OldPath != "" {
		return f.OldPath
	}
	return f.Path
}
