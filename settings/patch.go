package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Set changes one value in the file at path, as the tray's switches do,
// and leaves the rest as it is but for its order: keys come out sorted. A
// missing file is created with this value alone. The file keeps its
// permissions, as it holds keys.
func Set(path string, value any, keys ...string) error {
	if len(keys) == 0 {
		return errors.New("settings: no key")
	}
	doc := map[string]any{}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("配置文件 %s 不是有效的 JSON，没有修改它", path)
		}
	}
	m := doc
	for _, k := range keys[:len(keys)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	m[keys[len(keys)-1]] = value
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
