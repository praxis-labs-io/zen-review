package cli_test

import (
	"strings"
	"testing"
)

func TestUpdateRefusesBeforeItInstallsAnything(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "a base", args: []string{"update", "--base", "main"}, want: "--base"},
		{name: "json", args: []string{"update", "--json"}, want: "--json"},
		{name: "a binary the installer would not replace", args: []string{"update"}, want: "the installer writes zen-review"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)

			out, _, err := f.run(tt.args...)
			if err == nil {
				t.Fatal("update ran")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to mention %q", err, tt.want)
			}
			if out != "" {
				t.Errorf("stdout = %q, want nothing written before the refusal", out)
			}
		})
	}
}
