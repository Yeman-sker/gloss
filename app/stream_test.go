package app

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gloss/core"
)

const longMarkdown = `这次改动把面板的流式输出重新做了一遍，主要解决长文本时的抖动问题，也顺带把可读性提了上去。

### 做了什么

- **重写了节奏控制**：文字不再按网络到达的节奏显示，而是用一个平滑的速度释放出来。
- **修掉了上下弹跳**：之前每多一行，内容都会先往上跳一行再滑回来，看起来就是在抖。
- 窗口不再每行都改尺寸，改成按预估尺寸一次到位。

### 结果

1. make check 通过，新增的测试覆盖了跟随滚动、节奏和预估尺寸。
2. 在 macOS 27 上跑了同一段脚本的前后对比。

| 指标 | 改动前 | 改动后 |
|---|---|---|
| 文字反向移动 | 247 次 | 0 次 |

` + "```go\nfunc (p *panel) follow() {\n\tp.heightTo = p.natural\n}\n```" + `

### 需要你做什么

- 用真实服务跑几次长文本，看一下抖动是否消失。
- 确认新的字色和层级是否符合预期。
`

// streamRig runs the panel's real view in a Tester, on the Tester's clock.
type streamRig struct {
	p  *panel
	tt *ui.Tester
	// topY is the window Y of the first thing in the content, frame by frame.
	frames []rigFrame
}

type rigFrame struct {
	w, h, wantW, wantH, scrollY, maxY float32
	pressed, planned                  bool
}

func newRig(t *testing.T, source string, kind core.BlockKind, height int) *streamRig {
	t.Helper()
	if err := registerFonts(); err != nil {
		t.Fatal(err)
	}
	r := &streamRig{p: &panel{maxHeight: float32(height) * 0.6, maxReading: float32(height) * 0.75, wantCursor: ui.CursorDefault}}
	r.p.features = []feature{{"translate", "翻译"}, {"agent", "整理输出"}}
	r.p.copied = func(c *ui.Context, text string) { c.WriteClipboard(text) }
	r.p.local = func() (float32, float32) { return -500, -500 }
	r.p.frame = func(c *ui.Context, w, h, wantW, wantH float32, pressed bool) {
		r.frames = append(r.frames, rigFrame{w, h, wantW, wantH, r.p.scroll.Y, r.p.scroll.MaxY, pressed, r.p.planned})
	}
	r.tt = ui.NewTester(r.p.view(), 900, height)
	r.p.clock = r.tt.Now
	r.p.reset(source)
	r.p.lastFrame = r.tt.Now()
	r.p.entered = true
	r.p.beginRun(1, feature{"agent", "整理输出"}, "rule", "", kind, false)
	r.tt.Frame()
	return r
}

func (r *streamRig) delta(text string) {
	r.p.apply(core.Event{Run: 1, Type: core.EventDelta, Kind: r.p.output, Delta: text})
}

func (r *streamRig) finish() { r.p.apply(core.Event{Run: 1, Type: core.EventDone}) }

// advance runs d of frames, at the Tester's own pace.
func (r *streamRig) advance(d time.Duration) {
	for end := r.tt.Now().Add(d); r.tt.Now().Before(end); {
		r.tt.Advance(8 * time.Millisecond)
		r.tt.Frame()
	}
}

// stream sends text in the bursts a network does, and runs until it has all shown.
func (r *streamRig) stream(t *testing.T, text string, burst int, every time.Duration) {
	t.Helper()
	runes := []rune(text)
	for len(runes) > 0 {
		n := min(burst, len(runes))
		r.delta(string(runes[:n]))
		runes = runes[n:]
		r.advance(every)
	}
	r.finish()
	r.runDone(t)
}

func (r *streamRig) runDone(t *testing.T) {
	t.Helper()
	for i := 0; i < 2000 && !r.p.done; i++ {
		r.tt.Advance(8 * time.Millisecond)
		r.tt.Frame()
	}
	if !r.p.done {
		t.Fatalf("never finished: %d of %d bytes", r.p.mdAt.at, len(r.p.md))
	}
	r.advance(600 * time.Millisecond)
}

// The bug: each new line made the text hop up, because following used the height
// the surface had and not the one it was heading for. While the panel still has
// room, the text must not scroll at all.
func TestTextDoesNotHopWhileThePanelStillGrows(t *testing.T) {
	r := newRig(t, "summarise this", core.BlockMarkdown, 3000)
	r.stream(t, longMarkdown, 40, 120*time.Millisecond)
	for i, f := range r.frames {
		if f.scrollY != 0 {
			t.Fatalf("frame %d: scrolled to %v with the panel %v of %v tall", i, f.scrollY, f.h, r.p.limit())
		}
	}
}

