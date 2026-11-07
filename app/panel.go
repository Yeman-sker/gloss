package app

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"gloss/core"
)

const (
	iconCopySVG  = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="5.5" y="5.5" width="8" height="8" rx="2"/><path d="M3.5 10.5h-.2a1 1 0 0 1-1-1v-6a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v.2"/></svg>`
	iconCheckSVG = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round"><path d="M3 8.5l3.2 3.2L13 4.8"/></svg>`
)

var (
	iconCopy  = ui.MustParseSVG([]byte(iconCopySVG))
	iconCheck = ui.MustParseSVG([]byte(iconCheckSVG))
)

// chunk is a piece of a block, fading in from when it was released. brk says a
// new paragraph starts after it.
type chunk struct {
	text string
	at   time.Time
	brk  bool
}

// block paces what the network sends: pending waits, young fades in, settled
// has arrived. Finished paragraphs are their own elements, so that only the one
// being written lays out again each frame, however long the text gets.
type block struct {
	kind    core.BlockKind
	paras   []string
	settled strings.Builder
	young   []chunk
	pending []rune
}

// whole is the block's text as it has come, paced or not.
func (b *block) whole() string {
	var out strings.Builder
	for _, para := range b.paras {
		out.WriteString(para)
		out.WriteString("\n\n")
	}
	out.WriteString(b.settled.String())
	for _, ch := range b.young {
		out.WriteString(ch.text)
		if ch.brk {
			out.WriteString("\n\n")
		}
	}
	out.WriteString(string(b.pending))
	return out.String()
}

// springKey keeps a surface's springs apart from the last time it showed.
type springKey struct {
	name string
	gen  uint64
}

// spot is a part of the panel that answers the pointer.
type spot struct {
	key string
	box ui.Rect
	// tip is what the panel says about it once the pointer has rested there.
	tip string
	// quiet is one that only explains: a press there does nothing.
	quiet bool
	// foot is in the footer, which covers what scrolls behind it.
	foot bool
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
	// fix is where the user can mend what failed, as the settings page of
	// a key that was refused; fixDo is nil when there is none.
	fix         string
	fixDo       func()
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
	below       float32 // from the surface's top to the bottom of the screen
	screenW     float32
	anchor      float32 // where the surface grows from, in from its left edge
	lastFrame   time.Time
	justEntered bool
	givingWay   bool
	idleSince   time.Time
	doneAt      time.Time
	openedAt    time.Time
	heightTo    float32
	// held says the mouse button is down on the panel. A selection being made
	// would slide under the pointer if the text scrolled meanwhile.
	held bool

	// The plan for the frame, once the run's first text has told enough; until
	// then the result waits and the panel looks as it did. See settle.
	planned bool
	plan    layoutPlan
	firstAt time.Time
	widthTo float32
	cjk     bool
	pace    pacer
	mdAt    mdCursor

	// The features, the one running, how it was chosen and why, and what
	// it gives: text, or Markdown that streams into md.
	features    []feature
	current     string
	by, why     string
	output      core.BlockKind
	replaceable bool
	md          string
	mdOpts      markdown.Options
	// codeCopied is when each code block of the run was copied, by its
	// index in the document.
	codeCopied map[int]time.Time
	// reading is the layout for long or structured results: wider, taller,
	// the source folded. The plan decides it, once for each run.
	reading    bool
	sourceOpen bool
	scroll     ui.ScrollState
	following  bool
	followed   float32 // where following last headed for
	// picked is the text selected in the result, as of the last frame.
	picked     string
	status     string
	statusTone ui.Color
	statusAt   time.Time
	footerTop  float32
	// animating says this frame asked for the next: a gap before it is a
	// dropped frame, not the panel at rest.
	animating bool

	wantCursor ui.Cursor
	// textBox is the selectable text, where a press selects rather than moves
	// the panel, as the last frame laid it out.
	textBox ui.Rect
	// What the pointer is on, found by the panel itself: see hover. spots are
	// the things it can be on, as the last frame laid them out.
	hot      string
	hotSince time.Time
	spots    []spot
	inFooter bool
	tipping  bool
	tipGone  time.Time
	surfaceW float32
	local    func() (float32, float32)
	clock    func() time.Time
	closed   func()
	copied   func(*ui.Context, string)
	copyText func() string
	retry    func()
	// explain words an error for the user, and says where to fix it.
	explain  func(error) (msg, fix string, do func())
	switchTo func(id string)
	replace  func()
	frame    func(c *ui.Context, curW, curH, wantW, wantH float32, pressed bool)
}

// now is the panel's clock: the system's, or a test's, kept in step with the
// frames it drives.
func (p *panel) now() time.Time {
	if p.clock != nil {
		return p.clock()
	}
	return time.Now()
}

// reset starts the panel over for a new selection.
func (p *panel) reset(source string) {
	p.source = source
	p.word = isWord(source)
	p.notice = ""
	p.capsule, p.closing = true, false
	p.natural, p.heightTo, p.widthTo = heightCapsule, heightCapsule, 0
	p.givingWay = false
	p.openedAt = time.Time{}
	p.shownAt = p.now()
	p.current, p.by, p.why = "", "", ""
	p.reading, p.sourceOpen = false, false
	p.scroll = ui.ScrollState{}
	p.clearRun()
}

// clearRun empties what a run showed.
func (p *panel) clearRun() {
	p.blocks, p.failed, p.md = nil, "", ""
	p.fix, p.fixDo = "", nil
	p.codeCopied = nil
	p.netDone, p.done = false, false
	p.doneAt, p.copiedAt = time.Time{}, time.Time{}
	p.planned, p.plan, p.firstAt = false, layoutPlan{}, time.Time{}
	p.pace, p.mdAt = pacer{}, mdCursor{}
	p.picked = ""
	p.status = ""
	p.following, p.followed = true, 0
}

// beginRun shows a feature starting on the source: a fresh result, which
// arrives as kind. An open panel stays open, its source reading again.
func (p *panel) beginRun(run uint64, f feature, by, why string, kind core.BlockKind, replaceable bool) {
	p.clearRun()
	p.run = run
	p.current, p.by, p.why = f.id, by, why
	p.failedTitle = f.name + "失败"
	p.output, p.replaceable = kind, replaceable
	p.scroll = ui.ScrollState{}
	p.shownAt = p.now()
}

// showNotice turns the panel into a short message.
func (p *panel) showNotice(text string) {
	p.reset("")
	p.notice = text
	p.noticeAt = p.now()
	p.done = true
}

// waiting says the panel is open and its run has shown nothing yet.
func (p *panel) waiting() bool {
	return !p.capsule && p.notice == "" && p.failed == "" && !p.planned && !p.done
}

