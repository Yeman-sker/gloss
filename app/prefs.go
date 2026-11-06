package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gloss/core"
	"gloss/memory"
	"gloss/providers/openai"
	"gloss/settings"
)

// Version is the version of this build. mygo.json says the same, which a
// test checks.
const Version = "0.1.0"

var isMac = runtime.GOOS == "darwin"

// Pages of the settings window. A feature's page is pageFeature and its ID.
const (
	pageWelcome  = "welcome"
	pageGeneral  = "general"
	pageServices = "services"
	pageMemory   = "memory"
	pageFeatures = "features"
	pageFeature  = "feature:"
)

// commitAfter is how long a text field waits after the last key before it
// writes what was typed. Enter writes at once, as does closing the window.
const commitAfter = 700 * time.Millisecond

// prefs is the settings window. Switches and choices write the config file
// as they change; text fields write it a moment after the typing stops.
// Every write goes through settings.Update, which writes only a file that
// reads as a valid config, and the shell then applies it as it applies a
// file changed by hand.
type prefs struct {
	s   *shell
	win *mygo.Window
	// post runs a function on the main thread and draws a frame.
	post func(func())

	page        string
	shown       string // the page last drawn
	sectionOpen bool

	services      []*serviceDraft
	nextService   int
	deleting      *serviceDraft
	confirmDelete bool

	recording   bool
	shortcutErr string

	memDays, memMax numberDraft
	waitMs, minConf numberDraft
	confirmClear    bool
	stats           memory.Stats
	statsAt         time.Time
	memNote         string
	memNoteAt       time.Time

	// The features' models and text settings, by "id" and "id.key".
	models map[string]*textDraft
	texts  map[string]*textDraft

	// trusted is whether the helper has Accessibility access, as last
	// asked; checkTrust asks, away from the main thread.
	trusted    bool
	trustAsked time.Time
	asking     bool
	checkTrust func() bool

	// errs are what the last change of a control could not write, by the
	// control's key.
	errs map[string]string
	// justSaved and savedAt drive the marker that shows a value was written:
	// the write has no frame to hand, so the row takes it when it next paints.
	justSaved map[string]bool
	savedAt   map[string]time.Time
	// disclosed is which pages have their advanced part open.
	disclosed map[string]bool
}

// textDraft is a text field's value while it is typed.
type textDraft struct {
	value, saved string
	editedAt     time.Time
	commit       func(string) error
}

type numberDraft struct {
	value, saved float64
	editedAt     time.Time
	commit       func(float64) error
}

type serviceDraft struct {
	id                    int
	name, url, key, model textDraft
	showKey               bool
	open                  bool

	testing   bool
	firstWord time.Duration
	testGen   int
	tested    string
	testOK    bool
}

// openPrefs shows the settings window, at page if it is not empty.
func (s *shell) openPrefs(page string) {
	if s.prefs != nil {
		if page != "" {
			s.prefs.page = page
		}
		s.prefs.win.Show()
		s.prefs.front()
		return
	}
	p := newPrefs(s, page)
	s.prefs = p
	// An app that is only in the menu bar cannot take the keyboard: it
	// joins the Dock while its window is open, as macOS apps of its kind do.
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	p.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         Name + " 设置",
		Width:         820,
		Height:        600,
		MinWidth:      700,
		MinHeight:     480,
		Hidden:        true,
		TitleBarStyle: mygo.TitleBarHiddenInset,
		Content:       ui.View(p.view),
	})
	p.post = p.win.Update
	p.win.OnClose(func(*mygo.CloseEvent) { p.flush() })
	p.win.OnClosed(func() {
		if p.recording {
			p.stopRecording()
		}
		s.prefs = nil
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
		releaseIcons()
		settle()
	})
	p.win.OnBlur(func() {
		if p.recording {
			p.stopRecording()
			p.win.Invalidate()
		}
	})
	p.win.Center()
	p.win.Show()
	p.front()
	// The Dock policy takes a moment to apply, until when macOS will not
	// bring the app forward: ask again after it.
	time.AfterFunc(200*time.Millisecond, func() { mygo.RunOnMain(p.front) })
}

func (p *prefs) front() {
	if p.s.prefs != p {
		return
	}
	mygo.App.Focus()
	p.win.Focus()
}

func newPrefs(s *shell, page string) *prefs {
	p := &prefs{s: s, page: pageGeneral, sectionOpen: true, models: map[string]*textDraft{}, texts: map[string]*textDraft{}, errs: map[string]string{}, justSaved: map[string]bool{}, savedAt: map[string]time.Time{}, disclosed: map[string]bool{}}
	if s.helper != nil {
		p.checkTrust = s.helper.Trusted
	}

	if page != "" {
		p.page = page
	}
	p.sync()
	return p
}

// sync takes the values of the config as last read, but for fields being
// typed in.
func (p *prefs) sync() {
	cfg := p.s.config()
	byName := map[string]*serviceDraft{}
	for _, d := range p.services {
		byName[d.name.saved] = d
	}
	names := make([]string, 0, len(cfg.Services))
	for n := range cfg.Services {
		names = append(names, n)
	}
	sort.Strings(names)
	var next []*serviceDraft
	for _, n := range names {
		svc := cfg.Services[n]
		d := byName[n]
		if d == nil {
			p.nextService++
			d = &serviceDraft{id: p.nextService}
		}
		d.name.take(n)
		d.url.take(svc.BaseURL)
		d.key.take(svc.APIKey)
		d.model.take(svc.Model)
		next = append(next, d)
	}
	p.services = next
	p.memDays.take(float64(cfg.Memory.RetentionDays))
	p.memMax.take(float64(cfg.Memory.MaxRecords))
	p.waitMs.take(float64(cfg.Routing.WaitMillis))
	p.minConf.take(cfg.Routing.MinConfidence)
	for _, d := range p.s.descs {
		_, model := cfg.ActionService(d.ID)
		p.text(p.models, d.ID).take(model)
		for _, st := range d.Settings {
			if st.Type == core.SettingText || st.Type == core.SettingNumber {
				v, _ := cfg.ActionValue(d.ID, st.Key)
				p.text(p.texts, d.ID+"."+st.Key).take(v)
			}
		}
	}
}

func (p *prefs) text(m map[string]*textDraft, key string) *textDraft {
	d := m[key]
	if d == nil {
		d = &textDraft{}
		m[key] = d
	}
	return d
}

// take sets the draft to what the file holds, unless it is being typed.
func (d *textDraft) take(v string) {
	if d.editedAt.IsZero() {
		d.value = v
	}
	d.saved = v
}

func (d *numberDraft) take(v float64) {
	if d.editedAt.IsZero() {
		d.value = v
	}
	d.saved = v
}

// leavePage writes what the page left behind was still waiting to write:
// its fields commit as they draw, and the page will draw no more.
func (p *prefs) leavePage() {
	if p.page != p.shown {
		if p.shown != "" {
			p.flush()
		}
		p.shown = p.page
	}
}

// flush writes every field still waiting.
func (p *prefs) flush() {
	all := []*textDraft{}
	for _, d := range p.services {
		all = append(all, &d.name, &d.url, &d.key, &d.model)
	}
	for _, d := range p.models {
		all = append(all, d)
	}
	for _, d := range p.texts {
		all = append(all, d)
	}
	for _, d := range all {
		if !d.editedAt.IsZero() && d.commit != nil {
			d.editedAt = time.Time{}
			if d.value != d.saved {
				_ = d.commit(d.value)
			}
		}
	}
	for _, d := range []*numberDraft{&p.memDays, &p.memMax, &p.waitMs, &p.minConf} {
		if !d.editedAt.IsZero() && d.commit != nil {
			d.editedAt = time.Time{}
			if d.value != d.saved {
				_ = d.commit(d.value)
			}
		}
	}
}

