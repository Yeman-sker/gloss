// Package app is the desktop shell: the shortcut, the panel, and how blocks
// look and move. It is the only package that may import mygo.
package app

import (
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gloss/capture"
	"gloss/core"
	"gloss/providers/openai"
)

// Name is the working name of the product: change it here and in mygo.json.
const Name = "Gloss"

const (
	shortcut = "Alt+Shift+D"
	escape   = "Escape"

	defaultBaseURL = "https://api.openai.com/v1"
)

// shell ties the shortcut, the helper and the engine to the panel.
type shell struct {
	win    *mygo.Window
	helper *capture.Helper
	engine *core.Engine
	panel  panel
	action string
}

// Run starts the app and returns when it ends.
func Run() error {
	engine := core.NewEngine()
	translate := translator()
	if err := engine.Register(translate); err != nil {
		return err
	}
	helper, err := capture.Start(capture.Path())
	if err != nil {
		return err
	}
	s := &shell{helper: helper, engine: engine, action: translate.ID()}

	// The panel refuses to close, which holds App.Quit up: end directly.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		os.Exit(0)
	}()

	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.WhenReady(func() {
		s.win = mygo.NewWindow(mygo.WindowOptions{
			Title:        Name,
			Panel:        true,
			Hidden:       true,
			Frameless:    true,
			Transparent:  true,
			Vibrancy:     mygo.VibrancyGlass,
			CornerRadius: 16,
			Width:        panelWidth,
			Height:       120,
			Content:      ui.View(s.panel.view(func(c *ui.Context) { s.resize(c) })),
		})
		s.win.OnClose(func(e *mygo.CloseEvent) {
			e.PreventDefault()
			s.hide()
		})
		if err := mygo.GlobalShortcut.Register(shortcut, s.trigger); err != nil {
			log.Printf("shortcut %s: %v", shortcut, err)
		}
		log.Printf("ready: %s", shortcut)
	})
	return mygo.App.Run()
}

// translator reads the service from the environment until there is a config
// file. A missing key or model is reported by the action, in the panel.
func translator() openai.Action {
	base := os.Getenv("OPENAI_BASE_URL")
	if base == "" {
		base = defaultBaseURL
	}
	return openai.Action{BaseURL: base, APIKey: os.Getenv("OPENAI_API_KEY"), Model: os.Getenv("OPENAI_MODEL")}
}

// trigger reads the selection and starts the action on it.
func (s *shell) trigger() {
	r, err := s.helper.Selection()
	text := strings.TrimSpace(r.Text)
	switch {
	case err != nil:
		s.panel.reset("没有取到选中文本："+err.Error(), r.Method)
	case !r.OK:
		s.panel.reset("没有取到选中文本："+r.Error, r.Method)
	default:
		s.panel.reset(text, r.Method)
	}
	s.place()
	s.show()
	if err != nil || !r.OK {
		s.engine.Cancel()
		return
	}
	req := core.Request{Text: text, Context: core.Context{TargetLang: "zh-Hans"}}
	_, err = s.engine.Start(s.action, req, func(ev core.Event) {
		s.win.Update(func() { s.panel.apply(ev) })
	})
	if err != nil {
		log.Print(err)
	}
}

func (s *shell) place() {
	p := mygo.Screen.CursorScreenPoint()
	a := mygo.Screen.DisplayNearestPoint(p).WorkArea
	s.win.SetPosition(min(p.X+12, a.X+a.Width-panelWidth-12), min(p.Y+16, a.Y+a.Height-420))
}

// show keeps the keyboard with the app the user is in, so Escape is only
// ours while the panel shows.
func (s *shell) show() {
	s.win.ShowInactive()
	mygo.GlobalShortcut.Unregister(escape)
	if err := mygo.GlobalShortcut.Register(escape, s.hide); err != nil {
		log.Printf("shortcut %s: %v", escape, err)
	}
}

func (s *shell) hide() {
	mygo.GlobalShortcut.Unregister(escape)
	s.engine.Cancel()
	s.win.Hide()
}

// resize follows the content with the window: native UI cannot fit itself.
func (s *shell) resize(c *ui.Context) {
	h := c.Root().Spring("height", s.panel.height, ui.SpringOptions{Duration: heightSpring, Bounce: 0.12})
	if s.panel.height <= 0 || h <= 0 {
		return
	}
	if _, cur := s.win.ContentSize(); cur != int(h+0.5) {
		s.win.SetContentSize(panelWidth, int(h+0.5))
	}
}
