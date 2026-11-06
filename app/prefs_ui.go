package app

import (
	_ "embed"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// The parts of the settings window. Everything here draws in one language: the
// surfaces are ink, a selected or saved thing is swiped with the marker, and a
// control answers the pointer in the same few ways. See
// docs/decisions/0011-ink-and-marker.md.

const (
	widthSidebar float32 = 200
	heightNav    float32 = 36
	widthControl float32 = 220 // a text field, a long select
	widthSelect  float32 = 180 // a select of short names
	// A page is a column of widthPage, centred on the sheet. paddingPage is the
	// least it keeps from the sheet's edges when the window is too narrow for that.
	widthPage      float32 = 520
	paddingPage    float32 = 32
	paddingPageTop float32 = 16
	paddingPageEnd float32 = 56
	gapPage        float32 = 28 // between the head and the groups of a page
	paddingRow     float32 = 14 // above and below a setting
	paddingForm    float32 = 7  // above and below a field in a card
	marginSheet    float32 = 8
	// clearLights is how far the sidebar comes up into the title bar: macOS gives a
	// hidden-inset bar far more height than its controls take (66 on macOS 27).
	clearLights float32 = 16
	// heightSheetDrag is the strip along the top of the sheet that drags the window.
	heightSheetDrag float32 = 22

	// The type scale. Four sizes and no more: a title, what is read, what explains
	// it, and the small print. Hierarchy comes from these, the colours and the space.
	sizeTitle float32 = 24
	sizeUI    float32 = 14
	sizeHelp  float32 = 13
	sizeLabel float32 = 12
)

// pagePadding is the sheet's clear edge beside a page: what centres the column,
// but never less than paddingPage. The width is the last frame's, none on the first.
func pagePadding(sheet float32) float32 {
	if sheet == 0 {
		return paddingPage
	}
	return max(paddingPage, (sheet-widthPage)/2)
}

//go:embed icons/sticker.png
var stickerPNG []byte

var stickerImage = func() ui.ImageSource {
	img, err := ui.DecodeImage(stickerPNG)
	if err != nil {
		panic(err)
	}
	return img
}()

// inkTheme is the look of both windows, for what mygo draws by itself.
func inkTheme(base *ui.Theme) *ui.Theme {
	t := *base
	t.Dark = true
	t.Background = inkDeep
	t.Surface = inkRaised
	t.SurfaceHover = inkHover
	t.SurfacePressed = inkPressed
	t.Border = colorEdge
	t.Text = colorText
	t.TextMuted = colorMute
	t.Accent = colorMarker
	t.AccentHover = colorMarker
	t.AccentPressed = colorMarker
	t.AccentText = colorOnMarker
	t.Danger = colorDanger
	t.Success = colorSuccess
	t.Selection = colorSelected
	t.Focus = colorMarker.Alpha(0.6)
	t.Scrollbar = colorScrollbar
	t.Inverse, t.InverseText = inkPopup, colorText
	t.Radius = radiusField
	t.FontSize = sizeUI
	t.Font = fontText
	return &t
}

// hoverAnim and the rest are how a control's colours and pose answer the
// pointer: colour by curve, pose by spring, so a release overshoots a touch.
var (
	hoverAnim   = ui.Animation{Duration: fadeHover, Ease: ui.EaseOut}
	pressAnim   = ui.Animation{Spring: motionPress}
	releaseAnim = ui.Animation{Spring: motionRelease}
)

// pressable makes an element give way under the pointer, and spring back.
func pressable(e ui.Element, scale float32) {
	if e.Pressed() && !e.IsDisabled() {
		e.AnimatePose(ui.Pose{Scale: scale}, pressAnim)
	} else {
		e.AnimatePose(ui.Pose{}, releaseAnim)
	}
}

// markerBand fills the shape a marker leaves: edges a little off true, ends
// rounder than a rectangle's, none of it so loose that it reads as a mistake.
// width is how far along the stroke has come, from 0 to 1.
func markerBand(pt *ui.Painter, r ui.Rect, along float32, col ui.Color) {
	if along <= 0 {
		return
	}
	w := r.W * along
	lean := min(w*0.5, 2.6)
	var path ui.Path
	path.MoveTo(r.X+0.8, r.Y+1.6)
	path.QuadTo(r.X+w*0.42, r.Y-0.5, r.X+w-2, r.Y+1)
	path.QuadTo(r.X+w+lean, r.Y+r.H*0.48, r.X+w-2.6, r.Y+r.H-0.8)
	path.QuadTo(r.X+w*0.56, r.Y+r.H+0.7, r.X+1.4, r.Y+r.H-1.4)
	path.QuadTo(r.X-lean*0.8, r.Y+r.H*0.55, r.X+0.8, r.Y+1.6)
	path.Close()
	pt.FillPath(&path, col)
}

// swipe is how far a stroke of the marker has come, by when it began.
func swipe(since, d time.Duration, reduced bool) float32 {
	if reduced || since >= d {
		return 1
	}
	if since <= 0 {
		return 0
	}
	return iconEase(float32(since) / float32(d))
}

// sectionLabel is the small heading above a group of rows.
func sectionLabel(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).Font(fontSource).FontSize(sizeLabel).FontWeight(500).LetterSpacing(0.3).TextColor(colorMute).SingleLine()
}

