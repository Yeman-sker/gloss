package app

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gloss/core"
)

func TestIsWord(t *testing.T) {
	for s, want := range map[string]bool{
		"gloss": true, "注释": true, "a word": false, "这是一个完整的句子": false, "": false,
	} {
		if got := isWord(s); got != want {
			t.Errorf("isWord(%q) = %v", s, got)
		}
	}
}

func TestTokenLenKeepsWordsWhole(t *testing.T) {
	if n := tokenLen([]rune("don't stop")); n != 5 {
		t.Errorf("Latin token = %d", n)
	}
	if n := tokenLen([]rune("注释")); n != 1 {
		t.Errorf("CJK token = %d", n)
	}
}

func TestPointerZones(t *testing.T) {
	p := panel{done: true, surfaceW: 420, textTop: 66}
	for _, tt := range []struct {
		x, y float32
		want ui.Cursor
	}{
		{200, 30, ui.CursorGrab},     // the source
		{200, 100, ui.CursorPointer}, // the translation
		{415, 140, ui.CursorPointer}, // its padding and the corner cue
		{2, 140, ui.CursorPointer},   // the left edge
	} {
		if got := p.pointerAt(tt.x, tt.y); got != tt.want {
			t.Errorf("at %v,%v: %v", tt.x, tt.y, got)
		}
	}
}

func TestPointerBoundaryLags(t *testing.T) {
	p := panel{done: true, surfaceW: 420, textTop: 66}
	if p.pointerAt(200, 67); p.inCopy {
		t.Error("entered before passing the boundary")
	}
	p.pointerAt(200, 70)
	if p.pointerAt(200, 65); !p.inCopy {
		t.Error("left before passing the boundary")
	}
	if p.pointerAt(200, 62); p.inCopy {
		t.Error("did not leave")
	}
}

func TestClampWidth(t *testing.T) {
	if w := clampWidth(10, widthMinPanel, widthPanel); w != widthMinPanel {
		t.Errorf("short = %v", w)
	}
	if w := clampWidth(9999, widthMinPanel, widthPanel); w != widthPanel {
		t.Errorf("long = %v", w)
	}
}

func TestCheckIconDrawsAlongItsStroke(t *testing.T) {
	for _, q := range []float64{0, 0.2, 0.5, 1} {
		if checkIcon(q) == nil {
			t.Errorf("q=%v", q)
		}
	}
}

func TestPanelOpensOnFirstDeltaAndFinishesPaced(t *testing.T) {
	var p panel
	p.reset("hello")
	p.lastFrame = time.Now().Add(-time.Second)
	if !p.capsule {
		t.Fatal("starts as a capsule")
	}
	p.apply(core.Event{Run: 1, Type: core.EventDelta, Kind: core.BlockText, Delta: "你好"})
	p.apply(core.Event{Run: 1, Type: core.EventDone})
	if p.capsule {
		t.Fatal("first delta opens it")
	}
	p.release(time.Now())
	if p.done {
		t.Fatal("not done while text fades in")
	}
	p.release(time.Now().Add(time.Second))
	if !p.done || p.text() != "你好" {
		t.Fatalf("done=%v text=%q", p.done, p.text())
	}
}

func TestPanelFailureOpensCapsule(t *testing.T) {
	var p panel
	p.reset("hello")
	p.apply(core.Event{Run: 1, Type: core.EventError, Err: errors.New("boom")})
	if p.capsule || !p.done || p.failed != "boom" {
		t.Fatalf("%+v", p.failed)
	}
}

func TestSpringSettlesAtTarget(t *testing.T) {
	var s spring
	s.jump(0)
	s.velocity = 1000
	s.set(100)
	for i := 0; i < 200 && s.moving(); i++ {
		s.step(1.0/60, false)
	}
	if s.value != 100 {
		t.Fatalf("value = %v", s.value)
	}
}