// apply takes an event of the shell's current run, on the main thread.
func (p *panel) apply(ev core.Event) {
	switch ev.Type {
	case core.EventDelta:
		if p.firstAt.IsZero() {
			p.firstAt = p.now()
		}
		p.idleSince = p.now()
		if ev.Kind == core.BlockMarkdown {
			p.md += ev.Delta
			return
		}
		for len(p.blocks) <= ev.Index {
			p.blocks = append(p.blocks, &block{kind: ev.Kind})
		}
		b := p.blocks[ev.Index]
		b.pending = append(b.pending, []rune(ev.Delta)...)
	case core.EventDone:
		p.netDone = true
		if len(p.blocks) == 0 && p.md == "" {
			p.capsule, p.done, p.doneAt = false, true, p.now()
			p.openedAt = p.now()
		}
	case core.EventError:
		p.failed = ev.Err.Error()
		if p.explain != nil {
			p.failed, p.fix, p.fixDo = p.explain(ev.Err)
		}
		p.capsule, p.done, p.netDone = false, true, true
		p.doneAt = p.now()
		p.openedAt = p.now()
		p.flush(p.doneAt)
	}
}

// measureFunc is how wide text sets on one line.
type measureFunc func(text, font string, size float32) float32

// settle decides the frame once the run has told enough: all of it, for a short
// result; otherwise what has come within planWait of the first text, which is
// not noticed in a capsule already waiting on the network. Nothing shows before,
// so the text never has to wrap again for a width that changed, nor the source
// fold under it, and the panel opens once, to about the right size.
func (p *panel) settle(measure measureFunc, now time.Time) {
	if p.planned || p.notice != "" || (p.firstAt.IsZero() && !p.netDone) {
		return
	}
	got := p.received()
	if !p.netDone && now.Sub(p.firstAt) < planWait && utf8.RuneCountInString(got) < planEnough {
		return
	}
	p.cjk = isCJK(got) || isCJK(p.source)
	font, size := fontText, sizeBody
	if p.cjk {
		font = fontTextCJK
	}
	if p.word {
		size = sizeWord
	}
	in := planInput{
		source: p.source, output: p.output, result: got, ended: p.netDone,
		low: p.lowWidth(), screenW: p.screenWidth(), most: p.mostHeight(),
	}
	// Only what may end up on one line is worth measuring exactly.
	if em(p.source)*sizeSource < 2*widthPanel {
		in.sourceW = measure(p.source, sourceFont(p.source), sizeSource)
	}
	if em(got)*size < 2*widthPanel {
		in.resultW = measure(got, font, size)
	}
	p.plan, p.planned = planLayout(in), true
	p.reading = p.plan.reading
	p.widthTo = p.plan.width
	p.heightTo = max(p.plan.height, heightCapsule)
	if p.capsule {
		p.capsule = false
		p.openedAt = now
	}
	p.idleSince = now
}

// received is the result so far, whether or not it has shown.
func (p *panel) received() string {
	if p.md != "" {
		return p.md
	}
	var out strings.Builder
	for _, b := range p.blocks {
		if b.kind == core.BlockText {
			out.WriteString(b.whole())
		}
	}
	return out.String()
}

// setStatus shows how a replace went, over the footer: see statusView.
func (p *panel) setStatus(text string, tone ui.Color) {
	p.status, p.statusTone, p.statusAt = text, tone, p.now()
}

// release lets out what the plan is ready for, at the pacer's speed.
func (p *panel) release(now time.Time) {
	dt := min(now.Sub(p.lastFrame).Seconds(), 0.25)
	p.lastFrame = now
	if !p.planned {
		return
	}
	if p.md != "" {
		p.releaseMarkdown(dt)
	} else {
		p.releaseBlocks(dt, now)
	}
	if p.netDone && !p.done && p.drained() {
		p.done = true
		p.doneAt = now
	}
}

func (p *panel) releaseMarkdown(dt float64) {
	backlog := utf8.RuneCountInString(p.md[p.mdAt.at:])
	budget := p.pace.step(backlog, p.netDone, dt)
	for budget > 0 {
		n := p.mdAt.next(p.md, p.netDone)
		if n == 0 {
			break
		}
		budget -= n
	}
	if budget < 0 {
		p.pace.overdraw(-budget)
	}
}

func (p *panel) releaseBlocks(dt float64, now time.Time) {
	backlog := 0
	for _, b := range p.blocks {
		backlog += len(b.pending)
	}
	if backlog > 0 {
		p.idleSince = now
	}
	budget := p.pace.step(backlog, p.netDone, dt)
	for _, b := range p.blocks {
		for budget > 0 && len(b.pending) > 0 {
			n := tokenLen(b.pending, p.netDone)
			if n == 0 {
				break
			}
			budget -= n
			b.young = append(b.young, newChunk(string(b.pending[:n]), now))
			b.pending = b.pending[n:]
		}
		for len(b.young) > 0 && now.Sub(b.young[0].at) >= fadeReveal {
			ch := b.young[0]
			b.young = b.young[1:]
			b.settled.WriteString(ch.text)
			if ch.brk {
				b.paras = append(b.paras, b.settled.String())
				b.settled.Reset()
			}
		}
	}
	if budget < 0 {
		p.pace.overdraw(-budget)
	}
}

// flush lets out all that has come, at once: a run that failed has nothing left
// to pace, and no frame may come to let out the rest.
func (p *panel) flush(now time.Time) {
	p.mdAt.at = len(p.md)
	for _, b := range p.blocks {
		for len(b.pending) > 0 {
			n := max(tokenLen(b.pending, true), 1)
			b.young = append(b.young, newChunk(string(b.pending[:n]), now))
			b.pending = b.pending[n:]
		}
	}
}

// newChunk takes a token as it left the pacer: the blank lines that end one are
// the gap to the next paragraph.
func newChunk(text string, at time.Time) chunk {
	trimmed := strings.TrimRight(text, " \t\n")
	if strings.Count(text[len(trimmed):], "\n") >= 2 {
		return chunk{text: trimmed, at: at, brk: true}
	}
	return chunk{text: text, at: at}
}

func (p *panel) drained() bool {
	if p.mdAt.at < len(p.md) {
		return false
	}
	for _, b := range p.blocks {
		if len(b.pending) > 0 || len(b.young) > 0 {
			return false
		}
	}
	return true
}

