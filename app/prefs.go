package app

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"gloss/core"
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

	testing bool
	testGen int
	tested  string
	testOK  bool
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
		Width:         780,
		Height:        580,
		MinWidth:      660,
		MinHeight:     460,
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
	p := &prefs{s: s, page: pageGeneral, sectionOpen: true, models: map[string]*textDraft{}, texts: map[string]*textDraft{}, errs: map[string]string{}}
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

func prefsTheme(base *ui.Theme) *ui.Theme {
	t := *base
	t.Dark = true
	t.Background = colorSurface
	t.Surface = ui.Hex("#232328")
	t.SurfaceHover = ui.Hex("#2B2B31")
	t.SurfacePressed = ui.Hex("#323239")
	t.Border = ui.RGBA(255, 255, 255, 0.1)
	t.Text = colorText
	t.TextMuted = colorHint
	t.Accent = colorAccent
	t.AccentHover = ui.Hex("#8AC7FF")
	t.AccentPressed = ui.Hex("#55A6F0")
	t.AccentText = ui.Hex("#0B1622")
	t.Danger = colorDanger
	t.Success = colorSuccess
	t.Selection = colorAccent.Alpha(0.3)
	t.Focus = colorAccent.Alpha(0.55)
	t.Scrollbar = ui.RGBA(255, 255, 255, 0.25)
	t.Inverse, t.InverseText = ui.Hex("#2E2E34"), colorText
	t.Radius = radiusControl - 1
	t.FontSize = 13
	t.Font = fontText
	return &t
}

var (
	colorSidebar = ui.Hex("#121215")
	colorGroup   = ui.RGBA(255, 255, 255, 0.035)
)

const (
	widthSidebar float32 = 200
	widthContent float32 = 580
	widthControl float32 = 260
)

func (p *prefs) view(c *ui.Context) {
	c.SetTheme(prefsTheme(c.Theme()))
	p.leavePage()
	if !p.trusted {
		p.askTrust(c)
	}
	bar := c.TitleBar()
	top := max(bar.Height, 28)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(widthSidebar).Background(colorSidebar).BorderWidth(0, 1, 0, 0).BorderColor(colorRim).Children(func() {
			ui.Box(c).Height(top + 8).DragWindow()
			p.sidebar(c)
			p.leavePage()
		})
		ui.Column(c).Grow(1).Children(func() {
			ui.Box(c).Height(top).DragWindow()
			ui.Scroll(c).Key(p.page).Grow(1).Children(func() {
				ui.Column(c).Padding(4, 36, 36, 36).Gap(20).MaxWidth(widthContent + 72).Children(func() {
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

func (p *prefs) sidebar(c *ui.Context) {
	cfg := p.s.config()
	ui.Sidebar(c, &p.page, func() {
		if p.page == pageWelcome || !p.ready(cfg) {
			ui.SidebarItem(c, pageWelcome, nil, "开始使用")
		}
		ui.SidebarItem(c, pageGeneral, nil, "通用")
		ui.SidebarItem(c, pageServices, nil, "模型服务").Children(func() {
			if len(cfg.Services) == 0 || cfg.DefaultService == "" {
				ui.Badge(c, "待填写")
			}
		})
		ui.SidebarItem(c, pageMemory, nil, "记忆与路由")
		ui.SidebarSection(c, "功能", &p.sectionOpen, func() {
			ui.SidebarItem(c, pageFeatures, nil, "全部功能")
			for _, d := range ordered(p.s.descs, p.featureOrder(cfg)) {
				ui.SidebarItem(c, pageFeature+d.ID, nil, d.Name)
			}
		})
	}).Width(widthSidebar).Grow(1).Background(ui.Transparent)
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

// banner shows a config file Gloss cannot use, which no control here can
// change until it is fixed by hand.
func (p *prefs) banner(c *ui.Context) {
	p.s.cfgMu.Lock()
	err := p.s.cfgErr
	p.s.cfgMu.Unlock()
	if err == nil || strings.Contains(err.Error(), settings.ErrMissing.Error()) {
		return
	}
	ui.Column(c).Key("banner").Padding(12, 14).Gap(8).Radius(10).Background(colorDanger.Alpha(0.12)).Border(1, colorDanger.Alpha(0.3)).Children(func() {
		ui.Text(c, "配置文件有误，Gloss 仍在用上一次正确的设置").FontWeight(600).TextColor(colorText)
		ui.Text(c, p.brief(err)).TextColor(colorSource).Selectable()
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "打开配置文件").Clicked() {
				p.s.openConfig()
			}
		})
	})
}

func pageTitle(c *ui.Context, title, sub string) {
	ui.Column(c).Gap(4).Padding(0, 0, 4, 0).Children(func() {
		ui.Text(c, title).FontSize(20).FontWeight(600).TextColor(colorText)
		if sub != "" {
			ui.Text(c, sub).TextColor(colorHint)
		}
	})
}

// group is a card of rows, as macOS's settings are laid out.
func group(c *ui.Context, key, title string, rows func()) {
	ui.Column(c).Key(key).Gap(8).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(colorHint).Padding(0, 4)
		}
		ui.Column(c).Radius(10).Background(colorGroup).Border(1, colorRim).Children(rows)
	})
}

// row is one setting: its name and help on the left, its control on the
// right, and below them what went wrong with it.
func (p *prefs) row(c *ui.Context, key, title, help string, control func()) {
	ui.Column(c).Key(key).Padding(10, 14).Gap(6).BorderWidth(0, 0, 1, 0).BorderColor(ui.RGBA(255, 255, 255, 0.05)).Children(func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
				ui.Text(c, title).TextColor(colorText)
				if help != "" {
					ui.Text(c, help).FontSize(12).TextColor(colorHint)
				}
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(control)
		})
		if msg := p.errs[key]; msg != "" {
			ui.Text(c, msg).FontSize(12).TextColor(colorDanger).Selectable()
		}
	})
}

