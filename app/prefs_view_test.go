package app

import (
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"gloss/memory"
	"gloss/providers/openai"
	"gloss/providers/prompt"
	"gloss/settings"
)

// testPrefs is the settings view on a config file of its own, in a tester.
func testPrefs(t *testing.T, content string) (*prefs, *ui.Tester, string) {
	t.Helper()
	specs, err := prompt.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	s := &shell{keys: &fakeKeys{taken: map[string]bool{"Cmd+Space": true}, held: map[string]bool{}}}
	for _, spec := range specs {
		s.descs = append(s.descs, spec.Descriptor)
	}
	s.cfgPath = filepath.Join(t.TempDir(), settings.File)
	if content != "" {
		if err := os.WriteFile(s.cfgPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s.loadConfig()
	s.registerShortcut(s.config().Shortcut)
	p := newPrefs(s, pageServices)
	s.prefs = p // as openPrefs does: a reload syncs the window
	var mu sync.Mutex
	var pending []func()
	p.post = func(fn func()) {
		mu.Lock()
		pending = append(pending, fn)
		mu.Unlock()
	}
	tt := ui.NewTester(func(c *ui.Context) {
		mu.Lock()
		run := pending
		pending = nil
		mu.Unlock()
		for _, fn := range run {
			fn()
		}
		p.view(c)
	}, 780, 900)
	return p, tt, s.cfgPath
}

func fileJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func get(m map[string]any, keys ...string) any {
	var v any = m
	for _, k := range keys {
		mm, _ := v.(map[string]any)
		v = mm[k]
	}
	return v
}

func typeInto(t *testing.T, tt *ui.Tester, label, text string) {
	t.Helper()
	typeOnly(t, tt, label, text)
	tt.Advance(time.Second)
}

// contentWidth is a page's column in the tests' window of 780.
const contentWidth = 780 - widthSidebar - marginSheet - 2*paddingPage

// typeOnly types into a field and leaves it waiting to write.
func typeOnly(t *testing.T, tt *ui.Tester, label, text string) {
	t.Helper()
	// The row's title comes first; its input is level with it on the right.
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("no %s in %q", label, tt.Texts())
	}
	// Inside a card the fields stop at the card's padding; the point is on both.
	// Its height is from the title's middle: level with the field without a help
	// line, above its middle with one or two.
	tt.ClickAt(r.X+contentWidth-14-widthControl/2, r.Y+r.H/2+10)
	if !tt.Focused(label) {
		if f, err := os.Create(filepath.Join(os.TempDir(), "gloss-prefs.png")); err == nil {
			_ = png.Encode(f, tt.Image())
			f.Close()
		}
		t.Fatalf("%s did not take the focus (at %v)", label, r)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(text)
}

// openServices opens every service's line, to reach its fields.
func openServices(p *prefs, tt *ui.Tester) {
	for _, d := range p.services {
		d.open = true
	}
	tt.Frame()
}

func TestSettingsSetUpAServiceFromNothing(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	p, tt, path := testPrefs(t, "")
	if !tt.HasText("还没有模型服务") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click("添加服务"); err != nil {
		t.Fatal(err)
	}
	m := fileJSON(t, path)
	if get(m, "default_service") != "main" || get(m, "services", "main") == nil {
		t.Fatalf("file = %v", m)
	}
	typeInto(t, tt, "接口地址", srv.URL)
	typeInto(t, tt, "API Key", "sk-test-key")
	typeInto(t, tt, "模型", "test-model")
	m = fileJSON(t, path)
	if get(m, "services", "main", "api_key") != "sk-test-key" || get(m, "services", "main", "model") != "test-model" || get(m, "services", "main", "base_url") != srv.URL {
		t.Fatalf("file = %v", m)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("mode %v", fi.Mode().Perm())
		}
	}
	if tt.HasText("sk-test-key") {
		t.Error("the key shows while hidden")
	}

	if err := tt.Click("测试连接"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !p.services[0].testOK && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		tt.Frame()
	}
	if asked != "Bearer sk-test-key" || !strings.HasPrefix(p.services[0].tested, "连接成功") {
		t.Fatalf("asked %q, tested %q", asked, p.services[0].tested)
	}

	// Renaming follows every reference.
	typeInto(t, tt, "名称", "work")
	m = fileJSON(t, path)
	if get(m, "default_service") != "work" || get(m, "services", "work", "api_key") != "sk-test-key" || get(m, "services", "main") != nil {
		t.Fatalf("after rename: %v", m)
	}
}

func TestSettingsRejectAndExplain(t *testing.T) {
	p, tt, path := testPrefs(t, `{"services":{"a":{"api_key":"k","model":"m"}},"default_service":"a"}`)
	before, _ := os.ReadFile(path)
	openServices(p, tt)
	typeInto(t, tt, "名称", "")
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("an empty name was written:\n%s", after)
	}
	if !tt.HasText("服务名不能为空") {
		t.Errorf("no error shown: %q", tt.Texts())
	}

	// The last feature on cannot be turned off.
	p.page = pageFeatures
	tt.Frame()
	for _, name := range []string{"启用翻译", "启用释义", "启用优化提示词", "启用整理输出"} {
		if err := tt.Click(name); err != nil {
			t.Fatal(err)
		}
	}
	if acts, _ := get(fileJSON(t, path), "actions").([]any); len(acts) != 1 || acts[0] != "agent" {
		t.Errorf("actions = %v", acts)
	}
	if !tt.HasText("至少要启用一个功能") {
		t.Errorf("no error shown")
	}
}