// tokenLen is how many runes go out together: a Latin word whole, other text a
// rune at a time, and the white space after them, so that a line never ends on
// the screen before its next begins. Line ends wait for what comes after them
// unless final says nothing will: only then is it known how many there are.
func tokenLen(r []rune, final bool) int {
	n := 1
	if isLatin(r[0]) {
		for n < len(r) && (isLatin(r[n]) || r[n] == '\'' || r[n] == '-') {
			n++
		}
	} else if unicode.IsSpace(r[0]) {
		n = 0
	}
	end := n
	for end < len(r) && unicode.IsSpace(r[end]) {
		end++
	}
	if end == len(r) && !final && slices.Contains(r[n:end], '\n') {
		return n
	}
	return end
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

// lowWidth is the narrowest the panel may be: a footer of features needs room.
func (p *panel) lowWidth() float32 {
	if len(p.features) > 1 {
		return widthMinFooter
	}
	return widthMinPanel
}

// screenWidth is the widest the screen leaves for the panel, unbounded until the
// shell has said.
func (p *panel) screenWidth() float32 {
	if p.screenW > 0 {
		return p.screenW
	}
	return widthWide
}

// width is the plan's, or the narrowest until there is one.
func (p *panel) width() float32 {
	if p.widthTo > 0 {
		return p.widthTo
	}
	return p.lowWidth()
}

// footer says whether the panel shows its footer: the features to switch
// to, and what can be done with the result.
func (p *panel) footer() bool {
	return !p.capsule && p.notice == "" && (len(p.features) > 1 || p.failed == "")
}

func (p *panel) footerHeight() float32 {
	if p.footer() {
		return heightFooter
	}
	return 0
}

// mostHeight is how tall the plan may make the content: what the screen allows,
// wherever on it the panel is, as the panel can still move to make the room.
func (p *panel) mostHeight() float32 {
	return max(p.maxHeight, p.maxReading, heightCapsule)
}

// limit is how tall the surface may grow: the screen's share, and short of that
// the room below it. It moves up for what the plan said, and if the text outgrows
// that, until there is a page to read; then it stays where it is and the text
// scrolls, as a panel climbing the screen moves the line being read. A panel
// moved by hand stays where it was put.
func (p *panel) limit() float32 {
	most := p.maxHeight
	if p.reading {
		most = p.maxReading
	}
	if p.below > 0 {
		least := planRoomReading + p.footerHeight()
		if p.pinned {
			least = pinnedLeast
		}
		most = min(most, max(p.below, least))
	}
	return most
}

// target is the size the surface is heading for.
func (p *panel) target(c *ui.Context) (float32, float32) {
	switch {
	case p.notice != "":
		return clampWidth(measure(c, p.notice, fontSource, sizeSource)+52, 0, widthPanel), heightCapsule
	case p.capsule:
		return capsuleWidth(c, p.source), heightCapsule
	case p.failed != "":
		return max(capsuleWidth(c, p.source), widthError, min(p.width(), widthPanel)), min(p.heightTo+p.footerHeight(), p.limit())
	}
	return p.width(), min(p.heightTo+p.footerHeight(), p.limit())
}

// follow lets the height grow with the text at once, and shrink only at the
// end: lines that wrap differently along the way should not make it pump.
func (p *panel) follow() {
	if !p.capsule && p.notice == "" && (p.natural > p.heightTo || p.done) {
		p.heightTo = p.natural
	}
}

// text is what copying takes: what is selected in the result, or all of it.
func (p *panel) text() string {
	if p.picked != "" {
		return p.picked
	}
	if p.copyText != nil {
		if t := p.copyText(); t != "" {
			return t
		}
	}
	if p.md != "" {
		return p.md
	}
	return p.received()
}

// followEnd keeps the end of a stream in view once the text has outgrown the
// panel, and not before. viewH is the height the text will have when the surface
// gets where it is going: while it grows, its height is behind its content for a
// few frames, and following the content then made the text hop up by a line and
// slide back, each line. A wheel ends the following, and scrolling back down to
// the end starts it again. It goes on a moment past the end, as the last piece
// has yet to be measured when the run is done; after that the text is the user's,
// who may open the source above it without being scrolled away. A press stops the
// text where it is, for the selection it may start.
func (p *panel) followEnd(viewH float32) {
	if p.closing {
		return
	}
	at, most := p.scroll.Y, p.scroll.MaxY
	if p.held && p.following && p.scroll.Scrolling() {
		p.followed = at
		// Any spring would carry on with the glide's speed.
		p.scroll.ScrollTo(0, at, ui.Animation{Duration: time.Nanosecond})
		return
	}
	if p.done && p.now().Sub(p.doneAt) >= fadeNib {
		return
	}
	if p.following && !p.scroll.Scrolling() && at < min(p.followed, most)-1 {
		p.following = false
	}
	if !p.following {
		p.following = most > 0 && at >= most-1
	}
	if !p.following || p.held {
		return
	}
	over := p.natural - viewH
	if over < 0.5 {
		over = 0 // the two may differ by a rounding: that is not overflowing
	}
	if over != p.followed {
		p.followed = over
		p.scroll.ScrollTo(0, over, ui.Animation{Spring: motionFollow})
	}
}

// view builds the frame: a clear root, with the surface in it.
func (p *panel) view() func(*ui.Context) {
	return func(c *ui.Context) {
		now := p.now()
		reduce := c.Preferences().ReduceMotion
		p.settle(func(text, font string, size float32) float32 { return measure(c, text, font, size) }, now)
		p.release(now)
		p.follow()
		p.track()
		p.spots = p.spots[:0]
		c.SetTheme(inkTheme(c.Theme()))
		root := c.Root().Background(ui.Transparent)

		// The content keeps the width it was laid out for while the surface
		// folds around it on the way out: rewrapped, it would swim.
		wantW, wantH := p.target(c)
		contentW := wantW
		if p.closing {
			wantW, wantH = min(capsuleWidth(c, p.source), wantW), heightCapsule
		}
		motion, motionW := motionStream, motionGrow
		switch {
		case p.closing:
			motion, motionW = motionExit, motionExit
		case p.capsule || p.notice != "" || (!p.done && now.Sub(p.openedAt) < 400*time.Millisecond):
			motion = motionGrow
		}
		w := root.Spring(springKey{"w", p.gen}, wantW, motionW)
		h := root.Spring(springKey{"h", p.gen}, wantH, motion)
		r := radiusCapsule
		if !p.capsule && p.notice == "" && !p.closing {
			r = radiusPanel
		}
		radius := root.Spring(springKey{"r", p.gen}, r, motionW)

		fade := ui.Animation{Duration: 80 * time.Millisecond}
		opacity := float32(1)
		switch {
		case p.closing:
			// The surface stays for the fold, and goes with its end.
			opacity, fade = 0, ui.Animation{Duration: exitTime - exitFade, Delay: exitFade}
		case !p.entered:
			opacity = 0
		case p.givingWay:
			opacity, fade = 0.32, ui.Animation{Duration: 140 * time.Millisecond}
		}
		alpha := root.AnimateTo(springKey{"fade", p.gen}, opacity, fade)
		contentAlpha := root.AnimateTo(springKey{"content", p.gen}, b2f32(!p.closing), ui.Animation{Duration: exitFade})

		// Lifted by its shadow: scaling a surface full of text costs tens of
		// milliseconds a frame (measured), and this has to follow the hand.
		lift := root.Spring(springKey{"lift", p.gen}, b2f32(p.dragging), motionPress)

		// The shadow lives outside the clip, the content inside it.
		surface := ui.Box(c).Key(fmt.Sprintf("surface-%d", p.gen)).Absolute().Left(margin).Top(margin).
			Width(w).Height(h).Radius(radius).Background(colorSurface).
			Shadow(0, 14+8*lift, 36+14*lift, -6, colorShadow).Shadow(0, 2, 6, 0, colorContact).
			InsetShadow(0, 0, 0, 1, colorRim).
			Opacity(alpha).TransformOrigin(min(1, max(0, p.anchor/max(w, 1))), 0).Cursor(p.wantCursor)

		// What the text is, this frame: mygo's elements do not outlive the
		// build that made them.
		var inner, text ui.Element
		foot := p.footerHeight()
		viewH := max(0, h-foot)
		p.followEnd(max(0, wantH-foot))
		surface.Children(func() {
			ui.Column(c).Key("frame").Width(w).Height(h).Radius(radius).Clip().Opacity(contentAlpha).Children(func() {
				clip := ui.Scroll(c).Key("clip").Scrollbars(false).TrackScroll(&p.scroll).Width(w).Height(viewH)
				clip.Children(func() {
					inner = ui.Column(c).Key("inner").Width(contentW).Padding(paddingY, paddingX).Gap(gapGroup)
					inner.Children(func() {
						if p.notice != "" {
							p.noticeRow(c)
							return
						}
						p.sourceRow(c, now, reduce)
						if p.md != "" {
							if p.planned {
								text = p.markdownView(c)
							}
						} else {
							text = p.blocksView(c, now)
						}
						if p.failed != "" {
							text = p.failure(c)
						}
					})
				})
				if foot > 0 {
					p.footerRow(c, w, now)
				}
				p.scrollEdges(c, w, viewH)
			})
			p.pinDot(c, surface)
			p.toEnd(c, w, h-foot)
			p.statusView(c, now, w, foot)
			p.tipView(c, now, w, h)
		})

		pressed := surface.Pressed() && !p.overFooter()
		if p.closing && alpha <= 0.01 && p.closed != nil {
			p.closed()
		}
		scale := float32(1)
		anim := ui.Animation{Spring: motionRelease}
		// Only the capsule scales, with next to no text on it.
		switch {
		case !p.entered:
			scale, anim = scaleFrom, ui.Animation{Spring: motionPop}
		case p.justEntered:
			anim = ui.Animation{Spring: motionPop}
		}
		surface.AnimatePose(ui.Pose{Scale: scale, X: p.shake(now, reduce)}, anim)
		p.justEntered = false
		if !p.entered {
			p.entered, p.justEntered = true, true
			c.Invalidate()
		}

		p.surfaceW, p.textBox = w, text.Bounds()
		p.footerTop = h - foot
		p.picked = p.pickedText(text)
		// Before transforms: a pose must not read as text that grew.
		if b := inner.LayoutBounds(); b.H > 0 && b.H != p.natural {
			p.natural = b.H
			c.Invalidate()
		}
		if p.frame != nil {
			p.frame(c, w, h, wantW, wantH, pressed)
		}
		p.animating = p.busy(now) || abs32(w-wantW) > 0.5 || abs32(h-wantH) > 0.5
		if p.busy(now) {
			c.Invalidate()
		}
	}
}

// pickedText is what the user selected in the result, if anything. An element
// that was not built this frame has no selection to report.
func (p *panel) pickedText(e ui.Element) string {
	if e == (ui.Element{}) {
		return ""
	}
	var parts []string
	for _, part := range e.SelectedTexts() {
		runes := []rune(part.Text)
		from, to := min(part.Start, len(runes)), min(part.End, len(runes))
		if from < to {
			parts = append(parts, string(runes[from:to]))
		}
	}
	if len(parts) < 2 {
		return strings.Join(parts, "")
	}
	src := p.md
	if src == "" {
		src = p.received()
	}
	return joinPicked(parts, src)
}

// joinPicked puts selected paragraphs back together as they stand in the source:
// cells of a row with tabs, paragraphs with a blank line. The view only says which
// paragraphs they are, so they are looked for in the source, with the marks that
// do not show set aside; one that is not found joins with a newline. A part found
// early, in a longer line, at worst gets a blank line for a newline.
func joinPicked(parts []string, src string) string {
	src = inlineMarks.Replace(src)
	var out strings.Builder
	at := 0
	for i, part := range parts {
		key := inlineMarks.Replace(part)
		sep := "\n"
		if idx := strings.Index(src[at:], key); idx >= 0 {
			between := src[at : at+idx]
			switch {
			case strings.Contains(between, "\n\n"):
				sep = "\n\n"
			case !strings.Contains(between, "\n") && strings.Contains(between, "|"):
				sep = "\t"
			}
			at += idx + len(key)
		}
		if i > 0 {
			out.WriteString(sep)
		}
		out.WriteString(part)
	}
	return out.String()
}

var inlineMarks = strings.NewReplacer("*", "", "_", "", "`", "", "~", "")

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
	if !p.done || now.Sub(p.doneAt) < fadeNib {
		return true
	}
	if !p.openedAt.IsZero() && now.Sub(p.openedAt) < fadeSource {
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
		tog := ui.ButtonBase(c.Key("fold")).Gap(2).AlignItems(ui.Center).AlignSelf(ui.Start).Cursor(p.wantCursor).Label(hint)
		col := colorHint
		if p.hover("fold", tog, "") {
			col = colorSource
		}
		tog.TextColor(tog.AnimateColor("fg", col, hoverAnim))
		turn := tog.AnimateTo("turn", b2f32(p.sourceOpen)*180, ui.Animation{Duration: rotateMenu, Ease: iconEase})
		tog.Children(func() {
			ui.Text(c, hint).Font(fontText).FontSize(sizeHint).LineHeight(lineHint / sizeHint).NoWrap()
			glyphIcon(c, glyphCaret, sizeActionMark-1).Rotate(turn)
		})
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
	add(runes[from:to], colorMarker)
	add(runes[to:], colorText)
	return spans
}

// blocksView draws the blocks, and returns the selectable box around them, valid
// for this build only.
func (p *panel) blocksView(c *ui.Context, now time.Time) ui.Element {
	if len(p.blocks) == 0 || !p.planned {
		return ui.Element{}
	}
	box := ui.Column(c).Key("blocks").Gap(gapGroup).Selectable().Cursor(p.wantCursor)
	box.Children(func() {
		for i, b := range p.blocks {
			nib := (!p.done || now.Sub(p.doneAt) < fadeNib) && i == len(p.blocks)-1
			p.blockView(c, i, b, now, nib)
		}
	})
	return box
}

// blockView is a block's paragraphs, with a gap between them as a page has. Those
// done are the same element frame after frame: only the last changes.
func (p *panel) blockView(c *ui.Context, i int, b *block, now time.Time, nib bool) {
	ui.Column(c).Key(fmt.Sprintf("%d-%d", p.run, i)).Gap(gapBlock).Children(func() {
		k := 0
		para := func(spans []ui.Span) ui.Element {
			el := p.styled(ui.RichText(c.Key(k), spans...), b.kind)
			k++
			return el
		}
		for _, text := range b.paras {
			para([]ui.Span{{Text: text, Color: p.ink(b.kind)}})
		}
		// What is left: the settled start of a paragraph, and what fades in,
		// which may end this one and begin others.
		var spans []ui.Span
		if b.settled.Len() > 0 {
			spans = append(spans, ui.Span{Text: b.settled.String(), Color: p.ink(b.kind)})
		}
		for _, ch := range b.young {
			spans = appendFading(spans, ch, p.ink(b.kind), now)
			if ch.brk {
				para(spans)
				spans = nil
			}
		}
		if nib {
			size := p.textSize(b.kind)
			spans = append(spans, ui.Span{Text: "\uFFFC", ObjectWidth: 4, ObjectHeight: 0.85 * size, ObjectDescent: 0.1 * size})
		}
		if len(spans) > 0 {
			para(spans).DrawObjects(func(pt *ui.Painter, _ int, r ui.Rect) {
				pt.Fill(ui.Rect{X: r.X + 2, Y: r.Y, W: 2, H: r.H}, colorMarker.Alpha(p.nibAlpha(p.now())), 1)
			})
		}
	})
}

// appendFading adds a chunk as far as it has faded in. Its opacity is in steps, so
// chunks alike are one span, and so one run of text for the shaper: shaped apart,
// Latin text would close up by a hair as each chunk joined the last.
func appendFading(spans []ui.Span, ch chunk, base ui.Color, now time.Time) []ui.Span {
	q := min(1, float32(now.Sub(ch.at))/float32(fadeReveal))
	e := 1 - (1-q)*(1-q)*(1-q)
	col := base.Alpha(float32(int(e*8)) / 8)
	if n := len(spans); n > 0 && spans[n-1].Color == col {
		spans[n-1].Text += ch.text
		return spans
	}
	return append(spans, ui.Span{Text: ch.text, Color: col})
}

// ink is the color of a block's text: a soft one for reading, and the light of
// a single word, whose size already asks for attention.
func (p *panel) ink(kind core.BlockKind) ui.Color {
	switch {
	case kind != core.BlockText:
		return colorSource
	case p.word:
		return colorHead
	}
	return colorBody
}

func (p *panel) textSize(kind core.BlockKind) float32 {
	switch {
	case kind != core.BlockText:
		return sizeSource
	case p.word:
		return sizeWord
	}
	return sizeBody
}

func (p *panel) styled(el ui.Element, kind core.BlockKind) ui.Element {
	switch kind {
	case core.BlockText:
		size, line := sizeBody, lineBody
		switch {
		case p.word:
			size, line = sizeWord, lineWord
		case p.reading:
			line = lineReading
		}
		font := fontText
		if p.cjk {
			font = fontTextCJK
		}
		return el.Font(font).FontSize(size).LineHeight(line / size).SelectionColor(colorSelected)
	case core.BlockPhonetic:
		return el.Font(fontPhonetic).FontSize(sizeSource).LineHeight(lineSource / sizeSource)
	}
	return el.Font(fontSource).FontSize(sizeSource).LineHeight(lineSource / sizeSource)
}

// track reads the pointer each frame, and on the shell's ticks between them.
// It reports whether a frame must show the change: the elements carry the
// cursor too.
func (p *panel) track() bool {
	if p.local == nil {
		return false
	}
	x, y := p.local()
	cursor, hot := p.wantCursor, p.hot
	p.wantCursor = p.pointerAt(x, y)
	if p.hot != hot {
		p.hotSince = p.now()
	}
	return p.wantCursor != cursor || p.hot != hot
}

// hover names a control for the pointer and says whether the pointer is on it.
// mygo reports no hover and shows no tips while its app is not active, and
// Gloss's is not, so the panel finds this itself, as it does for the cursor.
func (p *panel) hover(key string, e ui.Element, tip string) bool {
	p.spots = append(p.spots, spot{key: key, box: e.Bounds(), tip: tip, foot: p.inFooter})
	return p.hot == key
}

// spotAt is the spot at a point in from the surface's top left: the last one
// laid out on top. Not what scrolled behind the footer.
func (p *panel) spotAt(x, y float32, inFooter bool) (spot, bool) {
	for i := len(p.spots) - 1; i >= 0; i-- {
		s := p.spots[i]
		if s.box.W > 0 && s.box.Contains(x+margin, y+margin) && (s.foot || !inFooter) {
			return s, true
		}
	}
	return spot{}, false
}

// tipView is the tip of what the pointer rests on: above it, on the surface,
// and through to whatever is under it.
func (p *panel) tipView(c *ui.Context, now time.Time, w, h float32) {
	var s spot
	if !p.dragging {
		for _, o := range p.spots {
			if o.key == p.hot {
				s = o
			}
		}
	}
	if s.tip == "" {
		if p.tipping {
			p.tipping, p.tipGone = false, now
		}
		return
	}
	if wait := tipDelay - now.Sub(p.hotSince); !p.tipping && wait > 0 && now.Sub(p.tipGone) > tipWarm {
		c.After(wait)
		return
	}
	p.tipping = true
	width := bubbleWidth(c, s.tip, 240)
	x := s.box.X - margin + (s.box.W-width)/2
	bubble(c, "tip", s.tip, colorText, width).Left(max(gapBubble, min(x, w-width-gapBubble))).Bottom(h - (s.box.Y - margin) + gapBubble).Role(ui.RoleTooltip)
}

// statusView says how a replace went, just above the buttons. It floats because
// the footer has no room to spare at its narrowest: a line in it pushed them out
// of the panel, and these messages say what to do next, so none can be cut short.
func (p *panel) statusView(c *ui.Context, now time.Time, w, foot float32) {
	since := now.Sub(p.statusAt)
	if p.status == "" || foot <= 0 || since >= statusFor {
		return
	}
	width := bubbleWidth(c, p.status, w-2*gapBubble)
	bubble(c, "status", p.status, p.statusTone, width).Right(gapBubble).Bottom(foot + gapBubble).Role(ui.RoleStatus).
		Opacity(min(1, float32(since)/float32(fadeHover), float32(statusFor-since)/float32(fadeHover)))
}

func bubbleWidth(c *ui.Context, text string, most float32) float32 {
	return min(measure(c, text, fontText, sizeHint)+2*padBubble+1, most)
}

// bubble is a few words over the panel, on the surface and through to whatever
// is under them.
func bubble(c *ui.Context, key, text string, tone ui.Color, width float32) ui.Element {
	return ui.Box(c).Key(key).Absolute().Width(width).Padding(4, padBubble).Radius(radiusControl).Background(inkPopup).
		InsetShadow(0, 0, 0, 1, colorRim).Shadow(0, 2, 6, 0, colorContact).PassThrough().Children(func() {
		ui.Text(c, text).Font(fontText).FontSize(sizeHint).LineHeight(lineHint / sizeHint).TextColor(tone)
	})
}

// pointerAt says what a press would do from where the pointer is, and nothing
// else decides it: on a control, its work; on the text, a selection; elsewhere on
// the surface, moving the panel. x and y are in from the surface's top left, in
// the layout, which a press's scale leaves alone.
func (p *panel) pointerAt(x, y float32) ui.Cursor {
	inFooter := p.footer() && y >= p.footerTop
	on, found := p.spotAt(x, y, inFooter)
	p.hot = on.key
	switch {
	case p.dragging:
		return ui.CursorGrabbing
	case found && !on.quiet:
		return ui.CursorPointer
	case inFooter:
		return ui.CursorDefault
	case p.pinned && x >= p.surfaceW-24 && y < 24:
		return ui.CursorPointer
	case p.textBox.W > 0 && p.textBox.Contains(x+margin, y+margin):
		return ui.CursorText
	}
	return ui.CursorGrab
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

// failure says what went wrong and offers the ways out. It returns the words,
// which can be selected to look up or report.
func (p *panel) failure(c *ui.Context) ui.Element {
	var words ui.Element
	ui.Column(c).Gap(2).Children(func() {
		title := p.failedTitle
		if title == "" {
			title = "翻译失败"
		}
		ui.Text(c, title).Font(fontSource).FontSize(sizeSource).LineHeight(lineSource / sizeSource).TextColor(colorDanger)
		words = ui.Text(c, p.failed).Font(fontSource).FontSize(sizeSource).LineHeight(lineSource / sizeSource).TextColor(colorSource).Selectable()
	})
	ui.Row(c).Gap(8).AlignSelf(ui.Start).Children(func() {
		if p.failureButton(c, "retry", "重试", glyphRetry, colorDanger.Alpha(0.18), colorDanger.Alpha(0.28)) && p.retry != nil {
			p.retry()
		}
		if p.fixDo != nil && p.failureButton(c, "fix", p.fix, glyphTune, colorControl, colorEdge) {
			p.fixDo()
		}
	})
	return words
}

// failureButton is one way out of a failure. It says whether it was clicked.
func (p *panel) failureButton(c *ui.Context, key, label string, g *ui.SVG, rest, lit ui.Color) bool {
	b := ui.ButtonBase(c.Key(key)).Height(24).Padding(0, 10, 0, 8).Gap(4).Radius(radiusControl).AlignItems(ui.Center).Cursor(p.wantCursor).Label(label)
	bg := rest
	if p.hover(key, b, "") {
		bg = lit
	}
	b.Background(b.AnimateColor("bg", bg, hoverAnim)).TextColor(colorText)
	pressable(b, scalePressed)
	b.Children(func() {
		glyphIcon(c, g, sizeActionMark)
		ui.Text(c, label).Font(fontSource).FontSize(sizeSource).LineHeight(2).NoWrap()
	})
	return b.Clicked()
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
		ui.Box(c).Width(6).Height(6).Radius(3).Background(colorMarker).Scale(size, size)
	})
	if dot.Clicked() {
		p.pinned = false
	}
}

