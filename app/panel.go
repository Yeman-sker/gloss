package app

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"gloss/core"
)

const (
	iconCopySVG = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="5.5" y="5.5" width="8" height="8" rx="2"/><path d="M3.5 10.5h-.2a1 1 0 0 1-1-1v-6a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v.2"/></svg>`
)

var (
	iconCopy  = ui.MustParseSVG([]byte(iconCopySVG))
	iconCheck = checkIcon(1)
)

// checkIcon is the check drawn along its stroke as far as q, from 0 to 1.
func checkIcon(q float64) *ui.SVG {
	const first, second = 4.53, 9.69
	along := q * (first + second)
	path := "M3 8.5L"
	if along <= first {
		t := along / first
		path += fmt.Sprintf("%.2f %.2f", 3+3.2*t, 8.5+3.2*t)
	} else {
		t := (along - first) / second
		path += fmt.Sprintf("6.2 11.7L%.2f %.2f", 6.2+6.8*t, 11.7-6.9*t)
	}
	return ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="` + path + `"/></svg>`))
}

// chunk is a piece of a block, fading in from when it was released.
type chunk struct {
	text string
	at   time.Time
}

// block paces what the network sends: pending waits, young fades in,
// settled has arrived.
type block struct {
	kind    core.BlockKind
	settled strings.Builder
	young   []chunk
	pending []rune
}

// springKey keeps a surface's springs apart from the last time it showed.
type springKey struct {
	name string
	gen  uint64
}

// feature is one the panel offers to switch to.
type feature struct {
	id, name string
}

// panel is what the window shows. It knows blocks and the features' names,
// not what the features do.
type panel struct {
	source      string
	word        bool
	blocks      []*block
	failed      string
	failedTitle string
	notice      string
	noticeAt    time.Time
	shownAt     time.Time
	run         uint64
	capsule     bool
	netDone     bool
	done        bool
	gen         uint64
	entered     bool
	closing     bool
	pinned      bool
	dragging    bool
	copiedAt    time.Time
	natural     float32 // the content's height last frame
	maxHeight   float32
	maxReading  float32
	anchor      float32 // where the surface grows from, in from its left edge
	lastFrame   time.Time
	releasing   float64
	hadSel      bool
	wasPress    bool
	justEntered bool
	selected    bool
	pressMoved  bool
	givingWay   bool
	idleSince   time.Time
	doneAt      time.Time
	openedAt    time.Time
	heightTo    float32
	measured    float32
	measuredN   int
	pressText   bool
	pressGlyphs bool
	pressX      int
	pressY      int

	cursor func() (int, int)

	// The features, the one running, how it was chosen and why, and what
	// it gives: text, or Markdown that streams into md.
	features    []feature
	current     string
	by, why     string
	output      core.BlockKind
	replaceable bool
	md          string
	mdOpts      markdown.Options
	// reading is the layout for long or structured results: wider, taller,
	// the source folded, following the end while it streams.
	reading    bool
	sourceOpen bool
	scroll     ui.ScrollState
	following  bool
	status     string
	statusTone ui.Color
	statusAt   time.Time
	footerTop  float32
	mdTop      float32
	// animating says this frame asked for the next: a gap before it is a
	// dropped frame, not the panel at rest.
	animating bool

	wantCursor ui.Cursor
	inCopy     bool
	retryBox   ui.Rect
	surfaceW   float32
	textTop    float32
	local      func() (float32, float32)
	closed     func()
	copied     func(*ui.Context, string)
	copyText   func() string
	retry      func()
	switchTo   func(id string)
	replace    func()
	frame      func(c *ui.Context, curW, curH, wantW, wantH float32, pressed, overText bool)
}

// reset starts the panel over for a new selection.
func (p *panel) reset(source string) {
	p.source = source
	p.word = isWord(source)
	p.notice = ""
	p.capsule, p.closing = true, false
	p.natural, p.heightTo = heightCapsule, heightCapsule
	p.measured, p.measuredN = 0, -1
	p.givingWay, p.pressMoved = false, false
	p.openedAt = time.Time{}
	p.shownAt = time.Now()
	p.current, p.by, p.why = "", "", ""
	p.reading, p.sourceOpen = false, false
	p.scroll = ui.ScrollState{}
	p.clearRun()
}

// clearRun empties what a run showed.
func (p *panel) clearRun() {
	p.blocks, p.failed, p.md = nil, "", ""
	p.netDone, p.done = false, false
	p.doneAt, p.copiedAt = time.Time{}, time.Time{}
	p.releasing = 0
	p.selected, p.pressText, p.wasPress = false, false, false
	p.status = ""
	p.following = true
	p.measuredN = -1
}