func TestSettingsFeaturePageWritesItsOwnSettings(t *testing.T) {
	p, tt, path := testPrefs(t, `{"services":{"a":{"api_key":"k","model":"m"}},"default_service":"a"}`)
	p.page = pageFeature + "explain"
	tt.Frame()
	if err := tt.Click("详细"); err != nil {
		t.Fatalf("%v: %q", err, tt.Texts())
	}
	if err := tt.Click("高级"); err != nil { // the model of one feature is not an everyday thing
		t.Fatal(err)
	}
	typeInto(t, tt, "模型", "bigger")
	m := fileJSON(t, path)
	if get(m, "action_settings", "explain", "depth") != "详细" || get(m, "action_settings", "explain", "model") != "bigger" {
		t.Fatalf("file = %v", m)
	}
	typeInto(t, tt, "模型", "")
	if get(fileJSON(t, path), "action_settings", "explain", "model") != nil {
		t.Error("an empty model stayed in the file")
	}
}

func TestSettingsShowABrokenFile(t *testing.T) {
	_, tt, path := testPrefs(t, `{"shortcut": 3}`)
	if !tt.HasText("配置文件有误，Gloss 仍在用上一次正确的设置") {
		t.Errorf("texts %q", tt.Texts())
	}
	if err := tt.Click("添加服务"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != `{"shortcut": 3}` {
		t.Errorf("a broken file was changed: %s", raw)
	}
	if !strings.Contains(strings.Join(tt.Texts(), " "), "shortcut") {
		t.Errorf("the change's error is not shown: %q", tt.Texts())
	}
}

// fakeKeys is the system's shortcuts, some of them taken by other apps.
type fakeKeys struct{ taken, held map[string]bool }

func (k *fakeKeys) Register(acc string, fn func()) error {
	if k.taken[acc] || k.held[acc] {
		return fmt.Errorf("%s is taken", acc)
	}
	k.held[acc] = true
	return nil
}

func (k *fakeKeys) Unregister(acc string) { delete(k.held, acc) }

func TestSettingsRecordAShortcut(t *testing.T) {
	p, tt, path := testPrefs(t, "")
	keys := p.s.keys.(*fakeKeys)
	p.page = pageGeneral
	tt.Frame()
	clickRecorder := func() {
		t.Helper()
		if err := tt.Click("录制快捷键"); err != nil {
			t.Fatal(err)
		}
	}
	clickRecorder()
	if !p.recording || keys.held["Alt+Shift+D"] {
		t.Fatalf("recording %v, old shortcut held %v", p.recording, keys.held)
	}
	tt.Key(ui.Super, ui.KeySpace) // taken by the system
	if keys.held["Cmd+Space"] || !keys.held["Alt+Shift+D"] || p.s.shortcut != "Alt+Shift+D" {
		t.Fatalf("held %v, shortcut %q", keys.held, p.s.shortcut)
	}
	if !strings.Contains(strings.Join(tt.Texts(), " "), "无法使用") {
		t.Errorf("no error shown: %q", tt.Texts())
	}

	clickRecorder()
	tt.Key(ui.Shift, ui.KeyE) // types a capital E: not a shortcut, keeps listening
	if !p.recording {
		t.Fatal("stopped on Shift+E")
	}
	tt.Key(ui.Super|ui.Shift, ui.KeyE)
	if p.recording || !keys.held["Cmd+Shift+E"] || keys.held["Alt+Shift+D"] {
		t.Fatalf("recording %v, held %v", p.recording, keys.held)
	}
	if get(fileJSON(t, path), "shortcut") != "Cmd+Shift+E" {
		t.Errorf("file = %v", fileJSON(t, path))
	}

	// Escape gives up and takes the old one back.
	clickRecorder()
	tt.Key(0, ui.KeyEscape)
	if p.recording || !keys.held["Cmd+Shift+E"] {
		t.Fatalf("recording %v, held %v", p.recording, keys.held)
	}
}

func TestWelcomeGuidesThroughPermissionAndService(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":{"message":"Incorrect API key provided: good***"}}`)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	p, tt, _ := testPrefs(t, "")
	var granted atomic.Bool
	p.checkTrust = granted.Load
	p.page = pageWelcome
	tt.Frame()
	tt.Advance(1100 * time.Millisecond)
	if !tt.HasText("打开系统设置") || !tt.HasText("开始使用") {
		t.Fatalf("texts %q", tt.Texts())
	}
	granted.Store(true) // the user turns it on in System Settings
	tt.Advance(1100 * time.Millisecond)
	waitFor(tt, func() bool { return tt.HasText("已允许") })
	if !tt.HasText("已允许") {
		t.Fatalf("the guide did not see the permission: %q", tt.Texts())
	}

	if err := tt.Click("添加服务"); err != nil {
		t.Fatal(err)
	}
	typeInto(t, tt, "接口地址", srv.URL)
	typeInto(t, tt, "API Key", "bad")
	typeInto(t, tt, "模型", "m")
	test := func() string {
		if err := tt.Click("测试连接"); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for p.services[0].testing && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
			tt.Frame()
		}
		return p.services[0].tested
	}
	if got := test(); !strings.Contains(got, "Key 无效") || strings.Contains(got, "good") {
		t.Fatalf("tested %q", got)
	}
	typeInto(t, tt, "API Key", "good")
	if got := test(); !strings.HasPrefix(got, "连接成功") {
		t.Fatalf("tested %q", got)
	}
	if !p.ready(p.s.config()) {
		t.Error("not ready after both steps")
	}
}

