package review

// DuringRefresh sets the seam the concurrency tests drive the refresh window
// through: it runs between the refresh naming what it is carrying and the
// transaction that writes the generation, which is where a write used to be read
// past.
//
// It lives in a test file so the field stays unreachable outside a test binary.
func (s *Session) DuringRefresh(run func()) { s.duringRefresh = run }

// AfterSwap sets the seam between the ref moving and the row being written,
// which is the window that leaves the two disagreeing if the row never lands.
func (s *Session) AfterSwap(run func()) { s.afterSwap = run }

// BeforeFreeze sets the other seam: it runs between a comment's state being read
// and the swap that changes it, which is the window a refresh orphans one in.
func (s *Session) BeforeFreeze(run func()) { s.beforeFreeze = run }
