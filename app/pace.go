package app

import (
	"math"
	"strings"
	"unicode/utf8"
)

// pacer lets out what the network sends at an even speed. A model's tokens
// reach the panel in bursts with gaps between them, and text shown as it comes
// lurches. The speed follows how much is waiting, so a backlog clears within
// paceCatchUp, and it eases toward that speed, so a burst changes it gradually.
type pacer struct {
	rate, carry float64
}

// step is how many runes leave a backlog of n after dt seconds. A run that has
// ended clears what is left within paceFinish, however much that is.
func (s *pacer) step(n int, ended bool, dt float64) int {
	if n <= 0 {
		s.rate, s.carry = 0, 0
		return 0
	}
	want := max(paceMin, min(paceMax, float64(n)/paceCatchUp))
	if ended {
		want = max(want, float64(n)/paceFinish)
	}
	if s.rate == 0 {
		s.rate = want
	}
	s.rate += (want - s.rate) * (1 - math.Exp(-dt/paceEase))
	s.carry += s.rate * dt
	if s.carry < 1 {
		return 0
	}
	out := int(s.carry)
	s.carry -= float64(out)
	return min(out, n)
}

// overdraw charges a piece that went whole for what it took past the budget, so
// that the speed stays honest, but only for a moment's worth: what comes after a
// long line of code should not wait minutes to pay for it.
func (s *pacer) overdraw(n int) {
	s.carry = max(s.carry-float64(n), -s.rate*paceOwe)
}

// mdCursor walks Markdown as the pacer lets it out, in pieces that show well:
// half a marker on the screen would first read as text and then jump into a
// heading or a list. A table's row, a line of code and a rule go whole.
type mdCursor struct {
	at    int  // how far it has let out, in bytes
	fence bool // inside a fenced code block
}

// next moves past the next piece of src and says how many runes that was. It
// says 0 when the piece has not all arrived, unless final says it never will.
func (m *mdCursor) next(src string, final bool) int {
	if m.at >= len(src) {
		return 0
	}
	if m.at == 0 || src[m.at-1] == '\n' {
		if n, whole := m.line(src, final); whole {
			return n
		}
		if m.at < len(src) && (m.at == 0 || src[m.at-1] == '\n') && !final && markerOpen(src[m.at:]) {
			return 0
		}
		m.at += markerLen(src[m.at:])
	}
	return m.word(src)
}

// line lets out a whole line when it goes whole. whole is false for any other.
func (m *mdCursor) line(src string, final bool) (int, bool) {
	rest := src[m.at:]
	end := strings.IndexByte(rest, '\n')
	complete := end >= 0 || final
	text := rest
	if end >= 0 {
		text = rest[:end]
	}
	trimmed := strings.TrimLeft(text, " \t>")
	fenceLine := strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
	switch {
	case m.fence || fenceLine || strings.HasPrefix(trimmed, "|") || isRule(trimmed):
	case !complete && (trimmed == "" || strings.Trim(trimmed, "`~-*_=") == ""):
		// It may yet become one of those.
		return 0, true
	default:
		return 0, false
	}
	if !complete {
		return 0, true
	}
	if fenceLine {
		m.fence = !m.fence
	}
	take := len(rest)
	if end >= 0 {
		take = end + 1
	}
	n := utf8.RuneCountInString(rest[:take])
	m.at += take
	return max(n, 1), true
}

// word moves past a word, or a rune of text written without spaces, with the
// emphasis that opens it and the spaces that follow.
func (m *mdCursor) word(src string) int {
	from := m.at
	for m.at < len(src) && strings.IndexByte("*_`~[", src[m.at]) >= 0 {
		m.at++
	}
	if m.at < len(src) {
		r, size := utf8.DecodeRuneInString(src[m.at:])
		m.at += size
		if isLatin(r) {
			for m.at < len(src) {
				r, size := utf8.DecodeRuneInString(src[m.at:])
				if !isLatin(r) && r != '\'' && r != '-' {
					break
				}
				m.at += size
			}
		}
	}
	for m.at < len(src) && (src[m.at] == ' ' || src[m.at] == '\n') {
		m.at++
	}
	return max(utf8.RuneCountInString(src[from:m.at]), 1)
}

// markerLen is how long the marker opening a line is, spaces included: of a
// heading, a list's item or a quote, nested as far as they go.
func markerLen(s string) int {
	i := 0
	for i < len(s) {
		j := i
		for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
			j++
		}
		k := j
		switch {
		case k < len(s) && s[k] == '#':
			for k < len(s) && s[k] == '#' {
				k++
			}
		case k < len(s) && strings.IndexByte("-*+", s[k]) >= 0:
			k++
		case k < len(s) && s[k] == '>':
			k++
		case k < len(s) && s[k] >= '0' && s[k] <= '9':
			for k < len(s) && s[k] >= '0' && s[k] <= '9' {
				k++
			}
			if k >= len(s) || (s[k] != '.' && s[k] != ')') {
				return i
			}
			k++
		default:
			return i
		}
		if k >= len(s) || (s[k] != ' ' && s[k] != '\t') {
			return i
		}
		for k < len(s) && (s[k] == ' ' || s[k] == '\t') {
			k++
		}
		i = k
	}
	return i
}

// markerOpen says the line has so far only what may still turn into a marker.
func markerOpen(s string) bool {
	if strings.IndexByte(s, '\n') >= 0 {
		return false
	}
	rest := s[markerLen(s):]
	return strings.Trim(rest, "#-*+>.) \t0123456789") == ""
}

func isRule(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 3 {
		return false
	}
	for _, c := range []string{"-", "*", "_", "="} {
		if strings.Trim(s, c+" ") == "" {
			return true
		}
	}
	return false
}
