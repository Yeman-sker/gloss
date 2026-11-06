// Package app is the desktop shell: the shortcut, the panel, and how blocks
// look and move. It is the only package that may import mygo.
package app

import (
	"context"
	_ "embed"
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gloss/capture"
	"gloss/core"
	"gloss/memory"
	"gloss/providers/openai"
	"gloss/providers/prompt"
	"gloss/routing"
	"gloss/settings"
)

// Name is the working name of the product: change it here and in mygo.json.
const Name = "Gloss"

const escape = "Escape"

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

	// The features, in their order, and how each is described.
	descs []core.Descriptor

	// The config, read again when its file changes. cfgErr says why it
	// cannot be used as it is; the features report it when they run.
	cfgMu    sync.Mutex
	cfgPath  string
	cfg      *settings.Config
	cfgErr   error
	cfgStamp time.Time
	shortcut string

	store     *memory.Store
	lastWrite time.Duration

	// sess is the selection showing, and captureGen the shortcut's latest
	// press: a capture that comes back after another press shows nothing.
	sess        *session
	captureGen  uint64
	routeCancel context.CancelFunc

	autoItem, memItem *mygo.MenuItem

	// logFile is the diagnostic log, empty when there is none.
	logFile string

	// keys registers the shortcut: the system's, or a test's.
	keys shortcuts

	// prefs is the settings window while it is open.
	prefs *prefs

	frameLog     bool
	lastPaint    time.Time
	wasAnimating bool

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
	logFile, err := startLog()
	if err != nil {
		log.Printf("log file: %v", err)
	}
	s := &shell{logFile: logFile, keys: mygo.GlobalShortcut, engine: core.NewEngine(), frameLog: os.Getenv("GLOSS_FRAMELOG") != ""}
	specs, err := prompt.Builtin()
	if err != nil {
		return err
	}
	for _, spec := range specs {
		if err := s.engine.Register(prompt.Action{Spec: spec, Chat: s.chat}); err != nil {
			return err
		}
		s.descs = append(s.descs, spec.Descriptor)
	}
	if s.cfgPath, err = settings.Path(Name); err != nil {
		return err
	}
	// One Gloss at a time: two would share the shortcut and the memory file.
	// Starting it again opens the settings of the one running, as opening
	// the app again from Finder does.
	if !mygo.App.RequestSingleInstanceLock() {
		log.Printf("already running: opened its settings")
		return nil
	}
	mygo.App.OnSecondInstance(func([]string, string) { mygo.RunOnMain(func() { s.openPrefs("") }) })
	mygo.App.OnActivate(func(bool) { s.openPrefs("") })
	s.loadConfig()
	cfg := s.config()
	s.store, err = memory.Open(filepath.Join(filepath.Dir(s.cfgPath), "memory"),
		memory.Options{RetentionDays: cfg.Memory.RetentionDays, MaxRecords: cfg.Memory.MaxRecords}, cfg.Memory.Enabled)
	if err != nil {
		log.Printf("memory: %v", err)
	}
	if s.helper, err = capture.Start(capture.Path()); err != nil {
		return err
	}
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
	s.panel.copied = func(c *ui.Context, text string) {
		c.WriteClipboard(text)
		if s.sess != nil {
			s.sess.copied = true
		}
	}
	s.panel.retry = s.retry
	s.panel.explain = s.explain
	s.panel.switchTo = s.switchTo
	s.panel.replace = s.replace

	// The panel refuses to close, which holds App.Quit up: signals and the
	// tray end directly. The helper exits when its stdin closes with us.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		s.endSession()
		os.Exit(0)
	}()

	// Gloss speaks Chinese alone, and so do mygo's own controls and menus.
	mygo.App.SetLanguages("zh-Hans")
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
		s.registerShortcut(cfg.Shortcut)
		s.makeTray()
		go s.watchConfig()
		log.Printf("ready: %s", s.shortcut)
		// The first run, or one that cannot work: the guide.
		go func() {
			trusted := s.helper.Trusted()
			mygo.RunOnMain(func() {
				if !trusted || !hasService(s.config()) {
					s.openPrefs(pageWelcome)
				}
			})
		}()
	})
	return mygo.App.Run()
}