// mdTheme is Markdown set for reading, in layers: the body a soft warm grey, the
// words that structure it brighter, and the marker for the sections and the
// bullets that a skimming eye lands on. Headings stay near the body's size, as a
// panel is not a page. Everything is on the panel's ink, as tokens.go has it.
func mdTheme(cjk bool) *markdown.Theme {
	font := fontText
	if cjk {
		font = fontTextCJK
	}
	th := &markdown.Theme{
		FontSize:            sizeBody,
		Font:                font,
		CodeFont:            fontSource,
		LineHeight:          lineReading / sizeBody,
		CodeLineHeight:      1.55,
		Text:                colorBody,
		Muted:               colorAccent,
		Link:                colorMarker,
		Strong:              colorHead,
		StrongWeight:        650,
		Code:                colorCodeInk,
		CodeBackground:      washCode,
		CodeSize:            0.88,
		BlockSpacing:        gapBlock / sizeBody,
		ItemSpacing:         0.42,
		ListIndent:          1.4,
		QuoteBar:            colorAccent.Alpha(0.55),
		QuoteBarWidth:       2,
		QuoteText:           colorSource,
		Rule:                colorRule,
		CodeBlockBackground: washCode,
		CodeBlockBorder:     colorRim,
		CodeBlockRadius:     radiusControl,
		CodeColors:          editor.DefaultTheme(ui.DarkTheme()),
		TableBorder:         colorRim,
		TableHeader:         washCode,
		// mygo draws the check in white: the marker would swallow it.
		Checkbox: inkPlate,
	}
	sizes := [6]float32{1.34, 1.2, 1.07, 1, 1, 1}
	colors := [6]ui.Color{colorHead, colorHead, colorAccent, colorAccent, colorHead, colorHead}
	for i := range th.Headings {
		th.Headings[i] = markdown.HeadingStyle{Size: sizes[i], Weight: 650, Color: colors[i], Top: 1.5, Bottom: 0.5}
	}
	return th
}

