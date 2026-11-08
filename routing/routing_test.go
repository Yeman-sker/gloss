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
	ID, Text, Before, After, App, Editable, Expect, Note string
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
		Before: s.Before, After: s.After, TargetLang: "zh-Hans", Candidates: builtin, Default: "translate"}
}

// TestSamples runs the rules on the fixed samples. Run with -v for the
// table; docs/research/2026-11-05-jev.md records the numbers.
func TestSamples(t *testing.T) {
	var decided, right, wrong, ambiguousDecided, clear, ctxSamples, ctxRight int
	var slowest time.Duration
	for _, s := range LoadSamples(t) {
		d := Router{}.Route(context.Background(), input(s))
		slowest = max(slowest, d.Elapsed)
		// The rules do not read the text around a selection: samples
		// that need it are for Jev, and only reported here.
		if s.Before+s.After != "" {
			ctxSamples++
			if d.By == memory.ByRule && d.ID == s.Expect {
				ctxRight++
			}
			t.Logf("%-4s 期望 %-9s → %-9s %-8s（带前后文，规则看不到）", s.ID, s.Expect, d.ID, d.By)
			continue
		}
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
	t.Logf("%d 个明确样例：规则判对 %d、判错 %d、未判定 %d；%d 个歧义样例中 %d 个被规则判定；带前后文的 %d 个样例规则判对 %d；最慢 %v",
		clear, right, wrong, clear-right-wrong, len(LoadSamples(t))-clear-ctxSamples, ambiguousDecided, ctxSamples, ctxRight, slowest)
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

func TestJevFirst(t *testing.T) {
	clearCase := Input{Text: "serendipity", Editable: core.EditableNo, TargetLang: "zh-Hans", Candidates: builtin, Default: "translate"}
	j := &fakeJudge{v: Verdict{Choice: "explain", Confidence: 0.9}}
	d := Router{Judge: j, JevFirst: true, MinConfidence: 0.6}.Route(context.Background(), clearCase)
	if d.ID != "explain" || d.By != memory.ByJev || j.asked != 1 {
		t.Errorf("Jev first: %+v, asked %d", d, j.asked)
	}
	// When Jev cannot be used, the rules still decide, and say why.
	j = &fakeJudge{err: errors.New("down")}
	d = Router{Judge: j, JevFirst: true, MinConfidence: 0.6}.Route(context.Background(), clearCase)
	if d.ID != "translate" || d.By != memory.ByRule || d.JevError == "" || j.asked != 1 {
		t.Errorf("fallback to rules: %+v, asked %d", d, j.asked)
	}
	// Jev is asked once, not again after the rules.
	amb := Input{Text: "明天下午三点开会", Editable: core.EditableYes, TargetLang: "zh-Hans", Candidates: builtin, Default: "explain"}
	j = &fakeJudge{v: Verdict{Choice: "prompt", Confidence: 0.2}}
	d = Router{Judge: j, JevFirst: true, MinConfidence: 0.6}.Route(context.Background(), amb)
	if d.By != memory.ByDefault || j.asked != 1 {
		t.Errorf("unsure Jev: %+v, asked %d", d, j.asked)
	}
}

func TestRichState(t *testing.T) {
	in := Input{Text: "rug pull", Before: strings.Repeat("前", 500) + "这个项目又是一个", After: "，币价一夜归零" + strings.Repeat("后", 500),
		App: "com.tinyspeck.slackmacgap", Title: "Secret Project", Editable: core.EditableNo, TargetLang: "zh-Hans", Candidates: builtin, Default: "translate",
		Prefs: memory.Snapshot{
			App:         memory.Preference{Scope: "相同应用", Shares: map[string]float64{"explain": 0.75, "translate": 0.25}, Samples: 4},
			Corrections: []memory.Correction{{From: "translate", To: "explain"}},
		}}
	basic, rich := Jev{}.State(in), Jev{Rich: true}.State(in)
	if strings.Contains(basic, "币价") {
		t.Error("the basic state sent the text around the selection")
	}
	for _, want := range []string{"一个[SELECTION]，币价", "Slack (a chat app)", "read-only", "a language other than the user's",
		"translate 0.", "explain 0.", "same app: explain 75%, translate 25% (4 explicit choices)", "translate → explain"} {
		if !strings.Contains(rich, want) {
			t.Errorf("rich state lacks %q:\n%s", want, rich)
		}
	}
	if strings.Contains(rich, "Secret Project") || strings.Contains(rich, "相同应用") {
		t.Errorf("rich state sent the title or a Chinese scope:\n%s", rich)
	}
	if strings.Count(rich, "前") > maxAround || strings.Count(rich, "后") > maxAround {
		t.Error("the text around the selection is not bounded")
	}
	if s := (Jev{Rich: true, NoHints: true}).State(in); strings.Contains(s, "keyword rules") {
		t.Error("NoHints sent the rules' scores")
	}
	if c := (Jev{Rich: true}).criteria(in); !strings.Contains(c["explain"], "foreign term used inside") {
		t.Errorf("rich criteria lack guidance: %q", c["explain"])
	}
	if c := (Jev{}).criteria(in); c["explain"] != "释义：解释一个词或一句话在上下文里的意思" {
		t.Errorf("basic criteria changed: %q", c["explain"])
	}
	if s := (Jev{Rich: true}).State(Input{Text: "x", Candidates: builtin}); !strings.Contains(s, "not available") || !strings.Contains(s, "none yet") {
		t.Errorf("empty context: %s", s)
	}
}
