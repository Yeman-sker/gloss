package settings

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gloss/core"
)

const key = "sk-secret-123"

var descs = []core.Descriptor{
	{ID: "translate", Settings: []core.Setting{{Key: "target_lang", Type: core.SettingText}}},
	{ID: "explain", Settings: []core.Setting{
		{Key: "depth", Type: core.SettingChoice, Default: "简短", Options: []string{"简短", "详细"}},
		{Key: "emoji", Type: core.SettingBool, Default: "false"},
	}},
}

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), File)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOldFormatStillWorks(t *testing.T) {
	c, err := Read(write(t, "\xef\xbb\xbf"+`{"api_key":" `+key+`\n","model":"m","target_lang":"en"}`), descs)
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Service("translate")
	if err != nil || s.APIKey != key || s.Model != "m" || s.BaseURL != DefaultBaseURL {
		t.Errorf("service = %+v, %v", s, err)
	}
	if !c.Migrated || c.TargetLang != "en" || c.Shortcut != DefaultShortcut {
		t.Errorf("config = %+v", c)
	}
	if !c.Memory.Enabled || !c.Routing.Enabled || len(c.Actions) != 2 {
		t.Errorf("defaults = %+v %+v %v", c.Memory, c.Routing, c.Actions)
	}
}

func TestTwoLayers(t *testing.T) {
	c, err := Read(write(t, `{
		"services": {
			"main": {"api_key": "`+key+`", "model": "small"},
			"big":  {"api_key": "other", "base_url": "http://x/v1", "model": "large"},
			"jev":  {"api_key": "j"}
		},
		"default_service": "main",
		"memory": {"enabled": false, "retention_days": 7},
		"routing": {"jev_service": "jev", "wait_ms": 600},
		"actions": ["explain", "translate", "explain"],
		"action_settings": {
			"explain": {"service": "big", "depth": "详细", "emoji": true},
			"translate": {"model": "tiny"}
		}
	}`), descs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.Actions, ",") != "explain,translate" {
		t.Errorf("actions = %v", c.Actions)
	}
	if s, _ := c.Service("explain"); s.Model != "large" || s.BaseURL != "http://x/v1" {
		t.Errorf("explain service = %+v", s)
	}
	if s, _ := c.Service("translate"); s.Model != "tiny" || s.APIKey != key {
		t.Errorf("translate service = %+v", s)
	}
	v := c.Values(descs[1])
	if v["depth"] != "详细" || v["emoji"] != "true" || len(v) != 2 {
		t.Errorf("explain values = %v", v)
	}
	// A feature sees its own settings, never another's, the service names or a key.
	tv := c.Values(descs[0])
	if len(tv) != 1 || tv["target_lang"] != "" {
		t.Errorf("translate values = %v", tv)
	}
	for _, val := range v {
		if strings.Contains(val, key) {
			t.Error("a value holds the key")
		}
	}
	if c.Memory.Enabled || c.Memory.RetentionDays != 7 || c.Memory.MaxRecords != 5000 {
		t.Errorf("memory = %+v", c.Memory)
	}
	j, ok := c.Jev()
	if !ok || j.BaseURL != DefaultJevURL || j.Model != DefaultJevModel || c.Routing.WaitMillis != 600 {
		t.Errorf("jev = %+v %v %+v", j, ok, c.Routing)
	}
}

func TestErrorsNameTheItem(t *testing.T) {
	for content, want := range map[string]string{
		"{\n  \"services\": {\n    \"main\": {\"api_key\": \"" + key + "\",}\n  }\n}": "第 3 行",
		`{"memory": {"enabled": "yes"}}`:                                              "memory.enabled 类型不对",
		`{"memroy": {}}`:                                                              `未知的配置项 "memroy"`,
		`{"api_key": "` + key + `", "services": {}}`:                                  "不能和 services 同时出现",
		`{"default_service": "nope"}`:                                                 `default_service 指向不存在的服务 "nope"`,
		`{"actions": ["dance"]}`:                                                      `未知的功能 "dance"`,
		`{"actions": []}`:                                                             "至少要启用一个功能",
		`{"action_settings": {"explain": {"depth": "中等"}}}`:                           "action_settings.explain.depth 应为 简短、详细 之一",
		`{"action_settings": {"explain": {"emoji": "maybe"}}}`:                        "action_settings.explain.emoji 应为 true 或 false",
		`{"action_settings": {"explain": {"colour": "red"}}}`:                         "不是这个功能的设置项",
		`{"action_settings": {"explain": {"api_key": "` + key + `"}}}`:                "Key 和地址只放在 services 中",
		`{"action_settings": {"explain": {"service": "x"}}}`:                          "指向不存在的服务",
		`{"routing": {"min_confidence": 2}}`:                                          "min_confidence",
		`{"memory": {"max_records": 0}}`:                                              "max_records",
	} {
		_, err := Read(write(t, content), descs)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s\nerr = %v\nwant %q", content, err, want)
			continue
		}
		if strings.Contains(err.Error(), key) {
			t.Errorf("error quotes the key: %v", err)
		}
	}
}

func TestMissingAndIncomplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	c, err := Read(path, descs)
	if !errors.Is(err, ErrMissing) || !strings.Contains(err.Error(), path) || !c.Memory.Enabled {
		t.Errorf("missing: %v", err)
	}
	c, err = Read(write(t, `{"api_key": "`+key+`"}`), descs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Service("translate"); err == nil || !strings.Contains(err.Error(), "缺少 model") {
		t.Errorf("no model: %v", err)
	}
	c, _ = Read(write(t, `{}`), descs)
	if _, err := c.Service("translate"); err == nil || !strings.Contains(err.Error(), "缺少 services") {
		t.Errorf("no service: %v", err)
	}
}

func TestReadRestrictsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	path := write(t, `{}`)
	Read(path, descs)
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
}

func TestSet(t *testing.T) {
	path := write(t, `{"api_key": "`+key+`", "model": "m", "memory": {"retention_days": 3}}`)
	if err := Set(path, false, "memory", "enabled"); err != nil {
		t.Fatal(err)
	}
	c, err := Read(path, descs)
	if err != nil {
		t.Fatal(err)
	}
	if c.Memory.Enabled || c.Memory.RetentionDays != 3 {
		t.Errorf("memory = %+v", c.Memory)
	}
	if s, _ := c.Service("translate"); s.APIKey != key {
		t.Error("lost the key")
	}
	fresh := filepath.Join(t.TempDir(), "sub", File)
	if err := Set(fresh, false, "routing", "enabled"); err != nil {
		t.Fatal(err)
	}
	if c, _ := Read(fresh, descs); c.Routing.Enabled {
		t.Error("new file did not take the value")
	}
	if err := Set(write(t, "{"), true, "x"); err == nil {
		t.Error("rewrote a broken file")
	}
}
