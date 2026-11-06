package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestUpdateWritesOnlyValidConfigs(t *testing.T) {
	path := write(t, `{"services":{"a":{"api_key":"`+key+`","model":"m"}},"default_service":"a","shortcut":"Alt+Shift+D"}`)
	before, _ := os.ReadFile(path)
	_, err := Update(path, descs, func(d *Doc) error {
		d.Set("nowhere", "default_service")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "default_service") {
		t.Fatalf("err = %v, want the broken reference", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("a rejected change wrote the file:\n%s", after)
	}
	if strings.Contains(err.Error(), key) {
		t.Errorf("the error shows the key: %v", err)
	}

	cfg, err := Update(path, descs, func(d *Doc) error {
		d.Set("Cmd+Shift+E", "shortcut")
		d.Set("详细", "action_settings", "explain", "depth")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Shortcut != "Cmd+Shift+E" || cfg.Values(descs[1])["depth"] != "详细" {
		t.Errorf("cfg = %+v", cfg)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
		}
	}
	if s, _ := cfg.Service("translate"); s.APIKey != key {
		t.Error("the key was lost")
	}
}

func TestUpdateLeavesBrokenFilesAlone(t *testing.T) {
	path := write(t, `{"services": `)
	if _, err := Update(path, descs, func(d *Doc) error { d.Set("x", "shortcut"); return nil }); err == nil {
		t.Fatal("a file that is not JSON was changed")
	}
	if raw, _ := os.ReadFile(path); string(raw) != `{"services": ` {
		t.Errorf("file = %q", raw)
	}
}

func TestUpdateCreatesAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dir", File)
	cfg, err := Update(path, descs, func(d *Doc) error {
		d.Set(map[string]any{"api_key": key, "model": "m"}, "services", "main")
		d.Set("main", "default_service")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultService != "main" {
		t.Errorf("default = %q", cfg.DefaultService)
	}
}

func TestUpdateMovesTheOldFormatIntoServices(t *testing.T) {
	path := write(t, `{"api_key":"`+key+`","model":"m"}`)
	cfg, err := Update(path, descs, func(d *Doc) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Migrated || cfg.DefaultService != "main" {
		t.Errorf("cfg = %+v", cfg)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), `"api_key": "`+key+`",`+"\n  \"default") {
		t.Errorf("old keys stayed at the top:\n%s", raw)
	}
}

func TestRenameAndDeleteServiceFollowReferences(t *testing.T) {
	path := write(t, `{
		"services": {"a": {"api_key": "k", "model": "m"}, "b": {"api_key": "k", "model": "m"}},
		"default_service": "a",
		"routing": {"jev_service": "a"},
		"action_settings": {"explain": {"service": "a", "depth": "详细"}}
	}`)
	cfg, err := Update(path, descs, func(d *Doc) error { return RenameService(d, "a", "c") })
	if err != nil {
		t.Fatal(err)
	}
	if svc, _ := cfg.ActionService("explain"); cfg.DefaultService != "c" || cfg.Routing.JevService != "c" || svc != "c" {
		t.Errorf("after rename: %+v", cfg)
	}
	if _, err := Update(path, descs, func(d *Doc) error { return RenameService(d, "c", "b") }); err == nil {
		t.Error("renamed onto another service")
	}
	cfg, err = Update(path, descs, func(d *Doc) error { DeleteService(d, "c"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if svc, _ := cfg.ActionService("explain"); cfg.DefaultService != "b" || cfg.Routing.JevService != "" || svc != "" {
		t.Errorf("after delete: %+v", cfg)
	}
	if cfg.Values(descs[1])["depth"] != "详细" {
		t.Error("the feature's own setting went with the service")
	}
}
