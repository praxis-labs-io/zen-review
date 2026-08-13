package cli

import "testing"

// TestInteractive. A pipe and a CI runner get the printed changeset, because a
// program waiting for keys on the other end of a pipe is a program that hangs.
func TestInteractive(t *testing.T) {
	tests := []struct {
		name   string
		asJSON bool
		isTTY  bool
		want   bool
	}{
		{"a terminal", false, true, true},
		{"a pipe", false, false, false},
		{"--json on a terminal", true, true, false},
		{"--json down a pipe", true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := interactive(tt.asJSON, tt.isTTY); got != tt.want {
				t.Errorf("interactive(asJSON=%v, isTTY=%v) = %v, want %v",
					tt.asJSON, tt.isTTY, got, tt.want)
			}
		})
	}
}
