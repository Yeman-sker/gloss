package app

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"gloss/capture"
	"gloss/providers/openai"
	"gloss/settings"
)

// The diagnostic log keeps events, timings, kinds of errors and the source
// app: never the selected text, a result, a window title or a key. It
// turns over at logLimit, keeping one older file, so it stays under twice
// that.
const logLimit = 1 << 20

// logPath is where the log lives: ~/Library/Logs/Gloss on macOS, beside
// the config elsewhere.
func logPath() (string, error) {
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Logs", Name, "gloss.log"), nil
	}
	cfg, err := settings.Path(Name)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(cfg), "logs", "gloss.log"), nil
}

// rotating is a log file that moves aside to path.1 once it reaches limit.
type rotating struct {
	mu    sync.Mutex
	path  string
	limit int64
	f     *os.File
	size  int64
}

func openRotating(path string, limit int64) (*rotating, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	r := &rotating{path: path, limit: limit}
	return r, r.open()
}

func (r *rotating) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, info.Size()
	return nil
}

func (r *rotating) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return 0, errors.New("log: closed")
	}
	if r.size > 0 && r.size+int64(len(p)) > r.limit {
		r.f.Close()
		_ = os.Rename(r.path, r.path+".1")
		if err := r.open(); err != nil {
			r.f = nil
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// startLog sends the standard logger to the file as well as to stderr. It
// returns the file's path, or an error when there can be no file, in which
// case the log goes to stderr alone.
func startLog() (string, error) {
	path, err := logPath()
	if err != nil {
		return "", err
	}
	w, err := openRotating(path, logLimit)
	if err != nil {
		return "", err
	}
	log.SetOutput(io.MultiWriter(os.Stderr, w))
	log.Printf("start: %s %s, %s/%s", Name, Version, runtime.GOOS, runtime.GOARCH)
	return path, nil
}

// errKind is an error as the log keeps it: its kind, never what a service
// said, which may echo what was sent.
func errKind(err error) string {
	var status *openai.StatusError
	var missing *settings.MissingError
	switch {
	case err == nil:
		return "none"
	case errors.As(err, &status):
		return fmt.Sprintf("http %d", status.Code)
	case errors.Is(err, openai.ErrUnreachable):
		return "unreachable"
	case errors.Is(err, settings.ErrNoService):
		return "no service"
	case errors.As(err, &missing):
		return "service incomplete"
	case errors.Is(err, settings.ErrInvalid):
		return "invalid config"
	case errors.Is(err, capture.ErrPermission):
		return "no permission"
	case errors.Is(err, capture.ErrNoSelection):
		return "no selection"
	case errors.Is(err, capture.ErrTimeout):
		return "helper timeout"
	case errors.Is(err, capture.ErrExited):
		return "helper exited"
	case errors.Is(err, capture.ErrChanged):
		return "target changed"
	case errors.Is(err, capture.ErrUnverified):
		return "unverified"
	case errors.Is(err, capture.ErrExpired):
		return "expired"
	}
	return "other"
}
