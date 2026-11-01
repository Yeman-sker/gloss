package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gloss/core"
	"gloss/providers/openai"
)

const (
	configFile = "config.json"

	defaultBaseURL    = "https://api.openai.com/v1"
	defaultTargetLang = "zh-Hans"
)

// config is the file the user edits by hand. There is no default model:
// models get retired, and every service names them differently.
type config struct {
	APIKey     string `json:"api_key"`
	BaseURL    string `json:"base_url"`
	Model      string `json:"model"`
	TargetLang string `json:"target_lang"`
}

// configPath follows the platform: Application Support on macOS, %AppData% on
// Windows, $XDG_CONFIG_HOME or ~/.config on Linux.
func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("找不到配置目录：%w", err)
	}
	return filepath.Join(dir, Name, configFile), nil
}

// readConfig returns an error fit for the panel: it says which file to edit
// and what to change. It never quotes the file, as that holds the key.
func readConfig(path string) (config, error) {
	var cfg config
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, fmt.Errorf("缺少配置文件 %s，请创建它并填写 api_key 和 model", path)
	}
	if err != nil {
		return cfg, fmt.Errorf("无法读取配置文件 %s：%w", path, err)
	}
	// Some Windows editors start UTF-8 files with a BOM, which JSON rejects.
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("配置文件 %s 不是有效的 JSON：%w", path, err)
	}

	for _, v := range []*string{&cfg.APIKey, &cfg.BaseURL, &cfg.Model, &cfg.TargetLang} {
		*v = strings.TrimSpace(*v)
	}
	var missing []string
	if cfg.APIKey == "" {
		missing = append(missing, "api_key")
	}
	if cfg.Model == "" {
		missing = append(missing, "model")
	}
	if len(missing) > 0 {
		return cfg, fmt.Errorf("配置文件 %s 缺少 %s", path, strings.Join(missing, " 和 "))
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	if cfg.TargetLang == "" {
		cfg.TargetLang = defaultTargetLang
	}
	return cfg, nil
}

// translator reads the config on every run, so a fixed file works on the next
// shortcut without a restart. Its errors reach the panel like any other.
type translator struct{}

func (translator) ID() string   { return "translate" }
func (translator) Name() string { return "翻译" }

func (translator) Run(ctx context.Context, req core.Request, emit core.Emit) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	cfg, err := readConfig(path)
	if err != nil {
		return err
	}
	req.Context.TargetLang = cfg.TargetLang
	return openai.Action{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model}.Run(ctx, req, emit)
}