// beginRun shows a feature starting on the source: a fresh result, which
// arrives as kind. An open panel stays open, its source reading again.
func (p *panel) beginRun(run uint64, f feature, by, why string, kind core.BlockKind, replaceable bool) {
	p.clearRun()
	p.run = run
	p.current, p.by, p.why = f.id, by, why
	p.failedTitle = f.name + "失败"
	p.output, p.replaceable = kind, replaceable
	p.reading = kind == core.BlockMarkdown
	p.scroll = ui.ScrollState{}
	p.shownAt = time.Now()
}

// showNotice turns the panel into a short message.
func (p *panel) showNotice(text string) {
	p.reset("")
	p.notice = text
	p.noticeAt = time.Now()
	p.done = true
}

// waiting says the panel is open and its run has shown nothing yet.
func (p *panel) waiting() bool {
	return !p.capsule && p.notice == "" && p.failed == "" && len(p.blocks) == 0 && p.md == "" && !p.done
}

// apply takes an event of the shell's current run, on the main thread.
func (p *panel) apply(ev core.Event) {
	switch ev.Type {
	case core.EventDelta:
		if p.capsule {
			p.openedAt = time.Now()
		}
		p.capsule = false
		p.idleSince = time.Now()
		if ev.Kind == core.BlockMarkdown {
			p.md += ev.Delta
			if len([]rune(p.md)) > 600 || strings.Count(p.md, "\n") > 6 {
				p.reading = true
			}
			return
		}
		for len(p.blocks) <= ev.Index {
			p.blocks = append(p.blocks, &block{kind: ev.Kind})
		}
		b := p.blocks[ev.Index]
		b.pending = append(b.pending, []rune(ev.Delta)...)
		if p.blocks[0].settled.Len()+len(p.blocks[0].pending) > 600 {
			p.reading = true
		}
	case core.EventDone:
		p.netDone = true
		if p.md != "" {
			// Markdown paces itself: the result is complete now.
			p.done, p.doneAt = true, time.Now()
		}
		if len(p.blocks) == 0 && p.md == "" {
			p.capsule, p.done = false, true
			p.openedAt = time.Now()
		}
	case core.EventError:
		p.failed = ev.Err.Error()
		p.capsule, p.done, p.netDone = false, true, true
		p.openedAt = time.Now()
	}
}

// setStatus shows a short message in the footer.
func (p *panel) setStatus(text string, tone ui.Color) {
	p.status, p.statusTone, p.statusAt = text, tone, time.Now()
}

// release lets pending text out at a pace that catches up with long gaps
// and hides the bursts of a stream.
func (p *panel) release(now time.Time) {
	dt := min(now.Sub(p.lastFrame).Seconds(), 0.25)
	p.lastFrame = now
	total := 0
	for _, b := range p.blocks {
		total += len(b.pending)
	}
	if total > 0 {
		p.releasing += paceRate(total) * dt
		p.idleSince = now
	}
	for _, b := range p.blocks {
		for p.releasing >= 1 && len(b.pending) > 0 {
			p.releasing--
			n := tokenLen(b.pending)
			b.young = append(b.young, chunk{string(b.pending[:n]), now})
			b.pending = b.pending[n:]
		}
		for len(b.young) > 0 && now.Sub(b.young[0].at) >= fadeText {
			b.settled.WriteString(b.young[0].text)
			b.young = b.young[1:]
		}
	}
	if p.netDone && !p.done && p.drained() {
		p.done = true
		p.doneAt = now
	}
}

func (p *panel) drained() bool {
	for _, b := range p.blocks {
		if len(b.pending) > 0 || len(b.young) > 0 {
			return false
		}
	}
	return true
}

// paceRate is how many tokens a second leave a backlog of n runes.
func paceRate(n int) float64 {
	return max(rateMin, min(rateMax, float64(n)/0.3))
}

// tokenLen keeps a Latin word whole; other text goes a rune at a time.
func tokenLen(r []rune) int {
	if !isLatin(r[0]) {
		return 1
	}
	n := 1
	for n < len(r) && (isLatin(r[n]) || r[n] == '\'' || r[n] == '-') {
		n++
	}
	return n
}

func isLatin(r rune) bool { return r < 0x250 && unicode.IsLetter(r) }

