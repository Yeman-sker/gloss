package memory

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stats are the measures of docs/decisions/0009-memory-and-routing.md: what the records
// show, and how much they leave unknown.
type Stats struct {
	Uses   int // sessions
	Routed int // sessions with routing on, the base of the rates
	// Corrected sessions are those the user switched away from the
	// initial feature. A correction is a known miss; no correction is
	// not a known hit.
	Corrected int
	// Labelled sessions carry an explicit label, Right of them right.
	Labelled, Right int
	// Unknown sessions have neither a correction nor a label.
	Unknown int
	// Fallback sessions were routed by the last explicit choice or the
	// default: the router knew nothing better.
	Fallback int

	RouteP50, RouteP95         time.Duration
	ExtraWaitP50, ExtraWaitP95 time.Duration
	BySource                   map[string]int
	ByFeature                  map[string]int // the final feature

	correctedLabelled int
}

// CorrectionRate is Corrected over Routed.
func (s Stats) CorrectionRate() float64 { return ratio(s.Corrected, s.Routed) }

// ConfirmedAccuracy is Right over the sessions whose outcome is known: the
// labelled ones and the corrected ones, a correction being a miss.
func (s Stats) ConfirmedAccuracy() float64 { return ratio(s.Right, s.Labelled+s.correctedUnlabelled()) }

// UnknownRate is Unknown over Routed.
func (s Stats) UnknownRate() float64  { return ratio(s.Unknown, s.Routed) }
func (s Stats) FallbackRate() float64 { return ratio(s.Fallback, s.Routed) }

// correctedUnlabelled are known misses that no label already counts.
func (s Stats) correctedUnlabelled() int { return s.Corrected - s.correctedLabelled }

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// Summarize measures the records from since on.
func Summarize(records []Record, since time.Time) Stats {
	s := Stats{BySource: map[string]int{}, ByFeature: map[string]int{}}
	var route, wait []time.Duration
	for _, r := range records {
		if r.Time.Before(since) {
			continue
		}
		s.Uses++
		s.ByFeature[r.Final]++
		if !r.Routing {
			continue
		}
		s.Routed++
		s.BySource[r.InitialBy]++
		route = append(route, time.Duration(r.Timings.RouteMs)*time.Millisecond)
		corrected := len(r.Corrections) > 0
		if corrected {
			s.Corrected++
			if r.Timings.ExtraWaitMs > 0 {
				wait = append(wait, time.Duration(r.Timings.ExtraWaitMs)*time.Millisecond)
			}
		}
		switch {
		case r.Confirmed != nil:
			s.Labelled++
			if *r.Confirmed {
				s.Right++
			}
			if corrected {
				s.correctedLabelled++
			}
		case !corrected:
			s.Unknown++
		}
		if r.InitialBy == ByRecent || r.InitialBy == ByDefault {
			s.Fallback++
		}
	}
	s.RouteP50, s.RouteP95 = percentile(route, 50), percentile(route, 95)
	s.ExtraWaitP50, s.ExtraWaitP95 = percentile(wait, 50), percentile(wait, 95)
	return s
}

func percentile(d []time.Duration, p int) time.Duration {
	if len(d) == 0 {
		return 0
	}
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[(len(d)-1)*p/100]
}

// String is a report for a terminal.
func (s Stats) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "使用 %d 次，其中自动路由 %d 次\n", s.Uses, s.Routed)
	if s.Routed > 0 {
		fmt.Fprintf(&b, "改正率      %5.1f%%  (%d 次改正；改正不等于全部错误)\n", 100*s.CorrectionRate(), s.Corrected)
		fmt.Fprintf(&b, "已确认准确率 %5.1f%%  (标注 %d 次、其中对 %d 次，加上未标注的改正 %d 次)\n", 100*s.ConfirmedAccuracy(), s.Labelled, s.Right, s.correctedUnlabelled())
		fmt.Fprintf(&b, "未知比例    %5.1f%%  (没有改正也没有标注的 %d 次，不能算对)\n", 100*s.UnknownRate(), s.Unknown)
		fmt.Fprintf(&b, "回退率      %5.1f%%  (最近选择或默认功能)\n", 100*s.FallbackRate())
		fmt.Fprintf(&b, "路由耗时    p50 %s  p95 %s\n", ms(s.RouteP50), ms(s.RouteP95))
		if s.Corrected > 0 {
			fmt.Fprintf(&b, "改正后额外等待 p50 %v  p95 %v\n", s.ExtraWaitP50, s.ExtraWaitP95)
		}
		fmt.Fprintf(&b, "初次选择来源 %s\n", counts(s.BySource))
	}
	fmt.Fprintf(&b, "最终功能    %s\n", counts(s.ByFeature))
	return b.String()
}

func counts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return m[keys[i]] > m[keys[j]] || m[keys[i]] == m[keys[j]] && keys[i] < keys[j] })
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(parts, "，")
}

// ms prints a duration kept in whole milliseconds.
func ms(d time.Duration) string {
	if d < time.Millisecond {
		return "<1ms"
	}
	return d.String()
}
