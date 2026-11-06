package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// Glyphs: the one-colour marks of small actions, too small for the icons that
// move. All are 16 by 16, stroked at 1.5 in the colour of the text, as the
// panel's copy mark is (panel.go).
func glyph(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">%s</svg>`, paths)))
}

var (
	glyphCheck  = glyph(`<path d="m3.2 8.4 3.1 3.1 6.5-7"/>`)
	glyphPlus   = glyph(`<path d="M8 3.2v9.6M3.2 8h9.6"/>`)
	glyphMinus  = glyph(`<path d="M3.2 8h9.6"/>`)
	glyphTrash  = glyph(`<path d="M2.8 4.2h10.4M6.2 4.2V3a.8.8 0 0 1 .8-.8h2a.8.8 0 0 1 .8.8v1.2M4.2 4.2l.6 8.2a1.2 1.2 0 0 0 1.2 1.1h4a1.2 1.2 0 0 0 1.2-1.1l.6-8.2"/>`)
	glyphEye    = glyph(`<path d="M1.6 8s2.3-4.4 6.4-4.4 6.4 4.4 6.4 4.4-2.3 4.4-6.4 4.4S1.6 8 1.6 8Z"/><circle cx="8" cy="8" r="1.9"/>`)
	glyphEyeOff = glyph(`<path d="M2.4 2.4 13.6 13.6M6.6 3.8A6 6 0 0 1 8 3.6c4.1 0 6.4 4.4 6.4 4.4a11 11 0 0 1-1.9 2.5M4.1 5.1A10.700 10.700 0 0 0 1.600 8s2.300 4.400 6.400 4.400c1.100 0 2.100-.3 2.900-.8M6.700 6.700a1.900 1.900 0 0 0 2.600 2.600"/>`)
	glyphPlug   = glyph(`<path d="M8.900 1.800 3.600 9h4l-.5 5.200L12.400 7h-4l.5-5.200Z"/>`)
	glyphFile   = glyph(`<path d="M9.200 1.900H4.700a1.200 1.200 0 0 0-1.200 1.200v9.800a1.200 1.200 0 0 0 1.200 1.200h6.600a1.200 1.200 0 0 0 1.200-1.200V5.200L9.200 1.900Z"/><path d="M9.200 1.900v3.300h3.300"/>`)
	glyphLog    = glyph(`<path d="M3 4h10M3 8h10M3 12h6"/>`)
	glyphReset  = glyph(`<path d="M3 3.200v3.300h3.300"/><path d="M3.300 6.500A5.200 5.200 0 1 1 3 9.300"/>`)
	glyphGrip   = glyph(`<path stroke-width="2" d="M5.800 3.800v.01M10.200 3.800v.01M5.800 8v.01M10.200 8v.01M5.800 12.200v.01M10.200 12.200v.01"/>`)
	glyphBack   = glyph(`<path d="M9.800 3.600 5.400 8l4.400 4.400"/>`)
	glyphNext   = glyph(`<path d="M6.200 3.600 10.600 8l-4.400 4.400"/>`)
	glyphStar   = glyph(`<path d="m8 2.400 1.700 3.500 3.900.6-2.800 2.700.7 3.800L8 11.200 4.500 13l.7-3.800-2.800-2.700 3.900-.6L8 2.400Z"/>`)
	glyphSwap   = glyph(`<path d="M3 5.600h9.600M10 3l2.600 2.600L10 8.200M13 10.400H3.400M6 7.800l-2.600 2.600L6 13"/>`)
	glyphRetry  = glyph(`<path d="M13 3.200v3.300H9.700"/><path d="M12.700 6.500A5.200 5.200 0 1 0 13 9.300"/>`)
	glyphTune   = glyph(`<path d="M2.600 5h5.100M11.900 5h1.500M2.600 11h1.500M8.300 11h5.100"/><circle cx="9.800" cy="5" r="2.100"/><circle cx="6.200" cy="11" r="2.100"/>`)
	glyphDown   = glyph(`<path d="M8 3v9.600M4 8.600l4 4 4-4"/>`)
	glyphCaret  = glyph(`<path d="m4.200 6.200 3.800 3.800 3.800-3.800"/>`)
)

// glyphIcon draws a glyph in the colour of the text around it.
func glyphIcon(c *ui.Context, g *ui.SVG, size float32) ui.Element {
	return ui.Icon(c, g).FontSize(size).Shrink(0)
}
