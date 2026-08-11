// Package diff turns git's unified diff text into files, hunks and lines.
//
// It knows nothing about review. A parsed diff stays a parsed diff: nothing here
// imports the engine above it, and nothing here decides what a changeset is.
package diff

// Kind is what a line does in the diff. The values are strings so a parsed diff
// reads as English in a golden file, and so the painter maps them with a switch
// rather than two packages agreeing to declare their constants in one order.
type Kind string

const (
	Context Kind = "context"
	Added   Kind = "added"
	Removed Kind = "removed"
)

// Status is what happened to a file. The constants carry a File prefix because
// Added already belongs to Kind.
type Status string

const (
	FileAdded    Status = "added"
	FileModified Status = "modified"
	FileDeleted  Status = "deleted"
	FileRenamed  Status = "renamed"
	FileCopied   Status = "copied"
)

// Line is one line of a hunk.
type Line struct {
	Kind Kind `json:"kind"`

	// Old and New are zero on the side the line does not belong to.
	Old int `json:"old,omitempty"`
	New int `json:"new,omitempty"`

	// Text is the content with the diff marker removed.
	Text string `json:"text"`

	// NoEOL is the "\ No newline at end of file" that followed this line. The
	// annotation takes no line number of its own, so it is carried here rather
	// than becoming a line the file does not have.
	NoEOL bool `json:"noEol,omitempty"`
}

// Hunk is one @@ block.
type Hunk struct {
	// Header is the @@ line verbatim, section heading and all, because the heading
	// names the function the change sits in.
	Header string `json:"header"`

	OldStart int `json:"oldStart"`
	OldLines int `json:"oldLines"`
	NewStart int `json:"newStart"`
	NewLines int `json:"newLines"`

	Lines []Line `json:"lines"`
}

// File is one file's diff.
type File struct {
	Path string `json:"path"`

	// OldPath is set on a rename or a copy. A deleted file carries its own path in
	// Path.
	OldPath string `json:"oldPath,omitempty"`

	Status Status `json:"status"`

	OldMode string `json:"oldMode,omitempty"`
	NewMode string `json:"newMode,omitempty"`

	// OldBlob and NewBlob are the full shas off the index line. The side a file
	// does not exist on is empty rather than forty zeros.
	OldBlob string `json:"oldBlob,omitempty"`
	NewBlob string `json:"newBlob,omitempty"`

	Binary bool `json:"binary,omitempty"`

	// Omitted says why there are no hunks, and is empty when the hunks are the
	// whole story. A file that reads as unchanged is worse than one that says why.
	Omitted string `json:"omitted,omitempty"`

	Hunks []Hunk `json:"hunks,omitempty"`

	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
}