// isCJK says whether a text needs the quotes of Chinese text.
func isCJK(s string) bool {
	for _, r := range s {
		if r >= 0x3400 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// isWord says whether a selection is one word, which shows larger.
func isWord(s string) bool {
	n := 0
	for _, r := range s {
		if unicode.IsSpace(r) {
			return false
		}
		n++
	}
	if isCJK(s) {
		return n > 0 && n <= 4
	}
	return n > 0 && n <= 24
}

// measure is how wide text sets on one line.
func measure(c *ui.Context, text, font string, size float32) float32 {
	w, _ := c.MeasureText(1e5, ui.Span{Text: text, Font: font, Size: size})
	return w
}

func sourceFont(s string) string {
	if isCJK(s) {
		return fontSourceCJK
	}
	return fontSource
}

func capsuleWidth(c *ui.Context, s string) float32 {
	return clampWidth(measure(c, s, sourceFont(s), sizeSource)+36, minCapsule, widthCapsule)
}

func clampWidth(w, low, high float32) float32 {
	return max(low, min(high, float32(math.Ceil(float64(w)))))
}

// all is the translation with what is still on its way: the panel widens for
// the text it knows about, up to a line length that reads well.
func (p *panel) all() string {
	var out strings.Builder
	for _, b := range p.blocks {
		if b.kind != core.BlockText {
			continue
		}
		out.WriteString(b.settled.String())
		for _, ch := range b.young {
			out.WriteString(ch.text)
		}
		out.WriteString(string(b.pending))
	}
	return out.String()
}

func (p *panel) width(c *ui.Context) float32 {
	if p.reading {
		return widthReading
	}
	low := widthMinPanel
	if p.footer() {
		low = widthMinFooter
	}
	text := p.all()
	if n := len(text); n != p.measuredN {
		size := sizeBody
		if p.word {
			size = sizeWord
		}
		font := fontText
		if isCJK(text) {
			font = fontTextCJK
		}
		p.measuredN = n
		p.measured = max(measure(c, text, font, size), measure(c, p.source, sourceFont(p.source), sizeSource))
	}
	return clampWidth(p.measured+34, low, widthPanel)
}

// footer says whether the panel shows its footer: the features to switch
// to, and what can be done with the result.
func (p *panel) footer() bool {
	return !p.capsule && p.notice == "" && (len(p.features) > 1 || p.replaceable || p.output == core.BlockMarkdown)
}

func (p *panel) footerHeight() float32 {
	if p.footer() {
		return heightFooter
	}
	return 0
}

// limit is how tall the surface may grow.
func (p *panel) limit() float32 {
	if p.reading {
		return p.maxReading
	}
	return p.maxHeight
}

// target is the size the surface is heading for.
func (p *panel) target(c *ui.Context) (float32, float32) {
	switch {
	case p.notice != "":
		return clampWidth(measure(c, p.notice, fontSource, sizeSource)+52, 0, widthPanel), heightCapsule
	case p.capsule:
		return capsuleWidth(c, p.source), heightCapsule
	case p.failed != "":
		return max(capsuleWidth(c, p.source), widthError, min(p.width(c), widthPanel)), min(p.heightTo+p.footerHeight(), p.limit())
	}
	return p.width(c), min(p.heightTo+p.footerHeight(), p.limit())
}

// follow lets the height grow with the text at once, and shrink only at the
// end: lines that wrap differently along the way should not make it pump.
func (p *panel) follow() {
	if !p.capsule && p.notice == "" && (p.natural > p.heightTo || p.done) {
		p.heightTo = p.natural
	}
}

func (p *panel) text() string {
	if p.copyText != nil {
		if t := p.copyText(); t != "" {
			return t
		}
	}
	if p.md != "" {
		return p.md
	}
	var out []string
	for _, b := range p.blocks {
		if b.kind == core.BlockText {
			out = append(out, b.settled.String())
		}
	}
	return strings.Join(out, "\n")
}

// view builds the frame: a clear root, with the surface in it.
func (p *panel) view() func(*ui.Context) {
	return func(c *ui.Context) {
		now := time.Now()
		reduce := c.Preferences().ReduceMotion
		p.release(now)
		p.follow()
		p.track()
		root := c.Root().Background(ui.Transparent)

		wantW, wantH := p.target(c)
		motion := motionStream
		if p.capsule || p.notice != "" || (!p.done && now.Sub(p.openedAt) < 400*time.Millisecond) {
			motion = motionGrow
		}
		w := root.Spring(springKey{"w", p.gen}, wantW, motionGrow)
		h := root.Spring(springKey{"h", p.gen}, wantH, motion)
		r := radiusCapsule
		if !p.capsule && p.notice == "" {
			r = radiusPanel
		}
		radius := root.Spring(springKey{"r", p.gen}, r, motionGrow)

		opacity, fadeFor := float32(1), 80*time.Millisecond
		switch {
		case p.closing:
			opacity, fadeFor = 0, exitTime
		case !p.entered:
			opacity = 0
		case p.givingWay:
			opacity, fadeFor = 0.32, 140*time.Millisecond
		}
		alpha := root.AnimateTo(springKey{"fade", p.gen}, opacity, ui.Animation{Duration: fadeFor})

		// The shadow lives outside the clip, the content inside it.
		surface := ui.Box(c).Key(fmt.Sprintf("surface-%d", p.gen)).Absolute().Left(margin).Top(margin).
			Width(w).Height(h).Radius(radius).Background(colorSurface).
			Shadow(0, 14, 36, -6, colorShadow).Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, 0.24)).
			InsetShadow(0, 0, 0, 1, colorRim).
			Opacity(alpha).TransformOrigin(min(1, max(0, p.anchor/max(w, 1))), 0).Cursor(p.wantCursor)

		// The selection is this frame's element: mygo's elements do not
		// outlive the build that made them.
		var inner, translation, selection, mdView ui.Element
		foot := p.footerHeight()
		surface.Children(func() {
			ui.Column(c).Key("frame").Width(w).Height(h).Radius(radius).Clip().Children(func() {
				// Follow the end while it streams, unless the user scrolled up.
				if p.reading && p.following && !p.done && p.scroll.MaxY > 0 {
					p.scroll.Y = math.MaxFloat32
				}
				clip := ui.Scroll(c).Key("clip").Scrollbars(false).TrackScroll(&p.scroll).Width(w).Height(max(0, h-foot))
				clip.Children(func() {
					inner = ui.Column(c).Key("inner").Width(wantW).Padding(paddingY, paddingX).Gap(gapGroup)
					inner.Children(func() {
						if p.notice != "" {
							p.noticeRow(c)
							return
						}
						p.sourceRow(c, now, reduce)
						if p.md != "" {
							mdView = p.markdownView(c)
						} else {
							translation, selection = p.blocksView(c, now)
						}
						if p.failed != "" {
							p.failure(c)
						}
					})
				})
				if foot > 0 {
					p.footerRow(c, w, now)
				}
			})
			p.corner(c, now)
			p.pinDot(c, surface)
			p.toEnd(c, w, h-foot)
		})
		if p.scroll.MaxY > 0 && p.scroll.Y < p.scroll.MaxY-4 && !p.scroll.Scrolling() {
			p.following = false
		} else if p.scroll.MaxY > 0 && p.scroll.Y >= p.scroll.MaxY-1 {
			p.following = true
		}

		pressedCopy := p.copyOnClick(c, surface, translation, selection, p.cursor, now)
		overText := selection.Hovered() || mdView.Hovered()
		pressed := surface.Pressed() && !translation.Pressed() && !selection.Pressed() && !mdView.Pressed() && !p.overFooter()
		if p.closing && alpha <= 0.01 && p.closed != nil {
			p.closed()
		}
		scale := float32(1)
		anim := ui.Animation{Spring: motionRelease}
		switch {
		case p.closing:
			scale, anim = scaleExit, ui.Animation{Spring: ui.SpringOptions{Duration: exitTime * 14 / 10}}
		case !p.entered:
			scale, anim = scaleFrom, ui.Animation{Spring: motionPop}
		case p.justEntered:
			anim = ui.Animation{Spring: motionPop}
		case p.givingWay:
			scale, anim = scaleGiveWay, ui.Animation{Spring: motionPress}
		case p.dragging:
			scale, anim = scaleLifted, ui.Animation{Spring: motionPress}
		case pressedCopy && p.done && p.failed == "" && !p.hadSel && !p.pressMoved:
			scale, anim = scalePressed, ui.Animation{Spring: motionPress}
		}
		surface.AnimatePose(ui.Pose{Scale: scale, X: p.shake(now, reduce)}, anim)
		p.justEntered = false
		if !p.entered {
			p.entered, p.justEntered = true, true
			c.Invalidate()
		}

		p.surfaceW, p.textTop, p.mdTop = w, 0, 0
		if b := mdView.Bounds(); b.H > 0 {
			p.mdTop = max(1, b.Y-margin)
		}
		p.footerTop = h - foot
		if b := selection.Bounds(); p.failed == "" && len(p.blocks) > 0 && b.H > 0 {
			p.textTop = max(1, b.Y-margin-gapGroup/2)
		}
		if b := inner.Bounds(); b.H > 0 && b.H != p.natural {
			p.natural = b.H
			c.Invalidate()
		}
		if p.frame != nil {
			p.frame(c, w, h, wantW, wantH, pressed, overText)
		}
		p.animating = p.busy(now) || abs32(w-wantW) > 0.5 || abs32(h-wantH) > 0.5
		if p.busy(now) {
			c.Invalidate()
		}
	}
}