// The navigation.

// navItem is one place in the sidebar. The marker hugs the label, not the row:
// a stroke over the word is what selecting means in Gloss. The label's ink turns
// as the stroke arrives, and the icon wakes on hover and stays awake while the
// place is chosen.
func (p *prefs) navItem(c *ui.Context, id, icon, label string, trailing func()) {
	chosen := p.place() == id
	const textAt float32 = 38
	const iconAt, iconSize float32 = 5, 20
	item := ui.SidebarItem(c, id, nil, label).Background(ui.Transparent).Height(heightNav).Padding(0, 8, 0, textAt).Radius(radiusField)
	h := heightNav
	// Inside a feature's page, the place is already chosen: a press still comes back.
	if item.Clicked() {
		p.page = id
	}
	hovered := item.Hovered()
	reduced := c.Preferences().ReduceMotion

	since := ui.Local(item, "chosenAt", func() time.Time { return time.Time{} })
	was := ui.Local(item, "wasChosen", func() bool { return chosen })
	switch {
	case chosen && !*was:
		*since = c.Now()
	case chosen && since.IsZero():
		*since = c.Now().Add(-swipeTime) // chosen as the window opened: already there
	}
	*was = chosen
	along := float32(1)
	if chosen {
		along = swipe(c.Now().Sub(*since), swipeTime, reduced)
		if along < 1 {
			c.AnimationFrame()
		}
	}
	// The ink goes when the stroke has passed; a place left lets go of its
	// stroke quickly, so the eye follows only the new one.
	fade := item.AnimateTo("mark", b2f32(chosen), ui.Animation{Duration: 130 * time.Millisecond, Ease: ui.EaseOut})
	tint := item.AnimateTo("tint", b2f32(hovered && !chosen), hoverAnim)

	ink := navInk
	if hovered {
		ink = colorText
	}
	if chosen {
		ink = colorOnMarker
	}
	item.TextColor(item.AnimateColor("ink", ink, ui.Animation{Duration: 110 * time.Millisecond, Ease: ui.EaseOut}))
	pressable(item, scaleGiveWay)

	textW := float32(0)
	item.Draw(func(pt *ui.Painter, r ui.Rect) {
		if textW == 0 {
			textW, _ = pt.MeasureText(0, ui.Span{Text: label, Font: fontText, Size: sizeUI})
		}
		band := ui.Rect{X: r.X + textAt - 6, Y: r.Y + (r.H-26)/2, W: textW + 12, H: 26}
		if tint > 0 {
			markerBand(pt, band, 1, colorMarker.Alpha(0.13*tint))
		}
		if fade > 0 {
			markerBand(pt, band, along, colorMarker.Alpha(fade))
		}
	})
	item.Children(func() {
		liveIcon(c, icon, iconSize, hovered || chosen).Absolute().Left(iconAt).Top((h - iconSize*visualSize) / 2)
		if trailing != nil {
			trailing()
		}
	})
}