func (s *shell) registerShortcut(key string) {
	if key == s.shortcut {
		return
	}
	if s.shortcut != "" {
		s.keys.Unregister(s.shortcut)
	}
	if err := s.keys.Register(key, s.trigger); err != nil {
		log.Printf("shortcut %s: %v", key, err)
		if s.shortcut != "" && s.shortcut != key {
			// Keep the one that worked.
			if s.keys.Register(s.shortcut, s.trigger) == nil {
				return
			}
		}
		s.shortcut = ""
		return
	}
	s.shortcut = key
}

// shortcuts is what mygo.GlobalShortcut does for the shell.
type shortcuts interface {
	Register(accelerator string, fn func()) error
	Unregister(accelerator string)
}

// pauseShortcut lets go of the shortcut while a new one is recorded, so
// that pressing the old one records it rather than reading a selection.
func (s *shell) pauseShortcut() {
	if s.shortcut != "" {
		s.keys.Unregister(s.shortcut)
	}
}

// resumeShortcut takes the shortcut back after pauseShortcut.
func (s *shell) resumeShortcut() {
	if s.shortcut == "" {
		return
	}
	if err := s.keys.Register(s.shortcut, s.trigger); err != nil {
		log.Printf("shortcut %s: %v", s.shortcut, err)
		s.shortcut = ""
	}
}

// tryShortcut moves to key after pauseShortcut, or takes the old one back
// when the system will not give Gloss key.
func (s *shell) tryShortcut(key string) error {
	if err := s.keys.Register(key, s.trigger); err != nil {
		s.resumeShortcut()
		return err
	}
	s.shortcut = key
	return nil
}

