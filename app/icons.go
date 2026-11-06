package app

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// The icons that move. design/icons bakes each one's hover motion into frames
// of plain SVG, one per line of an embedded file, because mygo draws no filters
// and runs no script; a frame is chosen here by how far the motion has gone.
// Run make icons after changing them. See docs/decisions/0012-baked-icons.md.
//
//go:embed icons/*.svgz
var iconFiles embed.FS

const (
	iconFrames = 24
	// iconMotion and iconEase are the engine's own, so the motion keeps the
	// pace it was designed at.
	iconMotion = 460 * time.Millisecond
	// visualSize relates an icon's drawn size to its box: the frames carry a
	// margin (viewBox 36, the art 31) for what moves past the canvas.
	visualSize float32 = 36.0 / 31.0
)

var iconEase = ui.CubicBezier(0.22, 1, 0.36, 1)

// iconSet holds the frames of the icons, parsing each only when first drawn.
// Only the two ends stay: the frames between are dropped as the motion settles,
// since an icon spends nearly all its life at rest.
var iconSet = map[string]*bakedIcon{}

type bakedIcon struct {
	raw    []string // the frames' sources
	frames []*ui.SVG
	moving time.Time
}

func loadIcon(id string) *bakedIcon {
	if ic, ok := iconSet[id]; ok {
		return ic
	}
	var ic *bakedIcon
	if raw, err := readIconFile(id); err == nil {
		lines := strings.Split(raw, "\n")
		ic = &bakedIcon{raw: lines, frames: make([]*ui.SVG, len(lines))}
	}
	iconSet[id] = ic
	return ic
}

func readIconFile(id string) (string, error) {
	b, err := iconFiles.ReadFile("icons/" + id + ".svgz")
	if err != nil {
		return "", err
	}
	z, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	defer z.Close()
	text, err := io.ReadAll(z)
	return string(text), err
}

// frame is the icon at q, 0 at rest and 1 awake.
func (ic *bakedIcon) frame(q float32, now time.Time) *ui.SVG {
	i := int(q*float32(len(ic.raw)-1) + 0.5)
	i = max(0, min(i, len(ic.raw)-1))
	if ic.frames[i] == nil {
		s, err := ui.ParseSVG([]byte(ic.raw[i]))
		if err != nil {
			return nil
		}
		ic.frames[i] = s
	}
	if i != 0 && i != len(ic.raw)-1 {
		ic.moving = now
	}
	return ic.frames[i]
}

// settle drops the frames between the ends once the icon has stopped moving.
func (ic *bakedIcon) settle(now time.Time) {
	if ic.moving.IsZero() || now.Sub(ic.moving) < iconMotion {
		return
	}
	ic.moving = time.Time{}
	for i := 1; i < len(ic.frames)-1; i++ {
		ic.frames[i] = nil
	}
}

// releaseIcons gives back every parsed frame, for when the window closes.
func releaseIcons() { iconSet = map[string]*bakedIcon{} }

// liveIcon draws an icon of the given size: awake, it plays its motion and
// stays at its end, lit a step; asleep, it plays back. With motion reduced it
// goes straight to its end. Its element is the box, which holds the margin.
func liveIcon(c *ui.Context, id string, size float32, awake bool) ui.Element {
	box := size * visualSize
	e := ui.Box(c.Key("icon."+id)).Size(box, box).Shrink(0)
	ic := loadIcon(id)
	if ic == nil {
		return e
	}
	target := float32(0)
	if awake {
		target = 1
	}
	q := e.AnimateWith("q", target, iconMotion, iconEase)
	if c.Preferences().ReduceMotion {
		q = target
	}
	if s := ic.frame(q, c.Now()); s != nil {
		// Drawn over the box's whole area: the box is what the row lays out.
		e.Children(func() { ui.Image(c, s).Size(box, box) })
	}
	if q == target {
		ic.settle(c.Now())
	}
	return e
}