// mdReveal fades in what the panel's pacer lets out: the pacer decides how fast,
// and mygo's own would hold it back a second time. It shows whatever it is given
// within a frame, and a mark on it says when, for the fade.
var mdReveal = markdown.Reveal{Unit: markdown.ByCharacter, CatchUp: 100 * time.Microsecond, Fade: fadeReveal}

// markdownView streams Markdown as far as the pacer has let it out, and shows it
// whole once it is done, in the same place, so the parse carries on. It returns
// the selectable view.
func (p *panel) markdownView(c *ui.Context) ui.Element {
	if th := p.mdOpts.Theme; th == nil || (th.Font == fontTextCJK) != p.cjk {
		p.mdOpts.Theme = mdTheme(p.cjk)
		p.mdOpts.WrapCode = true
		p.mdOpts.CodeBlock = p.codeBlock
		p.mdOpts.Reveal = &mdReveal
	}
	var el ui.Element
	shown := p.md[:p.mdAt.at]
	ui.Column(c).Key(fmt.Sprintf("md-%d", p.run)).Cursor(p.wantCursor).Children(func() {
		if p.done {
			el = markdown.View(c, shown, &p.mdOpts)
		} else {
			el = markdown.Stream(c, shown, &p.mdOpts)
		}
		el.SelectionColor(colorSelected)
	})
	return el
}