// overFooter says the pointer is on the footer, whose presses are its
// buttons' and do not move the panel.
func (p *panel) overFooter() bool {
	if p.local == nil || !p.footer() {
		return false
	}
	_, y := p.local()
	return y >= p.footerTop
}

func (p *panel) busy(now time.Time) bool {
	if !p.done || now.Sub(p.doneAt) < fadeNib || (!p.copiedAt.IsZero() && now.Sub(p.copiedAt) < 1500*time.Millisecond) {
		return true
	}
	if !p.openedAt.IsZero() && now.Sub(p.openedAt) < fadeSource {
		return true
	}
	// Markdown keeps fading in what came last, after the run is done.
	if p.md != "" && now.Sub(p.doneAt) < 2*time.Second {
		return true
	}
	if p.status != "" && now.Sub(p.statusAt) < statusFor+200*time.Millisecond {
		return true
	}
	return p.notice != "" && now.Sub(p.noticeAt) < 500*time.Millisecond
}

func (p *panel) shake(now time.Time, reduce bool) float32 {
	if p.notice == "" || reduce {
		return 0
	}
	t := now.Sub(p.noticeAt).Seconds()
	return float32(-5 * math.Sin(t*2*math.Pi/0.14) * math.Exp(-t*10))
}

func (p *panel) noticeRow(c *ui.Context) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Height(lineSource).Children(func() {
		ui.Box(c).Width(6).Height(6).Radius(3).Background(colorHint)
		ui.Text(c, p.notice).Font(fontSource).FontSize(sizeSource).LineHeight(lineSource / sizeSource).TextColor(colorText).NoWrap()
	})
}