// update changes the config file, applies it, and keeps what went wrong for
// the control at key to show.
func (p *prefs) update(key string, fn func(*settings.Doc) error) error {
	_, err := settings.Update(p.s.cfgPath, p.s.descs, fn)
	if err != nil {
		p.errs[key] = p.brief(err)
		log.Printf("settings: %s: %s", key, p.brief(err))
		return err
	}
	delete(p.errs, key)
	p.justSaved[key] = true
	p.s.reload()
	return nil
}

// brief is an error of the config file without the file's path, which the
// window shows elsewhere.
func (p *prefs) brief(err error) string { return brief(err, p.s.cfgPath) }

// set is an update of one value.
func (p *prefs) set(key string, v any, keys ...string) error {
	return p.update(key, func(d *settings.Doc) error {
		d.Set(v, keys...)
		return nil
	})
}

// The look.

func (p *prefs) view(c *ui.Context) {
	c.SetTheme(prefsTheme(c.Theme()))
	p.leavePage()
	if !p.trusted {
		p.askTrust(c)
	}
	// The window's controls sit over the sidebar, which keeps clear of them; the
	// sheet has them nowhere near, and only keeps a strip to drag the window by.
	top := max(c.TitleBar().Height-clearLights, 28)
	ui.Row(c).Fill().Background(inkDeep).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(widthSidebar).Shrink(0).Children(func() {
			ui.Box(c).Height(top).Shrink(0).DragWindow()
			p.sidebar(c)
			p.leavePage()
			p.sidebarFoot(c)
		})
		// The page is a sheet on the deep ink: its corners keep to the window's,
		// and the sidebar reads as the desk it lies on.
		sheet := ui.Column(c).Grow(1).Shrink(1).Margin(marginSheet, marginSheet, marginSheet, 0).Radius(radiusCard).Background(colorSurface).
			Border(1, colorRule).InsetShadow(0, 1, 0, 0, colorSheen).Clip()
		pad := pagePadding(sheet.Bounds().W)
		sheet.Children(func() {
			ui.Box(c).Height(heightSheetDrag).Shrink(0).DragWindow()
			ui.Scroll(c).Key(p.page).Grow(1).ScrollbarInsets(0, 2, 10, 0).Children(func() {
				ui.Column(c).Padding(paddingPageTop, pad, paddingPageEnd, pad).Gap(gapPage).
					Transition(ui.ElementTransition{Enter: &ui.Motion{Y: riseText * 2, Opacity: 0}, Duration: fadeText + 60*time.Millisecond, Ease: iconEase}).
					Children(func() {
						p.banner(c)
						switch {
						case p.page == pageWelcome:
							p.welcome(c)
						case p.page == pageGeneral:
							p.general(c)
						case p.page == pageServices:
							p.servicesPage(c)
						case p.page == pageMemory:
							p.memoryPage(c)
						case p.page == pageFeatures:
							p.featuresPage(c)
						case strings.HasPrefix(p.page, pageFeature):
							p.featurePage(c, strings.TrimPrefix(p.page, pageFeature))
						}
					})
			})
		})
	})
}

// place is the navigation's entry for the page shown: a feature's page is inside
// 功能, which lists them.
func (p *prefs) place() string {
	if strings.HasPrefix(p.page, pageFeature) {
		return pageFeatures
	}
	return p.page
}

func (p *prefs) sidebar(c *ui.Context) {
	cfg := p.s.config()
	t := *c.Theme()
	// The marker is the sidebar's selection: its items paint no fill of their own.
	t.SurfaceHover, t.SurfacePressed, t.Surface = ui.Transparent, ui.Transparent, ui.Transparent
	place := p.place()
	ui.Themed(c, &t).Grow(1).Shrink(1).Background(ui.Transparent).Children(func() {
		ui.Sidebar(c, &place, func() {
			if p.page == pageWelcome || !p.ready(cfg) {
				p.navItem(c, pageWelcome, "welcome", "开始使用", nil)
			}
			p.navItem(c, pageGeneral, "general", "通用", nil)
			p.navItem(c, pageServices, "services", "模型服务", func() {
				if !hasService(cfg) {
					statusDot(c, colorTomato).Absolute().Right(10).Top(14.5)
				}
			})
			p.navItem(c, pageFeatures, "features", "功能", nil)
			p.navItem(c, pageMemory, "memory", "自动选择", nil)
		}).Width(widthSidebar).Grow(1).Background(ui.Transparent).Padding(0, 10)
	})
	if place != p.place() {
		p.page = place
	}
}

// sidebarFoot says what this is, as the back of a sticker sheet does.
func (p *prefs) sidebarFoot(c *ui.Context) {
	ui.Row(c).Key("foot").Padding(8, 16, 14, 18).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Image(c, stickerImage).Size(18, 18)
		ui.Text(c, Name).FontSize(sizeLabel).FontWeight(500).TextColor(colorSource)
		ui.Text(c, Version).Font(fontSource).FontSize(sizeLabel).TextColor(colorMute)
	})
}

// featureOrder is every feature: those on in their order, then the rest.
func (p *prefs) featureOrder(cfg *settings.Config) []string {
	ids := append([]string(nil), cfg.Actions...)
	for _, d := range p.s.descs {
		if !cfg.Enabled(d.ID) {
			ids = append(ids, d.ID)
		}
	}
	return ids
}

const iconUnknownFeature = "features"

// featureIcon is the icon of a feature, by its ID; one the app does not know, as
// a plugin's, shows the star of the features page.
func featureIcon(id string) string {
	switch id {
	case "translate", "explain", "prompt", "agent":
		return id
	}
	return iconUnknownFeature
}

// banner shows a config file Gloss cannot use, which no control here can
// change until it is fixed by hand.
func (p *prefs) banner(c *ui.Context) {
	p.s.cfgMu.Lock()
	err := p.s.cfgErr
	p.s.cfgMu.Unlock()
	if err == nil || strings.Contains(err.Error(), settings.ErrMissing.Error()) {
		return
	}
	ui.Column(c).Key("banner").Padding(12, 14).Gap(8).Radius(radiusPopup).Background(colorDanger.Alpha(0.1)).Border(1, colorDanger.Alpha(0.28)).Children(func() {
		ui.Text(c, "配置文件有误，Gloss 仍在用上一次正确的设置").FontWeight(600).TextColor(colorText)
		ui.Text(c, p.brief(err)).FontSize(sizeHelp).LineHeight(1.5).TextColor(colorSource).Selectable()
		ui.Row(c).Gap(8).Children(func() {
			if button(c, buttonSpec{label: "打开配置文件", glyph: glyphFile, small: true}).Clicked() {
				p.s.openConfig()
			}
		})
	})
}

// General.

var languages = []struct{ code, name string }{
	{"zh-Hans", "简体中文"},
	{"zh-Hant", "繁體中文"},
	{"en", "English"},
	{"ja", "日本語"},
	{"ko", "한국어"},
	{"fr", "Français"},
	{"de", "Deutsch"},
	{"es", "Español"},
	{"ru", "Русский"},
}

