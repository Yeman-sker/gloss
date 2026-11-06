package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gloss/core"
)

// Doc is the file as JSON, for Update to change. Keys it does not touch
// stay as they are, but for their order: they come out sorted.
type Doc struct{ m map[string]any }

// Get returns the value at keys, or nil.
func (d *Doc) Get(keys ...string) any {
	var v any = d.m
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

// Set puts v at keys, making the objects on the way.
func (d *Doc) Set(v any, keys ...string) {
	m := d.m
	for _, k := range keys[:len(keys)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	m[keys[len(keys)-1]] = v
}

// Delete removes the value at keys, and the objects it leaves empty.
func (d *Doc) Delete(keys ...string) {
	var del func(m map[string]any, keys []string)
	del = func(m map[string]any, keys []string) {
		if len(keys) == 1 {
			delete(m, keys[0])
			return
		}
		next, ok := m[keys[0]].(map[string]any)
		if !ok {
			return
		}
		del(next, keys[1:])
		if len(next) == 0 {
			delete(m, keys[0])
		}
	}
	del(d.m, keys)
}

// Update changes the file at path with fn, and writes it only when the
// result is a file Read accepts for the features described: a change that
// would break the config returns why, in Read's words, and leaves the file
// as it was. A missing file starts empty; a file that is not JSON is not
// changed. The old format, one service at the top, moves into services
// first. The file is written whole, keeps others from reading it, and
// never shows half written.
func Update(path string, actions []core.Descriptor, fn func(*Doc) error) (*Config, error) {
	d := &Doc{m: map[string]any{}}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("无法读取配置文件 %s：%w", path, err)
	default:
		raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := json.Unmarshal(raw, &d.m); err != nil || d.m == nil {
				return nil, fmt.Errorf("配置文件 %s 不是有效的 JSON，先修好它或删掉它，才能在这里修改", path)
			}
		}
	}
	migrate(d)
	if err := fn(d); err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(d.m, "", "  ")
	if err != nil {
		return nil, err
	}
	out = append(out, '\n')
	cfg, err := parse(path, out, actions)
	if err != nil {
		return nil, err
	}
	if err := writeFile(path, out); err != nil {
		return nil, fmt.Errorf("无法写入配置文件 %s：%w", path, err)
	}
	return cfg, nil
}

func writeFile(path string, out []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// migrate moves the old format's service into services, as Read reads it.
func migrate(d *Doc) {
	_, k := d.m["api_key"]
	_, u := d.m["base_url"]
	_, m := d.m["model"]
	if !k && !u && !m {
		return
	}
	if _, ok := d.m["services"]; ok {
		return // Read rejects this; leave it for the user to see
	}
	s := map[string]any{}
	for _, key := range []string{"api_key", "base_url", "model"} {
		if v, ok := d.m[key]; ok {
			s[key] = v
			delete(d.m, key)
		}
	}
	d.m["services"] = map[string]any{defaultService: s}
	if _, ok := d.m["default_service"]; !ok {
		d.m["default_service"] = defaultService
	}
}

// ServiceName checks a name for a new or renamed service.
func ServiceName(d *Doc, name, old string) error {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return errors.New("服务名不能为空")
	case len([]rune(name)) > 32:
		return errors.New("服务名太长")
	case name != old && d.Get("services", name) != nil:
		return fmt.Errorf("已经有名为 %q 的服务", name)
	}
	return nil
}

// RenameService renames a service and every place that names it.
func RenameService(d *Doc, old, name string) error {
	name = strings.TrimSpace(name)
	if name == old {
		return nil
	}
	if err := ServiceName(d, name, old); err != nil {
		return err
	}
	s := d.Get("services", old)
	if s == nil {
		return fmt.Errorf("没有名为 %q 的服务", old)
	}
	d.Delete("services", old)
	d.Set(s, "services", name)
	if d.Get("default_service") == old {
		d.Set(name, "default_service")
	}
	if d.Get("routing", "jev_service") == old {
		d.Set(name, "routing", "jev_service")
	}
	if as, ok := d.Get("action_settings").(map[string]any); ok {
		for id := range as {
			if d.Get("action_settings", id, "service") == old {
				d.Set(name, "action_settings", id, "service")
			}
		}
	}
	return nil
}

// DeleteService removes a service and every place that names it: the
// features that ran on it run on the default, and routing loses Jev. The
// default moves to another service, if there is one.
func DeleteService(d *Doc, name string) {
	d.Delete("services", name)
	if d.Get("default_service") == name {
		d.Delete("default_service")
		if ss, ok := d.Get("services").(map[string]any); ok {
			for _, n := range sortedKeys(ss) {
				d.Set(n, "default_service")
				break
			}
		}
	}
	if d.Get("routing", "jev_service") == name {
		d.Set("", "routing", "jev_service")
	}
	if as, ok := d.Get("action_settings").(map[string]any); ok {
		for id := range as {
			if d.Get("action_settings", id, "service") == name {
				d.Delete("action_settings", id, "service")
			}
		}
	}
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ActionService is the service and model the file names for a feature,
// empty where it leaves them to the default.
func (c *Config) ActionService(id string) (service, model string) {
	ac := c.actions[id]
	return ac.service, ac.model
}

// ActionValue is the value the file sets for a feature's setting, and
// whether it sets one.
func (c *Config) ActionValue(id, key string) (string, bool) {
	v, ok := c.actions[id].values[key]
	return v, ok
}