// folds says the source is long enough to fold in the reading layout.
func (p *panel) folds() bool {
	return p.reading && (len([]rune(p.source)) > 90 || strings.Contains(p.source, "\n"))
}

func (p *panel) sourceRow(c *ui.Context, now time.Time, reduce bool) {
	font := sourceFont(p.source)
	lines := 0
	switch {
	case p.capsule:
		lines = 1
	case p.folds() && !p.sourceOpen:
		lines = 2
	}
	// The source reads while a feature works on it: in the capsule, and in
	// an open panel between a switch and the first text.
	if (p.capsule || p.waiting()) && !reduce {
		ui.RichText(c, sweepSpans(p.source, now.Sub(p.shownAt).Seconds())...).
			Font(font).FontSize(sizeSource).LineHeight(lineSource / sizeSource).MaxLines(max(lines, 1)).Unselectable()
		return
	}
	col := colorSource
	if p.capsule {
		col = colorText
	} else if q := float32(now.Sub(p.openedAt)) / float32(fadeSource); q < 1 {
		col = mix(colorText, colorSource, q)
	}
	t := ui.Text(c, p.source).Font(font).FontSize(sizeSource).LineHeight(lineSource / sizeSource).TextColor(col).Unselectable()
	if lines > 0 {
		t.MaxLines(lines)
	}
	if p.folds() && !p.capsule {
		hint := "展开原文"
		if p.sourceOpen {
			hint = "收起原文"
		}
		tog := ui.Text(c, hint).Key("fold").Font(fontText).FontSize(sizeHint).LineHeight(lineHint / sizeHint).TextColor(colorHint).Cursor(ui.CursorPointer)
		if tog.Hovered() {
			tog.TextColor(colorSource)
		}
		if tog.Clicked() {
			p.sourceOpen = !p.sourceOpen
		}
	}
}

func mix(a, b ui.Color, t float32) ui.Color {
	at := func(x, y uint8) uint8 { return uint8(float32(x) + (float32(y)-float32(x))*t) }
	return ui.RGB(at(a.R, b.R), at(a.G, b.G), at(a.B, b.B))
}

// sweepSpans lights a short run of the source: the panel is reading it.
func sweepSpans(source string, t float64) []ui.Span {
	const band = 6
	runes := []rune(source)
	n := min(len(runes), charsSource)
	head := int(t*30) % (n + band + 2)
	from, to := max(0, head-band), min(n, head)
	var spans []ui.Span
	add := func(s []rune, col ui.Color) {
		if len(s) > 0 {
			spans = append(spans, ui.Span{Text: string(s), Color: col})
		}
	}
	if from >= to {
		add(runes, colorText)
		return spans
	}
	add(runes[:from], colorText)
	add(runes[from:to], colorAccent)
	add(runes[to:], colorText)
	return spans
}

// blocksView draws the blocks. It returns the first text block's element
// and the selectable box around them all, both valid for this build only.
func (p *panel) blocksView(c *ui.Context, now time.Time) (ui.Element, ui.Element) {
	var first ui.Element
	found := false
	if len(p.blocks) == 0 {
		return first, ui.Element{}
	}
	box := ui.Column(c).Key("blocks").Gap(gapGroup).Selectable().Cursor(p.wantCursor)
	box.Children(func() {
		for i, b := range p.blocks {
			spans := p.spans(b, now, (!p.done || now.Sub(p.doneAt) < fadeNib) && i == len(p.blocks)-1)
			el := ui.RichText(c.Key(fmt.Sprintf("%d-%d", p.run, i)), spans...).DrawObjects(func(pt *ui.Painter, _ int, r ui.Rect) {
				pt.Fill(ui.Rect{X: r.X + 2, Y: r.Y, W: 2, H: r.H}, colorAccent.Alpha(p.nibAlpha(time.Now())), 1)
			})
			text := b.settled.String()
			switch b.kind {
			case core.BlockText:
				size, line := sizeBody, lineBody
				if p.word {
					size, line = sizeWord, lineWord
				}
				font := fontText
				if isCJK(text) || isCJK(string(b.pending)) {
					font = fontTextCJK
				}
				el.Font(font).FontSize(size).LineHeight(line / size).SelectionColor(colorAccent.Alpha(0.35))
				if !found {
					first, found = el, true
				}
			case core.BlockPhonetic:
				el.Font(fontPhonetic).FontSize(sizeSource).LineHeight(lineSource / sizeSource)
			default:
				el.Font(fontSource).FontSize(sizeSource).LineHeight(lineSource / sizeSource)
			}
		}
	})
	return first, box
}