func languageName(code string) string {
	for _, l := range languages {
		if l.code == code {
			return l.name
		}
	}
	return code
}

func (p *prefs) general(c *ui.Context) {
	cfg := p.s.config()
	pageHead(c, "general", "general", "通用", "")
	p.rows(c, "main", "", func() {
		help := "选中文字后按下它"
		if p.s.shortcut == "" && !p.recording {
			help = "这个快捷键没能注册，可能被其他应用占用，请换一个"
		}
		p.row(c, "shortcut", "快捷键", help, func() { p.shortcutField(c, cfg.Shortcut) })
		p.row(c, "target_lang", "目标语言", "翻译和回答都用这种语言", func() {
			names := languageNames(cfg.TargetLang)
			cur := languageName(cfg.TargetLang)
			if choice(c, "target_lang.sel", "目标语言", &cur, names, widthSelect).Changed() {
				_ = p.set("target_lang", languageCode(cur), "target_lang")
			}
		})
		p.row(c, "launch_at_login", "登录时启动", "", func() {
			on := cfg.LaunchAtLogin
			if markerSwitch(c, &on, "登录时启动", false).Changed() {
				_ = p.set("launch_at_login", on, "launch_at_login")
			}
		})
	})
	p.disclosure(c, "general.advanced", "高级", func() {
		p.row(c, "file", "配置文件", homeShort(p.s.cfgPath), func() {
			if button(c, buttonSpec{label: "打开", key: "file.open", glyph: glyphFile, small: true}).Clicked() {
				p.s.openConfig()
			}
		})
		if p.s.logFile != "" {
			p.row(c, "log", "诊断日志", "不含选中的文字、结果和 Key", func() {
				if button(c, buttonSpec{label: "显示", key: "log.show", glyph: glyphLog, small: true}).Clicked() {
					reveal(p.s.logFile)
				}
			})
		}
	})
}

// homeShort writes a path under the home folder as ~ does.
func homeShort(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + path[len(home):]
	}
	return path
}

// languageNames is the choices of a target language, with one the file holds
// that the list does not.
func languageNames(code string) []string {
	names := make([]string, 0, len(languages)+1)
	for _, l := range languages {
		names = append(names, l.name)
	}
	if cur := languageName(code); cur == code && code != "" {
		names = append(names, cur)
	}
	return names
}

func languageCode(name string) string {
	for _, l := range languages {
		if l.name == name {
			return l.code
		}
	}
	return name
}

// The shortcut recorder.

// shortcutField is the shortcut as keys. Pressed, it listens: the ring comes up
// in the marker's colour, the modifiers held rise as caps while they are held,
// and the first whole shortcut takes their place.
func (p *prefs) shortcutField(c *ui.Context, current string) {
	if p.recording {
		ui.Text(c, "Esc 取消").FontSize(sizeHelp).TextColor(colorMute).NoWrap()
	} else if current != settings.DefaultShortcut {
		if iconButton(c, "shortcut.reset", glyphReset, "恢复默认", false).Clicked() {
			p.s.pauseShortcut()
			p.setShortcut(settings.DefaultShortcut)
		}
	}
	b := ui.Row(c.Key("recorder")).Focusable().FocusRing(false).MinWidth(112).Height(42).Padding(0, 6).Gap(5).Radius(radiusPopup).
		AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).Label("录制快捷键").Shrink(0).Margin(0, -7, 0, 0)
	ring := ui.Transparent
	bg := ui.Transparent
	switch {
	case p.recording:
		ring, bg = colorMarker.Alpha(0.75), colorMarker.Alpha(0.07)
	case b.FocusVisible():
		ring = colorMarker.Alpha(0.6)
	case b.Hovered():
		bg = washHover
	}
	b.Border(1, b.AnimateColor("ring", ring, hoverAnim)).Background(b.AnimateColor("bg", bg, hoverAnim))
	b.HandleInput(func(ev ui.InputEvent) bool {
		if !p.recording || ev.Kind != ui.InputKeyDown {
			return false
		}
		if ev.Key == ui.KeyEscape && ev.Mods == 0 {
			p.stopRecording()
			return true
		}
		if acc, ok := accelerator(ev.Mods, ev.Key); ok {
			p.setShortcut(acc)
		}
		return true // a modifier alone: keep listening
	})
	hot, down := b.Hovered(), b.Pressed()
	b.Children(func() {
		if p.recording {
			if held := modKeys(c.Modifiers()); held != "" {
				keycaps(c, held, false, false, true, 30)
			} else {
				ui.Text(c, "按下新的快捷键").FontSize(sizeHelp).TextColor(colorMarker).NoWrap().Padding(0, 6)
			}
			return
		}
		keycaps(c, shortcutLabel(current), hot, down, false, 30)
	})
	if b.Clicked() && !p.recording {
		p.recording = true
		delete(p.errs, "shortcut")
		p.s.pauseShortcut()
		b.Focus()
	}
}

func (p *prefs) stopRecording() {
	p.recording = false
	p.s.resumeShortcut()
}

// setShortcut moves to acc when the system lets Gloss have it, and writes
// it; otherwise the old one stays.
func (p *prefs) setShortcut(acc string) {
	p.recording = false
	if err := p.s.tryShortcut(acc); err != nil {
		p.errs["shortcut"] = fmt.Sprintf("%s 无法使用，可能被系统或其他应用占用，仍用原来的快捷键", shortcutLabel(acc))
		log.Printf("shortcut %s: %v", acc, err)
		return
	}
	if p.set("shortcut", acc, "shortcut") != nil {
		p.s.reload() // back to the file's
	}
}

// accelerator is the shortcut for a key pressed with mods, as
// GlobalShortcut takes it: at least one of Command, Control or Option, or
// a function key.
func accelerator(mods ui.Modifiers, key ui.Key) (string, bool) {
	name, fn := keyName(key)
	if name == "" {
		return "", false
	}
	if mods&(ui.Super|ui.Ctrl|ui.Alt) == 0 && !fn {
		return "", false
	}
	var parts []string
	if mods&ui.Super != 0 {
		parts = append(parts, "Cmd")
	}
	if mods&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if mods&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	if mods&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	return strings.Join(append(parts, name), "+"), true
}

func keyName(k ui.Key) (name string, fn bool) {
	switch {
	case k >= ui.KeyA && k <= ui.KeyZ:
		return string(rune('A' + (k - ui.KeyA))), false
	case k >= ui.Key0 && k <= ui.Key9:
		return string(rune('0' + (k - ui.Key0))), false
	case k >= ui.KeyF1 && k <= ui.KeyF12:
		return "F" + strconv.Itoa(int(k-ui.KeyF1)+1), true
	}
	switch k {
	case ui.KeySpace:
		return "Space", false
	case ui.KeyTab:
		return "Tab", false
	case ui.KeyEnter:
		return "Enter", false
	case ui.KeyBackspace:
		return "Backspace", false
	case ui.KeyDelete:
		return "Delete", false
	case ui.KeyUp:
		return "Up", false
	case ui.KeyDown:
		return "Down", false
	case ui.KeyLeft:
		return "Left", false
	case ui.KeyRight:
		return "Right", false
	case ui.KeyHome:
		return "Home", false
	case ui.KeyEnd:
		return "End", false
	case ui.KeyPageUp:
		return "PageUp", false
	case ui.KeyPageDown:
		return "PageDown", false
	case ui.KeyMinus:
		return "-", false
	case ui.KeyEqual:
		return "=", false
	case ui.KeyComma:
		return ",", false
	case ui.KeyPeriod:
		return ".", false
	case ui.KeySlash:
		return "/", false
	case ui.KeySemicolon:
		return ";", false
	case ui.KeyQuote:
		return "'", false
	case ui.KeyBracketLeft:
		return "[", false
	case ui.KeyBracketRight:
		return "]", false
	case ui.KeyBackslash:
		return "\\", false
	case ui.KeyBackquote:
		return "`", false
	}
	return "", false
}

