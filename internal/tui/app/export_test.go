package app

import tea "charm.land/bubbletea/v2"

// BodyFailed is exported because the render harness lands one read before the next key, so two in flight cannot be staged.
func BodyFailed(path string, gen int64, err error) tea.Msg {
	return bodyFailedMsg{path: path, gen: gen, err: err}
}
