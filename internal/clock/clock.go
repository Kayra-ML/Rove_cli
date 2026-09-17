package clock

import "time"

type Clock interface {
	Now() time.Time
}

type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC() }

type Frozen struct{ T time.Time }

func (f Frozen) Now() time.Time { return f.T }

type Stepping struct {
	T    time.Time
	Step time.Duration
}

func (s *Stepping) Now() time.Time {
	n := s.T
	s.T = s.T.Add(s.Step)
	return n
}