// shortcutLabel shows a shortcut as the platform writes it: ⌥⇧D on macOS.
func shortcutLabel(acc string) string {
	if acc == "" {
		return "未设置"
	}
	if !isMac {
		return acc
	}
	parts := strings.Split(acc, "+")
	if strings.HasSuffix(acc, "++") { // the plus key
		parts = append(parts[:len(parts)-2], "+")
	}
	var mods, key string
	order := map[string]int{"⌃": 0, "⌥": 1, "⇧": 2, "⌘": 3}
	var ms []string
	for i, part := range parts {
		if i == len(parts)-1 {
			key = macKey(part)
			break
		}
		switch strings.ToLower(part) {
		case "cmd", "command", "super", "meta", "cmdorctrl", "commandorcontrol":
			ms = append(ms, "⌘")
		case "ctrl", "control":
			ms = append(ms, "⌃")
		case "alt", "option":
			ms = append(ms, "⌥")
		case "shift":
			ms = append(ms, "⇧")
		}
	}
	sort.Slice(ms, func(i, j int) bool { return order[ms[i]] < order[ms[j]] })
	mods = strings.Join(ms, "")
	return mods + key
}

func macKey(k string) string {
	switch strings.ToLower(k) {
	case "space":
		return "Space"
	case "enter", "return":
		return "↩"
	case "tab":
		return "⇥"
	case "backspace":
		return "⌫"
	case "delete":
		return "⌦"
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "left":
		return "←"
	case "right":
		return "→"
	case "escape", "esc":
		return "⎋"
	}
	return strings.ToUpper(k)
}

// Services.

func (p *prefs) servicesPage(c *ui.Context) {
	cfg := p.s.config()
	pageHead(c, "services", "services", "模型服务", "连接 OpenAI 兼容的服务。Key 只保存在这台电脑上。")
	if len(p.services) == 0 {
		ui.Column(c).Key("empty").Gap(4).Children(func() {
			ui.Text(c, "还没有模型服务").TextColor(colorText)
			ui.Text(c, "添加一个服务，Gloss 的功能才能运行。").FontSize(sizeHelp).LineHeight(1.5).TextColor(colorMute)
		})
	}
	ui.Column(c).Key("services").Gap(12).Children(func() {
		for _, d := range p.services {
			p.serviceCard(c, cfg, d, true)
		}
	})
	ui.Row(c).Children(func() {
		kind := buttonSecondary
		if len(p.services) == 0 {
			kind = buttonPrimary
		}
		if button(c, buttonSpec{label: "添加服务", glyph: glyphPlus, kind: kind}).Clicked() {
			p.addService()
		}
	})
	if p.deleting != nil {
		msg := "用到它的功能会改用默认服务。"
		if p.deleting.name.saved == cfg.Routing.JevService {
			msg = "自动选择将不再用它判断。" + msg
		}
		switch confirm(c, &p.confirmDelete, fmt.Sprintf("删除服务“%s”？", p.deleting.name.saved), msg, "删除") {
		case 1:
			name := p.deleting.name.saved
			if p.update("services", func(doc *settings.Doc) error {
				settings.DeleteService(doc, name)
				return nil
			}) == nil {
				p.sync()
			}
			p.deleting = nil
		case 0:
			p.deleting = nil
		}
		if !p.confirmDelete {
			p.deleting = nil
		}
	}
	if msg := p.errs["services"]; msg != "" {
		errorText(c, msg)
	}
}

func (p *prefs) addService() {
	cfg := p.s.config()
	name := "main"
	for i := 2; ; i++ {
		if _, taken := cfg.Services[name]; !taken {
			break
		}
		name = fmt.Sprintf("service%d", i)
	}
	if p.update("services", func(d *settings.Doc) error {
		// A file may leave the default out when it has one service; the
		// new one, still empty, must not take its place. Read from the
		// file, not from cfg: a rename may have written it this frame.
		if d.Get("default_service") == nil || d.Get("default_service") == "" {
			def := name
			if ss, ok := d.Get("services").(map[string]any); ok && len(ss) == 1 {
				for n := range ss {
					def = n
				}
			}
			d.Set(def, "default_service")
		}
		d.Set(map[string]any{"base_url": settings.DefaultBaseURL, "api_key": "", "model": ""}, "services", name)
		return nil
	}) == nil {
		p.sync()
	}
}

// serviceState is what is known of a service, in a word for its line.
func serviceState(d *serviceDraft) (string, ui.Color) {
	switch {
	case d.testing:
		return "连接中", colorMute
	case d.tested != "" && d.testOK:
		return "可用", colorSuccess
	case d.tested != "":
		return "连接失败", colorDanger
	case strings.TrimSpace(d.key.saved) == "" || strings.TrimSpace(d.model.saved) == "":
		return "待填写", colorTomato
	}
	return "未测试", colorMute
}