func TestExplainLeadsToTheFix(t *testing.T) {
	s := &shell{cfgPath: "/x/config.json"}
	for _, tt := range []struct {
		err      error
		fix, has string
	}{
		{settings.ErrNoService, "设置模型服务", "还没有设置模型服务"},
		{&settings.MissingError{Service: "main", Fields: []string{"API Key"}}, "设置模型服务", "API Key"},
		{&openai.StatusError{Code: 401}, "检查模型服务", "Key 无效"},
		{&openai.StatusError{Code: 404}, "检查模型服务", "接口地址或模型名"},
		{&openai.StatusError{Code: 500}, "", "暂时出错"},
		{fmt.Errorf("%w：dial tcp", openai.ErrUnreachable), "检查模型服务", "连不上"},
	} {
		msg, fix, do := s.explain(tt.err)
		if fix != tt.fix || !strings.Contains(msg, tt.has) || (fix != "") != (do != nil) {
			t.Errorf("explain(%v) = %q, %q", tt.err, msg, fix)
		}
	}
	_, err := settings.Read(writeTemp(t, `{"shortcut": 3}`), nil)
	if msg, fix, _ := s.explain(err); fix != "打开设置" || strings.Contains(msg, "shortcut") {
		t.Errorf("a broken file: %q, %q", msg, fix)
	}
}