func (s *shell) makeTray() {
	cfg := s.config()
	s.autoItem = &mygo.MenuItem{Label: "自动选择功能", Type: mygo.MenuItemCheckbox, Checked: cfg.Routing.Enabled,
		Click: func(it *mygo.MenuItem, _ *mygo.Window) { s.setOption(it.Checked, "routing", "enabled") }}
	s.memItem = &mygo.MenuItem{Label: "记忆使用习惯", Type: mygo.MenuItemCheckbox, Checked: cfg.Memory.Enabled,
		Click: func(it *mygo.MenuItem, _ *mygo.Window) { s.setOption(it.Checked, "memory", "enabled") }}
	var err error
	s.tray, err = mygo.NewTray(mygo.TrayOptions{
		ID:             "main",
		Icon:           trayIcon,
		IconIsTemplate: true,
		ToolTip:        Name,
		Menu: mygo.NewMenu([]*mygo.MenuItem{
			{Label: "关于 " + Name + " " + Version, Click: func(*mygo.MenuItem, *mygo.Window) { s.openPrefs(pageGeneral) }},
			{Label: "设置…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { s.openPrefs("") }},
			{Type: mygo.MenuItemSeparator},
			s.autoItem,
			s.memItem,
			{Type: mygo.MenuItemSeparator},
			{Label: "退出 " + Name, Click: func(*mygo.MenuItem, *mygo.Window) {
				if s.prefs != nil {
					s.prefs.flush()
				}
				s.endSession()
				os.Exit(0)
			}},
		}),
	})
	if err != nil {
		log.Printf("tray: %v", err)
	}
}

// trigger reads the selection away from the main thread and starts on it.
func (s *shell) trigger() {
	s.captureGen++
	gen := s.captureGen
	start := time.Now()
	go func() {
		r, err := s.helper.Selection()
		took := time.Since(start)
		mygo.RunOnMain(func() {
			if gen == s.captureGen {
				s.captured(r, err, took)
			} else if err == nil && r.Token != 0 {
				go s.helper.Forget(r.Token)
			}
		})
	}()
}

// captured starts on what the helper read. The same selection again closes
// the panel.
func (s *shell) captured(r capture.Result, err error, took time.Duration) {
	text := strings.TrimSpace(r.Text)
	if err == nil && text == "" {
		err = capture.ErrNoSelection
	}
	if err != nil {
		log.Printf("capture: %s", errKind(err))
		if s.open() && s.panel.notice == "" && errors.Is(err, capture.ErrNoSelection) {
			s.hide()
			return
		}
		s.endSession()
		s.engine.Cancel()
		s.showNotice(captureNotice(err))
		if errors.Is(err, capture.ErrPermission) {
			s.openPrefs(pageWelcome)
		}
		return
	}
	if s.open() && s.panel.notice == "" && text == s.panel.source {
		s.hide()
		return
	}
	cfg := s.config()
	src := newSource(r, took.Milliseconds(), cfg.TargetLang)
	log.Printf("capture: %s in %dms, app %s, editable %s, %s", r.Method, took.Milliseconds(), or(r.App, "unknown"), src.ctx.Editable, src.shape.Class())
	s.begin(src)
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// captureNotice says what went wrong with reading the selection, and what
// to do about it.
func captureNotice(err error) string {
	switch {
	case errors.Is(err, capture.ErrPermission):
		return "需要辅助功能权限：系统设置 › 隐私与安全性 › 辅助功能"
	case errors.Is(err, capture.ErrTimeout):
		return "取词超时，已重启取词程序，请再试一次"
	case errors.Is(err, capture.ErrExited):
		return "取词程序意外退出，已重启，请再试一次"
	case errors.Is(err, capture.ErrProtocol):
		return "取词程序返回了无法识别的结果，已重启"
	}
	return "先选中一段文字"
}

// candidates are the features that are on, in the config's order: the
// first is the default.
func (s *shell) candidates(cfg *settings.Config) []core.Descriptor {
	return ordered(s.descs, cfg.Actions)
}

func ordered(descs []core.Descriptor, ids []string) []core.Descriptor {
	var out []core.Descriptor
	for _, id := range ids {
		for _, d := range descs {
			if d.ID == id {
				out = append(out, d)
				break
			}
		}
	}
	if len(out) == 0 && len(descs) > 0 {
		out = descs[:1]
	}
	return out
}

func (s *shell) descriptor(id string) (core.Descriptor, bool) {
	for _, d := range s.descs {
		if d.ID == id {
			return d, true
		}
	}
	return core.Descriptor{}, false
}

// begin opens a session on a selection: the panel shows it at once, and
// the feature starts once routing has picked it.
func (s *shell) begin(src source) {
	s.endSession()
	cfg := s.config()
	cands := s.candidates(cfg)
	now := time.Now()
	sess := &session{id: newSessionID(), start: now, src: src}
	var features []feature
	for _, d := range cands {
		sess.candidates = append(sess.candidates, d.ID)
		features = append(features, feature{d.ID, d.Name})
	}
	s.sess = sess
	s.present(func() {
		s.panel.reset(src.text)
		s.panel.features = features
		s.panel.copyText = func() string {
			if sess.done {
				return sess.result.String()
			}
			return ""
		}
	})
	def := cands[0].ID
	if !cfg.Routing.Enabled || len(cands) == 1 {
		why := "自动选择已关闭"
		if len(cands) == 1 {
			why = "只有一个可用功能"
		}
		sess.decide(def, memory.ByDefault, why, nil, 0)
		s.run(sess, def, memory.ByDefault, why)
		return
	}
	sess.routing = true
	ctx, cancel := context.WithCancel(context.Background())
	s.routeCancel = cancel
	router := routing.Router{
		Wait:            time.Duration(cfg.Routing.WaitMillis) * time.Millisecond,
		MinConfidence:   cfg.Routing.MinConfidence,
		PreferenceShare: 0.6,
	}
	if svc, ok := cfg.Jev(); ok {
		router.Judge = routing.Jev{BaseURL: svc.BaseURL, APIKey: svc.APIKey, Model: svc.Model, SendTitle: cfg.Routing.SendWindowTitle}
	}
	var enabled []string
	for _, d := range cands {
		enabled = append(enabled, d.ID)
	}
	store := s.store
	go func() {
		var prefs memory.Snapshot
		if store != nil && store.Enabled() {
			prefs = memory.Prefer(store.Records(), memory.Situation{App: src.ctx.SourceApp, Editable: src.ctx.Editable.String(), Shape: src.shape}, enabled, time.Now())
		}
		d := router.Route(ctx, routing.Input{
			Text: src.text, Shape: src.shape, App: src.ctx.SourceApp, Title: src.title,
			Editable: src.ctx.Editable, TargetLang: src.ctx.TargetLang,
			Candidates: cands, Default: def, Prefs: prefs,
		})
		if d.JevError != "" {
			log.Printf("route: Jev not used: %s", d.JevError)
		}
		mygo.RunOnMain(func() {
			if s.sess != sess || !sess.decide(d.ID, d.By, d.Reason, d.Scores, d.Elapsed) {
				return
			}
			log.Printf("route: %s by %s in %v", d.ID, d.By, d.Elapsed.Round(time.Microsecond))
			s.run(sess, d.ID, d.By, d.Reason)
		})
	}()
}

// run starts a feature on the session's selection. Only its events show:
// the panel drops those of runs it replaced.
func (s *shell) run(sess *session, id, by, why string) {
	d, ok := s.descriptor(id)
	if !ok || sess.closed {
		return
	}
	cfg := s.config()
	req := core.Request{Text: sess.src.text, Context: sess.src.ctx, Settings: cfg.Values(d)}
	runID, err := s.engine.Start(id, req, func(ev core.Event) {
		s.win.Update(func() {
			if s.sess != sess || !sess.accepts(ev.Run) {
				return
			}
			first := sess.firstAt.IsZero()
			sess.observe(ev, time.Now())
			logRun(id, ev, sess, first)
			s.panel.apply(ev)
		})
	})
	if err != nil {
		log.Print(err)
		return
	}
	sess.begin(id, by, runID, time.Now())
	replaceable := d.Replaceable && !sess.replaced && sess.src.token != 0 && sess.src.ctx.Editable == core.EditableYes
	s.panel.beginRun(runID, feature{d.ID, d.Name}, by, why, d.Output, replaceable)
	s.win.Invalidate()
}

// logRun keeps a run's progress in the log: when it began to answer and
// how it ended, never what it said.
func logRun(id string, ev core.Event, sess *session, first bool) {
	switch {
	case ev.Type == core.EventDelta && first:
		log.Printf("run: %s first text in %dms", id, sess.firstAt.Sub(sess.runStart).Milliseconds())
	case ev.Type == core.EventDone:
		log.Printf("run: %s done in %dms", id, time.Since(sess.runStart).Milliseconds())
	case ev.Type == core.EventError:
		log.Printf("run: %s failed after %dms: %s", id, time.Since(sess.runStart).Milliseconds(), errKind(ev.Err))
	}
}

// switchTo runs another feature on the same selection: the user's
// correction, which routing will not override.
func (s *shell) switchTo(id string) {
	sess := s.sess
	if sess == nil || !sess.choose(id, time.Now()) {
		return
	}
	if s.routeCancel != nil {
		s.routeCancel()
	}
	s.run(sess, id, memory.ByManual, "你选择了它")
}

// retry runs the feature showing again, on the same selection.
func (s *shell) retry() {
	sess := s.sess
	if sess == nil || sess.feature == "" {
		return
	}
	s.run(sess, sess.feature, sess.by, s.panel.why)
}

// replace writes the result back where the selection was, if it is still
// there as it was. The helper checks; this only asks.
func (s *shell) replace() {
	sess := s.sess
	if sess == nil || !sess.done || sess.replaced || !s.panel.replaceable {
		return
	}
	result := strings.TrimSpace(sess.result.String())
	if result == "" {
		return
	}
	// Keep the whitespace the selection had around it.
	raw := sess.src.raw
	lead := raw[:len(raw)-len(strings.TrimLeftFunc(raw, unicode.IsSpace))]
	trail := raw[len(strings.TrimRightFunc(raw, unicode.IsSpace)):]
	text := lead + result + trail
	token := sess.src.token
	run := sess.run
	// The reply counts only for the run that asked: a switch to another
	// feature in the meantime makes it stale.
	current := func() bool { return s.sess == sess && sess.accepts(run) }
	s.panel.setStatus("正在替换…", colorHint)
	s.win.Invalidate()
	go func() {
		r, err := s.helper.Replace(token, text)
		mygo.RunOnMain(func() {
			// The request went out and no answer came: the text may have
			// been written. Treated as written, never offered again.
			unknown := errors.Is(err, capture.ErrTimeout) || errors.Is(err, capture.ErrExited)
			if err == nil || unknown {
				// The helper wrote, whatever shows now: never offer it again.
				sess.replaced = true
			}
			// What was written holds for the session, whichever feature
			// shows now: the button goes, and an unknown result is told.
			if s.sess == sess && !sess.closed && (sess.replaced || current()) {
				if sess.replaced {
					s.panel.replaceable = false
				}
				if unknown {
					log.Printf("replace: %s", errKind(err))
					s.panel.setStatus("取词程序没有回应，替换结果未知，请检查原文", colorDanger)
					s.win.Invalidate()
					return
				}
			}
			if !current() {
				s.win.Invalidate()
				return
			}
			if err != nil {
				log.Printf("replace: %s", errKind(err))
				s.panel.setStatus(replaceFailure(err), colorDanger)
				s.win.Invalidate()
				return
			}
			log.Printf("replace: %s, verified %v, %dµs", r.Method, r.Verified, r.Micros)
			if !r.Verified {
				s.panel.setStatus("已写入，但没能确认结果，请检查原文", colorDanger)
				s.win.Invalidate()
				return
			}
			s.panel.setStatus("已替换", colorSuccess)
			s.win.Invalidate()
			time.AfterFunc(700*time.Millisecond, func() {
				mygo.RunOnMain(func() {
					if current() && !s.panel.pinned {
						s.hide()
					}
				})
			})
		})
	}()
}

func replaceFailure(err error) string {
	switch {
	case errors.Is(err, capture.ErrChanged):
		return "原文或选区已变化，没有替换"
	case errors.Is(err, capture.ErrNotEditable):
		return "这里不能编辑，没有替换"
	case errors.Is(err, capture.ErrExpired):
		return "选区已失效，没有替换"
	case errors.Is(err, capture.ErrUnverified):
		return "读不到原选区，没有替换"
	case errors.Is(err, capture.ErrPermission):
		return "需要辅助功能权限才能替换"
	}
	return "替换失败"
}

// endSession closes the session showing and keeps what memory may keep of
// it: never its text.
func (s *shell) endSession() {
	sess := s.sess
	if sess == nil || sess.closed {
		return
	}
	if s.routeCancel != nil {
		s.routeCancel()
		s.routeCancel = nil
	}
	rec := sess.record(time.Now())
	sess.closed = true
	if sess.src.token != 0 {
		go s.helper.Forget(sess.src.token)
	}
	store := s.store
	if store == nil || !store.Enabled() || rec.Final == "" {
		return
	}
	rec.Timings.WriteMs = s.lastWrite.Milliseconds()
	took, err := store.Add(rec)
	s.lastWrite = took
	if err != nil {
		log.Printf("memory: %v", err)
	}
}

func (s *shell) clearMemory() {
	if s.store == nil {
		return
	}
	if err := s.store.Clear(); err != nil {
		log.Printf("memory: %v", err)
		s.showNotice("清空记忆失败")
		return
	}
	s.showNotice("已清空记忆")
}

// chat is how every feature reaches its model: the service the config
// names for it, read at the moment it runs.
func (s *shell) chat(ctx context.Context, actionID string, msgs []prompt.Message, delta func(string)) error {
	s.cfgMu.Lock()
	cfg, cfgErr := s.cfg, s.cfgErr
	s.cfgMu.Unlock()
	if cfgErr != nil {
		return cfgErr
	}
	svc, err := cfg.Service(actionID)
	if err != nil {
		return err
	}
	out := make([]openai.Message, len(msgs))
	for i, m := range msgs {
		out[i] = openai.Message{Role: m.Role, Content: m.Content}
	}
	return openai.Client{BaseURL: svc.BaseURL, APIKey: svc.APIKey, Model: svc.Model}.Stream(ctx, out, delta)
}

// present puts new content in the panel: it glides there when it is open,
// and appears there when it is not. A pinned panel stays where it is.
func (s *shell) present(content func()) {
	wasOpen := s.open()
	content()
	p := mygo.Screen.CursorScreenPoint()
	s.area = mygo.Screen.DisplayNearestPoint(p).WorkArea
	s.panel.maxHeight = float32(s.area.Height) * 0.6
	s.panel.maxReading = float32(s.area.Height) * 0.75
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
	// A capture still on its way shows nothing once the panel is closed.
	s.captureGen++
	s.engine.Cancel()
	s.endSession()
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
		settle()
	}
}

// settle hands the memory a closed window was using back to the system a
// moment later, once nothing is drawing: Go keeps freed memory for a while
// on its own, which shows as Gloss's size while it sits in the menu bar.
func settle() {
	settleOnce.Do(func() {
		settleTimer = time.AfterFunc(settleAfter, debug.FreeOSMemory)
	})
	settleTimer.Reset(settleAfter)
}

const settleAfter = 5 * time.Second

var (
	settleOnce  sync.Once
	settleTimer *time.Timer
)

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
	if s.frameLog {
		// Only gaps after a frame that asked for the next one count.
		now := time.Now()
		// panel.animating is still the last frame's here.
		if gap := now.Sub(s.lastPaint); (s.panel.animating || s.wasAnimating) && gap > 25*time.Millisecond && gap < time.Second {
			log.Printf("frame: gap %v, pressed %v, dragging %v, glide %v", gap.Round(time.Millisecond), pressed, s.drag.active, s.x.moving() || s.y.moving())
		}
		s.lastPaint = now
		s.wasAnimating = pressed || s.drag.active || s.x.moving() || s.y.moving()
	}
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