// serviceCard is a service: one line of what it is and whether it works, with
// the test at hand, opening to edit. An incomplete one stays open: it has
// nothing else to show.
func (p *prefs) serviceCard(c *ui.Context, cfg *settings.Config, d *serviceDraft, canClose bool) {
	key := fmt.Sprintf("svc%d", d.id)
	isDefault := cfg.DefaultService == d.name.saved
	incomplete := strings.TrimSpace(d.key.saved) == "" || strings.TrimSpace(d.model.saved) == ""
	if incomplete {
		d.open = true
	}
	open := d.open || !canClose
	card(c, key).Clip().Children(func() {
		head := ui.Row(c.Key("head")).Height(60).Padding(0, 10, 0, 16).Gap(10).AlignItems(ui.Center)
		if canClose {
			head.Cursor(ui.CursorPointer)
			if head.Clicked() {
				d.open = !d.open
			}
			if head.Hovered() {
				head.Background(washCard)
			}
		}
		head.Children(func() {
			state, col := serviceState(d)
			statusDot(c, col).Margin(0, 3, 0, 0)
			ui.Column(c).Gap(2).Shrink(1).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, d.name.saved).TextColor(colorText).SingleLine().Shrink(1)
					if isDefault && canClose && len(p.services) > 1 {
						tag(c, "默认", tagMarker)
					}
				})
				line := state
				if m := strings.TrimSpace(d.model.saved); m != "" {
					line = m + " · " + state
				}
				ui.Text(c, line+serviceLatency(d)).FontSize(sizeLabel).TextColor(colorMute).SingleLine()
			})
			ui.Box(c).Grow(1)
			if d.testing {
				ui.Spinner(c).Size(14, 14).Margin(0, 6, 0, 0)
			}
			kind := buttonQuiet
			if !canClose {
				kind = buttonSecondary // in the guide it is the step's whole point
			}
			test := button(c, buttonSpec{label: "测试连接", key: key + ".test", glyph: glyphPlug, small: true, kind: kind, off: d.testing})
			if test.Clicked() {
				p.test(d)
			}
			if canClose {
				caret := ui.Box(c.Key("caret")).Size(22, 22).AlignItems(ui.Center).Justify(ui.Center).Shrink(0)
				turn := caret.AnimateTo("turn", b2f32(open)*180, ui.Animation{Duration: rotateMenu, Ease: iconEase})
				caret.Children(func() {
					ui.Box(c).Size(10, 10).Rotate(turn).Draw(func(pt *ui.Painter, r ui.Rect) {
						var path ui.Path
						path.MoveTo(r.X+r.W*0.12, r.Y+r.H*0.34).LineTo(r.X+r.W*0.5, r.Y+r.H*0.68).LineTo(r.X+r.W*0.88, r.Y+r.H*0.34)
						pt.StrokePath(&path, 1.5, colorMute)
					})
				})
			}
		})
		if d.tested != "" && !d.testOK {
			ui.Row(c.Key("failed")).Padding(0, 14, 10, 14).Children(func() { errorText(c, d.tested).Shrink(1) })
		}
		if !open {
			return
		}
		ui.Column(c.Key("body")).Padding(0, 16, 12, 16).Children(func() {
			p.fieldRow(c, key+".url", "接口地址", func() {
				p.field(c, fieldSpec{key: key + ".url", label: "接口地址", draft: &d.url, mono: true, placeholder: settings.DefaultBaseURL, commit: func(v string) error {
					return p.set(key+".url", strings.TrimSpace(v), "services", d.name.saved, "base_url")
				}})
			})
			p.fieldRow(c, key+".key", "API Key", func() {
				label, g := "显示 Key", glyphEye
				if d.showKey {
					label, g = "隐藏 Key", glyphEyeOff
				}
				p.field(c, fieldSpec{key: key + ".key", label: "API Key", draft: &d.key, mono: true, password: !d.showKey, placeholder: "粘贴 Key", commit: func(v string) error {
					return p.set(key+".key", strings.TrimSpace(v), "services", d.name.saved, "api_key")
				}, trailing: func() {
					if iconButton(c, key+".eye", g, label, false).Size(24, 24).Clicked() {
						d.showKey = !d.showKey
					}
				}})
			})
			p.fieldRow(c, key+".model", "模型", func() {
				p.field(c, fieldSpec{key: key + ".model", label: "模型", draft: &d.model, mono: true, placeholder: "如 gpt-4.1-mini", commit: func(v string) error {
					return p.set(key+".model", strings.TrimSpace(v), "services", d.name.saved, "model")
				}})
			})
			p.fieldRow(c, key+".name", "名称", func() {
				p.field(c, fieldSpec{key: key + ".name", label: "名称", draft: &d.name, placeholder: "名称", commit: func(v string) error {
					// The draft takes the new name before the file does, so that
					// the reload finds it by that name and keeps what is still
					// being typed in its other fields.
					old, name := d.name.saved, strings.TrimSpace(v)
					d.name.saved = name
					err := p.update(key+".name", func(doc *settings.Doc) error { return settings.RenameService(doc, old, name) })
					if err != nil {
						d.name.saved = old
						return err
					}
					d.name.value = name
					return nil
				}})
			})
			ui.Row(c).Gap(4).Margin(8, 0, 0, 0).AlignItems(ui.Center).Children(func() {
				if !isDefault && button(c, buttonSpec{label: "设为默认", key: key + ".default", glyph: glyphStar, small: true, kind: buttonQuiet}).Clicked() {
					_ = p.set(key+".default", d.name.saved, "default_service")
				}
				ui.Box(c).Grow(1)
				if button(c, buttonSpec{label: "删除", key: key + ".delete", glyph: glyphTrash, small: true, kind: buttonDanger}).Clicked() {
					p.deleting, p.confirmDelete = d, true
				}
			})
		})
	})
}

// serviceLatency is the last test's first word's time, for the service's line.
func serviceLatency(d *serviceDraft) string {
	if d.testOK && d.firstWord > 0 {
		return fmt.Sprintf(" · %d ms", d.firstWord.Milliseconds())
	}
	return ""
}

// test asks the service for a word with what is typed, saved or not.
func (p *prefs) test(d *serviceDraft) {
	d.testing, d.tested = true, ""
	d.testGen++
	gen := d.testGen
	client := openai.Client{
		BaseURL: or(strings.TrimSpace(d.url.value), settings.DefaultBaseURL),
		APIKey:  strings.TrimSpace(d.key.value),
		Model:   strings.TrimSpace(d.model.value),
		Idle:    15 * time.Second,
	}
	post := p.post
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		start := time.Now()
		var first time.Duration
		err := client.Stream(ctx, []openai.Message{{Role: "user", Content: "Reply with the single word OK."}}, func(string) {
			if first == 0 {
				first = time.Since(start)
				cancel()
			}
		})
		post(func() {
			if d.testGen != gen {
				return
			}
			d.testing = false
			switch {
			case first > 0:
				d.testOK, d.tested, d.firstWord = true, fmt.Sprintf("连接成功，首字用时 %d ms", first.Milliseconds()), first
			case err != nil:
				d.testOK, d.tested = false, "连接失败："+err.Error()
			default:
				d.testOK, d.tested = false, "服务没有返回内容"
			}
		})
	}()
}

// Memory and routing.

func (p *prefs) memoryPage(c *ui.Context) {
	cfg := p.s.config()
	pageHead(c, "memory", "memory", "自动选择", "记住你在哪里用什么功能，下次替你选好。不保存选中的文字。")
	p.memoryStats(c)
	p.rows(c, "auto", "", func() {
		p.row(c, "routing.enabled", "自动选择功能", "关闭后总是用排在第一的功能", func() {
			on := cfg.Routing.Enabled
			if markerSwitch(c, &on, "自动选择功能", false).Changed() {
				_ = p.set("routing.enabled", on, "routing", "enabled")
			}
		})
		p.row(c, "memory.enabled", "记住使用习惯", "只记录应用和功能，不含文字", func() {
			on := cfg.Memory.Enabled
			if markerSwitch(c, &on, "记住使用习惯", false).Changed() {
				_ = p.set("memory.enabled", on, "memory", "enabled")
			}
		})
		p.row(c, "memory.clear", "清空记忆", p.memoryCount(), func() {
			if p.memNote != "" && c.Now().Sub(p.memNoteAt) < statusFor {
				ui.Text(c, p.memNote).FontSize(sizeHelp).TextColor(colorSuccess)
				c.After(statusFor - c.Now().Sub(p.memNoteAt))
			}
			if button(c, buttonSpec{label: "清空…", small: true, kind: buttonDanger, off: p.s.store == nil}).Margin(0, -10, 0, 0).Clicked() {
				p.confirmClear = true
			}
		})
	})
	if confirm(c, &p.confirmClear, "清空所有记忆？", "自动选择会回到第一次使用时的样子。此操作不能撤销。", "清空") == 1 {
		if err := p.s.store.Clear(); err != nil {
			p.errs["memory.clear"] = "清空失败：" + err.Error()
		} else {
			delete(p.errs, "memory.clear")
			p.memNote, p.memNoteAt = "已清空", c.Now()
			p.statsAt = time.Time{}
		}
	}

	p.disclosure(c, "memory.advanced", "高级", func() {
		p.row(c, "memory.days", "保留时间", "超过的记录会被删除", func() {
			p.stepper(c, "memory.days", "保留天数", &p.memDays, 1, 3650, 1, "天", func(v float64) error {
				return p.set("memory.days", int(v), "memory", "retention_days")
			})
		})
		p.row(c, "memory.max", "最多记录", "超过时先删最早的", func() {
			p.stepper(c, "memory.max", "最多记录", &p.memMax, 100, 100000, 100, "条", func(v float64) error {
				return p.set("memory.max", int(v), "memory", "max_records")
			})
		})
		p.row(c, "routing.jev", "判断用的模型", "规则拿不准时，请它帮忙选", func() {
			none := "不使用"
			names := []string{none}
			for _, d := range p.services {
				names = append(names, d.name.saved)
			}
			cur := or(cfg.Routing.JevService, none)
			if choice(c, "routing.jev.sel", "判断用的模型", &cur, names, widthSelect).Changed() {
				v := cur
				if v == none {
					v = ""
				}
				_ = p.set("routing.jev", v, "routing", "jev_service")
			}
		})
		p.row(c, "routing.title", "发送窗口标题", "有助于判断，但可能含有文件名", func() {
			on := cfg.Routing.SendWindowTitle
			if markerSwitch(c, &on, "发送窗口标题", cfg.Routing.JevService == "").Changed() {
				_ = p.set("routing.title", on, "routing", "send_window_title")
			}
		})
		p.row(c, "routing.wait", "最长等待", "模型迟迟不回答时，就按记忆或默认功能", func() {
			p.stepper(c, "routing.wait", "最长等待", &p.waitMs, 50, 10000, 50, "毫秒", func(v float64) error {
				return p.set("routing.wait", int(v), "routing", "wait_ms")
			})
		})
		p.row(c, "routing.confidence", "最低把握", "模型的把握低于它时不采纳（0 到 1）", func() {
			p.stepper(c, "routing.confidence", "最低把握", &p.minConf, 0, 1, 0.05, "", func(v float64) error {
				return p.set("routing.confidence", v, "routing", "min_confidence")
			})
		})
	})
}