// Once the text outgrows the panel the end stays in view, moving only one way.
func TestFollowingMovesOneWayOnceTheTextOutgrowsThePanel(t *testing.T) {
	r := newRig(t, "summarise this", core.BlockMarkdown, 500)
	r.stream(t, strings.Repeat(longMarkdown, 3), 60, 80*time.Millisecond)
	last := float32(0)
	moved := false
	for i, f := range r.frames {
		if f.scrollY < last-0.5 {
			t.Fatalf("frame %d: went back from %v to %v", i, last, f.scrollY)
		}
		if f.scrollY > 0 {
			moved = true
		}
		last = f.scrollY
	}
	if !moved {
		t.Fatal("never followed")
	}
	if end := r.frames[len(r.frames)-1]; end.scrollY < end.maxY-1 {
		t.Errorf("the end is out of view: %v of %v", end.scrollY, end.maxY)
	}
}

// The frame holds still: once it is decided, the width never changes, however the
// text goes on.
func TestWidthHoldsOnceDecided(t *testing.T) {
	for _, tc := range []struct {
		name, source, result string
		kind                 core.BlockKind
	}{
		{"long translation", strings.Repeat("A long sentence to translate. ", 40), strings.Repeat("这是一段很长的译文，用来观察宽度。", 60), core.BlockText},
		{"short source, long result", "gloss", strings.Repeat("释义比预想的长很多，超出了一行。", 30), core.BlockText},
		{"markdown", "what is this", longMarkdown, core.BlockMarkdown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, tc.source, tc.kind, 900)
			r.stream(t, tc.result, 30, 90*time.Millisecond)
			var width float32
			for i, f := range r.frames {
				if !f.planned {
					continue
				}
				if width == 0 {
					width = f.wantW
				}
				if width != 0 && f.wantW != width {
					t.Fatalf("frame %d: the width went from %v to %v", i, width, f.wantW)
				}
			}
			if width == 0 {
				t.Fatal("never opened")
			}
		})
	}
}

// A paragraph's break takes the same room before and after it settles, so that
// the page never gives back a line it took.
func TestPlainTextHeightNeverGivesBack(t *testing.T) {
	var text strings.Builder
	for i := range 6 {
		fmt.Fprintf(&text, "第 %d 段。%s\n\n", i, strings.Repeat("这是译文的一段内容，长到需要换行才放得下。", 4))
	}
	r := newRig(t, strings.Repeat("Paragraph to translate. ", 30), core.BlockText, 3000)
	r.stream(t, strings.TrimSpace(text.String()), 24, 90*time.Millisecond)
	var natural float32
	for i, f := range r.frames {
		if !f.planned {
			continue
		}
		if i > len(r.frames)-100 {
			break // the run is over: the frame may now fit what came
		}
		if f.wantH < natural-0.5 {
			t.Fatalf("frame %d: the panel asked for less, %v after %v", i, f.wantH, natural)
		}
		natural = max(natural, f.wantH)
	}
	if len(r.p.blocks[0].paras) != 5 {
		t.Errorf("paragraphs: %d", len(r.p.blocks[0].paras))
	}
}

// A wheel gets the page while it streams: it stops following, and scrolling back
// to the end starts it again. Following used to win, every frame.
func TestWheelTakesThePageFromFollowing(t *testing.T) {
	r := newRig(t, "summarise this", core.BlockMarkdown, 500)
	long := strings.Repeat(longMarkdown, 4)
	sent := len(long) / 2
	r.delta(long[:sent])
	for i := 0; i < 2000 && r.p.mdAt.at < sent; i++ {
		r.advance(8 * time.Millisecond)
	}
	r.advance(600 * time.Millisecond)
	if r.p.scroll.Y <= 0 || !r.p.following {
		t.Fatalf("not following: y %v, following %v", r.p.scroll.Y, r.p.following)
	}
	at := r.p.scroll.Y
	r.tt.Wheel(450, 150, 0, -60)
	r.advance(400 * time.Millisecond)
	if r.p.following || r.p.scroll.Y >= at {
		t.Fatalf("the wheel did not take it: y %v from %v, following %v", r.p.scroll.Y, at, r.p.following)
	}
	stayed := r.p.scroll.Y
	r.delta(long[sent : sent+400])
	r.advance(800 * time.Millisecond)
	if d := r.p.scroll.Y - stayed; d > 0.5 || d < -0.5 {
		t.Errorf("the page moved under the reader, by %v", d)
	}
	r.tt.Wheel(450, 150, 0, 100000)
	r.advance(600 * time.Millisecond)
	if !r.p.following {
		t.Errorf("back at the end but not following: y %v of %v", r.p.scroll.Y, r.p.scroll.MaxY)
	}
}

