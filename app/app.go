// Package app is the desktop shell: the shortcut, the panel, and how blocks
// look and move. It is the only package that may import mygo.
package app

import (
	_ "embed"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gloss/capture"
	"gloss/core"
)

// Name is the working name of the product: change it here and in mygo.json.
const Name = "Gloss"

const (
	shortcut = "Alt+Shift+D"
	escape   = "Escape"
)

// trayIcon is the ring of resources/icon.png, black on transparent so macOS
// can tint it.
//
//go:embed tray.png
var trayIcon []byte

// shell ties the shortcut, the helper and the engine to the panel.
type shell struct {
	win    *mygo.Window
	tray   *mygo.Tray
	helper *capture.Helper
	engine *core.Engine
	panel  panel
	action string

	area       mygo.Rectangle
	x, y       spring
	lastGlide  time.Time
	winW, winH int
	curW, curH float32
	visible    bool
	following  bool
	mouseIn    bool
	drag       dragState
	outside    outsidePress
}

// Run starts the app and returns when it ends.
func Run() error {
	if err := registerFonts(); err != nil {
		return err
	}
	engine := core.NewEngine()
	translate := translator{}
	if err := engine.Register(translate); err != nil {
		return err
	}
	helper, err := capture.Start(capture.Path())
	if err != nil {
		return err
	}
	s := &shell{helper: helper, engine: engine, action: translate.ID()}
	s.panel.frame = s.frame
	s.panel.closed = s.closed
	s.panel.local = func() (float32, float32) {
		p := mygo.Screen.CursorScreenPoint()
		x, y := s.win.Position()
		return float32(p.X - x - margin), float32(p.Y - y - margin)
	}
	s.panel.cursor = func() (int, int) {
		p := mygo.Screen.CursorScreenPoint()
		return p.X, p.Y
	}
	s.panel.copied = func(c *ui.Context, text string) { c.WriteClipboard(text) }
	s.panel.retry = func() { s.start(s.panel.source) }

	// The panel refuses to close, which holds App.Quit up: signals and the
	// tray end directly. The helper exits when its stdin closes with us.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		os.Exit(0)
	}()

	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.WhenReady(func() {
		s.win = mygo.NewWindow(mygo.WindowOptions{
			Title:         Name,
			Panel:         true,
			Hidden:        true,
			Frameless:     true,
			Transparent:   true,
			DisableShadow: true,
			Width:         int(widthPanel) + 2*margin,
			Height:        120,
			Content:       ui.View(s.panel.view()),
		})
		allowBackgroundCursor()
		s.win.OnClose(func(e *mygo.CloseEvent) {
			e.PreventDefault()
			s.hide()
		})
		if err := mygo.GlobalShortcut.Register(shortcut, s.trigger); err != nil {
			log.Printf("shortcut %s: %v", shortcut, err)
		}
		s.tray, err = mygo.NewTray(mygo.TrayOptions{
			ID:             "main",
			Icon:           trayIcon,
			IconIsTemplate: true,
			ToolTip:        Name,
			Menu: mygo.NewMenu([]*mygo.MenuItem{
				{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { os.Exit(0) }},
			}),
		})
		if err != nil {
			log.Printf("tray: %v", err)
		}
		log.Printf("ready: %s", shortcut)
	})
	return mygo.App.Run()
}

// trigger reads the selection and starts the action on it. The same
// selection again closes the panel.
func (s *shell) trigger() {
	r, err := s.helper.Selection()
	text := strings.TrimSpace(r.Text)
	if err != nil || !r.OK || text == "" {
		s.engine.Cancel()
		if s.open() && s.panel.notice == "" {
			s.hide()
			return
		}
		s.showNotice("先选中一段文字")
		return
	}
	if s.open() && s.panel.notice == "" && text == s.panel.source {
		s.hide()
		return
	}
	s.start(text)
}

// start shows text in the panel and runs the action on it.
func (s *shell) start(text string) {
	s.present(func() { s.panel.reset(text) })
	if _, err := s.engine.Start(s.action, core.Request{Text: text}, func(ev core.Event) {
		s.win.Update(func() { s.panel.apply(ev) })
	}); err != nil {
		log.Print(err)
	}
}

func (s *shell) showNotice(text string) {
	s.present(func() { s.panel.showNotice(text) })
	gen := s.panel.gen
	stamp := s.panel.noticeAt
	time.AfterFunc(noticeFor, func() {
		s.win.Update(func() {
			if s.panel.gen == gen && s.panel.noticeAt.Equal(stamp) && s.panel.notice != "" {
				s.hide()
			}
		})
	})
}

