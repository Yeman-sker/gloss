// Package capture reads the text selected in other apps through the Rust
// helper, a separate process that answers one JSON line per request. See
// docs/decisions/0003-capture-helper-process.md.
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
)

// Result is the helper's answer.
type Result struct {
	OK     bool   `json:"ok"`
	Text   string `json:"text"`
	Error  string `json:"error"`
	Method string `json:"method"`
	Micros int64  `json:"us"`
}

// Helper is a running helper process.
type Helper struct {
	mu  sync.Mutex
	in  io.Writer
	out *bufio.Scanner
}

// Path finds the helper: GLOSS_HELPER, or next to this program.
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
	return filepath.Join(filepath.Dir(exe), name)
}

// Start runs the helper at path. It exits when its stdin closes, which is
// when this process ends.
func Start(path string) (*Helper, error) {
	cmd := exec.Command(path)
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
	out := bufio.NewScanner(stdout)
	out.Buffer(make([]byte, 1<<20), 1<<24)
	return &Helper{in: in, out: out}, nil
}

// Selection asks for the selection, falling back to a simulated copy.
func (h *Helper) Selection() (Result, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := fmt.Fprintln(h.in, "selection copy"); err != nil {
		return Result{}, err
	}
	if !h.out.Scan() {
		return Result{}, errors.New("capture: helper exited")
	}
	var r Result
	err := json.Unmarshal(h.out.Bytes(), &r)
	return r, err
}