func b2f32(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// The rows.

// rows groups settings, with no box and no rules around them: space parts them,
// and the sheet is the box. A heading is for when a page has more than one group.
func (p *prefs) rows(c *ui.Context, key, title string, fn func()) {
	ui.Column(c).Key(key).Margin(-paddingRow, 0, 0, 0).Children(func() {
		if title != "" {
			sectionLabel(c, title).Margin(paddingRow, 0, 2, 0)
		}
		fn()
	})
}

// rowSpec is one setting: its name and help on the left, its control on the right,
// and below them what went wrong with it. Writing it swipes its name with the marker.
type rowSpec struct {
	key, icon, title, badge, help string
	pad                           float32
	control                       func()
}

func (p *prefs) rowOf(c *ui.Context, s rowSpec) {
	col := ui.Column(c).Key(s.key).Padding(s.pad, 0).Gap(6)
	reduced := c.Preferences().ReduceMotion
	col.Children(func() {
		ui.Row(c).Gap(24).AlignItems(ui.Center).Children(func() {
			if s.icon != "" {
				liveIcon(c, s.icon, 24, col.Hovered()).Margin(0, 14-24, 0, -2)
			}
			ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					name := ui.Box(c).Shrink(1)
					name.Draw(func(pt *ui.Painter, r ui.Rect) {
						p.drawSaved(pt, s.key, ui.Rect{X: r.X - 4, Y: r.Y + (r.H-24)/2, W: r.W + 8, H: 24}, reduced)
					})
					name.Children(func() { ui.Text(c, s.title).TextColor(colorText) })
					if s.badge != "" {
						tag(c, s.badge, tagMarker)
					}
				})
				if s.help != "" {
					ui.Text(c, s.help).FontSize(sizeHelp).LineHeight(1.5).TextColor(colorMute)
				}
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Justify(ui.End).Shrink(0).Children(s.control)
		})
		if msg := p.errs[s.key]; msg != "" {
			errorText(c, msg)
		}
	})
}

// row is a setting of a page.
func (p *prefs) row(c *ui.Context, key, title, help string, control func()) {
	p.rowOf(c, rowSpec{key: key, title: title, help: help, pad: paddingRow, control: control})
}

// fieldRow is a field of a form in a card: closer together, and with nothing to
// explain that its placeholder does not.
func (p *prefs) fieldRow(c *ui.Context, key, title string, control func()) {
	p.rowOf(c, rowSpec{key: key, title: title, pad: paddingForm, control: control})
}

// drawSaved passes the marker over a name once its value is written: a brief
// proof, in place, that there is no save button to look for.
func (p *prefs) drawSaved(pt *ui.Painter, key string, at ui.Rect, reduced bool) {
	if p.justSaved[key] {
		delete(p.justSaved, key)
		p.savedAt[key] = pt.Now()
	}
	began, ok := p.savedAt[key]
	if !ok {
		return
	}
	since := pt.Now().Sub(began)
	alpha := float32(0.3)
	if since > savedHold {
		f := float32(since-savedHold) / float32(savedFade)
		if f >= 1 {
			delete(p.savedAt, key)
			return
		}
		alpha *= 1 - f
	}
	markerBand(pt, at, swipe(since, swipeTime, reduced), colorMarker.Alpha(alpha))
	pt.AnimationFrame()
}

func errorText(c *ui.Context, msg string) ui.Element {
	return ui.Text(c, msg).FontSize(sizeHelp).LineHeight(1.45).TextColor(colorDanger).Selectable()
}

// Buttons.

type buttonKind int

const (
	buttonSecondary buttonKind = iota
	buttonPrimary
	buttonQuiet
	buttonDanger
)

type buttonSpec struct {
	label   string
	key     string
	kind    buttonKind
	glyph   *ui.SVG
	small   bool
	off     bool
	tooltip string
}

func (s buttonSpec) colors(hover, down bool) (bg, fg, edge ui.Color) {
	switch s.kind {
	case buttonPrimary:
		bg, fg = colorTomato, colorOnMarker
		if hover {
			bg = tomatoHover
		}
		if down {
			bg = tomatoPressed
		}
	case buttonQuiet:
		fg = colorSource
		if hover {
			bg, fg = colorControl, colorText
		}
		if down {
			bg = washHover
		}
	case buttonDanger:
		fg = colorDanger
		if hover {
			bg = colorDanger.Alpha(0.13)
		}
		if down {
			bg = colorDanger.Alpha(0.08)
		}
	default:
		bg, fg, edge = inkRaised, colorText, colorEdge
		if hover {
			bg, edge = inkHover, colorEdgeHi
		}
		if down {
			bg = inkPressed
		}
	}
	return
}