func center(b ui.Rect) (float32, float32) { return b.X + b.W/2, b.Y + b.H/2 }

// Dragging over the result selects it. A plain click on it does nothing at all:
// copying is the button's, and the panel does not move for either.
func TestPressOnTextOnlySelects(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind core.BlockKind
	}{
		{"text", core.BlockText},
		{"markdown", core.BlockMarkdown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "hello", tc.kind, 900)
			const result = "这是一段可以选中的结果文字，用来确认拖动是选中而不是移动面板，也不会复制。"
			r.tt.SetClipboard("before")
			r.stream(t, result, 200, 50*time.Millisecond)
			x, y := center(r.p.textBox)
			if r.p.textBox.W == 0 {
				t.Fatal("no text box")
			}
			// A plain click.
			r.frames = nil
			r.tt.ClickAt(x, y)
			r.advance(100 * time.Millisecond)
			if r.tt.Clipboard() != "before" || !r.p.copiedAt.IsZero() {
				t.Errorf("a click copied: %q", r.tt.Clipboard())
			}
			// A drag.
			left := r.p.textBox.X + 2
			r.tt.Press(left, y)
			for i := range 20 {
				r.tt.Move(left+float32(i)*8, y)
				r.tt.Advance(8 * time.Millisecond)
				r.tt.Frame()
			}
			r.tt.Release(left+160, y)
			r.advance(50 * time.Millisecond)
			for _, f := range r.frames {
				if f.pressed {
					t.Fatal("the press went to moving the panel")
				}
			}
			if r.p.picked == "" || !strings.Contains(result, r.p.picked) {
				t.Fatalf("selected %q", r.p.picked)
			}
			if r.tt.Clipboard() != "before" {
				t.Errorf("selecting copied: %q", r.tt.Clipboard())
			}
			if !r.tt.HasText("复制选中") {
				t.Errorf("the button does not say what it will do: %q", r.tt.Texts())
			}
			picked := r.p.picked
			if err := r.tt.Click("复制选中"); err != nil {
				t.Fatal(err)
			}
			if r.tt.Clipboard() != picked {
				t.Errorf("copied %q, selected %q", r.tt.Clipboard(), picked)
			}
		})
	}
}

// Off the text, a press moves the panel.
func TestPressOffTextMoves(t *testing.T) {
	r := newRig(t, "hello", core.BlockMarkdown, 900)
	r.stream(t, "结果文字", 200, 50*time.Millisecond)
	r.frames = nil
	r.tt.Press(margin+6, margin+6)
	r.tt.Advance(8 * time.Millisecond)
	r.tt.Frame()
	pressed := false
	for _, f := range r.frames {
		pressed = pressed || f.pressed
	}
	r.tt.Release(margin+6, margin+6)
	if !pressed {
		t.Error("the padding did not take the press")
	}
}

// The copy button answers in place and turns back; the whole result is what it takes.
func TestCopyButtonAnswersAndTurnsBack(t *testing.T) {
	r := newRig(t, "hello", core.BlockText, 900)
	const result = "完整的结果文字"
	r.stream(t, result, 200, 50*time.Millisecond)
	if err := r.tt.Click("复制"); err != nil {
		t.Fatalf("%v: %q", err, r.tt.Texts())
	}
	if r.tt.Clipboard() != result {
		t.Errorf("copied %q", r.tt.Clipboard())
	}
	r.advance(300 * time.Millisecond)
	if !r.tt.HasText("已") {
		t.Errorf("no answer: %q", r.tt.Texts())
	}
	done, _ := r.tt.Find("已")
	if done.X < 0 {
		t.Errorf("off the panel: %+v", done)
	}
	// While it answers the button is 已复制, so this finds the word 复制 in it.
	word, _ := r.tt.Find("复制")
	r.advance(copiedHold + 500*time.Millisecond)
	if r.tt.HasText("已") {
		t.Errorf("did not turn back: %q", r.tt.Texts())
	}
	// Turned back, it finds the button, which ends where the word and its padding do.
	button, _ := r.tt.Find("复制")
	if abs32(button.X+button.W-8-(word.X+word.W)) > 0.5 {
		t.Errorf("the word moved: it ended at %v, the button now ends at %v", word.X+word.W, button.X+button.W)
	}
}