func (p *panel) spans(b *block, now time.Time, nib bool) []ui.Span {
	base := colorText
	if b.kind != core.BlockText {
		base = colorSource
	}
	settled := b.settled.String()
	if b.kind == core.BlockText && !p.copiedAt.IsZero() {
		if q := float64(now.Sub(p.copiedAt)) / float64(fadeSweep); q < 1 {
			return copySweep(settled, q, base)
		}
	}
	var spans []ui.Span
	if settled != "" {
		spans = append(spans, ui.Span{Text: settled, Color: base})
	}
	size := sizeBody
	if p.word && b.kind == core.BlockText {
		size = sizeWord
	}
	if b.kind != core.BlockText {
		size = sizeSource
	}
	for _, ch := range b.young {
		q := min(1, float32(now.Sub(ch.at))/float32(fadeText))
		e := 1 - (1-q)*(1-q)*(1-q)
		spans = append(spans, ui.Span{Text: ch.text, Color: base.Alpha(e), BaselineShift: -riseText * (1 - e)})
	}
	if nib {
		spans = append(spans, ui.Span{Text: "\uFFFC", ObjectWidth: 4, ObjectHeight: 0.85 * size, ObjectDescent: 0.1 * size})
	}
	return spans
}

// track reads the pointer each frame, and on the shell's ticks between them.
// It reports whether a frame must show the change: the elements carry the
// cursor too, and the corner cue follows the copy zone.
func (p *panel) track() bool {
	if p.local == nil {
		return false
	}
	x, y := p.local()
	was, cursor := p.inCopy, p.wantCursor
	p.wantCursor = p.pointerAt(x, y)
	return p.inCopy != was || p.wantCursor != cursor
}

// pointerAt splits the surface into zones by where the pointer is, with one
// cursor for all of each: the translation down to the surface's edges is one
// zone, so its padding and the corner cue do not flip the cursor. x and y are
// in from the surface's top left, in the layout, which a press's scale leaves
// alone. The boundary lags by a little so that a pointer resting on it stays.
func (p *panel) pointerAt(x, y float32) ui.Cursor {
	const lag = 2
	top := p.textTop + lag
	if p.inCopy {
		top = p.textTop - lag
	}
	inFooter := p.footer() && y >= p.footerTop
	p.inCopy = p.textTop > 0 && y >= top && p.notice == "" && !inFooter
	switch {
	case p.dragging:
		return ui.CursorGrabbing
	case inFooter:
		return ui.CursorDefault
	case p.md != "" && p.mdTop > 0 && y >= p.mdTop:
		return ui.CursorText
	case p.pinned && x >= p.surfaceW-24 && y < 24:
		return ui.CursorPointer
	case p.failed != "":
		b := p.retryBox
		if b.W > 0 && b.Contains(x+margin, y+margin) {
			return ui.CursorPointer
		}
		return ui.CursorGrab
	case !p.inCopy:
		return ui.CursorGrab
	case !p.done || (p.pressText && p.pressMoved && p.pressGlyphs):
		return ui.CursorText
	}
	return ui.CursorPointer
}

// nibAlpha breathes once the text has stalled, and fades when it has ended.
func (p *panel) nibAlpha(now time.Time) float32 {
	if p.done {
		return max(0, 1-float32(now.Sub(p.doneAt))/float32(fadeNib))
	}
	idle := now.Sub(p.idleSince).Seconds() - 0.25
	if idle <= 0 {
		return 1
	}
	return float32(0.55 + 0.45*math.Cos(idle*5))
}

func copySweep(text string, q float64, base ui.Color) []ui.Span {
	const band = 8
	runes := []rune(text)
	head := int(q * float64(len(runes)+band))
	from, to := max(0, head-band), min(len(runes), head)
	if from >= to {
		return []ui.Span{{Text: text, Color: base}}
	}
	return []ui.Span{
		{Text: string(runes[:from]), Color: base},
		{Text: string(runes[from:to]), Color: colorSuccess},
		{Text: string(runes[to:]), Color: base},
	}
}

