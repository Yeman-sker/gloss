package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestUpdateRepoMatchesMygoJSON(t *testing.T) {
	raw, err := os.ReadFile("../mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Updates struct {
			GitHub    string `json:"github"`
			PublicKey string `json:"publicKey"`
			Deltas    *int   `json:"deltas"`
		} `json:"updates"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Updates.GitHub != updateRepo {
		t.Errorf("mygo.json updates from %q, the app from %q", m.Updates.GitHub, updateRepo)
	}
	if m.Updates.PublicKey == "" {
		t.Error("mygo.json has no public key: builds would not update")
	}
	// Deltas are made from the published manifest, which mygo reads without
	// a login: a private repository's is out of its reach.
	if m.Updates.Deltas == nil || *m.Updates.Deltas != 0 {
		t.Error("mygo.json should turn deltas off")
	}
}

// fakeGitHub serves a release of the private repository to the token "t0k",
// its files from a store on another host.
func fakeGitHub(t *testing.T) (api *httptest.Server, store *httptest.Server, storeAuth *[]string) {
	t.Helper()
	var auth []string
	store = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		fmt.Fprintf(w, "file %s", r.URL.Path)
	}))
	t.Cleanup(store.Close)
	manifest := "update-" + runtime.GOOS + "-" + runtime.GOARCH + ".json"
	// Another host than the API's, as GitHub's file store is.
	storeURL := strings.Replace(store.URL, "127.0.0.1", "localhost", 1)
	api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t0k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		release := func(tag string) {
			fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q,"url":"%s/assets/1"},{"name":"gloss-0.1.4-darwin-arm64.tar.gz","url":"%s/assets/2"}]}`,
				tag, manifest, "http://"+r.Host+"/repos/o/r/releases", "http://"+r.Host+"/repos/o/r/releases")
		}
		switch r.URL.Path {
		case "/repos/o/r/releases/latest", "/repos/o/r/releases/tags/v0.1.4":
			release("v0.1.4")
		case "/repos/o/r/releases/assets/1", "/repos/o/r/releases/assets/2":
			if r.Header.Get("Accept") != "application/octet-stream" {
				t.Errorf("asset asked as %q", r.Header.Get("Accept"))
			}
			http.Redirect(w, r, storeURL+"/stored"+strings.TrimPrefix(r.URL.Path, "/repos/o/r/releases"), http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(api.Close)
	return api, store, &auth
}

func testSource(api, token string) *ghSource {
	return &ghSource{api: api, repo: "o/r", client: &http.Client{},
		token: func(context.Context) (string, error) { return token, nil }}
}

func readAll(t *testing.T, open func() (io.ReadCloser, error)) string {
	t.Helper()
	rc, err := open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestGHSourceReadsThePrivateRelease(t *testing.T) {
	api, _, storeAuth := fakeGitHub(t)
	g := testSource(api.URL, "t0k")
	ctx := context.Background()
	if got := readAll(t, func() (io.ReadCloser, error) { return g.Manifest(ctx, false) }); got != "file /stored/assets/1" {
		t.Errorf("manifest: %q", got)
	}
	// The URL the manifest has for the archive: the public download link.
	got := readAll(t, func() (io.ReadCloser, error) {
		return g.Open(ctx, "https://github.com/o/r/releases/download/v0.1.4/gloss-0.1.4-darwin-arm64.tar.gz")
	})
	if got != "file /stored/assets/2" {
		t.Errorf("archive: %q", got)
	}
	for _, a := range *storeAuth {
		if a != "" {
			t.Fatalf("the token went to the file store: %q", a)
		}
	}
}

func TestGHSourceRefusesOtherFiles(t *testing.T) {
	api, _, _ := fakeGitHub(t)
	g := testSource(api.URL, "t0k")
	for _, u := range []string{
		"https://github.com/someone/else/releases/download/v0.1.4/gloss-0.1.4-darwin-arm64.tar.gz",
		"https://example.com/o/r/releases/download/v0.1.4/x",
		"https://github.com/o/r/releases/download/v0.1.4",
	} {
		if _, err := g.Open(context.Background(), u); err == nil {
			t.Errorf("opened %s", u)
		}
	}
	_, err := g.Open(context.Background(), "https://github.com/o/r/releases/download/v0.1.4/missing.tar.gz")
	if !errors.Is(err, errNoUpdate) {
		t.Errorf("a file the release lacks: %v", err)
	}
}

func TestGHSourceSaysWhyItCannotRead(t *testing.T) {
	api, _, _ := fakeGitHub(t)
	if _, err := testSource(api.URL, "expired").Manifest(context.Background(), false); !errors.Is(err, errGHLogin) {
		t.Errorf("a token GitHub refuses: %v", err)
	}
	other := testSource(api.URL, "t0k")
	other.repo = "o/hidden"
	if _, err := other.Manifest(context.Background(), false); !errors.Is(err, errNoAccess) {
		t.Errorf("a repository the account cannot see: %v", err)
	}
	noGH := testSource(api.URL, "")
	noGH.token = func(context.Context) (string, error) { return "", errNoGH }
	if _, err := noGH.Manifest(context.Background(), false); !errors.Is(err, errNoGH) {
		t.Errorf("without gh: %v", err)
	}
}

func TestUpdateFailureSaysWhatToDo(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("mygo: checking for updates: %w", err) }
	cases := map[string]error{
		"没找到 gh":        wrap(errNoGH),
		"gh auth login": wrap(errGHLogin),
		"看不到 Gloss 的仓库": wrap(errNoAccess),
		"没有更新包":         wrap(fmt.Errorf("%w: v0.1.4 has no x", errNoUpdate)),
		"签名不对":          errors.New("mygo: installing the update: the update is not signed with the app's key"),
		"连不上 GitHub":    wrap(&net.OpError{Op: "dial", Err: errors.New("no route")}),
		"诊断日志":          errors.New("mygo: installing the update: disk full"),
	}
	for want, err := range cases {
		if got := updateFailure(err); !strings.Contains(got, want) {
			t.Errorf("%v: %q, want %q", err, got, want)
		}
	}
}

func TestUpdatesCheckDailyAndRetrySooner(t *testing.T) {
	var u updates
	now := time.Now()
	if !u.due(now) {
		t.Fatal("the first check is not due")
	}
	if !u.begin() || u.begin() {
		t.Fatal("two checks at once")
	}
	if u.due(now) {
		t.Error("due while checking")
	}
	u.set(updateLatest, "", nil)
	if u.due(now.Add(updateEvery-time.Minute)) || !u.due(now.Add(updateEvery+time.Minute)) {
		t.Error("not daily after a check that worked")
	}
	u.set(updateFailed, "", errNoGH)
	if u.due(now.Add(updateRetry-time.Minute)) || !u.due(now.Add(updateRetry+time.Minute)) {
		t.Error("not hourly after one that failed")
	}
	u.set(updateReady, "0.1.4", nil)
	if u.due(now.Add(100*updateEvery)) || u.begin() {
		t.Error("checks again with a version waiting to install")
	}
}

func TestSettingsShowAnUpdateAndRestartIntoIt(t *testing.T) {
	p, tt, path := testPrefs(t, `{"services":{"a":{"api_key":"k","model":"m"}},"default_service":"a"}`)
	restarted := false
	p.s.relaunch = func() { restarted = true }
	p.page = pageGeneral
	tt.Frame()
	if !tt.HasText("启动后和每天检查一次") || !tt.HasText("检查更新") {
		t.Fatalf("no update row: %q", tt.Texts())
	}
	p.s.upd.set(updateReady, "0.1.4", nil)
	tt.Frame()
	if !tt.HasText("0.1.4 已下载") {
		t.Fatalf("the update waiting does not show: %q", tt.Texts())
	}
	if err := tt.Click("重启以更新"); err != nil {
		t.Fatal(err)
	}
	if !restarted {
		t.Error("restart did not restart")
	}
	p.s.upd.set(updateFailed, "", fmt.Errorf("x: %w", errGHLogin))
	tt.Frame()
	if !tt.HasText("gh auth login") {
		t.Errorf("the failure does not say what to do: %q", tt.Texts())
	}

	if err := tt.Click("高级"); err != nil {
		t.Fatal(err)
	}
	r, ok := tt.Find("自动检查更新")
	if !ok {
		t.Fatalf("no switch: %q", tt.Texts())
	}
	tt.ClickAt(r.X+contentWidth-14-widthSwitchT/2, r.Y+r.H/2+10)
	tt.Advance(time.Second)
	if got := get(fileJSON(t, path), "auto_update"); got != false {
		t.Errorf("auto_update is %v after turning it off", got)
	}
	tt.Frame()
	p.s.upd.set(updateIdle, "", nil)
	tt.Frame()
	if !tt.HasText("自动检查已关闭") {
		t.Errorf("the row does not say checks are off: %q", tt.Texts())
	}
}
