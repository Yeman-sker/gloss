// Package capture reads the text selected in other apps, and writes text
// back over it, through the Rust helper: a separate process that answers one
// JSON line per request. See docs/decisions/0003-capture-helper-process.md.
//
// Every request carries an id and a deadline. A helper that misses the
// deadline or exits is started again, and an answer that comes late, from
// it or from the one before, is dropped rather than taken for the next
// request's.
package capture

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Why a request failed, so that the panel can say what to do.
var (
	ErrPermission  = errors.New("capture: no accessibility permission")
	ErrNoSelection = errors.New("capture: nothing selected")
	ErrTimeout     = errors.New("capture: helper did not answer in time")
	ErrExited      = errors.New("capture: helper exited")
	ErrProtocol    = errors.New("capture: helper answered something unreadable")
	ErrChanged     = errors.New("capture: the target changed")
	ErrNotEditable = errors.New("capture: the target is not editable")
	ErrUnverified  = errors.New("capture: the target cannot be checked")
	ErrExpired     = errors.New("capture: the target is no longer known")
	ErrFailed      = errors.New("capture: the helper failed")
)

var codes = map[string]error{
	"permission":   ErrPermission,
	"no_selection": ErrNoSelection,
	"changed":      ErrChanged,
	"not_editable": ErrNotEditable,
	"unverifiable": ErrUnverified,
	"expired":      ErrExpired,
	"failed":       ErrFailed,
	"bad_request":  ErrProtocol,
}

// Result is a selection and what the helper could tell about where it is.
type Result struct {
	Text     string `json:"text"`
	Method   string `json:"method"`   // "ax" or "copy"
	Micros   int64  `json:"us"`       // the helper's time, context included
	App      string `json:"app"`      // the bundle id of the app, when known
	Title    string `json:"title"`    // its window's title, when known
	Editable string `json:"editable"` // "yes", "no" or "unknown"
	Before   string `json:"before"`   // bounded text before the selection
	After    string `json:"after"`    // and after it
	// Token names where the selection was read, for Replace. Zero when
	// the helper could not keep it.
	Token     uint64 `json:"token"`
	CtxMicros int64  `json:"ctx_us"`
}

// Replaced says how text was written back.
type Replaced struct {
	Method   string `json:"method"`
	Verified bool   `json:"verified"` // the helper read the text back where it went
	Micros   int64  `json:"us"`
}

type reply struct {
	ID      uint64 `json:"id"`
	OK      bool   `json:"ok"`
	Code    string `json:"code"`
	Error   string `json:"error"`
	Trusted bool   `json:"trusted"`
	Down    bool   `json:"down"`
	Result
	// Replaced shares Result's "us" and "method": UnmarshalJSON fills it.
	Replaced Replaced `json:"-"`
}

// UnmarshalJSON splits the shared fields: "us" and "method" belong to both
// a Result and a Replaced.
func (r *reply) UnmarshalJSON(b []byte) error {
	type plain reply
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	if err := json.Unmarshal(b, &p.Result); err != nil {
		return err
	}
	if err := json.Unmarshal(b, &p.Replaced); err != nil {
		return err
	}
	*r = reply(p)
	return nil
}

// Helper is the helper process, started again when it fails.
type Helper struct {
	path string

	mu     sync.Mutex // one request at a time
	proc   *process
	nextID uint64
	// Restarts counts helpers started after the first.
	Restarts int
}

type process struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	lines chan []byte // closed when the helper's output ends
}

// Path finds the helper: GLOSS_HELPER, next to this program, or in the
// Resources of the app bundle it runs from.
func Path() string {
	if p := os.Getenv("GLOSS_HELPER"); p != "" {
		return p
	}
	name := "gloss-capture"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	exe, err := os.Executable()
	if err != nil {
		return name
	}
	dir := filepath.Dir(exe)
	next := filepath.Join(dir, name)
	if _, err := os.Stat(next); err == nil {
		return next
	}
	if res := filepath.Join(dir, "..", "Resources", name); fileExists(res) {
		return res
	}
	return next
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Start runs the helper at path. It exits when its stdin closes, which is
// when this process ends.
func Start(path string) (*Helper, error) {
	h := &Helper{path: path}
	p, err := spawn(path, nil)
	if err != nil {
		return nil, err
	}
	h.proc = p
	return h, nil
}

func spawn(path string, args []string) (*process, error) {
	cmd := exec.Command(path, args...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("capture: start helper: %w", err)
	}
	p := &process{cmd: cmd, in: in, lines: make(chan []byte, 4)}
	go func() {
		defer close(p.lines)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64<<10), 1<<24)
		for sc.Scan() {
			p.lines <- append([]byte(nil), sc.Bytes()...)
		}
	}()
	go cmd.Wait()
	return p, nil
}

