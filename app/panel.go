package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"gloss/core"
)

const (
	panelWidth   = 440
	fadeIn       = 220 * time.Millisecond
	heightSpring = 320 * time.Millisecond
)

// chunk is a piece of a block, fading in from when it arrived.
type chunk struct {
	text string
	at   time.Time
}

type block struct {
	kind   core.BlockKind
	chunks []chunk
}

// panel is what the window shows. It knows blocks, not actions.
type panel struct {
	source string
	method string
	blocks []block
	failed string
	run    uint64
	height float32 // what the content measured last frame
}

func (p *panel) reset(source, method string) {
	p.source, p.method, p.failed = source, method, ""
	p.blocks = nil
}

// apply takes an event on the main thread.
func (p *panel) apply(ev core.Event) {
	if ev.Run != p.run {
		p.run = ev.Run
		p.blocks = nil
	}
	switch ev.Type {
	case core.EventDelta:
		for len(p.blocks) <= ev.Index {
			p.blocks = append(p.blocks, block{kind: ev.Kind})
		}
		b := &p.blocks[ev.Index]
		b.chunks = append(b.chunks, chunk{ev.Delta, time.Now()})
	case core.EventError:
		p.failed = ev.Err.Error()
	}
}

// view builds the panel; before runs first, with the root of the frame.
func (p *panel) view(before func(*ui.Context)) func(*ui.Context) {
	return func(c *ui.Context) {
		now := time.Now()
		t := c.Theme()
		root := c.Root()
		if c.Vibrancy() {
			root.Background(ui.Transparent)
		} else {
			root.Background(t.Background)
		}
		before(c)

		content := ui.Column(c).Key("content").Width(panelWidth).Padding(16, 18).Gap(10)
		content.Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, Name).FontSize(11).Bold().TextColor(t.TextMuted).Grow(1)
				ui.Text(c, p.method).FontSize(11).TextColor(t.TextMuted)
			})
			ui.Text(c, p.source).FontSize(13).LineHeight(1.45).TextColor(t.TextMuted).Selectable()
			ui.Divider(c)
			fading := false
			for i, b := range p.blocks {
				spans := make([]ui.Span, 0, len(b.chunks))
				for _, ch := range b.chunks {
					alpha := float32(now.Sub(ch.at)) / float32(fadeIn)
					if alpha < 1 {
						fading = true
					} else {
						alpha = 1
					}
					col := t.Text
					if b.kind != core.BlockText {
						col = t.TextMuted
					}
					spans = append(spans, ui.Span{Text: ch.text, Color: col.Alpha(alpha)})
				}
				el := ui.RichText(c.Key(fmt.Sprintf("%d-%d", p.run, i)), spans...).LineHeight(1.55).Selectable()
				if b.kind == core.BlockText {
					el.FontSize(17)
				} else {
					el.FontSize(13)
				}
				el.Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Y: 6}})
			}
			if p.failed != "" {
				ui.Text(c, p.failed).FontSize(13).TextColor(t.TextMuted)
			}
			if fading {
				c.Invalidate()
			}
		})
		if r := content.Bounds(); r.H > 0 && r.H != p.height {
			p.height = r.H
			c.Invalidate()
		}
	}
}