func (p *panel) failure(c *ui.Context) {
	ui.Column(c).Gap(2).Children(func() {
		title := p.failedTitle
		if title == "" {
			title = "翻译失败"
		}
		ui.Text(c, title).Font(fontSource).FontSize(sizeSource).LineHeight(lineSource / sizeSource).TextColor(colorDanger)
		ui.Text(c, p.failed).Font(fontSource).FontSize(sizeSource).LineHeight(lineSource / sizeSource).TextColor(colorSource).Selectable()
	})
	retry := ui.Box(c).Key("retry").Height(24).Padding(0, 10).Radius(radiusControl).Background(colorDanger.Alpha(0.18)).AlignSelf(ui.Start).Cursor(p.wantCursor).Children(func() {
		ui.Text(c, "重试").Font(fontSource).FontSize(sizeSource).LineHeight(2).TextColor(colorText)
	})
	p.retryBox = retry.Bounds()
	if retry.Clicked() && p.retry != nil {
		p.retry()
	}
}

// corner is the quiet copy cue, and the check that answers a copy.
func (p *panel) corner(c *ui.Context, now time.Time) {
	if !p.done || p.failed != "" || p.notice != "" || p.footer() {
		return
	}
	since := now.Sub(p.copiedAt)
	copied := !p.copiedAt.IsZero() && since < 1200*time.Millisecond
	on := copied || p.inCopy
	icon, col := iconCopy, colorHint
	if copied {
		icon, col = iconCheck, colorSuccess
		if since < drawCheck {
			icon = checkIcon(float64(since) / float64(drawCheck))
		}
	}
	pose := ui.Pose{Fade: 1, Y: 2}
	if on {
		pose = ui.Pose{}
	}
	ui.Icon(c, icon).Key("cue").Absolute().Right(9).Bottom(9).Size(15, 15).FontSize(15).TextColor(col).
		AnimatePose(pose, ui.Animation{Duration: 140 * time.Millisecond})
}

func (p *panel) pinDot(c *ui.Context, surface ui.Element) {
	target, motion := float32(0), motionPress
	if p.pinned {
		target, motion = 1, motionRelease
	}
	size := surface.Spring("pin", target, motion)
	if size < 0.01 && !p.pinned {
		return
	}
	dot := ui.Box(c).Key("pin").Absolute().Right(4).Top(4).Width(20).Height(20).AlignItems(ui.Center).Justify(ui.Center).Cursor(p.wantCursor).Children(func() {
		ui.Box(c).Width(6).Height(6).Radius(3).Background(colorAccent).Scale(size, size)
	})
	if dot.Clicked() {
		p.pinned = false
	}
}

// copyOnClick copies the whole translation on a plain click in the copy zone:
// a press that clears a selection, or that moves, copies nothing. It returns
// whether the zone is being pressed.
func (p *panel) copyOnClick(c *ui.Context, surface, el, selection ui.Element, cursor func() (int, int), now time.Time) bool {
	onText := el.Pressed() || selection.Pressed()
	press := surface.Pressed() || onText
	clicked := el.Clicked() || selection.Clicked() || surface.Clicked()
	x, y := cursor()
	if !p.wasPress && (press || clicked) {
		p.pressText, p.hadSel, p.pressX, p.pressY = p.inCopy, p.selected, x, y
		p.pressGlyphs = onText || selection.Hovered()
		p.pressMoved = false
	}
	dx, dy := x-p.pressX, y-p.pressY
	if dx*dx+dy*dy > dragStart*dragStart {
		p.pressMoved = true
	}
	released := clicked || (p.wasPress && !press)
	p.wasPress = press
	zone := press && p.pressText
	if !released || !p.pressText {
		return zone
	}
	p.pressText = false
	had := p.hadSel
	// mygo reports a selection late, so the panel keeps its own account.
	p.selected = p.pressGlyphs && (p.pressMoved || el.DoubleClicked() || selection.DoubleClicked() || len(selection.SelectedTexts()) > 0 && !had)
	if had || p.selected || p.pressMoved || !p.done || p.failed != "" {
		return zone
	}
	if text := p.text(); text != "" && p.copied != nil {
		p.copied(c, text)
		p.copiedAt = now
		c.Invalidate()
	}
	return zone
}

// mdTheme is Markdown in the panel's ink: its fonts and colors, headings
// kept close to the body's size, as a panel is not a page.
func mdTheme() *markdown.Theme {
	th := &markdown.Theme{
		FontSize:            sizeBody,
		Font:                fontText,
		CodeFont:            fontSource,
		LineHeight:          lineBody / sizeBody / 1.2,
		Text:                colorText,
		Muted:               colorSource,
		Link:                colorAccent,
		Strong:              colorText,
		Code:                colorText,
		CodeBackground:      colorControl,
		QuoteBar:            colorRim,
		QuoteText:           colorSource,
		Rule:                colorRim,
		CodeBlockBackground: ui.RGBA(255, 255, 255, 0.04),
		CodeBlockBorder:     colorRim,
		CodeBlockRadius:     radiusControl,
		CodeColors:          editor.DefaultTheme(ui.DarkTheme()),
		TableBorder:         colorRim,
		TableHeader:         ui.RGBA(255, 255, 255, 0.04),
		Checkbox:            colorAccent,
	}
	sizes := [6]float32{1.25, 1.12, 1.04, 1, 1, 1}
	for i := range th.Headings {
		th.Headings[i] = markdown.HeadingStyle{Size: sizes[i], Weight: 650, Color: colorText, Top: 1.1, Bottom: 0.45}
	}
	return th
}