func (p *process) kill() {
	p.in.Close()
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
}

// restart replaces the helper. Called with mu held.
func (h *Helper) restart() error {
	if h.proc != nil {
		h.proc.kill()
	}
	h.proc = nil
	p, err := spawn(h.path, nil)
	if err != nil {
		return err
	}
	h.proc = p
	h.Restarts++
	return nil
}

// Close ends the helper.
func (h *Helper) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.proc != nil {
		h.proc.kill()
		h.proc = nil
	}
}

// call sends one request and waits for its answer. Called with mu held.
func (h *Helper) call(req map[string]any, timeout time.Duration) (reply, error) {
	if h.proc == nil {
		if err := h.restart(); err != nil {
			return reply{}, err
		}
	}
	h.nextID++
	id := h.nextID
	req["id"] = id
	line, err := json.Marshal(req)
	if err != nil {
		return reply{}, err
	}
	p := h.proc
	// The deadline covers the write too: a helper that hangs and stops
	// reading fills the pipe, and the write would block with mu held.
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	wrote := make(chan error, 1)
	go func() {
		_, err := p.in.Write(append(line, '\n'))
		wrote <- err
	}()
	select {
	case err := <-wrote:
		if err != nil {
			h.restart()
			return reply{}, ErrExited
		}
	case <-deadline.C:
		h.restart() // kills it, which ends the write
		return reply{}, ErrTimeout
	}
	for {
		select {
		case b, ok := <-p.lines:
			if !ok {
				h.restart()
				return reply{}, ErrExited
			}
			var r reply
			if err := json.Unmarshal(b, &r); err != nil {
				// A helper that speaks nonsense is not one to keep.
				h.restart()
				return reply{}, fmt.Errorf("%w: %v", ErrProtocol, err)
			}
			if r.ID != id {
				continue // the answer to a request given up on
			}
			return r, nil
		case <-deadline.C:
			h.restart()
			return reply{}, ErrTimeout
		}
	}
}

func failure(r reply) error {
	if err, ok := codes[r.Code]; ok {
		return fmt.Errorf("%w: %s", err, r.Error)
	}
	return fmt.Errorf("%w: %s %s", ErrProtocol, r.Code, r.Error)
}

// SelectionTimeout bounds a selection, the simulated copy included.
var SelectionTimeout = 2 * time.Second

// Selection asks for the selection, falling back to a simulated copy.
func (h *Helper) Selection() (Result, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, err := h.call(map[string]any{"cmd": "selection", "copy": true}, SelectionTimeout)
	if err != nil {
		return Result{}, err
	}
	if !r.OK {
		return Result{}, failure(r)
	}
	return r.Result, nil
}

// Replace writes text over the selection read under token, if the helper
// finds it as it was.
func (h *Helper) Replace(token uint64, text string) (Replaced, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, err := h.call(map[string]any{"cmd": "replace", "token": token, "text": text}, 3*time.Second)
	if err != nil {
		return Replaced{}, err
	}
	if !r.OK {
		return Replaced{}, failure(r)
	}
	return r.Replaced, nil
}

// Forget lets the helper drop what it kept under token. It waits for a
// call in progress: callers run it on their own goroutine.
func (h *Helper) Forget(token uint64) {
	if token == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.call(map[string]any{"cmd": "forget", "token": token}, 500*time.Millisecond)
}

// Trusted says whether the helper has Accessibility access.
func (h *Helper) Trusted() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, err := h.call(map[string]any{"cmd": "check"}, time.Second)
	return err == nil && r.Trusted
}

// ButtonDown says whether the primary mouse button is down anywhere on the
// desktop. It answers false rather than wait for a selection being read.
func (h *Helper) ButtonDown() bool {
	if !h.mu.TryLock() {
		return false
	}
	defer h.mu.Unlock()
	r, err := h.call(map[string]any{"cmd": "button"}, 300*time.Millisecond)
	return err == nil && r.Down
}
