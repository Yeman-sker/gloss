package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gloss/providers/openai"
)

func TestLogTurnsOverAtItsLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "gloss.log")
	w, err := openRotating(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 39) + "\n"
	for i := 0; i < 10; i++ {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	cur, _ := os.Stat(path)
	old, err := os.Stat(path + ".1")
	if err != nil || cur.Size() > 100 || old.Size() > 100 {
		t.Fatalf("current %d, old %v %v", cur.Size(), old, err)
	}
	if cur.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", cur.Mode().Perm())
	}
	if _, err := os.Stat(path + ".2"); err == nil {
		t.Error("kept more than one old file")
	}
}

func TestErrKindKeepsWhatAServiceSaidOut(t *testing.T) {
	err := &openai.StatusError{Code: 401, Message: "you sent: secret text"}
	if got := errKind(err); got != "http 401" || strings.Contains(got, "secret") {
		t.Errorf("kind = %q", got)
	}
}
