// Package settings reads the file a user configures Gloss with. It has two
// layers: the product's (model services and their keys, the shortcut,
// memory, routing, which features are on) and each feature's own, which the
// feature declares in its core.Descriptor and the host checks and defaults.
// Keys live only in the product layer: a feature names a service, and only
// the host turns that name into a client. See docs/decisions/0008.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gloss/core"
)

const (
	File = "config.json"

	DefaultBaseURL    = "https://api.openai.com/v1"
	DefaultJevURL     = "https://api.typesafe.ai/v1"
	DefaultJevModel   = "jev-latest"
	DefaultTargetLang = "zh-Hans"
	DefaultShortcut   = "Alt+Shift+D"
	defaultService    = "main"
)

// Service is one model service. Its key never leaves this package except
// through Config.Service, which only the host calls.
type Service struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

// Memory is how Gloss remembers its use.
type Memory struct {
	Enabled       bool `json:"enabled"`
	RetentionDays int  `json:"retention_days"`
	MaxRecords    int  `json:"max_records"`
}

// Routing is how Gloss picks a feature for a selection.
type Routing struct {
	Enabled bool `json:"enabled"`
	// JevService names the service that answers as Jev does; empty turns
	// Jev off and leaves routing to rules and memory.
	JevService    string  `json:"jev_service"`
	WaitMillis    int     `json:"wait_ms"`
	MinConfidence float64 `json:"min_confidence"`
	// SendWindowTitle lets the window title go to Jev, which it does not
	// by default.
	SendWindowTitle bool `json:"send_window_title"`
}

// Config is the whole file, checked, with defaults in place.
type Config struct {
	Path           string
	Services       map[string]Service
	DefaultService string
	Shortcut       string
	TargetLang     string
	LaunchAtLogin  bool
	Memory         Memory
	Routing        Routing
	// Actions are the features that are on, in their order.
	Actions []string
	// Migrated says the file was in the format of before services, which
	// still works as it is.
	Migrated bool

	actions map[string]actionConfig
}

type actionConfig struct {
	service, model string
	values         map[string]string
}

// file is what the user writes. Pointers tell a value left out from a
// zero.
type file struct {
	// The format of before: one service at the top.
	APIKey  *string `json:"api_key"`
	BaseURL *string `json:"base_url"`
	Model   *string `json:"model"`

	Services       map[string]Service `json:"services"`
	DefaultService string             `json:"default_service"`
	Shortcut       string             `json:"shortcut"`
	TargetLang     string             `json:"target_lang"`
	LaunchAtLogin  bool               `json:"launch_at_login"`
	Memory         *struct {
		Enabled       *bool `json:"enabled"`
		RetentionDays *int  `json:"retention_days"`
		MaxRecords    *int  `json:"max_records"`
	} `json:"memory"`
	Routing *struct {
		Enabled         *bool    `json:"enabled"`
		JevService      string   `json:"jev_service"`
		WaitMillis      *int     `json:"wait_ms"`
		MinConfidence   *float64 `json:"min_confidence"`
		SendWindowTitle bool     `json:"send_window_title"`
	} `json:"routing"`
	Actions        []string                   `json:"actions"`
	ActionSettings map[string]json.RawMessage `json:"action_settings"`
}

// Path is where the file lives: Application Support on macOS, %AppData% on
// Windows, $XDG_CONFIG_HOME or ~/.config on Linux.
func Path(app string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("找不到配置目录：%w", err)
	}
	return filepath.Join(dir, app, File), nil
}

// ErrMissing is a file that does not exist.
var ErrMissing = errors.New("缺少配置文件")

// Defaults is the config of a file that says nothing.
func Defaults(path string, actions []core.Descriptor) *Config {
	c := &Config{
		Path:       path,
		Services:   map[string]Service{},
		Shortcut:   DefaultShortcut,
		TargetLang: DefaultTargetLang,
		Memory:     Memory{Enabled: true, RetentionDays: 90, MaxRecords: 5000},
		Routing:    Routing{Enabled: true, WaitMillis: 800, MinConfidence: 0.6},
		actions:    map[string]actionConfig{},
	}
	for _, d := range actions {
		c.Actions = append(c.Actions, d.ID)
	}
	return c
}

// Read reads and checks the file at path for the features described. Its
// errors are for the panel: they name the file and the item to fix, and
// never quote a value, as one may be a key. A missing file returns
// ErrMissing with Defaults.
func Read(path string, actions []core.Descriptor) (*Config, error) {
	c := Defaults(path, actions)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, fmt.Errorf("%w %s，请创建它并填写模型服务", ErrMissing, path)
	}
	if err != nil {
		return c, fmt.Errorf("无法读取配置文件 %s：%w", path, err)
	}
	restrict(path)
	// Some Windows editors start UTF-8 files with a BOM, which JSON rejects.
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	var f file
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return c, fmt.Errorf("配置文件 %s %s", path, describeJSONError(raw, err))
	}
	if err := c.apply(&f, actions); err != nil {
		return c, fmt.Errorf("配置文件 %s 中 %w", path, err)
	}
	return c, nil
}