// With one feature there is still somewhere to copy from: the button is the only
// way now, so it has to be there.
func TestOneFeatureStillHasTheCopyButton(t *testing.T) {
	r := newRig(t, "hello", core.BlockText, 900)
	r.p.features = r.p.features[:1]
	r.stream(t, "结果", 200, 50*time.Millisecond)
	if !r.tt.HasText("复制") {
		t.Errorf("no copy button: %q", r.tt.Texts())
	}
}

// Closing is a motion to see: the surface folds and fades over exitTime, and the
// text keeps its width while it does.
func TestClosingFoldsAndThenEnds(t *testing.T) {
	r := newRig(t, "hello", core.BlockMarkdown, 900)
	r.stream(t, longMarkdown, 200, 50*time.Millisecond)
	closed := 0
	r.p.closed = func() { closed++ }
	natural := r.p.natural
	open := r.frames[len(r.frames)-1]
	r.p.closing = true
	r.frames = nil
	start := r.tt.Now()
	var ended time.Time
	for r.tt.Now().Sub(start) < exitTime*3 {
		r.tt.Advance(4 * time.Millisecond)
		r.tt.Frame()
		if closed > 0 && ended.IsZero() {
			ended = r.tt.Now()
		}
	}
	if ended.IsZero() {
		t.Fatal("never ended")
	}
	if d := ended.Sub(start); d < exitTime*7/10 || d > exitTime*3/2 {
		t.Errorf("took %v, want about %v", d, exitTime)
	}
	if natural != r.p.natural {
		t.Errorf("the text rewrapped: %v from %v", r.p.natural, natural)
	}
	mid := r.frames[len(r.frames)/4]
	if mid.h >= open.h || mid.h <= heightCapsule {
		t.Errorf("not folding: %v from %v", mid.h, open.h)
	}
}

func TestPacerClearsABacklogSmoothly(t *testing.T) {
	var s pacer
	n := 1200
	dt := 1.0 / 160
	elapsed := 0.0
	last := 0.0
	for n > 0 && elapsed < 10 {
		out := s.step(n, false, dt)
		n -= out
		elapsed += dt
		if last > 0 && math.Abs(s.rate-last)/last > 0.05 {
			t.Fatalf("the rate jumped from %v to %v at %v", last, s.rate, elapsed)
		}
		last = s.rate
	}
	if n > 0 {
		t.Fatalf("%d left after %v s", n, elapsed)
	}
}

func TestPacerNeverRushesATrickle(t *testing.T) {
	var s pacer
	for range 160 {
		s.step(3, false, 1.0/160)
	}
	if s.rate > paceMin*1.01 {
		t.Errorf("rate %v for a trickle", s.rate)
	}
}

// Whatever is cut where, the cursor lets all of it out, in order, and no marker is
// ever split from what it opens.
func TestMarkdownCursorLetsAllOut(t *testing.T) {
	var m mdCursor
	for i := 0; i < 100000 && m.at < len(longMarkdown); i++ {
		if m.next(longMarkdown, true) == 0 {
			t.Fatalf("stuck at %d", m.at)
		}
	}
	if m.at != len(longMarkdown) {
		t.Fatalf("at %d of %d", m.at, len(longMarkdown))
	}
}

func TestMarkdownCursorKeepsPiecesWhole(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"### 做了什么\n", "### 做"},
		{"- 一\n", "- 一\n"},
		{"1. 一\n", "1. 一\n"},
		{"> - 一\n", "> - 一\n"},
		{"| a | b |\n|---|---|\n", "| a | b |\n"},
		{"---\n正文", "---\n"},
		{"```go\nx := 1\n```\n", "```go\n"},
		{"**重点**", "**重"},
		{"hello world", "hello "},
		{"中文", "中"},
	} {
		var m mdCursor
		if m.next(tc.src, false) == 0 {
			t.Errorf("%q: held", tc.src)
			continue
		}
		if got := tc.src[:m.at]; got != tc.want {
			t.Errorf("%q: let out %q, want %q", tc.src, got, tc.want)
		}
	}
}