// button is the one button of the window, in its four kinds. A press sinks it;
// a primary one keeps a line of light on its top edge, as a raised thing does.
func button(c *ui.Context, s buttonSpec) ui.Element {
	h, r, pad, size := heightField, radiusField, float32(12), sizeUI
	if s.small {
		h, r, pad, size = heightSmall, radiusSmall, 10, sizeHelp
	}
	b := ui.ButtonBase(c.Key("btn."+or(s.key, s.label))).Height(h).Radius(r).Padding(0, pad).Gap(6).FontSize(size).Label(s.label).Disabled(s.off)
	if s.tooltip != "" {
		b.Tooltip(s.tooltip)
	}
	hover, down := b.Hovered() && !s.off, b.Pressed() && !s.off
	bg, fg, edge := s.colors(hover, down)
	if s.off {
		switch s.kind {
		case buttonPrimary:
			bg, fg = inkRaised, colorMute
		default:
			fg = colorMute
		}
	}
	b.Background(b.AnimateColor("bg", bg, hoverAnim)).TextColor(b.AnimateColor("fg", fg, hoverAnim))
	if edge.A > 0 || s.kind == buttonSecondary {
		b.Border(1, b.AnimateColor("edge", edge, hoverAnim))
	}
	if s.kind == buttonPrimary && !s.off {
		b.InsetShadow(0, 1, 0, 0, sheenStrong)
	}
	pressable(b, scalePressed)
	b.Children(func() {
		if s.glyph != nil {
			glyphIcon(c, s.glyph, 14)
		}
		if s.label != "" {
			ui.Text(c, s.label).NoWrap().FontWeight(buttonWeight(s.kind))
		}
	})
	return b
}

func buttonWeight(k buttonKind) int {
	if k == buttonPrimary {
		return 600
	}
	return 400
}

// iconButton is a quiet button of one glyph, named for those who cannot see it.
func iconButton(c *ui.Context, key string, g *ui.SVG, label string, off bool) ui.Element {
	b := ui.ButtonBase(c.Key("icon."+key)).Size(26, 26).Radius(radiusSmall).Label(label).Disabled(off).Tooltip(label)
	hover, down := b.Hovered() && !off, b.Pressed() && !off
	var bg ui.Color
	fg := colorSource
	switch {
	case off:
		fg = colorMute.Alpha(0.5)
	case down:
		bg, fg = washHover, colorText
	case hover:
		bg, fg = colorControl, colorText
	}
	b.Background(b.AnimateColor("bg", bg, hoverAnim)).TextColor(b.AnimateColor("fg", fg, hoverAnim))
	pressable(b, 0.92)
	b.Children(func() { glyphIcon(c, g, 14) })
	return b
}

// The switch.

// markerSwitch is a switch whose on is the marker's colour. Its knob springs
// across and stretches while held, as a thumb flattens on it.
func markerSwitch(c *ui.Context, on *bool, label string, off bool) ui.Element {
	sw := ui.SwitchBase(c.Key("sw."+label), on).Size(widthSwitchT, heightSwitchT).Radius(heightSwitchT / 2).Label(label).Disabled(off)
	hover, down := sw.Hovered() && !off, sw.Pressed() && !off
	track := switchOff
	switch {
	case *on:
		track = colorMarker
	case hover:
		track = switchOffHover
	}
	if off {
		track = washFill
	}
	sw.Background(sw.AnimateColor("track", track, ui.Animation{Duration: 160 * time.Millisecond, Ease: ui.EaseOut}))
	pos := sw.Spring("pos", b2f32(*on), motionKnob)
	knobH := heightSwitchT - 2*insetKnob
	stretch := sw.Spring("stretch", b2f32(down)*4, motionSettle)
	colKnob := sw.AnimateColor("knob", map[bool]ui.Color{true: colorOnMarker, false: knobOff}[*on], ui.Animation{Duration: 160 * time.Millisecond, Ease: ui.EaseOut})
	if off {
		colKnob = colKnob.Alpha(0.5)
	}
	sw.Draw(func(pt *ui.Painter, r ui.Rect) {
		w := knobH + stretch
		x := r.X + insetKnob + pos*(r.W-2*insetKnob-w)
		knob := ui.Rect{X: x, Y: r.Y + insetKnob, W: w, H: knobH}
		pt.Shadow(knob, knobH/2, 0, 1, 2, 0, shadowKnob)
		pt.Fill(knob, colKnob, knobH/2)
	})
	return sw
}