// restrict keeps others from reading the keys. Gloss keeps them in this
// file rather than the keychain: see docs/decisions/0008.
func restrict(path string) {
	if runtime.GOOS == "windows" {
		return
	}
	if fi, err := os.Stat(path); err == nil && fi.Mode().Perm()&0o077 != 0 {
		_ = os.Chmod(path, fi.Mode().Perm()&0o700)
	}
}

func describeJSONError(raw []byte, err error) string {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		line, col := position(raw, syn.Offset)
		return fmt.Sprintf("第 %d 行第 %d 列不是有效的 JSON：%v", line, col, syn)
	case errors.As(err, &typ):
		return fmt.Sprintf("的 %s 类型不对：应为 %s", typ.Field, typeName(typ.Type.Kind().String()))
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		return fmt.Sprintf("有未知的配置项 %s", strings.TrimPrefix(err.Error(), "json: unknown field "))
	}
	return "不是有效的 JSON：" + err.Error()
}

func typeName(kind string) string {
	switch kind {
	case "string":
		return "文字"
	case "bool":
		return "true 或 false"
	case "int", "float64":
		return "数字"
	case "slice":
		return "列表"
	case "map", "struct":
		return "对象"
	}
	return kind
}

func position(raw []byte, offset int64) (line, col int) {
	line, col = 1, 1
	for i := int64(0); i < offset && i < int64(len(raw)); i++ {
		if raw[i] == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
	}
	return line, col
}

func (c *Config) apply(f *file, actions []core.Descriptor) error {
	if f.APIKey != nil || f.BaseURL != nil || f.Model != nil {
		if f.Services != nil {
			return errors.New("旧格式的 api_key、base_url、model 不能和 services 同时出现，请把它们移进 services")
		}
		c.Migrated = true
		f.Services = map[string]Service{defaultService: {BaseURL: deref(f.BaseURL), APIKey: deref(f.APIKey), Model: deref(f.Model)}}
		if f.DefaultService == "" {
			f.DefaultService = defaultService
		}
	}
	for name, s := range f.Services {
		s.BaseURL = strings.TrimSpace(s.BaseURL)
		s.APIKey = strings.TrimSpace(s.APIKey)
		s.Model = strings.TrimSpace(s.Model)
		if s.BaseURL == "" {
			s.BaseURL = DefaultBaseURL
			if f.Routing != nil && name == f.Routing.JevService {
				s.BaseURL = DefaultJevURL
			}
		}
		if s.Model == "" && f.Routing != nil && name == f.Routing.JevService {
			s.Model = DefaultJevModel
		}
		c.Services[name] = s
	}
	c.DefaultService = f.DefaultService
	if c.DefaultService == "" && len(c.Services) == 1 {
		for name := range c.Services {
			c.DefaultService = name
		}
	}
	if c.DefaultService != "" {
		if _, ok := c.Services[c.DefaultService]; !ok {
			return fmt.Errorf("default_service 指向不存在的服务 %q", c.DefaultService)
		}
	}
	if v := strings.TrimSpace(f.Shortcut); v != "" {
		c.Shortcut = v
	}
	if v := strings.TrimSpace(f.TargetLang); v != "" {
		c.TargetLang = v
	}
	c.LaunchAtLogin = f.LaunchAtLogin

	if m := f.Memory; m != nil {
		if m.Enabled != nil {
			c.Memory.Enabled = *m.Enabled
		}
		if m.RetentionDays != nil {
			if *m.RetentionDays < 1 {
				return errors.New("memory.retention_days 应为正整数")
			}
			c.Memory.RetentionDays = *m.RetentionDays
		}
		if m.MaxRecords != nil {
			if *m.MaxRecords < 1 {
				return errors.New("memory.max_records 应为正整数")
			}
			c.Memory.MaxRecords = *m.MaxRecords
		}
	}
	if r := f.Routing; r != nil {
		if r.Enabled != nil {
			c.Routing.Enabled = *r.Enabled
		}
		c.Routing.JevService = strings.TrimSpace(r.JevService)
		if c.Routing.JevService != "" {
			if _, ok := c.Services[c.Routing.JevService]; !ok {
				return fmt.Errorf("routing.jev_service 指向不存在的服务 %q", c.Routing.JevService)
			}
		}
		if r.WaitMillis != nil {
			if *r.WaitMillis < 50 || *r.WaitMillis > 10000 {
				return errors.New("routing.wait_ms 应在 50 到 10000 之间")
			}
			c.Routing.WaitMillis = *r.WaitMillis
		}
		if r.MinConfidence != nil {
			if *r.MinConfidence < 0 || *r.MinConfidence > 1 {
				return errors.New("routing.min_confidence 应在 0 到 1 之间")
			}
			c.Routing.MinConfidence = *r.MinConfidence
		}
		c.Routing.SendWindowTitle = r.SendWindowTitle
	}

	known := map[string]core.Descriptor{}
	for _, d := range actions {
		known[d.ID] = d
	}
	if f.Actions != nil {
		seen := map[string]bool{}
		c.Actions = nil
		for _, id := range f.Actions {
			if _, ok := known[id]; !ok {
				return fmt.Errorf("actions 中有未知的功能 %q（可用：%s）", id, ids(actions))
			}
			if !seen[id] {
				seen[id] = true
				c.Actions = append(c.Actions, id)
			}
		}
		if len(c.Actions) == 0 {
			return errors.New("actions 至少要启用一个功能")
		}
	}

	names := make([]string, 0, len(f.ActionSettings))
	for id := range f.ActionSettings {
		names = append(names, id)
	}
	sort.Strings(names)
	for _, id := range names {
		d, ok := known[id]
		if !ok {
			return fmt.Errorf("action_settings 中有未知的功能 %q", id)
		}
		var values map[string]any
		if err := json.Unmarshal(f.ActionSettings[id], &values); err != nil {
			return fmt.Errorf("action_settings.%s 应为对象", id)
		}
		ac := actionConfig{values: map[string]string{}}
		for key, v := range values {
			path := "action_settings." + id + "." + key
			switch key {
			case "service", "model":
				s, ok := v.(string)
				if !ok {
					return fmt.Errorf("%s 应为文字", path)
				}
				if key == "service" {
					if _, ok := c.Services[s]; !ok {
						return fmt.Errorf("%s 指向不存在的服务 %q", path, s)
					}
					ac.service = s
				} else {
					ac.model = strings.TrimSpace(s)
				}
				continue
			case "api_key", "base_url":
				return fmt.Errorf("%s 不能写在功能设置里，Key 和地址只放在 services 中", path)
			}
			st, ok := setting(d, key)
			if !ok {
				return fmt.Errorf("%s 不是这个功能的设置项（可用：%s）", path, settingKeys(d))
			}
			canon, err := st.Check(scalar(v))
			if err != nil {
				return fmt.Errorf("%s %v", path, err)
			}
			ac.values[key] = canon
		}
		c.actions[id] = ac
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool, float64:
		return fmt.Sprint(x)
	}
	return "\x00" // fails every check but text's
}