// memoryStats says what Gloss has learned in the last week, from the same
// records and by the same measure as `gloss stats`. It never holds any text:
// the records have none.
func (p *prefs) memoryStats(c *ui.Context) {
	if p.s.store == nil || !p.s.store.Enabled() {
		return
	}
	// The summary sorts every record: not a thing to do at each frame of a motion.
	if p.statsAt.IsZero() || c.Now().Sub(p.statsAt) > statusFor {
		p.stats, p.statsAt = memory.Summarize(p.s.store.Records(), c.Now().AddDate(0, 0, -7)), c.Now()
	}
	c.After(statusFor)
	st := p.stats
	if st.Uses == 0 {
		return
	}
	line := fmt.Sprintf("最近 7 天用了 %d 次，最常用的是%s", st.Uses, p.topFeature(st))
	if st.Routed > 0 {
		line += fmt.Sprintf("，%d%% 被你改选", int(st.CorrectionRate()*100+0.5))
	}
	ui.Text(c, line+"。").Key("stats").LineHeight(1.5).TextColor(colorSource)
}

func (p *prefs) topFeature(st memory.Stats) string {
	best, name := 0, "—"
	var ids []string
	for id := range st.ByFeature {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if n := st.ByFeature[id]; n > best {
			best, name = n, id
			if d, ok := p.s.descriptor(id); ok {
				name = d.Name
			}
		}
	}
	return name
}

// disclosure is a heading that opens what is below it, for what most never need.
// Each page keeps its own open.
func (p *prefs) disclosure(c *ui.Context, key, title string, fn func()) {
	open := p.disclosed[key]
	ui.Column(c).Key(key).Margin(-8, 0, 0, 0).Children(func() {
		head := ui.ButtonBase(c.Key("head")).Height(28).Gap(6).AlignItems(ui.Center).Justify(ui.Start).AlignSelf(ui.Start).Radius(radiusSmall).Padding(0, 8, 0, 0).Label(title)
		if head.Clicked() {
			p.disclosed[key] = !open
			open = !open
		}
		fg := colorMute
		if head.Hovered() {
			fg = colorText
		}
		head.TextColor(head.AnimateColor("fg", fg, hoverAnim))
		turn := head.AnimateTo("turn", b2f32(open)*90, ui.Animation{Duration: rotateMenu, Ease: iconEase})
		head.Children(func() {
			ui.Box(c).Size(10, 10).Rotate(turn).Draw(func(pt *ui.Painter, r ui.Rect) {
				var path ui.Path
				path.MoveTo(r.X+r.W*0.34, r.Y+r.H*0.14).LineTo(r.X+r.W*0.68, r.Y+r.H*0.5).LineTo(r.X+r.W*0.34, r.Y+r.H*0.86)
				pt.StrokePath(&path, 1.5, fg)
			})
			ui.Text(c, title).FontSize(sizeHelp).TextColor(fg).NoWrap()
		})
		if open {
			ui.Column(c.Key("body")).Margin(6, 0, 0, 0).Children(fn).Transition(ui.ElementTransition{Enter: &ui.Motion{Y: -3, Opacity: 0}, Duration: fadeText, Ease: iconEase})
		}
	})
}

func (p *prefs) memoryCount() string {
	if p.s.store == nil {
		return "记忆不可用"
	}
	if !p.s.store.Enabled() {
		return "记忆已关闭"
	}
	return fmt.Sprintf("现有 %d 条记录", len(p.s.store.Records()))
}

// Features.

func (p *prefs) featuresPage(c *ui.Context) {
	cfg := p.s.config()
	pageHead(c, "features", "features", "功能", "拖动调整顺序，排在第一的是默认功能。")
	ids := p.featureOrder(cfg)
	list := ordered(p.s.descs, ids)
	p.rows(c, "list", "", func() {
		for i, d := range list {
			p.featureRow(c, cfg, d, i)
		}
	})
}

