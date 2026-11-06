package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The look of Gloss, in one place. The fonts: docs/decisions/0006-bundled-fonts.md.
// The ink and the marker: docs/decisions/0011-ink-and-marker.md.
const (
	fontText      = "Gloss Sans, Gloss CJK"
	fontTextCJK   = "Gloss Quotes, Gloss Sans, Gloss CJK"
	fontSource    = "Gloss Mono, Gloss CJK"
	fontSourceCJK = "Gloss Quotes, Gloss Mono, Gloss CJK"
	fontPhonetic  = "Gloss Phonetic, Gloss Sans"
	fontCap       = "Gloss Sans, Gloss Mono, Gloss CJK"

	sizeBody, lineBody     float32 = 15, 24
	sizeWord, lineWord     float32 = 20, 28
	sizeSource, lineSource float32 = 12, 20

	widthMinPanel float32 = 240
	widthPanel    float32 = 420
	// widthReading is the reading layout's, for long and structured results.
	widthReading   float32 = 520
	widthMinFooter float32 = 344
	heightFooter   float32 = 36
	heightChip     float32 = 28 // a feature in the footer: its icon, 22, and the margin it carries
	sizeChipIcon   float32 = 22
	sizeActionMark float32 = 13
	padBubble      float32 = 8 // a tip or status over the panel: its side padding
	gapBubble      float32 = 6 // and how far it stays from what it is about
	sizeHint       float32 = 11
	lineHint       float32 = 16
	widthError     float32 = 280
	widthCapsule   float32 = 300
	minCapsule     float32 = 112
	heightCapsule  float32 = 48
	radiusCapsule  float32 = 24
	radiusPanel    float32 = 18
	radiusControl  float32 = 8 // the panel's controls; settings use the scale below
	paddingY       float32 = 14
	paddingX       float32 = 16
	gapGroup       float32 = 8
	// margin is the window's clear edge around the surface, for the shadow.
	margin = 48

	// charsSource bounds the reading sweep: every lit rune costs a span.
	charsSource = 48

	dragStart          = 4
	fadeText           = 160 * time.Millisecond
	exitTime           = 100 * time.Millisecond
	fadeSweep          = 380 * time.Millisecond
	noticeFor          = 1300 * time.Millisecond
	fadeNib            = 220 * time.Millisecond
	fadeSource         = 240 * time.Millisecond
	drawCheck          = 180 * time.Millisecond
	statusFor          = 3 * time.Second
	riseText   float32 = 3
	rateMin            = 30.0
	rateMax            = 120.0
)