// present puts new content in the panel: it glides there when it is open,
// and appears there when it is not. A pinned panel stays where it is.
func (s *shell) present(content func()) {
	wasOpen := s.open()
	content()
	p := mygo.Screen.CursorScreenPoint()
	s.area = mygo.Screen.DisplayNearestPoint(p).WorkArea
	s.panel.maxHeight = float32(s.area.Height) * 0.6
	wantW, wantH := widthCapsule, heightCapsule
	if wasOpen && s.panel.pinned {
		s.win.Invalidate()
		return
	}
	x, y := s.clamp(p.X-int(paddingX)-margin, p.Y+20-margin, wantW, wantH)
	if wasOpen {
		s.glideTo(x, y)
		s.win.Invalidate()
		return
	}
	s.panel.gen++
	s.panel.entered, s.panel.pinned, s.panel.dragging = false, false, false
	s.panel.lastFrame = time.Now()
	s.panel.anchor = float32(p.X - x - margin)
	s.x.jump(float64(x))
	s.y.jump(float64(y))
	s.setSize(wantW, wantH)
	s.win.SetPosition(x, y)
	s.show()
}

// clamp keeps a surface of the size on the work area.
func (s *shell) clamp(x, y int, w, h float32) (int, int) {
	a := s.area
	maxX := a.X + a.Width - int(w) - margin - 12
	maxY := a.Y + a.Height - int(h) - margin - 12
	return max(a.X-margin+12, min(x, maxX)), max(a.Y-margin+12, min(y, maxY))
}

func (s *shell) glideTo(x, y int) {
	s.x.set(float64(x))
	s.y.set(float64(y))
	s.lastGlide = time.Now()
}

func (s *shell) setSize(w, h float32) {
	cw, ch := int(w+0.999)+2*margin, int(h+0.999)+2*margin
	if cw != s.winW || ch != s.winH {
		s.winW, s.winH = cw, ch
		s.win.SetContentSize(cw, ch)
	}
}

func (s *shell) open() bool { return s.visible && !s.panel.closing }

// show keeps the keyboard with the app the user is in, so Escape is only
// ours while the panel shows.
func (s *shell) show() {
	s.visible = true
	s.win.ShowInactive()
	s.followCursor()
	mygo.GlobalShortcut.Unregister(escape)
	if err := mygo.GlobalShortcut.Register(escape, s.hide); err != nil {
		log.Printf("shortcut %s: %v", escape, err)
	}
}

// hide lets the surface leave, then takes the window away.
func (s *shell) hide() {
	if !s.open() {
		return
	}
	mygo.GlobalShortcut.Unregister(escape)
	s.engine.Cancel()
	s.panel.closing = true
	s.win.Invalidate()
	gen := s.panel.gen
	// The frame that finishes the fade takes the window away; this is for a
	// window that draws no more frames.
	time.AfterFunc(exitTime+300*time.Millisecond, func() {
		s.win.Update(func() {
			if s.panel.closing && s.panel.gen == gen {
				s.closed()
			}
		})
	})
}

func (s *shell) closed() {
	if s.visible {
		s.visible = false
		s.win.Hide()
	}
}

// followCursor lets clicks through the clear edge of the window, which only
// holds the shadow. The window takes the mouse or not as a whole, so this
// watches the cursor while the panel shows.
func (s *shell) followCursor() {
	if s.following {
		return
	}
	s.following = true
	go func() {
		t := time.NewTicker(33 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			down := s.helper.ButtonDown()
			stop := make(chan bool, 1)
			// Not win.Update, which draws a frame each time.
			mygo.RunOnMain(func() {
				if !s.visible {
					s.following = false
					stop <- true
					return
				}
				stop <- false
				s.updateClickThrough()
				s.watchOutside(down)
			})
			if <-stop {
				return
			}
		}
	}()
}

func (s *shell) updateClickThrough() {
	p := mygo.Screen.CursorScreenPoint()
	x, y := s.win.Position()
	inside := s.drag.active ||
		(p.X >= x+margin && p.X < x+margin+int(s.curW) && p.Y >= y+margin && p.Y < y+margin+int(s.curH))
	if inside != s.mouseIn {
		s.mouseIn = inside
		s.win.SetIgnoreMouseEvents(!inside)
		if !inside {
			setCursor(ui.CursorDefault)
		}
	}
	if inside {
		if s.panel.track() {
			s.win.Invalidate()
		}
		setCursor(s.panel.wantCursor)
	} else if s.panel.inCopy {
		s.panel.inCopy = false
		s.win.Invalidate()
	}
}

