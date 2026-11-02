package app

import "math"

// spring moves the window: it follows a target and keeps the velocity it is
// given, which a window's position cannot get from the view's springs. Same
// shape as mygo's: stiffness from the duration, critically damped.
type spring struct {
	value, target, velocity float64
}

func (s *spring) jump(v float64) { s.value, s.target, s.velocity = v, v, 0 }
func (s *spring) set(t float64)  { s.target = t }

func (s *spring) moving() bool {
	return math.Abs(s.value-s.target) >= 0.5 || math.Abs(s.velocity) >= 5
}

// step advances it by dt seconds, at once when the desktop asks for less
// motion.
func (s *spring) step(dt float64, reduce bool) {
	if reduce {
		s.jump(s.target)
		return
	}
	w := 2 * math.Pi / glideDuration
	const h = 1.0 / 480
	for n := int(math.Ceil(min(dt, 0.25) / h)); n > 0; n-- {
		a := -w*w*(s.value-s.target) - 2*w*s.velocity
		s.velocity += a * h
		s.value += s.velocity * h
	}
	if !s.moving() {
		s.jump(s.target)
	}
}