func writeTemp(t *testing.T, content string) string {
	path := filepath.Join(t.TempDir(), settings.File)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// waitFor draws frames until ok, for what a goroutine posts.
func waitFor(tt *ui.Tester, ok func() bool) {
	for deadline := time.Now().Add(5 * time.Second); !ok() && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
		tt.Frame()
	}
}

func TestSettingsRenameKeepsWhatIsStillBeingTyped(t *testing.T) {
	p, tt, path := testPrefs(t, `{"services":{"main":{"api_key":"sk-old","model":"m"}}}`)
	openServices(p, tt)
	typeOnly(t, tt, "名称", "work")
	tt.Advance(400 * time.Millisecond)
	typeOnly(t, tt, "API Key", "sk-new")
	tt.Advance(400 * time.Millisecond) // the name writes; the key waits
	tt.Advance(time.Second)
	if got := get(fileJSON(t, path), "services", "work", "api_key"); got != "sk-new" {
		t.Fatalf("api_key = %v, want the key typed during the rename; file %v", got, fileJSON(t, path))
	}
	if len(p.services) != 1 {
		t.Errorf("%d drafts, want 1", len(p.services))
	}
}

func TestServiceFoldsToOneLineThatOpens(t *testing.T) {
	p, tt, _ := testPrefs(t, `{"services":{"main":{"api_key":"k","model":"m"}}}`)
	tt.Frame()
	if tt.HasText("接口地址") {
		t.Fatal("a complete service shows its fields before it is opened")
	}
	if !tt.HasText("m · 未测试") {
		t.Errorf("the line does not say what it is: %q", tt.Texts())
	}
	if err := tt.Click("main"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("接口地址") || !p.services[0].open {
		t.Fatalf("the line did not open: %q", tt.Texts())
	}
	if err := tt.Click("main"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("接口地址") {
		t.Fatal("the line did not close")
	}
}

// A service open to be filled in stays open once it is complete: its fields must
// not fold away under the hand that typed the last of them.
func TestServiceStaysOpenWhileBeingFilled(t *testing.T) {
	_, tt, _ := testPrefs(t, `{"services":{"main":{"model":"m"}}}`)
	tt.Frame()
	if !tt.HasText("接口地址") {
		t.Fatalf("an incomplete service must show its fields: %q", tt.Texts())
	}
	typeInto(t, tt, "API Key", "sk-test-key")
	if !tt.HasText("接口地址") {
		t.Fatal("the fields folded as the service became complete")
	}
}

func TestFeaturesReorderByKeyboardAndDrag(t *testing.T) {
	p, tt, path := testPrefs(t, `{"services":{"a":{"api_key":"k","model":"m"}},"default_service":"a"}`)
	p.page = pageFeatures
	tt.Frame()
	tt.Frame()
	actions := func() []any {
		acts, _ := get(fileJSON(t, path), "actions").([]any)
		return acts
	}
	// Keyboard: the grip takes the focus, and the arrow moves the feature.
	r, ok := tt.Find("调整翻译的顺序")
	if !ok {
		t.Fatalf("no grip: %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if !tt.Focused("调整翻译的顺序") {
		t.Fatal("the grip did not take the focus")
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if acts := actions(); len(acts) != 4 || acts[0] != "explain" || acts[1] != "translate" {
		t.Fatalf("after Down: %v", acts)
	}

	// Pointer: take agent's grip up to the first row.
	tt.Advance(time.Second)
	from, ok := tt.Find("调整整理输出的顺序")
	if !ok {
		t.Fatal("no grip for agent")
	}
	to, ok := tt.Find("调整释义的顺序")
	if !ok {
		t.Fatal("no grip for explain")
	}
	tt.Press(from.X+from.W/2, from.Y+from.H/2)
	tt.Move(from.X+from.W/2+6, from.Y+from.H/2-12)
	tt.Frame()
	tt.Move(to.X+60, to.Y+to.H/2)
	tt.Frame()
	tt.Release(to.X+60, to.Y+to.H/2)
	tt.Frame()
	tt.Frame()
	if acts := actions(); len(acts) != 4 || acts[0] != "agent" {
		t.Fatalf("after the drag: %v", acts)
	}
	// A feature that is off keeps its place out of the order: it has no grip.
	if err := tt.Click("启用释义"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, ok := tt.Find("调整释义的顺序"); ok {
		t.Error("a feature that is off offers a grip")
	}
}

func TestRecorderShowsTheKeysAsTheyAreHeld(t *testing.T) {
	p, tt, _ := testPrefs(t, "")
	p.page = pageGeneral
	tt.Frame()
	if !isMac {
		t.Skip("the caps show macOS's marks")
	}
	for _, k := range []string{"⌥", "⇧", "D"} {
		if !tt.HasText(k) {
			t.Fatalf("no cap for %s: %q", k, tt.Texts())
		}
	}
	if err := tt.Click("录制快捷键"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("按下新的快捷键") {
		t.Fatalf("not listening: %q", tt.Texts())
	}
	tt.HoldModifiers(ui.Super | ui.Shift)
	tt.Frame()
	if !tt.HasText("⌘") || !tt.HasText("⇧") || tt.HasText("按下新的快捷键") {
		t.Fatalf("the held keys do not show: %q", tt.Texts())
	}
	tt.HoldModifiers(0)
	tt.Frame()
	if !tt.HasText("按下新的快捷键") {
		t.Fatalf("the caps stayed after letting go: %q", tt.Texts())
	}
}

func TestMemoryPageSaysWhatItLearnedAndForgets(t *testing.T) {
	p, tt, _ := testPrefs(t, `{"services":{"a":{"api_key":"k","model":"m"}},"default_service":"a"}`)
	store, err := memory.Open(t.TempDir(), memory.Options{MaxRecords: 100, RetentionDays: 30}, true)
	if err != nil {
		t.Fatal(err)
	}
	p.s.store = store
	now := tt.Now()
	for i, final := range []string{"translate", "translate", "explain"} {
		rec := memory.Record{Session: fmt.Sprintf("s%d", i), Time: now, Final: final, Initial: "translate", Routing: true, Outcome: "done"}
		if final != rec.Initial {
			rec.Corrections = []memory.Correction{{From: rec.Initial, To: final}}
		}
		if _, err := store.Add(rec); err != nil {
			t.Fatal(err)
		}
	}
	p.page = pageMemory
	tt.Frame()
	if !tt.HasText("用了 3 次") || !tt.HasText("最常用的是翻译") || !tt.HasText("33% 被你改选") {
		t.Fatalf("stats do not show: %q", tt.Texts())
	}
	if tt.HasText("最长等待") {
		t.Error("the inner settings show without opening 高级")
	}
	if err := tt.Click("高级"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("最长等待") {
		t.Fatalf("高级 did not open: %q", tt.Texts())
	}
	if err := tt.Click("清空…"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("清空"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("用了") || !tt.HasText("已清空") {
		t.Fatalf("the page still shows what was cleared: %q", tt.Texts())
	}
}

// With less motion asked for, a choice and a page are simply there: nothing is
// left waiting for a stroke to finish.
func TestReducedMotionShowsTheEndAtOnce(t *testing.T) {
	p, tt, _ := testPrefs(t, `{"services":{"a":{"api_key":"k","model":"m"}},"default_service":"a"}`)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	if got := swipe(10*time.Millisecond, swipeTime, true); got != 1 {
		t.Errorf("a stroke with motion reduced stands at %v", got)
	}
	for _, page := range []string{pageGeneral, pageServices, pageMemory, pageFeatures, pageFeature + "translate"} {
		p.page = page
		tt.Frame()
	}
	if got := swipe(10*time.Millisecond, swipeTime, false); got <= 0 || got >= 1 {
		t.Errorf("a stroke with motion stands at %v", got)
	}
}

func TestSettingsAddingAServiceKeepsTheDefault(t *testing.T) {
	p, tt, path := testPrefs(t, `{"services":{"main":{"api_key":"k","model":"m"}}}`)
	tt.Frame()
	p.addService()
	if got := get(fileJSON(t, path), "default_service"); got != "main" {
		t.Errorf("default_service = %v, want main kept", got)
	}
}

func TestSettingsWriteWhatWasTypedWhenThePageChanges(t *testing.T) {
	p, tt, path := testPrefs(t, `{"services":{"main":{"api_key":"k","model":"m"}}}`)
	openServices(p, tt)
	typeOnly(t, tt, "模型", "m2")
	p.page = pageGeneral
	tt.Frame()
	if got := get(fileJSON(t, path), "services", "main", "model"); got != "m2" {
		t.Errorf("model = %v, want what was typed before leaving the page", got)
	}
}

func TestSettingsAddingAServiceReadsTheDefaultFromTheFile(t *testing.T) {
	p, tt, path := testPrefs(t, `{"services":{"main":{"api_key":"k","model":"m"}}}`)
	tt.Frame()
	// A rename written this frame, before the window has the new config.
	if _, err := settings.Update(path, p.s.descs, func(d *settings.Doc) error { return settings.RenameService(d, "main", "work") }); err != nil {
		t.Fatal(err)
	}
	p.addService()
	m := fileJSON(t, path)
	if get(m, "default_service") != "work" || get(m, "services", "work") == nil {
		t.Errorf("file = %v, want work kept as the default", m)
	}
}

// A page is one column, the same at every size: centred once the window has room,
// and never out of the sheet when it does not.
func TestPagesFitEveryWindowSize(t *testing.T) {
	for _, size := range [][2]int{{700, 480}, {820, 600}, {1100, 760}, {1440, 900}} {
		p, tt, _ := testPrefs(t, `{"services":{"a":{"api_key":"k","model":"m"},"a-service-with-a-long-name":{"model":"gpt-4.1-mini"}},"default_service":"a"}`)
		tt.SetSize(size[0], size[1])
		for _, d := range p.services {
			d.open = true
		}
		for _, page := range []string{pageGeneral, pageServices, pageFeatures, pageFeature + "translate", pageFeature + "explain", pageMemory} {
			p.page = page
			p.disclosed["general.advanced"], p.disclosed["memory.advanced"] = true, true
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			sheet := float32(size[0]) - widthSidebar - marginSheet
			left, right := widthSidebar, float32(size[0])-marginSheet
			if pad := pagePadding(sheet); sheet >= widthPage+2*paddingPage {
				// Centred: the column's edges are the same distance from the sheet's.
				left, right = widthSidebar+pad, widthSidebar+pad+widthPage
			}
			for _, text := range tt.Texts() {
				r, ok := tt.Find(text)
				if !ok || r.X < widthSidebar {
					continue // the navigation
				}
				// A grip hangs in the margin, and a control may reach past the edge by
				// its own clear padding, so that what shows lines up.
				if r.X < left-24 || r.X+r.W > right+10 {
					t.Errorf("%dx%d %s: %q at %v is out of %v to %v", size[0], size[1], page, text, r, left, right)
				}
			}
		}
	}
}
