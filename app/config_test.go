package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gloss/core"
)

// write puts a config file in a temporary directory and returns its path.
func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), configFile)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadConfig(t *testing.T) {
	cfg, err := readConfig(write(t, `{
		"api_key": " test-key\n",
		"base_url": "http://localhost:1/v1",
		"model": "test-model",
		"target_lang": "en"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	want := config{APIKey: "test-key", BaseURL: "http://localhost:1/v1", Model: "test-model", TargetLang: "en"}
	if cfg != want {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func TestReadConfigDefaults(t *testing.T) {
	cfg, err := readConfig(write(t, `{"api_key": "k", "model": "m"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != defaultBaseURL || cfg.TargetLang != defaultTargetLang {
		t.Errorf("got %+v", cfg)
	}
}

func TestReadConfigBOM(t *testing.T) {
	cfg, err := readConfig(write(t, "\xef\xbb\xbf"+`{"api_key": "k", "model": "m"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "k" {
		t.Errorf("got %+v", cfg)
	}
}

// Every error has to tell the user which file to edit and what is wrong.
func TestReadConfigErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		content string
		want    string
	}{
		"no key":       {`{"model": "m"}`, "缺少 api_key"},
		"blank key":    {`{"api_key": "  ", "model": "m"}`, "缺少 api_key"},
		"no model":     {`{"api_key": "k"}`, "缺少 model"},
		"nothing":      {`{}`, "缺少 api_key 和 model"},
		"not json":     {`{"api_key": "secret-value",}`, "不是有效的 JSON"},
		"wrong type":   {`{"api_key": 1, "model": "m"}`, "不是有效的 JSON"},
		"wrong layout": {`[]`, "不是有效的 JSON"},
	} {
		t.Run(name, func(t *testing.T) {
			path := write(t, tc.content)
			_, err := readConfig(path)
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), path) {
				t.Errorf("got %q, want %q and the path", err, tc.want)
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Errorf("error quotes the key: %q", err)
			}
		})
	}
}

func TestReadConfigMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), configFile)
	_, err := readConfig(path)
	if err == nil || !strings.Contains(err.Error(), "缺少配置文件") || !strings.Contains(err.Error(), path) {
		t.Errorf("got %v", err)
	}
}

// useConfigDir makes os.UserConfigDir return a temporary directory and
// returns where the config file belongs in it, spelled out per platform.
func useConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", dir)
		return filepath.Join(dir, Name, configFile)
	case "darwin":
		t.Setenv("HOME", dir)
		return filepath.Join(dir, "Library", "Application Support", Name, configFile)
	default:
		t.Setenv("XDG_CONFIG_HOME", dir)
		return filepath.Join(dir, Name, configFile)
	}
}

func TestConfigPath(t *testing.T) {
	want := useConfigDir(t)
	got, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// The file decides the request, and is read again on every run.
func TestTranslatorUsesConfig(t *testing.T) {
	path := useConfigDir(t)
	run := func() error {
		return translator{}.Run(context.Background(), core.Request{Text: "hello"}, func(int, core.BlockKind, string) {})
	}
	if err := run(); err == nil || !strings.Contains(err.Error(), "缺少配置文件") {
		t.Fatalf("without a file: %v", err)
	}

	var auth string
	var body struct {
		Model    string `json:"model"`
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"你好\"},\"finish_reason\":\"stop\"}]}\n\n"))
	}))
	defer srv.Close()

	raw, err := json.Marshal(config{APIKey: "test-key", BaseURL: srv.URL, Model: "test-model", TargetLang: "ja"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer test-key" || body.Model != "test-model" {
		t.Errorf("auth %q, model %q", auth, body.Model)
	}
	if len(body.Messages) == 0 || !strings.Contains(body.Messages[0].Content, "into ja.") {
		t.Errorf("target language not sent: %+v", body.Messages)
	}
}
