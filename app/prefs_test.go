package app

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAcceleratorNeedsAModifier(t *testing.T) {
	for _, tt := range []struct {
		mods ui.Modifiers
		key  ui.Key
		want string
		ok   bool
	}{
		{ui.Alt | ui.Shift, ui.KeyD, "Alt+Shift+D", true},
		{ui.Super | ui.Ctrl | ui.Alt | ui.Shift, ui.Key1, "Cmd+Ctrl+Alt+Shift+1", true},
		{ui.Super, ui.KeyComma, "Cmd+,", true},
		{0, ui.KeyF5, "F5", true},
		{ui.Shift, ui.KeyD, "", false}, // types a capital D
		{0, ui.KeyD, "", false},
		{ui.Alt, ui.KeyUnknown, "", false}, // a modifier alone
	} {
		got, ok := accelerator(tt.mods, tt.key)
		if got != tt.want || ok != tt.ok {
			t.Errorf("accelerator(%v, %v) = %q, %v; want %q, %v", tt.mods, tt.key, got, ok, tt.want, tt.ok)
		}
	}
}

func TestShortcutLabel(t *testing.T) {
	if !isMac {
		if got := shortcutLabel("Alt+Shift+D"); got != "Alt+Shift+D" {
			t.Errorf("label = %q", got)
		}
		return
	}
	for in, want := range map[string]string{
		"Alt+Shift+D":       "⌥⇧D",
		"Cmd+Ctrl+Space":    "⌃⌘Space",
		"CmdOrCtrl+Shift+k": "⇧⌘K",
		"":                  "未设置",
	} {
		if got := shortcutLabel(in); got != want {
			t.Errorf("shortcutLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVersionMatchesMygoJSON(t *testing.T) {
	raw, err := os.ReadFile("../mygo.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct{ Version string }
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Version != Version {
		t.Errorf("mygo.json says %q, app.Version %q", m.Version, Version)
	}
}