func TestMarkdownCursorWaitsForWhatMayYetBecomeAMarker(t *testing.T) {
	for _, src := range []string{"#", "##", "-", "1", "12.", ">", "|", "| a | b", "```", "```go", "--", "**"} {
		var m mdCursor
		if m.next(src, false) != 0 && src != "**" {
			t.Errorf("%q: let out %q while it may become a marker", src, src[:m.at])
		}
		var f mdCursor
		for i := 0; i < 20 && f.at < len(src); i++ {
			f.next(src, true)
		}
		if f.at != len(src) {
			t.Errorf("%q: stuck at the end of the run, at %d", src, f.at)
		}
	}
}

// Inside a fence, lines go whole and nothing is read as a marker.
func TestMarkdownCursorWalksCodeByTheLine(t *testing.T) {
	src := "```\n# a comment\n- x\n```\n正文"
	var m mdCursor
	var got []string
	for m.at < len(src) {
		from := m.at
		if m.next(src, true) == 0 {
			t.Fatal("stuck")
		}
		got = append(got, src[from:m.at])
	}
	want := "```\n|# a comment\n|- x\n|```\n|正|文"
	if strings.Join(got, "|") != want {
		t.Errorf("pieces %q", got)
	}
}

func planFor(source, result string, kind core.BlockKind, ended bool) layoutPlan {
	return planLayout(planInput{
		source: source, output: kind, result: result, ended: ended,
		sourceW: em(source) * sizeSource, resultW: em(result) * sizeBody,
		low: widthMinFooter, screenW: 1900, most: 800,
	})
}

func TestPlanFitsWhatWillCome(t *testing.T) {
	short := planFor("hello", "你好", core.BlockText, true)
	if short.reading || short.width != widthMinFooter || short.height > 120 {
		t.Errorf("a word: %+v", short)
	}
	sentence := planFor(strings.Repeat("word ", 20), "", core.BlockText, false)
	if sentence.reading || sentence.width != widthPanel {
		t.Errorf("a sentence: %+v", sentence)
	}
	essay := planFor(strings.Repeat("A sentence of an essay. ", 60), "", core.BlockText, false)
	if !essay.reading || essay.width != widthReading || essay.height < 300 {
		t.Errorf("an essay: %+v", essay)
	}
	explain := planFor("gloss", "", core.BlockMarkdown, false)
	if !explain.reading || explain.height < 150 || explain.height > 330 {
		t.Errorf("explaining a word: %+v", explain)
	}
	code := planFor("look at this\n```go\nfunc a() {}\n```\n", "", core.BlockMarkdown, false)
	if code.width != widthWide {
		t.Errorf("code: %+v", code)
	}
	table := planFor("| a | b |\n|---|---|\n| 1 | 2 |", "", core.BlockText, false)
	if table.width != widthWide {
		t.Errorf("table: %+v", table)
	}
	tall := planFor(strings.Repeat("A sentence of an essay. ", 2000), "", core.BlockText, false)
	if tall.height != 800 {
		t.Errorf("the screen is the limit: %+v", tall)
	}
	narrow := planLayout(planInput{source: "x", output: core.BlockMarkdown, low: 344, screenW: 400, most: 500})
	if narrow.width != 400 {
		t.Errorf("a narrow screen: %+v", narrow)
	}
}

// The plan is what the text needs, near enough to hold it to a few lines' growth.
func TestPlanIsCloseToTheResult(t *testing.T) {
	r := newRig(t, strings.Repeat("A sentence of an essay that goes on. ", 12), core.BlockText, 3000)
	const result = "这是一段译文，长度和原文差不多，用来核对预估的高度和实际需要的高度相差多少，让面板打开时就接近最终大小，流式过程里不用一直长高。"
	r.stream(t, strings.Repeat(result, 3), 18, 80*time.Millisecond)
	if d := r.p.natural - r.p.plan.height; d > r.p.natural*0.45 || d < -r.p.natural*0.45 {
		t.Errorf("planned %v for %v", r.p.plan.height, r.p.natural)
	}
}