func TestSessionAcceptsOnlyItsLatestRun(t *testing.T) {
	s := &session{}
	s.begin("translate", "rule", 3, time.Now())
	if !s.accepts(3) || s.accepts(2) || s.accepts(0) {
		t.Fatal("accepts the wrong runs")
	}
	s.begin("explain", "manual", 4, time.Now())
	if s.accepts(3) {
		t.Fatal("an old run still shows")
	}
	s.closed = true
	if s.accepts(4) {
		t.Fatal("a closed session shows")
	}
}

func TestSessionLateRoutingDoesNotOverrideTheUser(t *testing.T) {
	now := time.Now()
	s := &session{start: now}
	if !s.choose("prompt", now) {
		t.Fatal("first choice")
	}
	if s.decide("translate", "rule", "", nil, 0) {
		t.Fatal("routing replaced the user's choice")
	}
	if s.initial != "prompt" || s.initialBy != "manual" {
		t.Fatalf("initial = %s by %s", s.initial, s.initialBy)
	}
}

func TestSessionRecordsCorrectionsAndNoText(t *testing.T) {
	now := time.Now()
	s := &session{id: "x", start: now, src: source{text: "secret words", raw: "secret words"}}
	s.decide("translate", "rule", "规则", nil, time.Millisecond)
	s.begin("translate", "rule", 1, now)
	s.choose("explain", now.Add(time.Second))
	s.begin("explain", "manual", 2, now.Add(time.Second))
	s.observe(core.Event{Run: 2, Type: core.EventDelta, Delta: "释义"}, now.Add(1500*time.Millisecond))
	s.observe(core.Event{Run: 2, Type: core.EventDone}, now.Add(2*time.Second))
	r := s.record(now.Add(3 * time.Second))
	if r.Initial != "translate" || r.Final != "explain" || len(r.Corrections) != 1 || r.Outcome != "done" {
		t.Fatalf("%+v", r)
	}
	if r.Timings.ExtraWaitMs != 500 {
		t.Fatalf("extra wait = %d", r.Timings.ExtraWaitMs)
	}
	if strings.Contains(fmt.Sprintf("%+v", r), "secret") {
		t.Fatal("the record holds the text")
	}
}

func TestPanelMarkdownIsDoneWithTheRun(t *testing.T) {
	var p panel
	p.reset("long text")
	p.beginRun(1, feature{"agent", "整理输出"}, "rule", "", core.BlockMarkdown, false)
	p.apply(core.Event{Run: 1, Type: core.EventDelta, Kind: core.BlockMarkdown, Delta: "# 标题\n\n- 一\n"})
	if p.done || !p.reading {
		t.Fatal("streams in the reading layout")
	}
	p.apply(core.Event{Run: 1, Type: core.EventDone})
	if !p.done || p.text() != "# 标题\n\n- 一\n" {
		t.Fatalf("done=%v text=%q", p.done, p.text())
	}
}

func TestPanelSwitchClearsTheResult(t *testing.T) {
	var p panel
	p.reset("hello")
	p.features = []feature{{"translate", "翻译"}, {"explain", "释义"}}
	p.beginRun(1, feature{"translate", "翻译"}, "rule", "", core.BlockText, false)
	p.apply(core.Event{Run: 1, Type: core.EventDelta, Kind: core.BlockText, Delta: "你好"})
	p.beginRun(2, feature{"explain", "释义"}, "manual", "", core.BlockText, false)
	if len(p.blocks) != 0 || p.capsule || !p.waiting() || p.failedTitle != "释义失败" {
		t.Fatalf("blocks=%d capsule=%v waiting=%v", len(p.blocks), p.capsule, p.waiting())
	}
	if !p.footer() {
		t.Fatal("two features show the footer")
	}
}

func TestPointerOnFooterIsNotCopy(t *testing.T) {
	p := panel{done: true, surfaceW: 420, textTop: 66, footerTop: 150, features: []feature{{"a", "A"}, {"b", "B"}}}
	if c := p.pointerAt(200, 160); c != ui.CursorDefault || p.inCopy {
		t.Fatalf("footer cursor %v, inCopy %v", c, p.inCopy)
	}
}

