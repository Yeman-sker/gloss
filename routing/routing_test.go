package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"gloss/core"
	"gloss/memory"
)

var builtin = []core.Descriptor{
	{ID: "translate", Name: "翻译", Purpose: "把外语文字翻译成用户的语言"},
	{ID: "explain", Name: "释义", Purpose: "解释一个词或一句话在上下文里的意思"},
	{ID: "prompt", Name: "优化提示词", Purpose: "改写用户写给 AI 的提示词"},
	{ID: "agent", Name: "整理输出", Purpose: "把 Agent 的长输出整理得好读"},
}

// Sample is one case of testdata/samples.json.
type Sample struct {
	ID, Text, App, Editable, Expect, Note string
}

func LoadSamples(t testing.TB) []Sample {
	raw, err := os.ReadFile("testdata/samples.json")
	if err != nil {
		t.Fatal(err)
	}
	var s []Sample
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func input(s Sample) Input {
	return Input{Text: s.Text, Shape: memory.ShapeOf(s.Text), App: s.App, Editable: core.ParseEditable(s.Editable),
		TargetLang: "zh-Hans", Candidates: builtin, Default: "translate"}
}

// TestSamples runs the rules on the fixed samples. Run with -v for the
// table; docs/research/2026-11-05-jev.md records the numbers.
func TestSamples(t *testing.T) {
	var decided, right, wrong, ambiguousDecided, clear int
	var slowest time.Duration
	for _, s := range LoadSamples(t) {
		d := Router{}.Route(context.Background(), input(s))
		slowest = max(slowest, d.Elapsed)
		mark := ""
		if s.Expect != "ambiguous" {
			clear++
		}
		if d.By == memory.ByRule {
			decided++
			switch {
			case s.Expect == "ambiguous":
				ambiguousDecided++
				mark = "（歧义样例被规则判定）"
			case d.ID == s.Expect:
				right++
			default:
				wrong++
				mark = "✗"
			}
		}
		t.Logf("%-4s 期望 %-9s → %-9s %-8s %s %s", s.ID, s.Expect, d.ID, d.By, mark, d.Reason)
	}
	t.Logf("%d 个明确样例：规则判对 %d、判错 %d、未判定 %d；%d 个歧义样例中 %d 个被规则判定；最慢 %v",
		clear, right, wrong, clear-right-wrong, len(LoadSamples(t))-clear, ambiguousDecided, slowest)
	_ = decided
	if wrong > 1 {
		t.Errorf("rules decided %d clear samples wrongly", wrong)
	}
	if right < clear*2/3 {
		t.Errorf("rules decided only %d of %d clear samples", right, clear)
	}
	if slowest > 5*time.Millisecond {
		t.Errorf("routing took %v", slowest)
	}
}

func TestNoSingleSignalDecides(t *testing.T) {
	for name, in := range map[string]Input{
		"agent":     {Text: "OK, that works for me. See you then.", App: "com.anthropic.claudefordesktop", Editable: core.EditableUnknown},
		"prompt":    {Text: "周五之前把报销单交给财务", Editable: core.EditableYes},
		"translate": {Text: "Proceed with the migration once the backup finishes.", Editable: core.EditableYes},
	} {
		in.Candidates, in.Default, in.TargetLang = builtin, "explain", "zh-Hans"
		if d := (Router{}).Route(context.Background(), in); d.By == memory.ByRule && d.ID == name {
			t.Errorf("one signal made the rules pick %s: %s", d.ID, d.Reason)
		}
	}
}

func TestFallbacks(t *testing.T) {
	amb := Input{Text: "明天下午三点开会", Editable: core.EditableYes, App: "com.apple.Notes", TargetLang: "zh-Hans", Candidates: builtin, Default: "explain"}
	if d := (Router{}).Route(context.Background(), amb); d.ID != "explain" || d.By != memory.ByDefault {
		t.Errorf("empty memory: %+v", d)
	}
	amb.Prefs.Recent = []string{"gone", "prompt"}
	if d := (Router{}).Route(context.Background(), amb); d.ID != "prompt" || d.By != memory.ByRecent {
		t.Errorf("recent: %+v", d)
	}
	// Contradicting preferences do not decide; a clear one does.
	amb.Prefs.App = memory.Preference{Scope: "app", Top: "agent", Shares: map[string]float64{"agent": 0.5, "prompt": 0.45}, Samples: 9, Conflicting: true}
	if d := (Router{}).Route(context.Background(), amb); d.By != memory.ByRecent {
		t.Errorf("conflicting preference decided: %+v", d)
	}
	amb.Prefs.App = memory.Preference{Scope: "app", Top: "agent", Shares: map[string]float64{"agent": 0.8}, Samples: 9}
	if d := (Router{}).Route(context.Background(), amb); d.ID != "agent" || d.By != memory.ByPreference {
		t.Errorf("preference: %+v", d)
	}
	// A disabled feature is never returned, even when preferred.
	amb.Candidates = builtin[:2]
	if d := (Router{}).Route(context.Background(), amb); d.ID == "agent" || d.ID == "prompt" {
		t.Errorf("returned a disabled feature: %+v", d)
	}
	// One candidate, no routing.
	if d := (Router{}).Route(context.Background(), Input{Text: "x", Candidates: builtin[1:2], Default: "translate"}); d.ID != "explain" {
		t.Errorf("single: %+v", d)
	}
}

type fakeJudge struct {
	v     Verdict
	err   error
	delay time.Duration
	asked int
}

func (f *fakeJudge) Judge(ctx context.Context, _ Input) (Verdict, error) {
	f.asked++
	select {
	case <-time.After(f.delay):
		return f.v, f.err
	case <-ctx.Done():
		return Verdict{}, ctx.Err()
	}
}

func TestJev(t *testing.T) {
	amb := Input{Text: "明天下午三点开会", Editable: core.EditableYes, TargetLang: "zh-Hans", Candidates: builtin, Default: "translate"}
	clearCase := Input{Text: "serendipity", Editable: core.EditableNo, TargetLang: "zh-Hans", Candidates: builtin, Default: "translate"}
	for name, tt := range map[string]struct {
		j      *fakeJudge
		in     Input
		id, by string
		asked  int
	}{
		"rules first":  {&fakeJudge{v: Verdict{Choice: "agent", Confidence: 1}}, clearCase, "translate", memory.ByRule, 0},
		"taken":        {&fakeJudge{v: Verdict{Choice: "prompt", Confidence: 0.9}}, amb, "prompt", memory.ByJev, 1},
		"unsure":       {&fakeJudge{v: Verdict{Choice: "prompt", Confidence: 0.3}}, amb, "translate", memory.ByDefault, 1},
		"not a choice": {&fakeJudge{v: Verdict{Choice: "明天下午三点开会", Confidence: 1}}, amb, "translate", memory.ByDefault, 1},
		"fails":        {&fakeJudge{err: errors.New("401: 明天下午三点开会")}, amb, "translate", memory.ByDefault, 1},
		"slow":         {&fakeJudge{v: Verdict{Choice: "prompt", Confidence: 1}, delay: time.Second}, amb, "translate", memory.ByDefault, 1},
	} {
		start := time.Now()
		d := Router{Judge: tt.j, Wait: 60 * time.Millisecond, MinConfidence: 0.6}.Route(context.Background(), tt.in)
		if d.ID != tt.id || d.By != tt.by || tt.j.asked != tt.asked {
			t.Errorf("%s: %+v, asked %d", name, d, tt.j.asked)
		}
		if time.Since(start) > 300*time.Millisecond {
			t.Errorf("%s: waited %v", name, time.Since(start))
		}
		if tt.by == memory.ByDefault && d.JevError == "" {
			t.Errorf("%s: no reason for leaving Jev out", name)
		}
		if strings.Contains(d.JevError, "三点") {
			t.Errorf("%s: the reason quotes the text: %q", name, d.JevError)
		}
	}
}

func TestJevClient(t *testing.T) {
	var got jevRequest
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/systemone" {
			t.Errorf("path %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, `{"model":"jev-1.14.0","answers":{"feature":{"type":"choice","choice":"explain","confidence":0.82,"probabilities":{"explain":0.9,"translate":0.1}}}}`)
	}))
	defer srv.Close()
	in := input(Sample{Text: strings.Repeat("长", 3000), App: "com.example", Editable: "no"})
	in.Title = "Secret Project — Notes"
	j := Jev{BaseURL: srv.URL + "/v1", APIKey: "k"}
	v, err := j.Judge(context.Background(), in)
	if err != nil || v.Choice != "explain" || v.Confidence != 0.82 || v.Probabilities["translate"] != 0.1 {
		t.Fatalf("v = %+v, %v", v, err)
	}
	if auth != "Bearer k" || got.Model != "jev-latest" || got.Questions["feature"].Type != "choice" || len(got.Questions["feature"].Criteria) != 4 {
		t.Errorf("request = %+v", got)
	}
	if strings.Contains(got.State, "Secret Project") {
		t.Error("sent the window title without leave")
	}
	if n := len([]rune(got.State)); n > maxSent+600 {
		t.Errorf("sent %d runes", n)
	}
	j.SendTitle = true
	j.Judge(context.Background(), in)
	if !strings.Contains(got.State, "Secret Project") {
		t.Error("title not sent when allowed")
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }))
	defer bad.Close()
	if _, err := (Jev{BaseURL: bad.URL, APIKey: "k"}).Judge(context.Background(), in); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("401: %v", err)
	}
	if _, err := (Jev{BaseURL: bad.URL}).Judge(context.Background(), in); err == nil {
		t.Error("no key accepted")
	}
}
