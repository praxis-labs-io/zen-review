package review

func (s *Session) DuringRefresh(run func()) { s.duringRefresh = run }

func (s *Session) AfterSwap(run func()) { s.afterSwap = run }

func (s *Session) BeforeFreeze(run func()) { s.beforeFreeze = run }
