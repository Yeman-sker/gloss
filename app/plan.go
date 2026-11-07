package app

import (
	"strings"

	"gloss/core"
)

// layoutPlan is what the panel settles on before it opens on a result, so that
// the text streams into a frame that holds still: a width the text never has to
// wrap again for, and a height near what it will need. The panel opens with
// what it can know by then, which is the selection, the kind of result, and the
// start of the result, or all of it when it is short.
type layoutPlan struct {
	width float32
	// height is what the content is expected to need, the footer aside.
	height float32
	// reading folds a long source and sets the page for long text.
	reading bool
	// room is how much height to leave below the panel's top, so that a panel
	// near the bottom of the screen moves once, as it opens, and not again.
	room float32
}

type planInput struct {
	source string
	output core.BlockKind
	result string // what has come so far
	ended  bool   // and it is all there is
	// sourceW and resultW are how wide they set on one line. low is the
	// narrowest the footer allows, screenW and limit the most the screen does.
	sourceW, resultW   float32
	low, screenW, most float32
}

// planLayout decides the frame. It guesses little: a result about as long as its
// source, a Markdown one less so but seldom short, and it leans short on height,
// as a panel that grows by a line is calmer than one left half empty.
func planLayout(in planInput) layoutPlan {
	expect := em(in.result)
	if !in.ended {
		guess := em(in.source)
		if in.output == core.BlockMarkdown {
			guess = max(guess*0.7, emMarkdownFloor)
		}
		expect = max(expect, guess)
	}
	structured := codeLike(in.source) || codeLike(in.result)
	long := max(expect, em(in.source))*sizeBody > widthPanel*planCompactLines
	// A result all there and short needs no page, Markdown or not.
	reading := structured || long || (in.output == core.BlockMarkdown && !in.ended)

	var width float32
	switch {
	case structured:
		width = widthWide
	case reading:
		width = widthReading
	default:
		fit := max(in.sourceW, in.resultW)
		if !in.ended {
			fit = max(fit, expect*sizeBody)
		}
		width = clampWidth(fit+34, in.low, widthPanel)
	}
	width = max(in.low, min(width, in.screenW))

	inner := width - 2*paddingX
	lines := wrappedLines(in.result, expect, inner, in.ended)
	body := float32(lines) * lineBody
	if in.output == core.BlockMarkdown {
		line := lineReading
		body = float32(lines)*line + float32(lines/4)*(gapBlock)
	}
	if !in.ended {
		body *= planLean
	}
	head := paddingY*2 + gapGroup + lineSource
	if reading {
		head += lineSource + lineHint
	} else {
		head += float32(sourceLines(in.source, in.sourceW, inner)-1) * lineSource
	}
	plan := layoutPlan{width: width, height: min(head+body, in.most), reading: reading}
	plan.room = plan.height
	if reading && !in.ended {
		plan.room = max(plan.height, min(planRoomReading, in.most))
	}
	return plan
}

// sourceLines is how many lines the source takes unfolded. measured is how wide it
// sets, which for several lines says only how wide the widest is.
func sourceLines(source string, measured, inner float32) int {
	if measured > 0 && !strings.Contains(source, "\n") {
		return int(measured/inner) + 1
	}
	n := 0
	for _, line := range strings.Split(source, "\n") {
		n += int(em(line)*sizeSource/inner) + 1
	}
	return n
}

// wrappedLines is how many lines the result takes at this width: counted when
// it is all there, its length over the width when it has yet to come.
func wrappedLines(result string, expect, inner float32, ended bool) int {
	if !ended {
		return int(expect*sizeBody/inner) + 1
	}
	n := 0
	for _, line := range strings.Split(result, "\n") {
		n += int(em(line)*sizeBody/inner) + 1
	}
	return n
}

// em is how wide text sets in ems, near enough for planning: the panel measures
// what must fit exactly.
func em(s string) float32 {
	var w float32
	for _, r := range s {
		switch {
		case r >= 0x2E80:
			w++
		case r == ' ' || r == '\n' || r == '\t':
			w += 0.28
		case r >= 'A' && r <= 'Z':
			w += 0.62
		default:
			w += 0.52
		}
	}
	return w
}

// codeLike says text holds what wrapping ruins, code or a table, and needs the
// width for it.
func codeLike(s string) bool {
	if strings.Contains(s, "```") || strings.Contains(s, "|---") || strings.Contains(s, "| ---") {
		return true
	}
	indented, lines := 0, 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines++
		if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			indented++
		}
	}
	return lines >= 3 && indented*2 >= lines
}