// codeBlock is a code block in the panel's words and icons: its language,
// and a copy button that waits for the block to close.
func (p *panel) codeBlock(c *ui.Context, b *markdown.CodeBlockView) {
	box := ui.Column(c).Background(washCode).Border(1, colorRim).Radius(radiusControl).AlignSelf(ui.Stretch).Clip()
	box.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Padding(4, 6, 0, 12).Children(func() {
			ui.Text(c, b.Language).Font(fontSource).FontSize(sizeHint).TextColor(colorHint).Unselectable()
			ui.Spacer(c)
			at := p.codeCopied[b.Index]
			copied := !at.IsZero() && c.Now().Sub(at) < 2*time.Second
			btn := ui.Row(c).Height(22).Padding(0, 8).Gap(4).Radius(radiusControl).AlignItems(ui.Center).Cursor(p.wantCursor).Label("复制代码")
			hot := p.hover(fmt.Sprintf("code-%d", b.Index), btn, "")
			if b.Open {
				btn.Opacity(0.4)
			} else if hot {
				btn.Background(colorControl)
			}
			icon, label, col := iconCopy, "复制", colorHint
			if copied {
				icon, label, col = iconCheck, "已复制", colorSuccess
			}
			btn.Children(func() {
				ui.Icon(c, icon).Size(13, 13).TextColor(col)
				ui.Text(c, label).Font(fontText).FontSize(sizeHint).TextColor(col).NoWrap()
			})
			if btn.Clicked() && !b.Open {
				c.WriteClipboard(b.Code())
				if p.codeCopied == nil {
					p.codeCopied = map[int]time.Time{}
				}
				p.codeCopied[b.Index] = c.Now()
				c.After(2 * time.Second)
			}
		})
		b.Body(c)
	})
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
	p.inFooter = true
	defer func() { p.inFooter = false }()
	ui.Row(c).Key("footer").Width(w).Height(heightFooter).Padding(0, 10, 0, 8).Gap(4).AlignItems(ui.Center).
		InsetShadow(0, 1, 0, 0, colorRim).Children(func() {
		if len(p.features) > 1 {
			ui.Row(c).Key("features").Gap(2).AlignItems(ui.Center).Children(func() {
				for _, f := range p.features {
					p.featureChip(c, f)
				}
			})
			if p.current != "" {
				lab := ui.Text(c, byLabel(p.by)).Key("by").Font(fontText).FontSize(sizeHint - 1).LineHeight(lineHint / (sizeHint - 1)).TextColor(colorHint).NoWrap().Shrink(0)
				p.spots = append(p.spots, spot{key: "by", box: lab.Bounds(), tip: p.why, quiet: true, foot: true})
			}
		}
		ui.Box(c).Grow(1)
		p.footerActions(c, now)
	})
}

