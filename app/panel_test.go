package app

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gloss/core"
)

func TestIsWord(t *testing.T) {
	for s, want := range map[string]bool{
		"gloss": true, "注释": true, "a word": false, "这是一个完整的句子": false, "": false,
	} {
		if got := isWord(s); got != want {
			t.Errorf("isWord(%q) = %v", s, got)
		}
	}
}

func TestTokenLenKeepsWordsWhole(t *testing.T) {
	if n := tokenLen([]rune("don't stop")); n != 5 {
		t.Errorf("Latin token = %d", n)
	}
	if n := tokenLen([]rune("注释")); n != 1 {
		t.Errorf("CJK token = %d", n)
	}
}

func TestPointerZones(t *testing.T) {
	p := panel{done: true, surfaceW: 420, textTop: 66}
	for _, tt := range []struct {
		x, y float32
		want ui.Cursor
	}{
		{200, 30, ui.CursorGrab},     // the source
		{200, 100, ui.CursorPointer}, // the translation
		{415, 140, ui.CursorPointer}, // its padding and the corner cue
		{2, 140, ui.CursorPointer},   // the left edge
	} {
		if got := p.pointerAt(tt.x, tt.y); got != tt.want {
			t.Errorf("at %v,%v: %v", tt.x, tt.y, got)
		}
	}
}

func TestPointerBoundaryLags(t *testing.T) {
	p := panel{done: true, surfaceW: 420, textTop: 66}
	if p.pointerAt(200, 67); p.inCopy {
		t.Error("entered before passing the boundary")
	}
	p.pointerAt(200, 70)
	if p.pointerAt(200, 65); !p.inCopy {
		t.Error("left before passing the boundary")
	}
	if p.pointerAt(200, 62); p.inCopy {
		t.Error("did not leave")
	}
}

func TestClampWidth(t *testing.T) {
	if w := clampWidth(10, widthMinPanel, widthPanel); w != widthMinPanel {
		t.Errorf("short = %v", w)
	}
	if w := clampWidth(9999, widthMinPanel, widthPanel); w != widthPanel {
		t.Errorf("long = %v", w)
	}
}

func TestCheckIconDrawsAlongItsStroke(t *testing.T) {
	for _, q := range []float64{0, 0.2, 0.5, 1} {
		if checkIcon(q) == nil {
			t.Errorf("q=%v", q)
		}
	}
}

func TestPanelOpensOnFirstDeltaAndFinishesPaced(t *testing.T) {
	var p panel
	p.reset("hello")
	p.lastFrame = time.Now().Add(-time.Second)
	if !p.capsule {
		t.Fatal("starts as a capsule")
	}
	p.apply(core.Event{Run: 1, Type: core.EventDelta, Kind: core.BlockText, Delta: "你好"})
	p.apply(core.Event{Run: 1, Type: core.EventDone})
	if p.capsule {
		t.Fatal("first delta opens it")
	}
	p.release(time.Now())
	if p.done {
		t.Fatal("not done while text fades in")
	}
	p.release(time.Now().Add(time.Second))
	if !p.done || p.text() != "你好" {
		t.Fatalf("done=%v text=%q", p.done, p.text())
	}
}

func TestPanelFailureOpensCapsule(t *testing.T) {
	var p panel
	p.reset("hello")
	p.apply(core.Event{Run: 1, Type: core.EventError, Err: errors.New("boom")})
	if p.capsule || !p.done || p.failed != "boom" {
		t.Fatalf("%+v", p.failed)
	}
}

func TestSpringSettlesAtTarget(t *testing.T) {
	var s spring
	s.jump(0)
	s.velocity = 1000
	s.set(100)
	for i := 0; i < 200 && s.moving(); i++ {
		s.step(1.0/60, false)
	}
	if s.value != 100 {
		t.Fatalf("value = %v", s.value)
	}
}