// The select.

// choice is a drop-down of options shown as they are named. Its arrow turns as
// it opens, and the menu's corners keep to the trigger's and to its items'.
func choice(c *ui.Context, key, label string, cur *string, options []string, width float32) ui.Element {
	sel := ui.SelectBase(c.Key(key), cur)
	b := sel.Trigger.Height(heightField).Width(width).Radius(radiusField).Padding(0, 10, 0, 11).Gap(8).Justify(ui.SpaceBetween).Label(label)
	open := sel.Open()
	hover := b.Hovered()
	bg, edge := inkRaised, colorEdge
	if hover || open {
		bg, edge = inkHover, colorEdgeHi
	}
	b.Background(b.AnimateColor("bg", bg, hoverAnim)).Border(1, b.AnimateColor("edge", edge, hoverAnim))
	pressable(b, scaleGiveWay)
	turn := b.AnimateTo("turn", b2f32(open)*180, ui.Animation{Duration: rotateMenu, Ease: iconEase})
	b.Children(func() {
		ui.Text(c, *cur).SingleLine().Shrink(1).TextColor(colorText)
		ui.Box(c).Size(10, 10).Shrink(0).Rotate(turn).Draw(func(pt *ui.Painter, r ui.Rect) {
			var path ui.Path
			path.MoveTo(r.X+r.W*0.12, r.Y+r.H*0.34).LineTo(r.X+r.W*0.5, r.Y+r.H*0.68).LineTo(r.X+r.W*0.88, r.Y+r.H*0.34)
			pt.StrokePath(&path, 1.5, colorMute)
		})
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(6, 0, 0, 0).Padding(paddingPopup).Radius(radiusPopup).Background(inkPopup).Border(1, colorEdge).Column().Gap(1).
			Shadow(0, 12, 32, 0, colorShadow).MaxHeight(320).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -4, Opacity: 0, Scale: 0.97}, Duration: 150 * time.Millisecond, Ease: iconEase})
		panel.TransformOrigin(0.5, 0)
		for _, opt := range options {
			item := sel.Item(opt).Height(32).Padding(0, 8).Gap(8).Radius(radiusItem).AlignItems(ui.Center)
			if item.Highlighted() {
				item.Background(colorControl)
			}
			item.Children(func() {
				ui.Text(c, opt).SingleLine().Grow(1).Shrink(1).TextColor(colorText)
				if opt == *cur {
					glyphIcon(c, glyphCheck, 13).TextColor(colorMarker)
				}
			})
		}
	})
	return b
}

// The segmented control.

// segmented chooses among a few short options: a plate slides under the chosen
// one, on a track whose corners hold it concentrically.
func segmented(c *ui.Context, key, label string, selected *int, options []string) ui.Element {
	parts := ui.SegmentedBase(c.Key(key), selected, len(options))
	track := parts.Track.Height(heightField).Padding(paddingCard / 2).Radius(radiusField).Background(washFill).Label(label)
	pos := track.Spring("pos", float32(*selected), motionKnob)
	var widths = ui.Local(track, "widths", func() []float32 { return nil })
	track.Draw(func(pt *ui.Painter, r ui.Rect) {
		ws := *widths
		if len(ws) != len(options) {
			return
		}
		// The plate goes from one option's box to the next, its width too.
		i := int(pos)
		i = max(0, min(i, len(ws)-1))
		j := min(i+1, len(ws)-1)
		f := pos - float32(i)
		var x float32
		for k := 0; k < i; k++ {
			x += ws[k]
		}
		w := ws[i]*(1-f) + ws[j]*f
		x += ws[i] * f
		plate := ui.Rect{X: r.X + 2 + x, Y: r.Y + 2, W: w, H: r.H - 4}
		pt.Shadow(plate, radiusField-2, 0, 1, 2, 0, shadowKnob)
		pt.Fill(plate, inkPlate, radiusField-2)
	})
	next := make([]float32, 0, len(options))
	track.Children(func() {
		for i, opt := range options {
			seg := parts.Segment(i).Padding(0, 12).Radius(radiusField - 2).Height(heightField - paddingCard)
			fg := colorSource
			if i == *selected {
				fg = colorText
			} else if seg.Hovered() {
				fg = colorText
			}
			seg.TextColor(seg.AnimateColor("fg", fg, hoverAnim))
			seg.Children(func() { ui.Text(c, opt).NoWrap() })
			next = append(next, seg.Bounds().W)
		}
	})
	*widths = next
	return track
}