// footerActions are what can be done with the result. They keep their place from
// the start and fade in once it can be done, which is once the network has
// finished: copying takes the whole result, wherever the pacer has got to.
func (p *panel) footerActions(c *ui.Context, now time.Time) {
	ready := p.netDone && p.failed == ""
	if p.failed != "" {
		return
	}
	box := ui.Row(c).Key("actions").Gap(4).AlignItems(ui.Center).Shrink(0)
	box.Opacity(box.AnimateTo("in", b2f32(ready), ui.Animation{Duration: fadeSource}))
	box.Children(func() {
		p.copyButton(c, ready, now)
		if p.replaceable && p.replace != nil {
			p.action(c, "replace", "替换", glyphSwap, colorSource, ready, p.replace)
		}
	})
}

// copyButton copies what is selected, or else the whole result, and answers in
// place. The word changes by what it gains or loses, 已 before it or 选中 after
// it, each taking its room with it, where a crossfade of two words that share 复制
// reads as a smudge. 复制 stays put unless 选中 is leaving from beside it. It turns
// back after copiedHold.
func (p *panel) copyButton(c *ui.Context, ready bool, now time.Time) {
	since := now.Sub(p.copiedAt)
	copied := !p.copiedAt.IsZero() && since < copiedHold
	picked := p.picked != "" && !copied
	label, mark, tone, ink := "复制", iconCopy, colorSource, colorSource
	switch {
	case copied:
		label, mark, tone, ink = "已复制", iconCheck, colorSuccess, colorSuccess
		c.After(copiedHold - since)
	case picked:
		label = "复制选中"
	}
	b := ui.ButtonBase(c.Key("copy")).Height(22).Radius(radiusControl).AlignItems(ui.Center).Justify(ui.Start).Shrink(0).Clip().Cursor(p.wantCursor).Label(label)
	var bg ui.Color
	if ready && p.hover("copy", b, "") {
		bg = colorControl
		if ink == colorSource {
			ink = colorText
		}
	}
	b.Background(b.AnimateColor("bg", bg, hoverAnim)).TextColor(b.AnimateColor("fg", ink, hoverAnim))
	done := max(0, min(b.Spring("done", b2f32(copied), motionSettle), 1))
	more := max(0, min(b.Spring("more", b2f32(picked), motionSettle), 1))
	word := func(s string) float32 { return measure(c, s, fontText, sizeHint) }
	const pad = 6 + 3 + 8 // the padding either side and the gap to the mark
	b.Width(sizeActionMark + pad + word("复制") + done*word("已") + more*word("选中"))
	pressable(b, scalePressed)
	b.Children(func() {
		ui.Row(c).MarginStart(6).Gap(3).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Icon(c, mark).FontSize(sizeActionMark).Shrink(0).TextColor(b.AnimateColor("tone", tone, hoverAnim)).ContentTransition(ui.ContentTransition{Animation: &copyTurn})
			ui.Row(c).AlignItems(ui.Center).Shrink(0).Children(func() {
				wordPart(c, "已", done)
				actionWord(c, "复制")
				wordPart(c, "选中", more)
			})
		})
	})
	if b.Clicked() && ready {
		if text := p.text(); text != "" && p.copied != nil {
			p.copied(c, text)
			p.copiedAt = now
			c.Invalidate()
		}
	}
}

