package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The look of the panel, in one place. See docs/decisions/0006-bundled-fonts.md.
const (
	fontText      = "Gloss Sans, Gloss CJK"
	fontTextCJK   = "Gloss Quotes, Gloss Sans, Gloss CJK"
	fontSource    = "Gloss Mono, Gloss CJK"
	fontSourceCJK = "Gloss Quotes, Gloss Mono, Gloss CJK"
	fontPhonetic  = "Gloss Phonetic, Gloss Sans"

	sizeBody, lineBody     float32 = 15, 24
	sizeWord, lineWord     float32 = 20, 28
	sizeSource, lineSource float32 = 12, 20

	widthMinPanel float32 = 240
	widthPanel    float32 = 420
	widthError    float32 = 280
	widthCapsule  float32 = 300
	minCapsule    float32 = 112
	heightCapsule float32 = 48
	radiusCapsule float32 = 24
	radiusPanel   float32 = 18
	radiusControl float32 = 8
	paddingY      float32 = 14
	paddingX      float32 = 16
	gapGroup      float32 = 8
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
	riseText   float32 = 3
	rateMin            = 30.0
	rateMax            = 120.0
)

var (
	colorSurface = ui.Hex("#17171B")
	colorRim     = ui.RGBA(255, 255, 255, 0.1)
	colorShadow  = ui.RGBA(0, 0, 0, 0.42)
	colorText    = ui.Hex("#E6E6E8")
	colorSource  = ui.Hex("#ACACAD")
	colorHint    = ui.Hex("#828284")
	colorAccent  = ui.Hex("#6CB8FF")
	colorSuccess = ui.Hex("#5EDBA5")
	colorDanger  = ui.Hex("#FF7F6E")
	colorControl = ui.RGBA(255, 255, 255, 0.08)

	motionPop     = ui.SpringOptions{Duration: 150 * time.Millisecond}
	motionGrow    = ui.SpringOptions{Duration: 280 * time.Millisecond, Bounce: 0.08}
	motionStream  = ui.SpringOptions{Duration: 200 * time.Millisecond}
	motionPress   = ui.SpringOptions{Duration: 120 * time.Millisecond}
	motionRelease = ui.SpringOptions{Duration: 300 * time.Millisecond, Bounce: 0.2}
)

const (
	glideDuration = 0.28 // seconds, the window's move to a new place
	scaleFrom     = 0.97
	scaleExit     = 0.98
	scalePressed  = 0.975
	scaleLifted   = 1.02
	scaleGiveWay  = 0.985
)