// featureRow is one feature in the list. A press opens its page; a drag takes the
// whole row, and the rest make way for it. Its grip, hanging in the margin, says so,
// and takes the arrow keys. A feature that is off has no place in the order, so no
// grip.
func (p *prefs) featureRow(c *ui.Context, cfg *settings.Config, d core.Descriptor, i int) {
	on := cfg.Enabled(d.ID)
	key := "f." + d.ID
	row := ui.Box(c.Key("rowof." + d.ID)).Cursor(ui.CursorPointer).Transition(ui.ElementTransition{Animation: &ui.Animation{Spring: motionSettle}, Position: true})
	if row.Clicked() {
		p.page = pageFeature + d.ID
	}
	if on {
		row.Drag(d.ID)
	}
	if dropped, ok := ui.Drop[string](row); ok && dropped != d.ID {
		p.moveBefore("f."+dropped, cfg, dropped, d.ID) // the mark goes to the row that moved
	}
	if dragging, over := ui.DragOver[string](row); over && dragging != d.ID && on {
		// The line shows where it lands: below this row when it comes from above.
		below := indexOf(cfg.Actions, dragging) < i
		row.DrawOver(func(pt *ui.Painter, r ui.Rect) {
			y := r.Y - 1
			if below {
				y = r.Y + r.H - 1
			}
			pt.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: 2}, colorMarker, 1)
		})
	}
	lifted := row.Dragging()
	hovered := row.Hovered()
	tint := row.AnimateTo("tint", b2f32(hovered && !lifted), hoverAnim)
	// The plate takes in the grip on the left. Under the pointer it says the row
	// opens. Lifted, it lets the copy that follows the pointer (this very element)
	// read over the rows it passes, and left behind it is the slot.
	plateOf := func(r ui.Rect) ui.Rect { return ui.Rect{X: r.X - 28, Y: r.Y, W: r.W + 40, H: r.H} }
	if lifted {
		row.Opacity(0.35)
		row.Draw(func(pt *ui.Painter, r ui.Rect) {
			pt.Shadow(plateOf(r), radiusCard, 0, 8, 24, 0, colorShadow)
			pt.Fill(plateOf(r), inkRaised, radiusCard)
		})
	} else if tint > 0 {
		row.Draw(func(pt *ui.Painter, r ui.Rect) { pt.Fill(plateOf(r), washFill.Alpha(0.05*tint), radiusCard) })
	}
	row.Children(func() {
		badge := ""
		if on && i == 0 {
			badge = "默认"
		}
		p.rowOf(c, rowSpec{key: key, icon: featureIcon(d.ID), title: d.Name, badge: badge, help: describe(d), pad: paddingRow, control: func() {
			p.enableSwitch(c, cfg, d)
			if iconButton(c, key+".open", glyphNext, d.Name+"的设置", false).Margin(0, -6, 0, 0).Clicked() {
				p.page = pageFeature + d.ID
			}
		}})
		if !on {
			return
		}
		ui.Box(c).Absolute().Left(-24).Top(0).Bottom(0).Width(20).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			grip := ui.Box(c.Key("grip."+d.ID)).Size(20, 28).Radius(radiusSmall).AlignItems(ui.Center).Justify(ui.Center).
				Focusable().Label("调整" + d.Name + "的顺序").Tooltip("拖动，或用方向键调整顺序").Cursor(ui.CursorGrab)
			_ = grip.Clicked() // a press on it is not a press on the row
			show := colorMute.Alpha(0)
			switch {
			case lifted: // the copy shows the row, not the handle it was taken by
			case grip.Hovered() || grip.Focused():
				show = colorText
			case hovered:
				show = colorMute
			}
			grip.TextColor(grip.AnimateColor("fg", show, hoverAnim))
			grip.Children(func() { glyphIcon(c, glyphGrip, 14) })
			if grip.Shortcut(0, ui.KeyUp) && i > 0 {
				p.move(key, cfg, d.ID, -1)
			}
			if grip.Shortcut(0, ui.KeyDown) && i < len(cfg.Actions)-1 {
				p.move(key, cfg, d.ID, 1)
			}
		})
	})
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// describe is what a feature does, in the words of the person using it.
func describe(d core.Descriptor) string {
	return or(d.Summary, d.Purpose)
}

func (p *prefs) enableSwitch(c *ui.Context, cfg *settings.Config, d core.Descriptor) {
	on := cfg.Enabled(d.ID)
	if markerSwitch(c, &on, "启用"+d.Name, false).Changed() {
		var next []string
		for _, id := range cfg.Actions {
			if id != d.ID {
				next = append(next, id)
			}
		}
		if on {
			next = append(next, d.ID)
		}
		if len(next) == 0 {
			p.errs["f."+d.ID] = "至少要启用一个功能"
			return
		}
		_ = p.set("f."+d.ID, next, "actions")
	}
}

func (p *prefs) move(key string, cfg *settings.Config, id string, by int) {
	next := append([]string(nil), cfg.Actions...)
	for i, a := range next {
		if a == id && i+by >= 0 && i+by < len(next) {
			next[i], next[i+by] = next[i+by], next[i]
			break
		}
	}
	_ = p.set(key, next, "actions")
}

// moveBefore puts one feature where another is, for a drag.
func (p *prefs) moveBefore(key string, cfg *settings.Config, id, before string) {
	if !cfg.Enabled(id) || !cfg.Enabled(before) {
		return
	}
	from, to := -1, -1
	for i, a := range cfg.Actions {
		if a == id {
			from = i
		}
		if a == before {
			to = i
		}
	}
	if from < 0 || to < 0 {
		return
	}
	next := append([]string(nil), cfg.Actions...)
	next = append(next[:from], next[from+1:]...)
	next = append(next[:to], append([]string{id}, next[to:]...)...)
	_ = p.set(key, next, "actions")
}

func (p *prefs) featurePage(c *ui.Context, id string) {
	d, ok := p.s.descriptor(id)
	if !ok {
		p.page = pageFeatures
		return
	}
	cfg := p.s.config()
	back := button(c, buttonSpec{label: "功能", key: "back", glyph: glyphBack, small: true, kind: buttonQuiet}).AlignSelf(ui.Start).Margin(-4, 0, -16, -8)
	if back.Clicked() {
		p.page = pageFeatures
	}
	pageHead(c, "f."+id, featureIcon(id), d.Name, describe(d))
	values := cfg.Values(d)
	if len(d.Settings) > 0 {
		p.rows(c, "settings", "", func() {
			for _, st := range d.Settings {
				p.settingRow(c, cfg, d, st, values[st.Key])
			}
		})
	}
	p.disclosure(c, "f."+id+".advanced", "高级", func() {
		service, _ := cfg.ActionService(id)
		p.row(c, "f."+id+".service", "模型服务", "", func() {
			follow := "跟随默认"
			if cfg.DefaultService != "" {
				follow += "（" + cfg.DefaultService + "）"
			}
			names := []string{follow}
			for _, s := range p.services {
				names = append(names, s.name.saved)
			}
			cur := or(service, follow)
			if choice(c, "f."+id+".service.sel", "模型服务", &cur, names, widthControl).Changed() {
				key := "f." + id + ".service"
				if cur == follow {
					_ = p.update(key, func(doc *settings.Doc) error {
						doc.Delete("action_settings", id, "service")
						return nil
					})
				} else {
					_ = p.set(key, cur, "action_settings", id, "service")
				}
			}
		})
		model := cfg.Services[or(service, cfg.DefaultService)].Model
		p.row(c, "f."+id+".model", "模型", "留空时用服务的模型", func() {
			p.field(c, fieldSpec{key: "f." + id + ".model", label: "模型", draft: p.text(p.models, id), mono: true, placeholder: or(model, "模型名"), commit: func(v string) error {
				key := "f." + id + ".model"
				v = strings.TrimSpace(v)
				if v == "" {
					return p.update(key, func(doc *settings.Doc) error {
						doc.Delete("action_settings", id, "model")
						return nil
					})
				}
				return p.set(key, v, "action_settings", id, "model")
			}})
		})
	})
}

