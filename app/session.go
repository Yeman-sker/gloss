package app

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"gloss/capture"
	"gloss/core"
	"gloss/memory"
)

// source is what one capture found, fixed once read: switching features or
// retrying runs on it again, and nothing reads a "current selection" in
// the meantime.
type source struct {
	raw  string // as the app gave it, untrimmed, for writing back
	text string // what actions see
	ctx  core.Context
	// title is the window's, for routing only.
	title string
	// token names the place it was read, for writing back; zero when the
	// helper kept none.
	token     uint64
	method    string
	captureMs int64
	shape     memory.Shape
}

func newSource(r capture.Result, captureMs int64, targetLang string) source {
	text := strings.TrimSpace(r.Text)
	src := source{
		raw:  r.Text,
		text: text,
		ctx: core.Context{
			SourceApp:  r.App,
			TargetLang: targetLang,
			Editable:   core.ParseEditable(r.Editable),
		},
		title:     r.Title,
		token:     r.Token,
		method:    r.Method,
		captureMs: captureMs,
		shape:     memory.ShapeOf(text),
	}
	// The selection in its place, bounded by the helper.
	if strings.TrimSpace(r.Before+r.After) != "" {
		src.ctx.Surrounding = r.Before + r.Text + r.After
	}
	return src
}

// session is one capture and the runs of features on it. Only the run it
// started last may show; a closed session shows nothing.
type session struct {
	id    string
	start time.Time
	src   source

	routing    bool
	candidates []string
	initial    string
	initialBy  string
	reason     string
	scores     map[string]float64
	routeMs    int64

	// feature is the one running or shown, by how it was chosen.
	feature, by string
	// manual locks the session: a routing decision that comes late does
	// not replace what the user picked.
	manual      bool
	routed      bool
	run         uint64
	runStart    time.Time
	firstAt     time.Time
	result      strings.Builder // the run's whole main result, as it came
	done        bool            // the run ended with EventDone
	failed      bool
	corrections []memory.Correction
	correctedAt time.Time
	extraWait   time.Duration
	copied      bool
	replaced    bool
	closed      bool
}

func newSessionID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// decide records the routing decision, unless the user chose first. It
// reports whether the decision stands.
func (s *session) decide(id, by, reason string, scores map[string]float64, took time.Duration) bool {
	if s.closed || s.manual || s.routed {
		return false
	}
	s.routed = true
	s.initial, s.initialBy, s.reason, s.scores = id, by, reason, scores
	s.routeMs = took.Milliseconds()
	return true
}

// choose is the user's pick of another feature: a correction of what was
// shown, and a lock on the session.
func (s *session) choose(id string, now time.Time) bool {
	if s.closed || id == s.feature {
		return false
	}
	if s.feature != "" {
		s.corrections = append(s.corrections, memory.Correction{From: s.feature, To: id, AtMs: now.Sub(s.start).Milliseconds()})
		if s.correctedAt.IsZero() {
			s.correctedAt = now
		}
	}
	if s.initial == "" {
		s.initial, s.initialBy = id, memory.ByManual
		s.routed = true
	}
	s.manual = true
	return true
}

// begin starts a run of feature under the engine's run number.
func (s *session) begin(feature, by string, run uint64, now time.Time) {
	s.feature, s.by, s.run = feature, by, run
	s.runStart, s.firstAt = now, time.Time{}
	s.result.Reset()
	s.done, s.failed = false, false
}

// accepts says whether an event belongs to what the session shows now.
func (s *session) accepts(run uint64) bool {
	return s != nil && !s.closed && run != 0 && run == s.run
}

// observe keeps the run's result whole, apart from how the panel paces it,
// as copying and writing back need it.
func (s *session) observe(ev core.Event, now time.Time) {
	switch ev.Type {
	case core.EventDelta:
		if s.firstAt.IsZero() {
			s.firstAt = now
			if !s.correctedAt.IsZero() && s.extraWait == 0 {
				s.extraWait = now.Sub(s.correctedAt)
			}
		}
		if ev.Index == 0 {
			s.result.WriteString(ev.Delta)
		}
	case core.EventDone:
		s.done = true
	case core.EventError:
		s.failed = true
	}
}

// record is what memory keeps of the session: no text, only its shape.
func (s *session) record(now time.Time) memory.Record {
	outcome := "cancelled"
	switch {
	case s.done:
		outcome = "done"
	case s.failed:
		outcome = "error"
	}
	r := memory.Record{
		Session:     s.id,
		Time:        s.start,
		Routing:     s.routing,
		App:         s.src.ctx.SourceApp,
		Editable:    s.src.ctx.Editable.String(),
		Shape:       s.src.shape,
		Candidates:  s.candidates,
		Initial:     s.initial,
		InitialBy:   s.initialBy,
		Reason:      s.reason,
		Scores:      s.scores,
		Corrections: s.corrections,
		Final:       s.feature,
		Outcome:     outcome,
		Copied:      s.copied,
		Replaced:    s.replaced,
		Timings: memory.Timings{
			CaptureMs:   s.src.captureMs,
			RouteMs:     s.routeMs,
			TotalMs:     now.Sub(s.start).Milliseconds(),
			ExtraWaitMs: s.extraWait.Milliseconds(),
		},
	}
	if !s.firstAt.IsZero() {
		r.Timings.FirstMs = s.firstAt.Sub(s.runStart).Milliseconds()
	}
	return r
}