// markdownView streams Markdown while the run goes on, and shows it whole
// once it is done, in the same place, so the parse carries on.
func (p *panel) markdownView(c *ui.Context) ui.Element {
	if p.mdOpts.Theme == nil {
		p.mdOpts.Theme = mdTheme()
		p.mdOpts.WrapCode = true
	}
	var el ui.Element
	box := ui.Column(c).Key(fmt.Sprintf("md-%d", p.run)).Cursor(p.wantCursor)
	box.Children(func() {
		if p.netDone {
			el = markdown.View(c, p.md, &p.mdOpts)
		} else {
			el = markdown.Stream(c, p.md, &p.mdOpts)
		}
	})
	_ = el
	return box
}

// byLabel says how the feature showing was chosen.
func byLabel(by string) string {
	switch by {
	case "manual":
		return "手动"
	case "default", "":
		return "默认"
	}
	return "自动"
}

// footerRow is the bar under the result: the features, how the one
// showing was chosen, and what can be done with the result.
func (p *panel) footerRow(c *ui.Context, w float32, now time.Time) {
	ui.Row(c).Key("footer").Width(w).Height(heightFooter).Padding(0, 10).Gap(4).AlignItems(ui.Center).
		InsetShadow(0, 1, 0, 0, colorRim).Children(func() {
		if len(p.features) > 1 {
			for _, f := range p.features {
				f := f
				on := f.id == p.current
				chip := ui.Box(c).Key("f-"+f.id).Height(22).Padding(0, 8).Radius(11).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
				col := colorHint
				if on {
					chip.Background(colorControl)
					col = colorText
				} else if chip.Hovered() {
					col = colorSource
				}
				chip.Children(func() {
					ui.Text(c, f.name).Font(fontText).FontSize(sizeHint).LineHeight(lineHint / sizeHint).TextColor(col).NoWrap()
				})
				if chip.Clicked() && !on && p.switchTo != nil {
					p.switchTo(f.id)
				}
			}
			if p.current != "" {
				lab := ui.Text(c, byLabel(p.by)).Key("by").Font(fontText).FontSize(sizeHint - 1).LineHeight(lineHint / (sizeHint - 1)).TextColor(colorHint).NoWrap().MarginStart(2)
				if p.why != "" {
					lab.Tooltip(p.why)
				}
			}
		}
		ui.Box(c).Grow(1)
		if p.status != "" && now.Sub(p.statusAt) < statusFor {
			ui.Text(c, p.status).Key("status").Font(fontText).FontSize(sizeHint).LineHeight(lineHint / sizeHint).TextColor(p.statusTone).NoWrap()
		}
		if p.done && p.failed == "" && p.notice == "" {
			p.action(c, "copy", "复制", func() {
				if text := p.text(); text != "" && p.copied != nil {
					p.copied(c, text)
					p.copiedAt = now
					c.Invalidate()
				}
			})
			if p.replaceable && p.replace != nil {
				p.action(c, "replace", "替换", p.replace)
			}
		}
	})
}

func (p *panel) action(c *ui.Context, key, label string, do func()) {
	b := ui.Box(c).Key(key).Height(22).Padding(0, 9).Radius(radiusControl).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
	col := colorSource
	if b.Hovered() {
		b.Background(colorControl)
		col = colorText
	}
	b.Children(func() {
		ui.Text(c, label).Font(fontText).FontSize(sizeHint).LineHeight(lineHint / sizeHint).TextColor(col).NoWrap()
	})
	if b.Clicked() {
		do()
	}
}

// toEnd brings a long result back to its end, once the user scrolled up
// while it streamed.
func (p *panel) toEnd(c *ui.Context, w, bottom float32) {
	if !p.reading || p.following || p.scroll.MaxY <= 0 || p.done {
		return
	}
	b := ui.Box(c).Key("to-end").Absolute().Left(w/2-44).Top(bottom-34).Width(88).Height(24).Radius(12).
		Background(ui.Hex("#26262C")).InsetShadow(0, 0, 0, 1, colorRim).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer)
	b.Children(func() {
		ui.Text(c, "回到底部").Font(fontText).FontSize(sizeHint).LineHeight(lineHint / sizeHint).TextColor(colorText).NoWrap()
	})
	if b.Clicked() {
		p.following = true
		p.scroll.Y = math.MaxFloat32
		c.Invalidate()
	}
}