// settingRow is a control for what a feature declares, by its type.
func (p *prefs) settingRow(c *ui.Context, cfg *settings.Config, d core.Descriptor, st core.Setting, value string) {
	key := "f." + d.ID + "." + st.Key
	path := []string{"action_settings", d.ID, st.Key}
	commit := func(v string) error {
		v = strings.TrimSpace(v)
		if v == "" && st.Type != core.SettingChoice && st.Type != core.SettingBool {
			return p.update(key, func(doc *settings.Doc) error {
				doc.Delete(path...)
				return nil
			})
		}
		canon, err := st.Check(v)
		if err != nil {
			p.errs[key] = err.Error()
			return err
		}
		var out any = canon
		switch st.Type {
		case core.SettingNumber:
			out, _ = strconv.ParseFloat(canon, 64)
		case core.SettingBool:
			out = canon == "true"
		}
		return p.set(key, out, path...)
	}
	p.row(c, key, or(st.Label, st.Key), st.Help, func() {
		switch {
		case st.Type == core.SettingBool:
			on := value == "true"
			if markerSwitch(c, &on, or(st.Label, st.Key), false).Changed() {
				_ = commit(strconv.FormatBool(on))
			}
		case st.Type == core.SettingChoice && len(st.Options) <= 3:
			idx := 0
			for i, o := range st.Options {
				if o == value {
					idx = i
				}
			}
			if segmented(c, key+".seg", or(st.Label, st.Key), &idx, st.Options).Changed() {
				_ = commit(st.Options[idx])
			}
		case st.Type == core.SettingChoice:
			cur := value
			if choice(c, key+".sel", or(st.Label, st.Key), &cur, st.Options, widthSelect).Changed() {
				_ = commit(cur)
			}
		case d.ID == "translate" && st.Key == "target_lang":
			// The language of a translation is a language, here as in General.
			follow := "跟随通用设置（" + languageName(cfg.TargetLang) + "）"
			names := append([]string{follow}, languageNames(value)...)
			cur := follow
			if value != "" {
				cur = languageName(value)
			}
			if choice(c, key+".sel", or(st.Label, st.Key), &cur, names, widthControl).Changed() {
				if cur == follow {
					_ = commit("")
				} else {
					_ = commit(languageCode(cur))
				}
			}
		default:
			p.field(c, fieldSpec{key: key, label: or(st.Label, st.Key), draft: p.text(p.texts, d.ID+"."+st.Key), placeholder: st.Default, commit: commit})
		}
	})
}

// The first run.

// ready says Gloss can run: it has the Accessibility permission and a
// service to run the default feature on.
func (p *prefs) ready(cfg *settings.Config) bool {
	return p.trusted && hasService(cfg)
}

func hasService(cfg *settings.Config) bool {
	if len(cfg.Actions) == 0 {
		return false
	}
	_, err := cfg.Service(cfg.Actions[0])
	return err == nil
}

// askTrust checks the permission again, every second while the guide shows
// it missing, so that the guide moves on as soon as it is given.
func (p *prefs) askTrust(c *ui.Context) {
	if p.checkTrust == nil || p.asking {
		return
	}
	if !p.trustAsked.IsZero() && c.Now().Sub(p.trustAsked) < time.Second {
		c.After(time.Second - c.Now().Sub(p.trustAsked))
		return
	}
	p.asking, p.trustAsked = true, c.Now()
	check, post := p.checkTrust, p.post
	go func() {
		ok := check()
		post(func() { p.trusted, p.asking = ok, false })
	}()
}

func (p *prefs) welcome(c *ui.Context) {
	cfg := p.s.config()
	pageHead(c, "welcome", "welcome", "欢迎使用 "+Name, "选中文字，按下快捷键，就能得到翻译或解释。")
	var svc *serviceDraft
	for _, d := range p.services {
		if d.name.saved == cfg.DefaultService {
			svc = d
		}
	}
	serviceOK := svc != nil && svc.testOK && hasService(cfg)

	ui.Column(c).Key("steps").Children(func() {
		trust := "已允许"
		step(c, "s1", 1, "允许辅助功能", p.trusted, trust, false, func() {
			ui.Text(c, "Gloss 只用它读取和替换你选中的文字。").FontSize(sizeHelp).LineHeight(1.5).TextColor(colorMute)
			ui.Row(c).Margin(6, 0, 0, 0).Children(func() {
				if button(c, buttonSpec{label: "打开系统设置", kind: buttonPrimary}).Clicked() {
					p.s.openAccessibility()
				}
			})
		})
		summary := ""
		if svc != nil {
			summary = svc.name.saved + serviceLatency(svc)
		}
		step(c, "s2", 2, "连接模型服务", serviceOK, summary, false, func() {
			ui.Text(c, "填入接口地址、Key 和模型名，然后测试连接。").FontSize(sizeHelp).LineHeight(1.5).TextColor(colorMute)
			if svc == nil {
				ui.Row(c).Margin(6, 0, 0, 0).Children(func() {
					if button(c, buttonSpec{label: "添加服务", glyph: glyphPlus, kind: buttonPrimary}).Clicked() {
						p.addService()
					}
				})
				return
			}
			ui.Box(c).Margin(6, 0, 0, 0).Children(func() { p.serviceCard(c, cfg, svc, false) })
		})
		step(c, "s3", 3, "试一试", false, "", true, func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Wrap().Margin(2, 0, 4, 0).Children(func() {
				ui.Text(c, "在任意应用里选中一段外文，按下").FontSize(sizeHelp).TextColor(colorSource)
				ui.Row(c).Gap(3).Children(func() { keycaps(c, shortcutLabel(cfg.Shortcut), false, false, false, 20) })
			})
			ready := p.trusted && serviceOK
			why := "以后从菜单栏的 Gloss 图标打开设置"
			switch {
			case !p.trusted && !serviceOK:
				why = "完成前两步后才能开始"
			case !p.trusted:
				why = "还需要允许辅助功能"
			case !serviceOK:
				why = "还需要测试连接成功"
			}
			ui.Row(c).Gap(12).AlignItems(ui.Center).Margin(10, 0, 0, 0).Children(func() {
				if button(c, buttonSpec{label: "完成", kind: buttonPrimary, off: !ready}).Clicked() {
					p.page = pageGeneral
					if p.win != nil {
						p.win.Close()
					}
				}
				if why != "" {
					ui.Text(c, why).FontSize(sizeHelp).TextColor(colorMute)
				}
			})
		})
	})
}

// step is one step of the guide, on a thread down the page: a numbered node that
// a finished step fills with the marker, its body folding to one line of summary.
func step(c *ui.Context, key string, n int, title string, done bool, summary string, last bool, body func()) {
	row := ui.Row(c).Key(key).Gap(14).AlignItems(ui.Stretch)
	row.Children(func() {
		ui.Column(c).Width(24).AlignItems(ui.Center).Shrink(0).Children(func() {
			mark := ui.Box(c.Key("mark")).Size(24, 24).Radius(12).AlignItems(ui.Center).Justify(ui.Center).Shrink(0)
			bg := mark.AnimateColor("bg", map[bool]ui.Color{true: colorMarker, false: colorControl}[done], ui.Animation{Duration: swipeTime, Ease: ui.EaseOut})
			mark.Background(bg)
			mark.Children(func() {
				if done {
					glyphIcon(c, glyphCheck, 14).TextColor(colorOnMarker).Transition(ui.ElementTransition{Enter: &ui.Motion{Scale: 0.5, Opacity: 0}, Animation: &ui.Animation{Spring: motionKnob}})
				} else {
					ui.Text(c, strconv.Itoa(n)).Font(fontSource).FontSize(sizeHelp).TextColor(colorText)
				}
			})
			if !last {
				ui.Box(c).Width(1).Grow(1).Background(colorRule).Margin(4, 0)
			}
		})
		col := ui.Column(c).Grow(1).Shrink(1).Gap(6)
		if !last {
			col.Padding(2, 0, 28, 0)
		} else {
			col.Padding(2, 0, 0, 0)
		}
		col.Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontWeight(600).TextColor(map[bool]ui.Color{true: colorSource, false: colorText}[done])
				if done && summary != "" {
					ui.Text(c, summary).FontSize(sizeHelp).TextColor(colorSuccess)
				}
			})
			if !done {
				body()
			}
		})
	})
}