// textInput is a field that writes after the typing stops, or on Enter.
func (p *prefs) textInput(c *ui.Context, key, label string, d *textDraft, placeholder string, commit func(string) error) ui.Element {
	d.commit = commit
	in := ui.TextInput(c.Key(key), &d.value).Placeholder(placeholder).Label(label).Width(widthControl)
	if p.errs[key] != "" {
		in.Error(p.errs[key])
	}
	if in.Changed() {
		d.editedAt = c.Now()
	}
	if in.Submitted() && !d.editedAt.IsZero() {
		d.editedAt = c.Now().Add(-commitAfter)
	}
	if !d.editedAt.IsZero() {
		if left := commitAfter - c.Now().Sub(d.editedAt); left > 0 {
			c.After(left)
		} else {
			d.editedAt = time.Time{}
			if d.value != d.saved && commit(d.value) == nil {
				d.saved = d.value
			}
		}
	}
	return in
}

func (p *prefs) numberInput(c *ui.Context, key, label string, d *numberDraft, lo, hi, step float64, commit func(float64) error) {
	d.commit = commit
	in := ui.NumberInput(c.Key(key), &d.value, lo, hi, step).Label(label).Width(120)
	if in.Changed() {
		d.editedAt = c.Now()
	}
	if !d.editedAt.IsZero() {
		if left := commitAfter - c.Now().Sub(d.editedAt); left > 0 {
			c.After(left)
		} else {
			d.editedAt = time.Time{}
			if d.value != d.saved && commit(d.value) == nil {
				d.saved = d.value
			}
		}
	}
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
	pageTitle(c, "通用", "")
	group(c, "keys", "", func() {
		help := "在任何应用里选中文字后按下它"
		if p.s.shortcut == "" && !p.recording {
			help = "当前快捷键没能注册，可能被系统或其他应用占用，请换一个"
		}
		p.row(c, "shortcut", "快捷键", help, func() { p.shortcutField(c, cfg.Shortcut) })
		p.row(c, "target_lang", "目标语言", "翻译成哪种语言，也是其他功能回答用的语言", func() {
			names := make([]string, 0, len(languages)+1)
			for _, l := range languages {
				names = append(names, l.name)
			}
			cur := languageName(cfg.TargetLang)
			if cur == cfg.TargetLang {
				names = append(names, cur)
			}
			if ui.Select(c, &cur, names).Width(180).Label("目标语言").Changed() {
				code := cur
				for _, l := range languages {
					if l.name == cur {
						code = l.code
					}
				}
				_ = p.set("target_lang", code, "target_lang")
			}
		})
	})
	group(c, "system", "", func() {
		p.row(c, "launch_at_login", "登录时启动", "需要从打包的 Gloss.app 运行", func() {
			on := cfg.LaunchAtLogin
			if ui.Switch(c, &on).Label("登录时启动").Changed() {
				_ = p.set("launch_at_login", on, "launch_at_login")
			}
		})
	})
	group(c, "about", "", func() {
		p.row(c, "version", Name, "版本 "+Version, func() {})
		p.row(c, "file", "配置文件", p.s.cfgPath, func() {
			if ui.Button(c, "在编辑器中打开").Clicked() {
				p.s.openConfig()
			}
		})
		if p.s.logFile != "" {
			p.row(c, "log", "诊断日志", "只记录事件、耗时和错误类别，不含选中的文字、结果和 Key", func() {
				if ui.Button(c, "显示日志").Clicked() {
					reveal(p.s.logFile)
				}
			})
		}
	})
}