func setting(d core.Descriptor, key string) (core.Setting, bool) {
	for _, s := range d.Settings {
		if s.Key == key {
			return s, true
		}
	}
	return core.Setting{}, false
}

func ids(ds []core.Descriptor) string {
	var out []string
	for _, d := range ds {
		out = append(out, d.ID)
	}
	return strings.Join(out, "、")
}

func settingKeys(d core.Descriptor) string {
	out := []string{"service", "model"}
	for _, s := range d.Settings {
		out = append(out, s.Key)
	}
	return strings.Join(out, "、")
}

// Values are the settings of the feature d: what the file sets, checked, and
// the defaults for the rest. A feature gets these and nothing else.
func (c *Config) Values(d core.Descriptor) map[string]string {
	out := map[string]string{}
	set := c.actions[d.ID].values
	for _, s := range d.Settings {
		if v, ok := set[s.Key]; ok {
			out[s.Key] = v
		} else {
			out[s.Key] = s.Default
		}
	}
	return out
}

// Service is the model service and model a feature runs on, with its key:
// the feature's own choice, or the default service. Only the host calls it.
func (c *Config) Service(actionID string) (Service, error) {
	ac := c.actions[actionID]
	name := ac.service
	if name == "" {
		name = c.DefaultService
	}
	if name == "" {
		return Service{}, fmt.Errorf("配置文件 %s 缺少 services，或没有指定 default_service", c.Path)
	}
	s := c.Services[name]
	if ac.model != "" {
		s.Model = ac.model
	}
	var missing []string
	if s.APIKey == "" {
		missing = append(missing, "api_key")
	}
	if s.Model == "" {
		missing = append(missing, "model")
	}
	if len(missing) > 0 {
		where := "services." + name
		if c.Migrated {
			where = "顶层"
		}
		return Service{}, fmt.Errorf("配置文件 %s 的 %s 缺少 %s", c.Path, where, strings.Join(missing, " 和 "))
	}
	return s, nil
}

// Enabled says whether the feature is on.
func (c *Config) Enabled(id string) bool {
	for _, a := range c.Actions {
		if a == id {
			return true
		}
	}
	return false
}

// Jev is the service that routes as Jev does, when routing uses one.
func (c *Config) Jev() (Service, bool) {
	if !c.Routing.Enabled || c.Routing.JevService == "" {
		return Service{}, false
	}
	s := c.Services[c.Routing.JevService]
	return s, s.APIKey != ""
}