// Fields.

// fieldLook gives a field its surface: raised ink, an edge that warms on hover,
// and on focus the marker's edge with a soft ring around it.
func fieldLook(box ui.Element, focused, invalid bool) {
	bg, edge := inkRaised, colorEdge
	ring := ui.Transparent
	switch {
	case invalid:
		edge, ring = colorDanger.Alpha(0.8), colorDanger.Alpha(0.14)
	case focused:
		bg, edge, ring = inkPressed, colorMarker.Alpha(0.75), colorMarker.Alpha(0.14)
	case box.Hovered():
		bg, edge = inkHover, colorEdgeHi
	}
	box.Radius(radiusField).Background(box.AnimateColor("bg", bg, hoverAnim)).Border(1, box.AnimateColor("edge", edge, hoverAnim))
	box.Shadow(0, 0, 0, 3, box.AnimateColor("ring", ring, hoverAnim))
}

type fieldSpec struct {
	key, label, placeholder string
	draft                   *textDraft
	commit                  func(string) error
	width                   float32
	mono, password          bool
	trailing                func()
}

// field is a text box that writes after the typing stops, or on Enter.
func (p *prefs) field(c *ui.Context, s fieldSpec) ui.Element {
	d := s.draft
	d.commit = s.commit
	width := s.width
	if width == 0 {
		width = widthControl
	}
	box := ui.Row(c.Key(s.key+".box")).Width(width).Height(heightField).AlignItems(ui.Center).Padding(0, 4, 0, 10).Gap(4).Shrink(0)
	var in ui.Element
	box.Children(func() {
		in = ui.TextInputBase(c.Key(s.key), &d.value).Placeholder(s.placeholder).PlaceholderColor(colorMute).Label(s.label).Grow(1).Shrink(1).FocusRing(false).TextColor(colorText).SingleLine().CaretWidth(1.5)
		if s.mono {
			in.Font(fontSource).FontSize(sizeHelp)
		}
		if s.password {
			in.Password()
		}
		if s.trailing != nil {
			s.trailing()
		}
	})
	// The box is the field: a click anywhere on it, not only on the line of text
	// inside, goes to the text.
	box.Cursor(ui.CursorText)
	if box.Clicked() {
		in.Focus()
	}
	invalid := p.errs[s.key] != ""
	if invalid {
		in.Error(p.errs[s.key])
	}
	fieldLook(box, in.Focused(), invalid)
	if in.Changed() {
		d.editedAt = c.Now()
	}
	if in.Submitted() && !d.editedAt.IsZero() {
		d.editedAt = c.Now().Add(-commitAfter)
	}
	if !d.editedAt.IsZero() {
		if left := commitAfter - c.Now().Sub(d.editedAt); left > 0 {
			c.After(left)
		} else {
			d.editedAt = time.Time{}
			if d.value != d.saved && s.commit(d.value) == nil {
				d.saved = d.value
			}
		}
	}
	return in
}