// A source that wraps takes its lines from the height, and when all of the result
// is there the plan is what the panel needs: nothing moves or grows after it.
func TestPlanCountsTheLinesOfTheSource(t *testing.T) {
	const source = "Please summarise what you did in this change and what is left for me to check."
	r := newRig(t, source, core.BlockMarkdown, 900)
	r.p.apply(core.Event{Run: 1, Type: core.EventDelta, Kind: core.BlockMarkdown, Delta: "一句很短的结果。"})
	r.p.apply(core.Event{Run: 1, Type: core.EventDone})
	r.advance(600 * time.Millisecond)
	if !r.p.planned || r.p.reading {
		t.Fatalf("planned %v, reading %v", r.p.planned, r.p.reading)
	}
	if d := abs32(r.p.natural - r.p.plan.height); d > 1 {
		t.Errorf("planned %v, needs %v", r.p.plan.height, r.p.natural)
	}
}

// Where the panel is does not change what it plans: near the screen's edge it
// moves to make the room, and is not left short with a long text inside.
func TestRoomIsTheSameWhereverThePanelIs(t *testing.T) {
	for _, below := range []float32{0, 900, 120} {
		r := newRig(t, "Please summarise what you did in this change.", core.BlockMarkdown, 1000)
		r.p.below = below
		r.stream(t, strings.Repeat("A line of the result that takes its place on the page. ", 8), 400, 16*time.Millisecond)
		if !r.p.plan.reading || r.p.plan.room < planRoomReading {
			t.Errorf("below %v: plan = %+v, want the reading room", below, r.p.plan)
		}
	}
}

