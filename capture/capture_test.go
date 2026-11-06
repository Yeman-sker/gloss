package capture

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as a fake helper, run with GLOSS_FAKE set to how
// it misbehaves. GLOSS_FAKE_RUNS counts the helpers started, in a file, so
// that a fault can hit only the first.
func TestMain(m *testing.M) {
	if mode := os.Getenv("GLOSS_FAKE"); mode != "" {
		fake(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fake(mode string) {
	n := 1
	if f := os.Getenv("GLOSS_FAKE_RUNS"); f != "" {
		b, _ := os.ReadFile(f)
		n, _ = strconv.Atoi(string(b))
		n++
		os.WriteFile(f, []byte(strconv.Itoa(n)), 0o600)
	}
	first := n == 1
	if first && mode == "deaf" {
		time.Sleep(time.Hour) // never reads, so the pipe fills
	}
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var req map[string]any
		json.Unmarshal(in.Bytes(), &req)
		id := req["id"]
		switch {
		case first && mode == "hang":
			time.Sleep(time.Hour)
		case first && mode == "exit":
			os.Exit(1)
		case first && mode == "garbage":
			fmt.Println("not json")
			continue
		case first && mode == "late":
			// Answer an older request first, as one given up on would.
			fmt.Printf(`{"id":%v,"ok":true,"text":"stale"}`+"\n", id.(float64)-1)
		case mode == "permission":
			fmt.Printf(`{"id":%v,"ok":false,"code":"permission","error":"no access"}`+"\n", id)
			continue
		case mode == "none":
			fmt.Printf(`{"id":%v,"ok":false,"code":"no_selection","error":"empty"}`+"\n", id)
			continue
		}
		switch req["cmd"] {
		case "replace":
			fmt.Printf(`{"id":%v,"ok":true,"method":"paste","verified":true,"us":9}`+"\n", id)
		default:
			fmt.Printf(`{"id":%v,"ok":true,"text":"fresh","method":"ax","us":5,"app":"com.example","editable":"yes","before":"a ","after":" b","token":3}`+"\n", id)
		}
	}
}

func start(t *testing.T, mode string) *Helper {
	t.Helper()
	t.Setenv("GLOSS_FAKE", mode)
	t.Setenv("GLOSS_FAKE_RUNS", filepath.Join(t.TempDir(), "runs"))
	exe, _ := os.Executable()
	h, err := Start(exe)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h
}

func TestSelection(t *testing.T) {
	h := start(t, "ok")
	r, err := h.Selection()
	if err != nil || r.Text != "fresh" || r.App != "com.example" || r.Token != 3 || r.Before != "a " || r.Micros != 5 {
		t.Fatalf("r = %+v, err = %v", r, err)
	}
	rep, err := h.Replace(3, "new")
	if err != nil || rep.Method != "paste" || !rep.Verified || rep.Micros != 9 {
		t.Errorf("replace = %+v, %v", rep, err)
	}
}

func TestFailuresAreTold(t *testing.T) {
	if _, err := start(t, "permission").Selection(); !errors.Is(err, ErrPermission) {
		t.Errorf("permission: %v", err)
	}
	if _, err := start(t, "none").Selection(); !errors.Is(err, ErrNoSelection) {
		t.Errorf("none: %v", err)
	}
}

func TestRecovers(t *testing.T) {
	old := SelectionTimeout
	SelectionTimeout = 200 * time.Millisecond
	defer func() { SelectionTimeout = old }()
	for mode, want := range map[string]error{"hang": ErrTimeout, "exit": ErrExited, "garbage": ErrProtocol} {
		h := start(t, mode)
		if _, err := h.Selection(); !errors.Is(err, want) {
			t.Errorf("%s: first err = %v, want %v", mode, err, want)
		}
		r, err := h.Selection()
		if err != nil || r.Text != "fresh" || h.Restarts != 1 {
			t.Errorf("%s: after restart r = %+v, err = %v, restarts = %d", mode, r, err, h.Restarts)
		}
	}
}

func TestDropsLateAnswers(t *testing.T) {
	h := start(t, "late")
	h.Selection()
	r, err := h.Selection()
	if err != nil || r.Text != "fresh" {
		t.Errorf("took a late answer: %+v, %v", r, err)
	}
}

func TestButtonDownDoesNotWait(t *testing.T) {
	h := start(t, "ok")
	h.mu.Lock()
	defer h.mu.Unlock()
	start := time.Now()
	if h.ButtonDown() || time.Since(start) > 50*time.Millisecond {
		t.Error("waited for a busy helper")
	}
}

// A helper that stops reading fills the pipe: the deadline covers the
// write, and the next request reaches a new helper.
func TestWriteToADeafHelperTimesOut(t *testing.T) {
	h := start(t, "deaf")
	start := time.Now()
	if _, err := h.Replace(3, strings.Repeat("x", 1<<20)); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("took %v", took)
	}
	if r, err := h.Selection(); err != nil || r.Text != "fresh" {
		t.Errorf("after restart: %+v, %v", r, err)
	}
}
