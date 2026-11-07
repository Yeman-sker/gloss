package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/netproxy"
)

// Updates. The releases are in a private repository, so Gloss reads them
// with the GitHub login of gh on this Mac, and mygo's Updater checks what it
// downloads against the public key in mygo.json before it stages it. The
// staged version replaces Gloss when Gloss exits: at once with "重启以更新",
// or at the next quit. See docs/DEVELOPMENT.md#发布.

// updateRepo holds the releases; mygo.json's updates.github says the same,
// which a test checks.
const updateRepo = "Yeman-sker/gloss"

const (
	// updateFirstWait keeps the first check clear of launch, when memory
	// peaks anyway.
	updateFirstWait = 30 * time.Second
	// updateEvery is how often Gloss looks after a check that worked, and
	// updateRetry after one that did not, as when the Mac was offline.
	updateEvery = 24 * time.Hour
	updateRetry = time.Hour
	// updateTick is how often the loop asks whether a check is due. The
	// time is the wall clock's, which a sleeping Mac moves on.
	updateTick = 10 * time.Minute
	// updateTimeout bounds a check and its download.
	updateTimeout = 10 * time.Minute
)

type updateState int

const (
	updateIdle updateState = iota // not checked yet
	updateChecking
	updateLatest
	updateDownloading
	updateReady // downloaded, checked and staged: it installs when Gloss exits
	updateFailed
	updateOff // this build cannot update itself
)

// updates is where updating stands. Its methods may be called from any
// goroutine.
type updates struct {
	mu      sync.Mutex
	state   updateState
	version string    // being downloaded, or ready
	err     error     // of the last check that failed
	last    time.Time // when the last check ended, on the wall clock
}

func (u *updates) get() (updateState, string, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state, u.version, u.err
}

// begin starts a check unless one is running or a version waits to install.
func (u *updates) begin() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	switch u.state {
	case updateChecking, updateDownloading, updateReady, updateOff:
		return false
	}
	u.state, u.err = updateChecking, nil
	return true
}

func (u *updates) set(state updateState, version string, err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.state, u.version, u.err = state, version, err
	if state != updateChecking && state != updateDownloading {
		u.last = time.Now().Round(0)
	}
}

// due says whether the loop should check now.
func (u *updates) due(now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	wait := updateEvery
	switch u.state {
	case updateIdle:
		return true
	case updateFailed:
		wait = updateRetry
	case updateLatest:
	default:
		return false
	}
	return now.Round(0).Sub(u.last) >= wait
}

// startUpdates looks for a new version after launch and then once a day,
// while the settings say to.
func (s *shell) startUpdates() {
	if !mygo.Updater.Enabled() {
		s.upd.set(updateOff, "", nil)
		return
	}
	mygo.Updater.SetSource(newGHSource())
	go func() {
		time.Sleep(updateFirstWait)
		for {
			if s.config().AutoUpdate && s.upd.due(time.Now()) {
				s.checkUpdate()
			}
			time.Sleep(updateTick)
		}
	}()
}

// checkUpdate looks for a new version and stages it. It blocks, so the
// settings window runs it on a goroutine of its own.
func (s *shell) checkUpdate() {
	if !s.upd.begin() {
		return
	}
	s.updateChanged()
	defer s.updateChanged()
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	up, err := mygo.Updater.Check(ctx)
	switch {
	case err != nil:
		log.Printf("update: check: %v", err)
		s.upd.set(updateFailed, "", err)
		return
	case up == nil:
		log.Printf("update: %s is the latest", Version)
		s.upd.set(updateLatest, "", nil)
		return
	}
	s.upd.set(updateDownloading, up.Version, nil)
	s.updateChanged()
	if err := up.Install(ctx, nil); err != nil {
		log.Printf("update: %s: %v", up.Version, err)
		s.upd.set(updateFailed, up.Version, err)
		return
	}
	log.Printf("update: %s staged in %v", up.Version, time.Since(start).Round(time.Millisecond))
	s.upd.set(updateReady, up.Version, nil)
}

// updateChanged shows where updating stands in the menu and the settings.
func (s *shell) updateChanged() {
	mygo.RunOnMain(func() {
		state, version, _ := s.upd.get()
		if s.updItem != nil {
			if state == updateReady {
				s.updItem.SetLabel("重启以更新到 " + version)
			}
			s.updItem.SetVisible(state == updateReady)
		}
		if s.prefs != nil && s.prefs.win != nil {
			s.prefs.win.Invalidate()
		}
	})
}

// restartToUpdate quits, and the version staged opens in Gloss's place.
func (s *shell) restartToUpdate() {
	if s.relaunch != nil {
		s.relaunch()
		return
	}
	log.Printf("update: restarting")
	if s.prefs != nil {
		s.prefs.flush()
	}
	s.endSession()
	// The panel hides rather than closes; quitting must close it.
	s.quitting = true
	mygo.App.Relaunch()
}

// updateNote says where updating stands, under the row in the settings.
func updateNote(state updateState, version string, err error, auto bool) string {
	switch state {
	case updateOff:
		if mygo.IsDev() {
			return "开发版不自动更新"
		}
		return "Gloss 所在的文件夹不能写入，没法自己更新"
	case updateChecking:
		return "正在检查…"
	case updateDownloading:
		return "正在下载 " + version + "…"
	case updateReady:
		return version + " 已下载，重启 Gloss 后生效"
	case updateLatest:
		return "已是最新版本"
	case updateFailed:
		return updateFailure(err)
	}
	if !auto {
		return "自动检查已关闭"
	}
	return "启动后和每天检查一次"
}

