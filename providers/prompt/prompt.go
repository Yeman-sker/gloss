// Package prompt is the action most features are: a prompt template, a
// model, and the kind of result. A feature is a spec in specs/, not code:
// spec.json describes it as core.Descriptor does, system.md and user.md are
// its messages. It is the first shape of a plugin. The action never holds a
// key: it asks the host's Chat for the model, by its own ID.
package prompt

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gloss/core"
)

//go:embed specs
var builtin embed.FS

// Message is one turn of a chat.
type Message struct {
	Role    string
	Content string
}

// Chat streams a model's reply to msgs for the action with the ID given, as
// the host configured that action's model. It returns nil only for a reply
// that finished.
type Chat func(ctx context.Context, actionID string, msgs []Message, delta func(string)) error

// Spec is a feature described by its prompts.
type Spec struct {
	core.Descriptor
	System, User string

	system, user []node
}

type specFile struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Purpose     string    `json:"purpose"`
	Output      string    `json:"output"`
	Replaceable bool      `json:"replaceable"`
	Order       int       `json:"order"`
	Settings    []setting `json:"settings"`
}

type setting struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Default string   `json:"default"`
	Options []string `json:"options"`
	Help    string   `json:"help"`
}

// Builtin returns the specs that ship with Gloss, in their order.
func Builtin() ([]Spec, error) { return Load(builtin, "specs") }

// Load reads every spec under dir: one directory per spec.
func Load(fsys fs.FS, dir string) ([]Spec, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	type ordered struct {
		Spec
		order int
	}
	var all []ordered
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, order, err := loadOne(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s：%w", e.Name(), err)
		}
		all = append(all, ordered{s, order})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].order < all[j].order })
	out := make([]Spec, len(all))
	for i, o := range all {
		out[i] = o.Spec
	}
	return out, nil
}

func loadOne(fsys fs.FS, dir string) (Spec, int, error) {
	raw, err := fs.ReadFile(fsys, path.Join(dir, "spec.json"))
	if err != nil {
		return Spec{}, 0, err
	}
	var f specFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return Spec{}, 0, fmt.Errorf("spec.json：%w", err)
	}
	system, err := fs.ReadFile(fsys, path.Join(dir, "system.md"))
	if err != nil {
		return Spec{}, 0, err
	}
	user, err := fs.ReadFile(fsys, path.Join(dir, "user.md"))
	if err != nil {
		return Spec{}, 0, err
	}
	s := Spec{
		Descriptor: core.Descriptor{
			ID: f.ID, Name: f.Name, Purpose: f.Purpose,
			Output: core.BlockKind(f.Output), Replaceable: f.Replaceable,
		},
		System: string(system),
		User:   string(user),
	}
	for _, st := range f.Settings {
		s.Settings = append(s.Settings, core.Setting{
			Key: st.Key, Label: st.Label, Type: core.SettingType(st.Type), Default: st.Default, Options: st.Options, Help: st.Help,
		})
	}
	return s, f.Order, s.compile()
}

// compile checks the spec and parses its templates.
func (s *Spec) compile() error {
	if s.ID == "" || s.Name == "" || s.Purpose == "" {
		return errors.New("缺少 id、name 或 purpose")
	}
	if s.Output != core.BlockText && s.Output != core.BlockMarkdown {
		return fmt.Errorf("output 应为 text 或 markdown，而不是 %q", s.Output)
	}
	keys := map[string]bool{}
	for _, st := range s.Settings {
		if st.Key == "" || keys[st.Key] {
			return fmt.Errorf("设置 %q 为空或重复", st.Key)
		}
		if _, err := st.Check(st.Default); err != nil {
			return fmt.Errorf("设置 %s 的默认值 %q %v", st.Key, st.Default, err)
		}
		keys[st.Key] = true
	}
	var err error
	if s.system, err = parse(s.System, keys); err != nil {
		return fmt.Errorf("system.md：%w", err)
	}
	if s.user, err = parse(s.User, keys); err != nil {
		return fmt.Errorf("user.md：%w", err)
	}
	return nil
}

// Messages renders the spec for a request.
func (s Spec) Messages(req core.Request) []Message {
	values := map[string]string{
		"text":        req.Text,
		"surrounding": req.Context.Surrounding,
		"source_app":  req.Context.SourceApp,
		"source_lang": req.Context.SourceLang,
		"target_lang": req.Context.TargetLang,
	}
	for _, st := range s.Settings {
		v, ok := req.Settings[st.Key]
		if !ok {
			v = st.Default
		}
		values["setting."+st.Key] = v
	}
	if v := strings.TrimSpace(req.Settings["target_lang"]); v != "" {
		values["target_lang"] = v
	}
	var msgs []Message
	if sys := render(s.system, values); sys != "" {
		msgs = append(msgs, Message{"system", sys})
	}
	return append(msgs, Message{"user", render(s.user, values)})
}

// Action runs a spec.
type Action struct {
	Spec Spec
	Chat Chat
}

func (a Action) ID() string                { return a.Spec.ID }
func (a Action) Name() string              { return a.Spec.Name }
func (a Action) Describe() core.Descriptor { return a.Spec.Descriptor }

func (a Action) Run(ctx context.Context, req core.Request, emit core.Emit) error {
	if a.Chat == nil {
		return errors.New("没有配置模型服务")
	}
	return a.Chat(ctx, a.Spec.ID, a.Spec.Messages(req), func(d string) { emit(0, a.Spec.Output, d) })
}