// stepper is a number in one piece with its − and +, and what it counts.
func (p *prefs) stepper(c *ui.Context, key, label string, d *numberDraft, lo, hi, step float64, unit string, commit func(float64) error) {
	d.commit = commit
	decimals := 0
	if s := strconv.FormatFloat(step, 'f', -1, 64); strings.Contains(s, ".") {
		decimals = len(s) - strings.IndexByte(s, '.') - 1
	}
	format := func(v float64) string { return strconv.FormatFloat(v, 'f', decimals, 64) }
	box := ui.Row(c.Key(key+".box")).Height(heightField).AlignItems(ui.Center).Padding(0, 3).Gap(0).Shrink(0)
	text := ui.Local(box, "text", func() string { return format(d.value) })
	set := func(v float64) {
		v = max(lo, min(hi, v))
		if v != d.value {
			d.value = v
			d.editedAt = c.Now()
		}
	}
	var focused bool
	var in ui.Element
	box.Children(func() {
		minus := iconButton(c, key+".minus", glyphMinus, "减少"+label, d.value <= lo)
		if minus.Clicked() {
			set(d.value - step)
			*text = format(d.value)
		}
		in = ui.TextInputBase(c.Key(key), text).Label(label).Width(46).FocusRing(false).TextAlign(ui.Center).Font(fontSource).FontSize(sizeHelp).TextColor(colorText).SingleLine().CaretWidth(1.5)
		focused = in.Focused()
		if in.Changed() {
			if v, err := strconv.ParseFloat(strings.TrimSpace(*text), 64); err == nil && v >= lo && v <= hi {
				set(v)
			}
		}
		if in.Shortcut(0, ui.KeyUp) {
			set(d.value + step)
			*text = format(d.value)
		}
		if in.Shortcut(0, ui.KeyDown) {
			set(d.value - step)
			*text = format(d.value)
		}
		if !focused {
			*text = format(d.value)
		}
		if unit != "" {
			ui.Text(c, unit).FontSize(sizeHelp).TextColor(colorMute).NoWrap().Margin(0, 4, 0, 0)
		}
		plus := iconButton(c, key+".plus", glyphPlus, "增加"+label, d.value >= hi)
		if plus.Clicked() {
			set(d.value + step)
			*text = format(d.value)
		}
	})
	if box.Clicked() {
		in.Focus()
	}
	fieldLook(box, focused, p.errs[key] != "")
	if !d.editedAt.IsZero() {
		if left := commitAfter - c.Now().Sub(d.editedAt); left > 0 {
			c.After(left)
		} else {
			d.editedAt = time.Time{}
			if d.value != d.saved && commit(d.value) == nil {
				d.saved = d.value
			}
		}
	}
}

// tagTone is the colour of a tag.
type tagTone int

const (
	tagQuiet tagTone = iota
	tagMarker
	tagGood
	tagWarn
)

// tag is a small mark beside a name: 默认, Jev, 待填写.
func tag(c *ui.Context, text string, tone tagTone) ui.Element {
	fg, bg := colorSource, washFill
	switch tone {
	case tagMarker:
		fg, bg = colorMarker, colorMarker.Alpha(0.12)
	case tagGood:
		fg, bg = colorSuccess, colorSuccess.Alpha(0.12)
	case tagWarn:
		fg, bg = colorTomatoSoft, colorTomato.Alpha(0.15)
	}
	return ui.Box(c).Height(18).Padding(0, 6).Radius(radiusTag).Background(bg).AlignItems(ui.Center).Justify(ui.Center).Shrink(0).Children(func() {
		ui.Text(c, text).Font(fontSource).FontSize(11).FontWeight(500).TextColor(fg).NoWrap()
	})
}

// statusDot is a small light beside what it reports on.
func statusDot(c *ui.Context, col ui.Color) ui.Element {
	return ui.Box(c).Size(7, 7).Radius(3.5).Background(col).Shrink(0).Shadow(0, 0, 6, 0, col.Alpha(0.45))
}

// The page's head.

// pageHead names a page: its icon plays once as the page comes up, and stays awake
// while the page is shown. The words below it start where the rows do, so a page
// has one left edge.
func pageHead(c *ui.Context, key, icon, title, sub string) {
	ui.Column(c).Key("head." + key).Gap(8).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Margin(0, 0, 0, -3).Children(func() {
			box := ui.Box(c).Shrink(0)
			arrived := ui.Local(box, "arrived", func() bool { return false })
			box.Children(func() { liveIcon(c, icon, 32, *arrived) })
			if !*arrived {
				*arrived = true
				c.AnimationFrame()
			}
			ui.Text(c, title).FontSize(sizeTitle).LineHeight(1.25).FontWeight(600).TextColor(colorText)
		})
		if sub != "" {
			ui.Text(c, sub).LineHeight(1.5).TextColor(colorSource)
		}
	})
}

// card is what stands apart from a page's rows: a service, a step. Corners are
// the sheet's less nothing: it sits on it, with a line of light along the top.
func card(c *ui.Context, key string) ui.Element {
	return ui.Column(c.Key(key)).Radius(radiusCard).Background(washCard).Border(1, colorRule).InsetShadow(0, 1, 0, 0, colorSheen)
}