// The shortcut recorder.

func (p *prefs) shortcutField(c *ui.Context, current string) {
	t := c.Theme()
	b := ui.Box(c.Key("recorder")).Focusable().Height(28).MinWidth(150).Padding(0, 12).Radius(t.Radius).
		Background(t.Surface).AlignItems(ui.Center).Justify(ui.Center).Cursor(ui.CursorPointer).Label("快捷键")
	border := t.Border
	if p.recording {
		border = colorAccent
	} else if b.Hovered() {
		b.Background(t.SurfaceHover)
	}
	b.Border(1, border)
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
	b.Children(func() {
		if p.recording {
			ui.Text(c, "按下新的快捷键…").TextColor(colorAccent).NoWrap()
			return
		}
		ui.Text(c, shortcutLabel(current)).TextColor(colorText).NoWrap()
	})
	if b.Clicked() && !p.recording {
		p.recording = true
		delete(p.errs, "shortcut")
		p.s.pauseShortcut()
		b.Focus()
	}
	if p.recording {
		ui.Text(c, "Esc 取消").FontSize(12).TextColor(colorHint).NoWrap()
	} else if current != settings.DefaultShortcut {
		if ui.Button(c, "恢复默认").Clicked() {
			p.s.pauseShortcut()
			p.setShortcut(settings.DefaultShortcut)
		}
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
	pageTitle(c, "模型服务", "OpenAI 兼容的接口。Key 只保存在本机的配置文件里，权限仅限当前用户。")
	if len(p.services) == 0 {
		ui.Column(c).Key("empty").Padding(18).Gap(6).Radius(10).Background(colorGroup).Border(1, colorRim).Children(func() {
			ui.Text(c, "还没有模型服务").TextColor(colorText)
			ui.Text(c, "添加一个服务，填入接口地址、Key 和模型名，Gloss 的功能才能运行。").FontSize(12).TextColor(colorHint)
		})
	}
	for _, d := range p.services {
		p.serviceCard(c, cfg, d)
	}
	ui.Row(c).Children(func() {
		if ui.Button(c, "添加服务").Clicked() {
			p.addService()
		}
	})
	if p.deleting != nil {
		msg := "用到它的功能会改用默认服务。"
		if p.deleting.name.saved == cfg.Routing.JevService {
			msg = "自动选择功能将不再使用 Jev。" + msg
		}
		switch ui.AlertDialog(c, &p.confirmDelete, fmt.Sprintf("删除服务“%s”？", p.deleting.name.saved), msg, "取消", "删除") {
		case 1:
			name := p.deleting.name.saved
			if p.update("services", func(doc *settings.Doc) error {
				settings.DeleteService(doc, name)
				return nil
			}) == nil {
				p.sync()
			}
			p.deleting = nil
		case 0, ui.AlertDismissed:
			p.deleting = nil
		}
	}
	if msg := p.errs["services"]; msg != "" {
		ui.Text(c, msg).FontSize(12).TextColor(colorDanger)
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

func (p *prefs) serviceCard(c *ui.Context, cfg *settings.Config, d *serviceDraft) {
	key := fmt.Sprintf("svc%d", d.id)
	isDefault := cfg.DefaultService == d.name.saved
	ui.Column(c).Key(key).Radius(10).Background(colorGroup).Border(1, colorRim).Children(func() {
		ui.Row(c).Padding(12, 14).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(ui.RGBA(255, 255, 255, 0.05)).Children(func() {
			ui.Text(c, d.name.saved).FontWeight(600).TextColor(colorText)
			if isDefault {
				ui.Badge(c, "默认")
			}
			if d.name.saved == cfg.Routing.JevService {
				ui.Badge(c, "Jev")
			}
			ui.Box(c).Grow(1)
			if !isDefault && ui.Button(c, "设为默认").Clicked() {
				_ = p.set(key+".default", d.name.saved, "default_service")
			}
			if ui.Button(c, "删除").Clicked() {
				p.deleting, p.confirmDelete = d, true
			}
		})
		p.row(c, key+".name", "名称", "功能和路由用这个名字指向它", func() {
			p.textInput(c, key+".name", "名称", &d.name, "名称", func(v string) error {
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
			})
		})
		p.row(c, key+".url", "接口地址", "到版本号为止，通常以 /v1 结尾", func() {
			p.textInput(c, key+".url", "接口地址", &d.url, settings.DefaultBaseURL, func(v string) error {
				return p.set(key+".url", strings.TrimSpace(v), "services", d.name.saved, "base_url")
			})
		})
		p.row(c, key+".key", "API Key", "", func() {
			in := p.textInput(c, key+".key", "API Key", &d.key, "粘贴 Key", func(v string) error {
				return p.set(key+".key", strings.TrimSpace(v), "services", d.name.saved, "api_key")
			}).Width(widthControl - 64)
			if !d.showKey {
				in.Password()
			}
			label := "显示"
			if d.showKey {
				label = "隐藏"
			}
			if ui.Button(c, label).Clicked() {
				d.showKey = !d.showKey
			}
		})
		p.row(c, key+".model", "模型", "如 gpt-4.1-mini", func() {
			p.textInput(c, key+".model", "模型", &d.model, "模型名", func(v string) error {
				return p.set(key+".model", strings.TrimSpace(v), "services", d.name.saved, "model")
			})
		})
		ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).Children(func() {
			b := ui.Button(c, "测试连接").Disabled(d.testing)
			if b.Clicked() {
				p.test(d)
			}
			switch {
			case d.testing:
				ui.Spinner(c).Size(14, 14)
				ui.Text(c, "正在连接…").FontSize(12).TextColor(colorHint)
			case d.tested != "":
				col := colorDanger
				if d.testOK {
					col = colorSuccess
				}
				ui.Text(c, d.tested).FontSize(12).TextColor(col).Shrink(1).Selectable()
			}
		})
	})
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
				d.testOK, d.tested = true, fmt.Sprintf("连接成功，首字用时 %d ms", first.Milliseconds())
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
	pageTitle(c, "记忆与路由", "")
	group(c, "memory", "记忆", func() {
		p.row(c, "memory.enabled", "记住使用习惯", "只记录来源应用、功能和是否改选，不保存选中的文字和结果", func() {
			on := cfg.Memory.Enabled
			if ui.Switch(c, &on).Label("记住使用习惯").Changed() {
				_ = p.set("memory.enabled", on, "memory", "enabled")
			}
		})
		p.row(c, "memory.days", "保留天数", "超过的记录会被删除", func() {
			p.numberInput(c, "memory.days", "保留天数", &p.memDays, 1, 3650, 1, func(v float64) error {
				return p.set("memory.days", int(v), "memory", "retention_days")
			})
		})
		p.row(c, "memory.max", "最多记录", "超过时先删最早的", func() {
			p.numberInput(c, "memory.max", "最多记录", &p.memMax, 100, 100000, 100, func(v float64) error {
				return p.set("memory.max", int(v), "memory", "max_records")
			})
		})
		p.row(c, "memory.clear", "清空记忆", p.memoryCount(), func() {
			if p.memNote != "" && c.Now().Sub(p.memNoteAt) < statusFor {
				ui.Text(c, p.memNote).FontSize(12).TextColor(colorSuccess)
				c.After(statusFor - c.Now().Sub(p.memNoteAt))
			}
			if ui.Button(c, "清空…").Disabled(p.s.store == nil).Clicked() {
				p.confirmClear = true
			}
		})
	})
	switch ui.AlertDialog(c, &p.confirmClear, "清空所有记忆？", "自动选择会像第一次使用时一样，只靠规则和 Jev。此操作不能撤销。", "取消", "清空") {
	case 1:
		if err := p.s.store.Clear(); err != nil {
			p.errs["memory.clear"] = "清空失败：" + err.Error()
		} else {
			delete(p.errs, "memory.clear")
			p.memNote, p.memNoteAt = "已清空", c.Now()
		}
	}

	group(c, "routing", "自动选择功能", func() {
		p.row(c, "routing.enabled", "自动选择", "按来源应用、选中内容和记忆选一个功能；关闭时总用排在第一的功能", func() {
			on := cfg.Routing.Enabled
			if ui.Switch(c, &on).Label("自动选择").Changed() {
				_ = p.set("routing.enabled", on, "routing", "enabled")
			}
		})
		p.row(c, "routing.jev", "Jev 服务", "规则判断不了时请 Jev 决定；不使用时只靠规则和记忆", func() {
			none := "不使用"
			names := []string{none}
			for _, d := range p.services {
				names = append(names, d.name.saved)
			}
			cur := or(cfg.Routing.JevService, none)
			if ui.Select(c, &cur, names).Width(180).Label("Jev 服务").Changed() {
				v := cur
				if v == none {
					v = ""
				}
				_ = p.set("routing.jev", v, "routing", "jev_service")
			}
		})
		p.row(c, "routing.title", "发送窗口标题", "窗口标题有助于判断，但可能含有文件名等信息", func() {
			on := cfg.Routing.SendWindowTitle
			if ui.Switch(c, &on).Label("发送窗口标题").Disabled(cfg.Routing.JevService == "").Changed() {
				_ = p.set("routing.title", on, "routing", "send_window_title")
			}
		})
		p.row(c, "routing.wait", "最长等待", "Jev 超过这么久没有回答，就按记忆或默认功能（毫秒）", func() {
			p.numberInput(c, "routing.wait", "最长等待", &p.waitMs, 50, 10000, 50, func(v float64) error {
				return p.set("routing.wait", int(v), "routing", "wait_ms")
			})
		})
		p.row(c, "routing.confidence", "最低把握", "Jev 的把握低于它时不采纳（0 到 1）", func() {
			p.numberInput(c, "routing.confidence", "最低把握", &p.minConf, 0, 1, 0.05, func(v float64) error {
				return p.set("routing.confidence", v, "routing", "min_confidence")
			})
		})
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
	pageTitle(c, "全部功能", "排在第一的启用功能是默认功能：关闭自动选择，或自动选择拿不准时，就用它。")
	ids := p.featureOrder(cfg)
	group(c, "list", "", func() {
		for i, d := range ordered(p.s.descs, ids) {
			on := cfg.Enabled(d.ID)
			p.row(c, "f."+d.ID, d.Name, d.Purpose, func() {
				if on && i == 0 {
					ui.Badge(c, "默认")
				}
				if on {
					if ui.Button(c, "↑").Label("上移").Tooltip("上移").Disabled(i == 0).Clicked() {
						p.move("f."+d.ID, cfg, d.ID, -1)
					}
					if ui.Button(c, "↓").Label("下移").Tooltip("下移").Disabled(i >= len(cfg.Actions)-1).Clicked() {
						p.move("f."+d.ID, cfg, d.ID, 1)
					}
				}
				p.enableSwitch(c, cfg, d)
			})
		}
	})
}

func (p *prefs) enableSwitch(c *ui.Context, cfg *settings.Config, d core.Descriptor) {
	on := cfg.Enabled(d.ID)
	if ui.Switch(c, &on).Label("启用" + d.Name).Changed() {
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

func (p *prefs) featurePage(c *ui.Context, id string) {
	d, ok := p.s.descriptor(id)
	if !ok {
		p.page = pageFeatures
		return
	}
	cfg := p.s.config()
	pageTitle(c, d.Name, d.Purpose)
	group(c, "use", "", func() {
		help := "关闭后不会被自动选中，也不出现在面板的功能栏里"
		if len(cfg.Actions) > 0 && cfg.Actions[0] == id {
			help = "这是默认功能"
		}
		p.row(c, "f."+id, "启用", help, func() { p.enableSwitch(c, cfg, d) })
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
			if ui.Select(c, &cur, names).Width(widthControl).Label("模型服务").Changed() {
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
			p.textInput(c, "f."+id+".model", "模型", p.text(p.models, id), or(model, "模型名"), func(v string) error {
				key := "f." + id + ".model"
				v = strings.TrimSpace(v)
				if v == "" {
					return p.update(key, func(doc *settings.Doc) error {
						doc.Delete("action_settings", id, "model")
						return nil
					})
				}
				return p.set(key, v, "action_settings", id, "model")
			})
		})
	})
	if len(d.Settings) == 0 {
		return
	}
	values := cfg.Values(d)
	group(c, "settings", "这个功能的设置", func() {
		for _, st := range d.Settings {
			p.settingRow(c, cfg, d, st, values[st.Key])
		}
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
		switch st.Type {
		case core.SettingBool:
			on := value == "true"
			if ui.Switch(c, &on).Label(or(st.Label, st.Key)).Changed() {
				_ = commit(strconv.FormatBool(on))
			}
		case core.SettingChoice:
			if len(st.Options) <= 3 {
				idx := 0
				for i, o := range st.Options {
					if o == value {
						idx = i
					}
				}
				if ui.Segmented(c, &idx, st.Options...).Label(or(st.Label, st.Key)).Changed() {
					_ = commit(st.Options[idx])
				}
			} else {
				cur := value
				if ui.Select(c, &cur, st.Options).Width(180).Label(or(st.Label, st.Key)).Changed() {
					_ = commit(cur)
				}
			}
		default:
			placeholder := st.Default
			if d.ID == "translate" && st.Key == "target_lang" {
				placeholder = "跟随通用设置（" + languageName(cfg.TargetLang) + "）"
			}
			p.textInput(c, key, or(st.Label, st.Key), p.text(p.texts, d.ID+"."+st.Key), placeholder, commit)
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
	pageTitle(c, "欢迎使用 "+Name, "选中文字，按下快捷键，就能得到翻译或解释。先花一分钟完成下面三步。")
	var svc *serviceDraft
	for _, d := range p.services {
		if d.name.saved == cfg.DefaultService {
			svc = d
		}
	}
	serviceOK := svc != nil && svc.testOK && hasService(cfg)

	step(c, "s1", 1, "允许辅助功能", p.trusted, func() {
		ui.Text(c, "Gloss 用它读取你选中的文字，在你要求时把结果替换回去。它不会读取屏幕上的其他内容。").FontSize(12).TextColor(colorHint)
		if p.trusted {
			ui.Text(c, "已允许").FontSize(12).TextColor(colorSuccess)
			return
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			if ui.PrimaryButton(c, "打开系统设置").Clicked() {
				p.s.openAccessibility()
			}
			ui.Text(c, "在列表里打开 Gloss 的开关，这里会自动继续").FontSize(12).TextColor(colorHint).Shrink(1)
		})
	})
	step(c, "s2", 2, "连接模型服务", serviceOK, func() {
		ui.Text(c, "填入 OpenAI 兼容接口的地址、Key 和模型名，测试连接成功后就完成了。").FontSize(12).TextColor(colorHint)
		if svc == nil {
			ui.Row(c).Children(func() {
				if ui.PrimaryButton(c, "添加服务").Clicked() {
					p.addService()
				}
			})
			return
		}
		p.serviceCard(c, cfg, svc)
	})
	step(c, "s3", 3, "试一试", false, func() {
		ui.Text(c, "在任意应用里选中一段外文，按下 "+shortcutLabel(cfg.Shortcut)+"。结果出现在选区旁边，按 Esc 关闭。").FontSize(12).TextColor(colorHint)
		ui.Text(c, "以后可以从菜单栏的 Gloss 图标打开设置。").FontSize(12).TextColor(colorHint)
		ui.Row(c).Children(func() {
			b := ui.PrimaryButton(c, "完成").Disabled(!p.trusted || !serviceOK)
			if b.Clicked() {
				p.page = pageGeneral
				if p.win != nil {
					p.win.Close()
				}
			}
		})
	})
}

// step is one numbered step of the guide, checked when it is done.
func step(c *ui.Context, key string, n int, title string, done bool, body func()) {
	ui.Row(c).Key(key).Gap(14).Padding(16).AlignItems(ui.Start).Radius(10).Background(colorGroup).Border(1, colorRim).Children(func() {
		mark := ui.Box(c).Width(24).Height(24).Radius(12).AlignItems(ui.Center).Justify(ui.Center)
		if done {
			mark.Background(colorSuccess.Alpha(0.18)).Children(func() {
				ui.Text(c, "✓").FontSize(13).TextColor(colorSuccess)
			})
		} else {
			mark.Background(colorControl).Children(func() {
				ui.Text(c, strconv.Itoa(n)).FontSize(12).TextColor(colorText)
			})
		}
		ui.Column(c).Grow(1).Shrink(1).Gap(8).Children(func() {
			ui.Text(c, title).FontWeight(600).TextColor(colorText)
			body()
		})
	})
}