// The config's order decides the candidates and so the default: the first.
func TestCandidatesFollowTheConfig(t *testing.T) {
	descs := []core.Descriptor{{ID: "translate"}, {ID: "explain"}, {ID: "prompt"}, {ID: "agent"}}
	var ids []string
	for _, d := range ordered(descs, []string{"prompt", "translate", "gone"}) {
		ids = append(ids, d.ID)
	}
	if strings.Join(ids, ",") != "prompt,translate" {
		t.Errorf("candidates = %v", ids)
	}
	if got := ordered(descs, nil); len(got) != 1 || got[0].ID != "translate" {
		t.Errorf("with none on: %v", got)
	}
}

func TestPanelFailureOffersTheFix(t *testing.T) {
	var p panel
	fixed := false
	p.explain = func(err error) (string, string, func()) {
		return "还没有设置模型服务", "设置模型服务", func() { fixed = true }
	}
	p.reset("hello")
	p.apply(core.Event{Run: 1, Type: core.EventError, Err: errors.New("raw")})
	if p.failed != "还没有设置模型服务" {
		t.Fatalf("failed = %q", p.failed)
	}
	tt := ui.NewTester(p.failure, 420, 120)
	if err := tt.Click("设置模型服务"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	if !fixed {
		t.Error("the fix did not run")
	}
	p.reset("again")
	if p.fixDo != nil {
		t.Error("the fix outlived its run")
	}
}

// footerPanel has everything the footer can show, with the longest name on.
func footerPanel() *panel {
	return &panel{
		done: true, current: "prompt", by: "rule", why: "规则判断", replaceable: true,
		footerTop: -1000, output: core.BlockText, wantCursor: ui.CursorDefault,
		features: []feature{{"translate", "翻译"}, {"explain", "释义"}, {"prompt", "优化提示词"}, {"agent", "整理输出"}},
		replace:  func() {},
		copied:   func(*ui.Context, string) {},
	}
}

func countText(tt *ui.Tester, s string) int {
	n := 0
	for _, t := range tt.Texts() {
		if t == s {
			n++
		}
	}
	return n
}

// pointAt puts the panel's pointer on a spot, as the shell's ticks read it.
func pointAt(t *testing.T, p *panel, key string) {
	t.Helper()
	for _, s := range p.spots {
		if s.key == key {
			x, y := s.box.X+s.box.W/2-margin, s.box.Y+s.box.H/2-margin
			p.local = func() (float32, float32) { return x, y }
			p.track()
			return
		}
	}
	t.Fatalf("no spot %q in %+v", key, p.spots)
}

func footerTester(p *panel, width int) *ui.Tester {
	tt := ui.NewTester(func(c *ui.Context) {
		p.spots = p.spots[:0]
		p.footerRow(c, float32(width), c.Now())
		p.tipView(c, c.Now(), float32(width), 200)
	}, width, 200)
	tt.Advance(time.Second)
	tt.Frame() // Bounds are the last frame's: the spots are good from this one.
	return tt
}

// The one feature showing wears its name; the others are named for those who
// cannot see their icons, and answer a click all the same.
func TestFooterFeaturesAreIconsWithNames(t *testing.T) {
	p := footerPanel()
	var chose string
	p.switchTo = func(id string) { chose = id }
	tt := footerTester(p, 420)
	if got := countText(tt, "优化提示词"); got != 2 {
		t.Errorf("the one showing: label and text are %d, %q", got, tt.Texts())
	}
	if got := countText(tt, "释义"); got != 1 {
		t.Errorf("another: label only is %d, %q", got, tt.Texts())
	}
	if err := tt.Click("释义"); err != nil {
		t.Fatal(err)
	}
	if chose != "explain" {
		t.Errorf("chose %q", chose)
	}
}

// At its narrowest the footer still holds everything, side by side.
func TestFooterFitsAtItsNarrowest(t *testing.T) {
	p := footerPanel()
	tt := footerTester(p, int(widthMinFooter))
	last := float32(0)
	for _, s := range []string{"翻译", "释义", "优化提示词", "整理输出", byLabel(p.by), "复制", "替换"} {
		r, ok := tt.Find(s)
		if !ok {
			t.Fatalf("no %q in %q", s, tt.Texts())
		}
		if r.X < last {
			t.Errorf("%q starts at %v, before the last ended at %v", s, r.X, last)
		}
		last = r.X + r.W
	}
	if last > widthMinFooter {
		t.Errorf("ends at %v, past %v", last, widthMinFooter)
	}
}

// mygo reports no hover while Gloss is not active, so the panel's own account
// is what wakes a control and gives it the hand.
func TestFooterHoverIsThePanelsOwn(t *testing.T) {
	p := footerPanel()
	footerTester(p, 420)
	pointAt(t, p, "f-explain")
	if p.hot != "f-explain" || p.wantCursor != ui.CursorPointer {
		t.Errorf("on a feature: hot %q, cursor %v", p.hot, p.wantCursor)
	}
	pointAt(t, p, "by")
	if p.hot != "by" || p.wantCursor != ui.CursorDefault {
		t.Errorf("on what only explains: hot %q, cursor %v", p.hot, p.wantCursor)
	}
	p.local = func() (float32, float32) { return -500, -500 }
	if !p.track() || p.hot != "" {
		t.Errorf("off the footer: hot %q", p.hot)
	}
}

func TestSpotBehindTheFooterIsNotHot(t *testing.T) {
	p := &panel{features: []feature{{"a", "A"}, {"b", "B"}}, footerTop: 100, spots: []spot{{key: "code-0", box: ui.Rect{X: margin, Y: margin + 90, W: 50, H: 30}}}}
	p.pointerAt(10, 95)
	if p.hot != "code-0" {
		t.Errorf("above the footer: hot %q", p.hot)
	}
	p.pointerAt(10, 110)
	if p.hot != "" {
		t.Errorf("behind the footer: hot %q", p.hot)
	}
}

func TestTipWaitsThenStaysWarm(t *testing.T) {
	p := footerPanel()
	tt := footerTester(p, 420)
	on := func(key string) {
		t.Helper()
		pointAt(t, p, key)
		p.hotSince = tt.Now()
		tt.Frame()
	}
	on("f-explain")
	if got := countText(tt, "释义"); got != 1 {
		t.Errorf("before the wait: %d", got)
	}
	tt.Advance(tipDelay + 50*time.Millisecond)
	if got := countText(tt, "释义"); got != 2 {
		t.Errorf("after the wait: %d in %q", got, tt.Texts())
	}
	on("copy") // a control with no tip
	if got := countText(tt, "释义"); got != 1 {
		t.Errorf("it stayed: %d", got)
	}
	on("f-agent") // straight after, along the row
	if got := countText(tt, "整理输出"); got != 2 {
		t.Errorf("warm: %d", got)
	}
	p.dragging = true
	tt.Frame()
	if got := countText(tt, "整理输出"); got != 1 {
		t.Errorf("while dragged: %d", got)
	}
}

// The one showing is named beside its icon, so it needs no tip.
func TestTipNotOnTheFeatureShowing(t *testing.T) {
	p := footerPanel()
	tt := footerTester(p, 420)
	pointAt(t, p, "f-prompt")
	p.hotSince = tt.Now()
	tt.Advance(tipDelay * 2)
	if got := countText(tt, "优化提示词"); got != 2 {
		t.Errorf("%d in %q", got, tt.Texts())
	}
}

// Both windows take their colours from tokens.go. A colour worked out from
// others, as mix does, is not a literal.
func TestColorsComeFromTokens(t *testing.T) {
	literal := regexp.MustCompile(`ui\.(Hex\(|RGBA?\(\s*\d)`)
	for _, name := range []string{"panel.go", "prefs.go", "prefs_ui.go", "app.go", "glyphs.go", "icons.go"} {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if literal.MatchString(line) {
				t.Errorf("%s:%d: %s", name, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// The marker would swallow the white check mygo draws.
func TestCheckedBoxKeepsItsCheck(t *testing.T) {
	if mdTheme().Checkbox == colorMarker {
		t.Error("a checked box in the marker's colour hides its check")
	}
}