func actionWord(c *ui.Context, s string) {
	ui.Text(c, s).Font(fontText).FontSize(sizeHint).LineHeight(lineHint / sizeHint).NoWrap().Shrink(0)
}

// wordPart is a word that comes and goes inside a label: k of its width, and as
// visible.
func wordPart(c *ui.Context, s string, k float32) {
	if k <= 0.01 {
		return
	}
	ui.Box(c).Key(s).Width(k * measure(c, s, fontText, sizeHint)).Clip().Shrink(0).Opacity(k).Children(func() {
		actionWord(c, s)
	})
}

var copyTurn = ui.Animation{Spring: ui.SpringOptions{Duration: 240 * time.Millisecond}}

// featureChip is one feature in the footer. The one showing wears its name and
// its icon awake; the others are an icon, which wakes under the pointer and is
// named by a tip. Opening another moves the name across.
func (p *panel) featureChip(c *ui.Context, f feature) {
	on := f.id == p.current
	key := "f-" + f.id
	chip := ui.ButtonBase(c.Key(key)).Height(heightChip).Radius(radiusControl).AlignItems(ui.Center).Justify(ui.Start).Clip().Shrink(0).Cursor(p.wantCursor).Label(f.name)
	tip := f.name
	if on {
		tip = ""
	}
	hot := p.hover(key, chip, tip)
	var bg ui.Color
	switch {
	case on:
		bg = colorControl
	case hot:
		bg = washHover
	}
	chip.Background(chip.AnimateColor("bg", bg, hoverAnim))
	name := max(0, min(chip.Spring("name", b2f32(on), motionSettle), 1))
	const inset = 9 // from the name to the chip's end
	chip.Width(heightChip + (measure(c, f.name, fontText, sizeHint)+inset)*name)
	pressable(chip, scaleGiveWay)
	chip.Children(func() {
		box := sizeChipIcon * visualSize
		liveIcon(c, featureIcon(f.id), sizeChipIcon, on || hot).MarginStart((heightChip - box) / 2).MarginEnd((heightChip - box) / 2)
		if name > 0.01 {
			ui.Text(c, f.name).Font(fontText).FontSize(sizeHint).LineHeight(lineHint / sizeHint).TextColor(colorText).NoWrap().Shrink(0).Opacity(name)
		}
	})
	if chip.Clicked() && !on && p.switchTo != nil {
		p.switchTo(f.id)
	}
}

// action is one thing to do with the result: a mark in the colour of its word.
func (p *panel) action(c *ui.Context, key, label string, mark *ui.SVG, tone ui.Color, ready bool, do func()) {
	b := ui.ButtonBase(c.Key(key)).Height(22).Padding(0, 8, 0, 6).Gap(3).Radius(radiusControl).AlignItems(ui.Center).Justify(ui.Center).Shrink(0).Cursor(p.wantCursor).Label(label)
	var bg ui.Color
	text := colorSource
	if ready && p.hover(key, b, "") {
		bg, text = colorControl, colorText
	}
	b.Background(b.AnimateColor("bg", bg, hoverAnim)).TextColor(b.AnimateColor("fg", text, hoverAnim))
	pressable(b, scalePressed)
	b.Children(func() {
		ui.Icon(c, mark).FontSize(sizeActionMark).Shrink(0).TextColor(tone)
		ui.Text(c, label).Font(fontText).FontSize(sizeHint).LineHeight(lineHint / sizeHint).NoWrap()
	})
	if b.Clicked() && ready {
		do()
	}
}

// toEnd brings a long result back to its end, once the user scrolled up
// while it streamed.
func (p *panel) toEnd(c *ui.Context, w, bottom float32) {
	if p.following || p.scroll.MaxY <= 0 || p.done {
		return
	}
	b := ui.ButtonBase(c.Key("to-end")).Absolute().Left(w/2-44).Top(bottom-34).Width(88).Height(24).Radius(12).Gap(3).
		InsetShadow(0, 0, 0, 1, colorRim).AlignItems(ui.Center).Justify(ui.Center).Cursor(p.wantCursor).Label("回到底部").TextColor(colorText)
	bg := inkPopup
	if p.hover("to-end", b, "") {
		bg = inkPlate
	}
	b.Background(b.AnimateColor("bg", bg, hoverAnim))
	pressable(b, scalePressed)
	b.Children(func() {
		glyphIcon(c, glyphDown, sizeActionMark)
		ui.Text(c, "回到底部").Font(fontText).FontSize(sizeHint).LineHeight(lineHint / sizeHint).NoWrap()
	})
	if b.Clicked() {
		p.following = true
		p.followed = p.scroll.MaxY
		p.scroll.ScrollTo(0, math.MaxFloat32, ui.Animation{Spring: motionFollow})
		c.Invalidate()
	}
}

// scrollEdges shows there is more above or below: the text dissolves into the
// panel instead of ending at a hard line, which also lets new lines rise out of
// nothing, and a thin mark says how far along the page is. They only tell: a
// wheel scrolls, and every press goes through to what is under them.
func (p *panel) scrollEdges(c *ui.Context, w, viewH float32) {
	most := p.scroll.MaxY
	if most <= 0.5 || viewH <= 0 || p.capsule {
		return
	}
	at := max(0, min(p.scroll.Y, most))
	edge := func(key string, top, alpha float32, from, to ui.Color) {
		if alpha > 0.01 {
			ui.Box(c).Key(key).Absolute().Left(0).Top(top).Width(w).Height(heightEdge).PassThrough().Opacity(alpha).
				LinearGradient(ui.LinearGradient{From: from, To: to, Angle: 180})
		}
	}
	edge("edge-top", 0, min(1, at/heightEdge), colorSurface, colorSurface.Alpha(0))
	edge("edge-end", viewH-heightEdge, min(1, (most-at)/heightEdge), colorSurface.Alpha(0), colorSurface)
	long := max(24, viewH*viewH/(viewH+most))
	ui.Box(c).Key("mark").Absolute().Left(w - 6).Top(4 + at/most*(viewH-long-8)).Width(3).Height(long).Radius(1.5).Background(colorScrollbar).PassThrough()
}