// keycaps shows a shortcut as what it is: keys, on the cream of the sticker.
func keycaps(c *ui.Context, label string, hot, down, enter bool, size float32) {
	parts := splitKeys(label)
	if len(parts) == 0 {
		return
	}
	lean := []float32{-1.6, 1.1, -0.8, 1.4, -1.2}
	for i, k := range parts {
		w := size
		if len([]rune(k)) > 1 {
			w = 0
		}
		cp := ui.Box(c.Key("cap." + k)).Height(size).Radius(radiusCap * size / 30).Background(colorCream).AlignItems(ui.Center).Justify(ui.Center).Rotate(lean[i%len(lean)]).Shrink(0)
		if w > 0 {
			cp.Width(w)
		} else {
			cp.Padding(0, 9)
		}
		// Depth: a hard edge below the cap, taller as it rises to the pointer,
		// gone as it is pressed flat.
		depth := size / 15
		lift := float32(0)
		switch {
		case down:
			depth, lift = 0, depth
		case hot:
			depth, lift = depth+1, -1
		}
		d := cp.Spring("depth", depth, motionSettle)
		cp.Shadow(0, d, 0, 0, capEdge)
		cp.AnimatePose(ui.Pose{Y: lift}, ui.Animation{Spring: motionSettle})
		if enter {
			cp.Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -6, Scale: 0.82, Opacity: 0}, Animation: &ui.Animation{Spring: motionKnob}})
		}
		cp.Children(func() {
			ui.Text(c, k).Font(fontCap).FontSize(size * 14 / 30).FontWeight(600).TextColor(colorOnMarker).NoWrap()
		})
	}
}

// splitKeys parts a shortcut's label into its keys: "⌥⇧D" into ⌥, ⇧, D.
// A key named in words, as Space or F12, stays whole.
func splitKeys(label string) []string {
	var parts []string
	var word []rune
	flush := func() {
		if len(word) > 0 {
			parts = append(parts, string(word))
			word = nil
		}
	}
	for _, r := range label {
		switch r {
		case '⌃', '⌥', '⇧', '⌘':
			flush()
			parts = append(parts, string(r))
		case '+':
			if len(word) > 0 {
				flush()
			} else {
				word = append(word, r)
			}
		default:
			word = append(word, r)
		}
	}
	flush()
	return parts
}

// modKeys is what the modifiers now held look like, in the order macOS writes.
func modKeys(m ui.Modifiers) string {
	s := ""
	if m&ui.Ctrl != 0 {
		s += "⌃"
	}
	if m&ui.Alt != 0 {
		s += "⌥"
	}
	if m&ui.Shift != 0 {
		s += "⇧"
	}
	if m&ui.Super != 0 {
		s += "⌘"
	}
	return s
}

// confirm asks before something that cannot be undone, in the window's look:
// it returns 1 for the action, 0 for leaving it, -1 until one is chosen.
func confirm(c *ui.Context, open *bool, title, message, action string) int {
	chosen := -1
	ui.DialogBase(c, open, func(back, panel ui.Element) {
		back.Background(scrim).Transition(ui.ElementTransition{Enter: &ui.Motion{Opacity: 0}, Duration: fadeHover})
		panel.Column().Width(340).Padding(24).Gap(10).Radius(radiusDialog).Background(inkPopup).Border(1, colorEdge).
			Shadow(0, 18, 48, 0, shadowDialog).InsetShadow(0, 1, 0, 0, colorSheen).
			Transition(ui.ElementTransition{Enter: &ui.Motion{Y: 6, Scale: 0.97, Opacity: 0}, Duration: 180 * time.Millisecond, Ease: iconEase})
		ui.Text(c, title).FontSize(16).FontWeight(600).TextColor(colorText)
		if message != "" {
			ui.Text(c, message).LineHeight(1.5).TextColor(colorSource)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Margin(10, 0, 0, 0).Children(func() {
			// What cannot be undone is not what Enter should do.
			if button(c, buttonSpec{label: "取消", kind: buttonQuiet}).AutoFocus().Clicked() {
				chosen = 0
			}
			if button(c, buttonSpec{label: action, kind: buttonPrimary}).Clicked() {
				chosen = 1
			}
		})
	})
	if chosen >= 0 {
		*open = false
	}
	return chosen
}