func TestSurfaceLimit(t *testing.T) {
	page := planRoomReading + heightFooter
	for _, tc := range []struct {
		name   string
		pinned bool
		below  float32
		want   float32
	}{
		{"not told yet", false, 0, 750},
		{"room to spare", false, 900, 750},
		{"the room there is", false, 700, 700},
		{"too little: it moves up for a page", false, 300, page},
		{"pinned, the room there is", true, 300, 300},
		{"pinned at the edge still reads: keepOnScreen moves it", true, 40, pinnedLeast},
	} {
		p := &panel{maxHeight: 600, maxReading: 750, reading: true, pinned: tc.pinned, below: tc.below, features: []feature{{"a", "a"}}}
		if got := p.limit(); got != tc.want {
			t.Errorf("%s: limit = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A result that is all there by the time the panel opens is short of a page,
// so nothing moves for room it will not use.
func TestAShortResultAsksForNoRoom(t *testing.T) {
	plan := planLayout(planInput{source: "gloss", output: core.BlockMarkdown, result: "一句话的结果", ended: true, low: 344, screenW: 1900, most: 700})
	if plan.reading || plan.width >= widthReading || plan.room != plan.height {
		t.Errorf("plan = %+v", plan)
	}
	long := strings.Repeat("A long source that needs to fold when it is shown. ", 8)
	if plan := planLayout(planInput{source: long, output: core.BlockMarkdown, result: "一句话的结果", ended: true, low: 344, screenW: 1900, most: 700}); !plan.reading {
		t.Errorf("a long source does not fold: %+v", plan)
	}
}

// Text that outgrows the plan moves the window in steps, not with each line.
func TestRoomGrowsInSteps(t *testing.T) {
	const room = 200
	if got := stepRoom(room, 150); got != room {
		t.Fatalf("within the plan: %v", got)
	}
	if got := stepRoom(room, room+1); got != room+stepWindow {
		t.Fatalf("first step: %v", got)
	}
	if got := stepRoom(room, room+stepWindow-1); got != room+stepWindow {
		t.Fatalf("still the first step: %v", got)
	}
	if got := stepRoom(room, room+stepWindow+1); got != room+2*stepWindow {
		t.Fatalf("second step: %v", got)
	}
}

// A panel moved by hand does not move for the text, unless it would leave the
// screen: what it holds and its buttons stay where they can be reached.
func TestPinnedPanelMovesOnlyToStayOnScreen(t *testing.T) {
	bottom := 900
	place := func(y int, wantH float32) *shell {
		s := &shell{area: mygo.Rectangle{X: 0, Y: 0, Width: 1500, Height: bottom}}
		s.panel.pinned = true
		s.y.jump(float64(y))
		s.x.jump(100)
		s.keepOnScreen(400, wantH)
		return s
	}
	roomy := place(100, 400)
	if roomy.y.target != 100 || roomy.panel.below != float32(bottom-100-margin-12) {
		t.Errorf("moved with room to spare: y %v, below %v", roomy.y.target, roomy.panel.below)
	}
	// Dropped near the bottom, it stays while the text fits.
	low := place(bottom-margin-12-250, 250)
	if low.y.target != float64(bottom-margin-12-250) {
		t.Errorf("moved though it fits: %v", low.y.target)
	}
	// With less room than a page, it moves up as far as it has to.
	edge := place(bottom-margin-12-48, 200)
	surfaceEnd := int(edge.y.target) + margin + 200
	if surfaceEnd > bottom-12 {
		t.Errorf("the surface ends at %v, past the screen's %v", surfaceEnd, bottom-12)
	}
	if edge.panel.below < 200 {
		t.Errorf("below = %v after moving", edge.panel.below)
	}
}

// A long line goes out whole and does not hold up what follows it.
func TestALongLineDoesNotHoldUpWhatFollows(t *testing.T) {
	var p panel
	p.reset("long text")
	now := time.Now()
	p.clock = func() time.Time { return now }
	p.lastFrame = now
	p.beginRun(1, feature{"agent", "整理输出"}, "rule", "", core.BlockMarkdown, false)
	src := "```json\n" + strings.Repeat("x", 20000) + "\n```\n\n尾文\n"
	p.apply(core.Event{Run: 1, Type: core.EventDelta, Kind: core.BlockMarkdown, Delta: src})
	p.apply(core.Event{Run: 1, Type: core.EventDone})
	p.settle(stubMeasure, now)
	start := now
	for i := 0; i < 2000 && !p.done; i++ {
		now = now.Add(16 * time.Millisecond)
		p.release(now)
	}
	if !p.done {
		t.Fatal("never done")
	}
	if took := now.Sub(start); took > 3*time.Second {
		t.Errorf("took %v", took)
	}
}

func TestPacerOwesOnlyAMoment(t *testing.T) {
	s := pacer{rate: 100}
	s.overdraw(100000)
	if s.carry != -100*paceOwe {
		t.Errorf("owes %v", s.carry)
	}
	if got := s.step(10, false, 0.016); got != 0 {
		t.Errorf("let out %d while owing", got)
	}
}

// What came before a failure shows, without waiting for frames that would not come.
func TestAFailureShowsWhatCame(t *testing.T) {
	for _, kind := range []core.BlockKind{core.BlockText, core.BlockMarkdown} {
		r := newRig(t, "hello", kind, 900)
		const part = "第一段已经到了。\n\n第二段也到了，后面就断了。"
		r.p.apply(core.Event{Run: 1, Type: core.EventDelta, Kind: kind, Delta: part})
		r.advance(30 * time.Millisecond)
		r.p.apply(core.Event{Run: 1, Type: core.EventError, Err: errors.New("broken")})
		for _, b := range r.p.blocks {
			if len(b.pending) > 0 {
				t.Errorf("%s: pending %q", kind, string(b.pending))
			}
		}
		if r.p.mdAt.at != len(r.p.md) {
			t.Errorf("%s: at %d of %d", kind, r.p.mdAt.at, len(r.p.md))
		}
		r.advance(fadeNib)
		if !r.p.drained() {
			t.Errorf("%s: not all shown", kind)
		}
		if busy := r.p.busy(r.tt.Now().Add(time.Second)); busy {
			t.Errorf("%s: keeps drawing", kind)
		}
	}
}

// The last piece of a run is measured after the run is done: it is followed too.
func TestTheLastPieceIsFollowed(t *testing.T) {
	r := newRig(t, strings.Repeat("A sentence of an essay that goes on. ", 12), core.BlockMarkdown, 700)
	r.stream(t, strings.Repeat("这一段足够长，长到要滚动才看得完，跟随要一直跟到真正的末尾。\n\n", 40)+"| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n", 400, 16*time.Millisecond)
	r.advance(800 * time.Millisecond)
	if r.p.scroll.MaxY == 0 {
		t.Fatal("no scrolling")
	}
	if r.p.scroll.Y < r.p.scroll.MaxY-1 {
		t.Errorf("stopped at %v of %v", r.p.scroll.Y, r.p.scroll.MaxY)
	}
}

// A press stops the following where it is, even mid-way, and letting go resumes it.
func TestAPressStopsTheFollowing(t *testing.T) {
	r := newRig(t, strings.Repeat("A sentence of an essay that goes on. ", 12), core.BlockMarkdown, 700)
	text := strings.Repeat("这一段足够长，长到要滚动才看得完，跟随要一直跟到真正的末尾。\n\n", 80)
	r.p.apply(core.Event{Run: 1, Type: core.EventDelta, Kind: core.BlockMarkdown, Delta: text})
	r.advance(2500 * time.Millisecond)
	if !r.p.scroll.Scrolling() {
		t.Fatal("not mid-way through a follow")
	}
	r.p.held = true
	r.advance(16 * time.Millisecond)
	at := r.p.scroll.Y
	r.advance(600 * time.Millisecond)
	if r.p.scroll.Y != at {
		t.Errorf("moved from %v to %v while held", at, r.p.scroll.Y)
	}
	if !r.p.following {
		t.Error("a press ended the following")
	}
	r.p.held = false
	r.advance(600 * time.Millisecond)
	if r.p.scroll.Y <= at {
		t.Errorf("did not resume from %v", at)
	}
}

func TestJoinPickedSeesPastInlineMarks(t *testing.T) {
	src := "| **甲**乙 | `丙` |\n|---|---|\n| ~~丁~~ | 戊 |\n"
	if got, want := joinPicked([]string{"甲乙", "丙", "丁", "戊"}, src), "甲乙\t丙\n丁\t戊"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
}

func TestJoinPicked(t *testing.T) {
	src := "# 标题\n\n第一段\n\n- 上\n- 下\n\n| a | b |\n|---|---|\n| c | d |\n\n**粗**体\n"
	for _, tc := range []struct {
		parts []string
		want  string
	}{
		{[]string{"标题", "第一段"}, "标题\n\n第一段"},
		{[]string{"上", "下"}, "上\n下"},
		{[]string{"a", "b", "c", "d"}, "a\tb\nc\td"},
		{[]string{"d", "粗体", "第一段"}, "d\n\n粗体\n第一段"},
		{[]string{"不在原文里", "第一段"}, "不在原文里\n\n第一段"},
	} {
		if got := joinPicked(tc.parts, src); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.parts, got, tc.want)
		}
	}
}

// Selecting across a table's cells copies its rows and columns.
func TestSelectedTableKeepsItsShape(t *testing.T) {
	r := newRig(t, "hello", core.BlockMarkdown, 900)
	const table = "一段开头。\n\n| 甲 | 乙 |\n|---|---|\n| 丙 | 丁 |\n"
	r.stream(t, table, 200, 16*time.Millisecond)
	r.advance(300 * time.Millisecond)
	r.tt.ClickAt(r.p.textBox.X+4, r.p.textBox.Y+10)
	r.tt.Command("selectAll")
	r.advance(50 * time.Millisecond)
	if want := "一段开头。\n\n甲\t乙\n丙\t丁"; r.p.picked != want {
		t.Errorf("picked %q, want %q", r.p.picked, want)
	}
}

// Past the end of a run, a press still stops a glide that is on its way.
func TestAPressStopsTheFollowingAfterTheEnd(t *testing.T) {
	r := newRig(t, strings.Repeat("A sentence of an essay that goes on. ", 12), core.BlockMarkdown, 700)
	r.stream(t, strings.Repeat("这一段足够长，长到要滚动才看得完，跟随要一直跟到真正的末尾。\n\n", 40), 400, 16*time.Millisecond)
	r.advance(800 * time.Millisecond)
	if r.p.scroll.Y < 1 {
		t.Fatal("no scrolling")
	}
	// A glide still on its way, well past the moment the run ended.
	r.p.following = true
	r.p.scroll.ScrollTo(0, 0, ui.Animation{Spring: motionFollow})
	r.advance(60 * time.Millisecond)
	if !r.p.scroll.Scrolling() {
		t.Fatal("not on its way")
	}
	r.p.held = true
	r.advance(16 * time.Millisecond)
	at := r.p.scroll.Y
	r.advance(400 * time.Millisecond)
	if r.p.scroll.Y != at {
		t.Errorf("moved from %v to %v while held", at, r.p.scroll.Y)
	}
}

// A source of several short lines takes them all from the height.
func TestPlanCountsTheLineBreaksOfTheSource(t *testing.T) {
	source := strings.Repeat("字\n", 9) + "字"
	r := newRig(t, source, core.BlockText, 900)
	r.p.apply(core.Event{Run: 1, Type: core.EventDelta, Kind: core.BlockText, Delta: "短"})
	r.p.apply(core.Event{Run: 1, Type: core.EventDone})
	r.advance(600 * time.Millisecond)
	if r.p.reading {
		t.Fatalf("reading: %+v", r.p.plan)
	}
	if d := abs32(r.p.natural - r.p.plan.height); d > 1 {
		t.Errorf("planned %v, needs %v", r.p.plan.height, r.p.natural)
	}
}
