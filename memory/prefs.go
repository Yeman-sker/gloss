package memory

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Situation is where a selection was made, as preferences are told apart.
type Situation struct {
	App      string
	Editable string
	Shape    Shape
}

// Preference is what the records say a user chose in some scope, with the
// evidence behind it.
type Preference struct {
	Scope string
	// Shares are the weighted share of each enabled feature, summing to 1
	// when there is any evidence.
	Shares map[string]float64
	Top    string
	// Samples are the explicit choices counted: corrections, and routed
	// results the user went on to copy or write back.
	Samples     int
	Corrections int
	Last        time.Time
	// Insufficient says there is too little to go on. Conflicting says the
	// evidence points two ways.
	Insufficient, Conflicting bool
}

// Snapshot is what routing reads about the user, for one selection.
type Snapshot struct {
	// Similar is this app and this kind of text; App is this app; All is
	// everywhere.
	Similar, App, All Preference
	// Recent are the latest explicit choices anywhere, newest first.
	Recent []string
	// Corrections are the latest switches in this app, newest first.
	Corrections []Correction
}

const (
	halfLife = 21 * 24 * time.Hour
	// An explicit choice weighs this much; a routed result the user used
	// counts for less, and one the user merely saw for nothing, so the
	// router does not reinforce itself.
	weightExplicit = 1.0
	weightUsed     = 0.3
	minSamples     = 3
	minWeight      = 1.5
	closeShares    = 0.2
)

// Prefer distils the records for a situation, over the enabled features.
func Prefer(records []Record, sit Situation, enabled []string, now time.Time) Snapshot {
	on := map[string]bool{}
	for _, id := range enabled {
		on[id] = true
	}
	class := sit.Shape.Class()
	var snap Snapshot
	sim := tally{scope: "相同应用、相似文本", on: on}
	app := tally{scope: "相同应用", on: on}
	all := tally{scope: "全部", on: on}
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		w, explicit := evidence(r)
		age := now.Sub(r.Time)
		if age < 0 {
			age = 0
		}
		w *= math.Pow(0.5, float64(age)/float64(halfLife))
		sameApp := sit.App != "" && r.App == sit.App
		for _, t := range []*tally{&all, &app, &sim} {
			if t == &app && !sameApp || t == &sim && (!sameApp || r.Shape.Class() != class) {
				continue
			}
			t.add(r, w, explicit)
		}
		if explicit && len(snap.Recent) < 5 && on[r.Final] {
			snap.Recent = append(snap.Recent, r.Final)
		}
		if sameApp {
			for j := len(r.Corrections) - 1; j >= 0 && len(snap.Corrections) < 5; j-- {
				snap.Corrections = append(snap.Corrections, r.Corrections[j])
			}
		}
	}
	snap.Similar, snap.App, snap.All = sim.done(), app.done(), all.done()
	return snap
}

// evidence is how much a record says about what the user wanted, and
// whether the user said it.
func evidence(r Record) (float64, bool) {
	switch {
	case len(r.Corrections) > 0, r.InitialBy == ByManual:
		return weightExplicit, true
	case r.Outcome == "done" && (r.Copied || r.Replaced):
		return weightUsed, false
	}
	return 0, false
}

type tally struct {
	scope   string
	on      map[string]bool
	weights map[string]float64
	p       Preference
}

func (t *tally) add(r Record, w float64, explicit bool) {
	if w <= 0 || !t.on[r.Final] {
		return
	}
	if t.weights == nil {
		t.weights = map[string]float64{}
	}
	t.weights[r.Final] += w
	if explicit {
		t.p.Samples++
	}
	t.p.Corrections += len(r.Corrections)
	if r.Time.After(t.p.Last) {
		t.p.Last = r.Time
	}
}

func (t *tally) done() Preference {
	p := t.p
	p.Scope = t.scope
	p.Shares = map[string]float64{}
	var total float64
	for _, w := range t.weights {
		total += w
	}
	type kv struct {
		id string
		w  float64
	}
	var ranked []kv
	for id, w := range t.weights {
		p.Shares[id] = w / total
		ranked = append(ranked, kv{id, w / total})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].w > ranked[j].w || ranked[i].w == ranked[j].w && ranked[i].id < ranked[j].id
	})
	if len(ranked) > 0 {
		p.Top = ranked[0].id
	}
	p.Insufficient = p.Samples < minSamples || total < minWeight
	p.Conflicting = len(ranked) > 1 && ranked[0].w-ranked[1].w < closeShares
	return p
}

// Lean is the feature this preference points to, when it is sure enough:
// enough evidence, one feature ahead, and at least share of it.
func (p Preference) Lean(share float64) (string, bool) {
	if p.Insufficient || p.Conflicting || p.Top == "" || p.Shares[p.Top] < share {
		return "", false
	}
	return p.Top, true
}

// Summary is the snapshot as a few lines for a model to read.
func (s Snapshot) Summary() string {
	var b strings.Builder
	for _, p := range []Preference{s.Similar, s.App, s.All} {
		fmt.Fprintf(&b, "%s：%s\n", p.Scope, p.describe())
	}
	if len(s.Recent) > 0 {
		fmt.Fprintf(&b, "最近明确的选择：%s\n", strings.Join(s.Recent, "、"))
	}
	if len(s.Corrections) > 0 {
		var cs []string
		for _, c := range s.Corrections {
			cs = append(cs, c.From+"→"+c.To)
		}
		fmt.Fprintf(&b, "在这个应用里的改正：%s\n", strings.Join(cs, "、"))
	}
	return strings.TrimSpace(b.String())
}

func (p Preference) describe() string {
	if len(p.Shares) == 0 {
		return "没有记录"
	}
	ids := make([]string, 0, len(p.Shares))
	for id := range p.Shares {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return p.Shares[ids[i]] > p.Shares[ids[j]] || p.Shares[ids[i]] == p.Shares[ids[j]] && ids[i] < ids[j]
	})
	var parts []string
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", id, 100*p.Shares[id]))
	}
	note := fmt.Sprintf("（%d 次明确选择", p.Samples)
	if p.Insufficient {
		note += "，证据不足"
	}
	if p.Conflicting {
		note += "，偏好不一致"
	}
	return strings.Join(parts, "，") + note + "）"
}