var (
	colorSurface = ui.Hex("#17171B")
	colorRim     = ui.RGBA(255, 255, 255, 0.1)
	colorShadow  = ui.RGBA(0, 0, 0, 0.42)
	colorContact = ui.RGBA(0, 0, 0, 0.24) // the shadow close under the panel
	colorText    = ui.Hex("#E6E6E8")
	colorSource  = ui.Hex("#ACACAD")
	colorHint    = ui.Hex("#828284")
	colorSuccess = ui.Hex("#5EDBA5")
	colorDanger  = ui.Hex("#FF7F6E")
	colorControl = ui.RGBA(255, 255, 255, 0.08)

	// The settings window: the ink of the panel, the marker of the sticker.
	inkDeep    = ui.Hex("#111114") // the window, under the sheet
	inkRaised  = ui.Hex("#222227") // controls on the sheet
	inkHover   = ui.Hex("#2A2A30")
	inkPressed = ui.Hex("#1D1D22")
	inkPopup   = ui.Hex("#26262C")
	inkPlate   = ui.Hex("#37373E") // the plate of a segmented control

	colorMarker     = ui.Hex("#FFD43B")
	colorOnMarker   = ui.Hex("#16161A")
	colorTomato     = ui.Hex("#FF5B3A")
	tomatoHover     = ui.Hex("#FF6F52")
	tomatoPressed   = ui.Hex("#E84E2F")
	colorCream      = ui.Hex("#F4F1E8")
	colorMute       = ui.Hex("#8C8C90")
	colorRule       = ui.RGBA(255, 255, 255, 0.07)
	colorEdge       = ui.RGBA(255, 255, 255, 0.11)
	colorEdgeHi     = ui.RGBA(255, 255, 255, 0.2)
	colorSheen      = ui.RGBA(255, 255, 255, 0.05)
	colorTomatoSoft = ui.Hex("#FF8B73")
	capEdge         = ui.Hex("#A9A594")
	colorScrollbar  = ui.RGBA(255, 255, 255, 0.18)
	navInk          = ui.Hex("#B8B8BC") // the words of the navigation at rest
	switchOff       = ui.RGBA(255, 255, 255, 0.13)
	switchOffHover  = ui.RGBA(255, 255, 255, 0.19)
	knobOff         = ui.Hex("#D8D8DC")
	sheenStrong     = ui.RGBA(255, 255, 255, 0.22) // the top edge of a primary button
	shadowKnob      = ui.RGBA(0, 0, 0, 0.3)        // what slides on a track
	shadowDialog    = ui.RGBA(0, 0, 0, 0.5)
	scrim           = ui.RGBA(0, 0, 0, 0.45) // the page behind a dialog

	// Washes: white over ink, for what has no edge of its own.
	washCard  = ui.RGBA(255, 255, 255, 0.028) // cards, tiles, a card's head under the pointer
	washHover = ui.RGBA(255, 255, 255, 0.05)  // a quiet control under the pointer
	washFill  = ui.RGBA(255, 255, 255, 0.07)  // tags, tracks, a switch that is off limits
	washCode  = ui.RGBA(255, 255, 255, 0.04)  // code blocks and table heads in a result

	colorSelected = colorMarker.Alpha(0.28) // selected text, in both windows

	motionPop     = ui.SpringOptions{Duration: 150 * time.Millisecond}
	motionGrow    = ui.SpringOptions{Duration: 280 * time.Millisecond, Bounce: 0.08}
	motionStream  = ui.SpringOptions{Duration: 200 * time.Millisecond}
	motionPress   = ui.SpringOptions{Duration: 120 * time.Millisecond}
	motionRelease = ui.SpringOptions{Duration: 300 * time.Millisecond, Bounce: 0.2}
	motionKnob    = ui.SpringOptions{Duration: 280 * time.Millisecond, Bounce: 0.22}
	motionSettle  = ui.SpringOptions{Duration: 220 * time.Millisecond}
)

// The radii of the settings window, from one rule: a radius is about 28% of the
// height it rounds, and a surface holding another keeps their corners concentric,
// outer = inner + the padding between. Nothing rounds by eye.
const (
	radiusTag     float32 = 5  // a tag, 18 high
	radiusSmall   float32 = 7  // a small button, 26 high
	radiusItem    float32 = 8  // a menu's item: radiusPopup less the menu's padding
	radiusField   float32 = 9  // inputs, selects, buttons, 32 high
	radiusPopup   float32 = 12 // menus and tooltips; the recorder, 42 high
	radiusCard    float32 = 14 // the sheet of a page, a service's card
	radiusDialog  float32 = 16 // a confirmation, the one thing above the sheet
	paddingPopup  float32 = 4
	paddingCard   float32 = 4
	radiusCap     float32 = 8 // a key's cap, 30 high
	heightField   float32 = 32
	heightSmall   float32 = 26
	heightSwitchT float32 = 22 // the switch's track
	widthSwitchT  float32 = 38
	insetKnob     float32 = 2
)

// Motion of the settings window.
const (
	fadeHover  = 130 * time.Millisecond
	tipDelay   = 600 * time.Millisecond // how long the pointer rests before a tip shows
	tipWarm    = 400 * time.Millisecond // how soon after one tip another shows at once
	swipeTime  = 240 * time.Millisecond
	savedHold  = 700 * time.Millisecond
	savedFade  = 320 * time.Millisecond
	rotateMenu = 180 * time.Millisecond
)

const (
	glideDuration = 0.28 // seconds, the window's move to a new place
	scaleFrom     = 0.97
	scaleExit     = 0.98
	scalePressed  = 0.975
	scaleLifted   = 1.02
	scaleGiveWay  = 0.985
)