// watchOutside closes the panel on a click elsewhere, and makes it give way
// while the user drags there, selecting text it may cover. A pinned panel
// stays put.
func (s *shell) watchOutside(down bool) {
	o := &s.outside
	if down && !o.down {
		p := mygo.Screen.CursorScreenPoint()
		*o = outsidePress{down: true, x: p.X, y: p.Y, active: s.open() && !s.panel.pinned && !s.mouseIn}
		return
	}
	if !o.active {
		o.down = down
		return
	}
	if down {
		p := mygo.Screen.CursorScreenPoint()
		dx, dy := p.X-o.x, p.Y-o.y
		if !o.moved && dx*dx+dy*dy > dragStart*dragStart {
			o.moved = true
			s.panel.givingWay = true
			s.win.Invalidate()
		}
		return
	}
	if !o.moved {
		s.hide()
	}
	*o = outsidePress{}
}

// outsidePress is a press outside the surface, as watchOutside follows it.
type outsidePress struct {
	down, active, moved bool
	x, y                int
}

// frame follows the surface with the window: it is large enough at once,
// moves where the surface should be, and is dragged by the surface.
func (s *shell) frame(c *ui.Context, curW, curH, wantW, wantH float32, pressed, overText bool) {
	s.curW, s.curH = curW, curH
	// A window that resizes every frame drops frames: it grows at once, to
	// where the surface is heading, and shrinks once the surface is there.
	if w, h := int(wantW+0.999)+2*margin, int(wantH+0.999)+2*margin; w > s.winW || h > s.winH || (abs32(curW-wantW) < 0.5 && abs32(curH-wantH) < 0.5) {
		s.setSize(max(curW, wantW), max(curH, wantH))
	}
	s.dragFrame(c, pressed, overText)
	if !s.panel.pinned && !s.drag.active {
		x, y := s.clamp(int(s.x.target), int(s.y.target), wantW, wantH)
		if x != int(s.x.target) || y != int(s.y.target) {
			s.glideTo(x, y)
		}
	}
	if s.x.moving() || s.y.moving() {
		now := time.Now()
		dt := now.Sub(s.lastGlide).Seconds()
		s.lastGlide = now
		reduce := c.Preferences().ReduceMotion
		s.x.step(dt, reduce)
		s.y.step(dt, reduce)
		s.win.SetPosition(int(s.x.value+0.5), int(s.y.value+0.5))
		c.Invalidate()
	}
}

// dragState is a press on the surface that may become a move.
type dragState struct {
	active, pressing bool
	startX, startY   int
	originX, originY int
	lastX, lastY     int
	velX, velY       float64
	last             time.Time
}

func (s *shell) dragFrame(c *ui.Context, pressed, overText bool) {
	d := &s.drag
	if !pressed {
		if d.active {
			s.endDrag()
		}
		d.active, d.pressing = false, false
		return
	}
	c.Invalidate()
	p := mygo.Screen.CursorScreenPoint()
	if !d.pressing {
		d.pressing = true
		if overText {
			d.startX = -1 << 30 // the press belongs to the text
			return
		}
		x, y := s.win.Position()
		*d = dragState{pressing: true, startX: p.X, startY: p.Y, originX: x, originY: y, lastX: p.X, lastY: p.Y, last: time.Now()}
		return
	}
	if d.startX == -1<<30 {
		return
	}
	dx, dy := p.X-d.startX, p.Y-d.startY
	if !d.active && dx*dx+dy*dy > dragStart*dragStart {
		d.active = true
		s.panel.dragging, s.panel.pinned = true, true
	}
	if d.active {
		now := time.Now()
		if dt := now.Sub(d.last).Seconds(); dt > 0.004 {
			d.velX = float64(p.X-d.lastX) / dt
			d.velY = float64(p.Y-d.lastY) / dt
			d.lastX, d.lastY, d.last = p.X, p.Y, now
		}
		s.win.SetPosition(d.originX+dx, d.originY+dy)
	}
}

// endDrag keeps the throw going and rests on the work area.
func (s *shell) endDrag() {
	d := &s.drag
	s.panel.dragging = false
	x, y := s.win.Position()
	vx, vy := d.velX, d.velY
	if time.Since(d.last) > 80*time.Millisecond {
		vx, vy = 0, 0
	}
	s.x.jump(float64(x))
	s.y.jump(float64(y))
	s.x.velocity, s.y.velocity = vx, vy
	tx, ty := s.clamp(x+int(vx*0.12), y+int(vy*0.12), s.curW, s.curH)
	s.glideTo(tx, ty)
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
