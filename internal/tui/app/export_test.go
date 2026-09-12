package app

import tea "charm.land/bubbletea/v2"

// BodyFailed is the message a failed whole-file read delivers.
//
// It is here rather than driven through a key because two reads in flight cannot
// be staged from outside: the render harness runs a command before it sends the
// next key, so the first read has always landed by then.
func BodyFailed(path string, gen int64, err error) tea.Msg {
	return bodyFailedMsg{path: path, gen: gen, err: err}
}