// updateFailure tells the user what to do about a failed check.
func updateFailure(err error) string {
	var netErr net.Error
	switch {
	case errors.Is(err, errNoGH):
		return "没找到 gh：装好 GitHub CLI 并登录后才能更新"
	case errors.Is(err, errGHLogin):
		return "gh 没有登录：在终端运行 gh auth login"
	case errors.Is(err, errNoAccess):
		return "这个 GitHub 账号看不到 Gloss 的仓库"
	case errors.Is(err, errNoUpdate):
		return "最新的发布里没有更新包"
	case err != nil && strings.Contains(err.Error(), "not signed with the app's key"):
		return "更新包的签名不对，没有安装"
	case errors.As(err, &netErr):
		return "连不上 GitHub，稍后会再试"
	}
	return "检查更新失败，详情在诊断日志里"
}

var (
	errNoGH     = errors.New("gh is not installed")
	errGHLogin  = errors.New("gh is not signed in")
	errNoAccess = errors.New("this GitHub account cannot see the repository")
	errNoUpdate = errors.New("the latest release has no update")
)

// ghSource serves mygo's Updater the files of the private repository's
// releases, through the GitHub API with the token of gh.
type ghSource struct {
	api, repo string
	token     func(ctx context.Context) (string, error)
	client    *http.Client
}

func newGHSource() *ghSource {
	return &ghSource{
		api:    "https://api.github.com",
		repo:   updateRepo,
		token:  ghToken,
		client: &http.Client{Transport: netproxy.Transport()},
	}
}

type ghRelease struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"assets"`
}

// Manifest is the update manifest of the latest release: drafts and
// prereleases are not offered.
func (g *ghSource) Manifest(ctx context.Context, _ bool) (io.ReadCloser, error) {
	rel, err := g.release(ctx, "latest")
	if err != nil {
		return nil, err
	}
	return g.asset(ctx, rel, "update-"+runtime.GOOS+"-"+runtime.GOARCH+".json")
}

// Open fetches a file the manifest names, which mygo writes as the public
// download URL of a release's file.
func (g *ghSource) Open(ctx context.Context, fileURL string) (io.ReadCloser, error) {
	prefix := "https://github.com/" + g.repo + "/releases/download/"
	rest, ok := strings.CutPrefix(fileURL, prefix)
	if !ok {
		return nil, fmt.Errorf("update file outside %s: %s", g.repo, fileURL)
	}
	tag, name, ok := strings.Cut(rest, "/")
	if !ok {
		return nil, fmt.Errorf("update file without a release: %s", fileURL)
	}
	var err error
	if tag, err = url.PathUnescape(tag); err != nil {
		return nil, err
	}
	if name, err = url.PathUnescape(name); err != nil {
		return nil, err
	}
	rel, err := g.release(ctx, "tags/"+url.PathEscape(tag))
	if err != nil {
		return nil, err
	}
	return g.asset(ctx, rel, name)
}

func (g *ghSource) release(ctx context.Context, which string) (*ghRelease, error) {
	body, err := g.get(ctx, g.api+"/repos/"+g.repo+"/releases/"+which, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("reading the release: %w", err)
	}
	return &rel, nil
}

// asset downloads a file of rel. The API answers with a redirect to the
// file's store, which Go follows without the token: it never leaves GitHub's
// API host.
func (g *ghSource) asset(ctx context.Context, rel *ghRelease, name string) (io.ReadCloser, error) {
	for _, a := range rel.Assets {
		if a.Name == name {
			return g.get(ctx, a.URL, "application/octet-stream")
		}
	}
	return nil, fmt.Errorf("%w: %s has no %s", errNoUpdate, rel.Tag, name)
}

func (g *ghSource) get(ctx context.Context, u, accept string) (io.ReadCloser, error) {
	token, err := g.token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	// ASCII only: GitHub's file store refuses other bytes here.
	req.Header.Set("User-Agent", "Gloss-updater")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		return resp.Body, nil
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return nil, errGHLogin
	case http.StatusNotFound:
		return nil, errNoAccess
	case http.StatusForbidden:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, errors.New("GitHub API rate limit reached")
		}
		return nil, errNoAccess
	}
	return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
}

// ghToken is the token gh signed in with. Gloss opened from Finder has a
// short PATH, so gh is also looked for where installers put it.
func ghToken(ctx context.Context) (string, error) {
	gh, err := findGH()
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, gh, "auth", "token", "--hostname", "github.com").Output()
	token := strings.TrimSpace(string(out))
	if err != nil || token == "" {
		return "", errGHLogin
	}
	return token, nil
}

func findGH() (string, error) {
	if p, err := exec.LookPath("gh"); err == nil {
		return p, nil
	}
	places := []string{"/opt/homebrew/bin/gh", "/usr/local/bin/gh", "/opt/local/bin/gh"}
	if home, err := os.UserHomeDir(); err == nil {
		places = append(places, filepath.Join(home, ".local", "bin", "gh"), filepath.Join(home, "bin", "gh"))
	}
	for _, p := range places {
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", errNoGH
}
