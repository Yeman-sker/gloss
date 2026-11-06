package core

import (
	"fmt"
	"strconv"
	"strings"
)

// BlockMarkdown is a result written in Markdown. Its structure is the app's
// to render: an action never splits Markdown into blocks of its own.
const BlockMarkdown BlockKind = "markdown"

// Editable tells whether the place the text came from can be written. It is
// a signal for routing and the panel, never permission to write: Unknown is not
// a yes, and not a no either.
type Editable int

const (
	EditableUnknown Editable = iota
	EditableYes
	EditableNo
)

func (e Editable) String() string {
	switch e {
	case EditableYes:
		return "yes"
	case EditableNo:
		return "no"
	}
	return "unknown"
}

// ParseEditable reads what String writes; anything else is Unknown.
func ParseEditable(s string) Editable {
	switch s {
	case "yes":
		return EditableYes
	case "no":
		return EditableNo
	}
	return EditableUnknown
}

// Descriptor says what an action is for, apart from running it: the router
// reads its purpose, the host its result's kind and settings.
type Descriptor struct {
	// ID names the use, as "translate", never the service behind it. It
	// stays the same across versions: memory and settings are keyed by it.
	ID   string
	Name string
	// Purpose is one sentence on when a user wants this action. Routing
	// shows it to a model choosing between actions.
	Purpose string
	// Output is the kind of the action's main result, block 0: BlockText
	// or BlockMarkdown. Its kind never changes during a run, and deltas
	// only append to it.
	Output BlockKind
	// Replaceable says the result is fit to stand in for the selected text,
	// as a rewritten prompt is and an explanation is not. Whether the
	// selection can be written back is for the host to find out at the
	// time; this only says whether it makes sense to.
	Replaceable bool
	// Settings are what a user can set for this action. The host stores,
	// checks and defaults them, and hands the action only its own.
	Settings []Setting
}

// Describer is an action that describes itself. Actions that do not are
// described by their ID and Name alone, with a text result.
type Describer interface {
	Describe() Descriptor
}

// Describe returns a's descriptor, filling what a plain action cannot say.
func Describe(a Action) Descriptor {
	var d Descriptor
	if x, ok := a.(Describer); ok {
		d = x.Describe()
	}
	if d.ID == "" {
		d.ID = a.ID()
	}
	if d.Name == "" {
		d.Name = a.Name()
	}
	if d.Output == "" {
		d.Output = BlockText
	}
	return d
}

// SettingType is the kind of value a setting holds.
type SettingType string

const (
	SettingText   SettingType = "text"
	SettingBool   SettingType = "bool"
	SettingChoice SettingType = "choice"
	SettingNumber SettingType = "number"
)

// Setting describes one thing a user can set for an action. Values travel
// as strings in their canonical form: "true" or "false", a decimal number,
// one of Options, or any text.
type Setting struct {
	Key     string
	Type    SettingType
	Default string
	Options []string // the values of a choice
	Help    string
}

// Check returns the canonical form of v, or why v does not fit.
func (s Setting) Check(v string) (string, error) {
	v = strings.TrimSpace(v)
	switch s.Type {
	case SettingText:
		return v, nil
	case SettingBool:
		b, err := strconv.ParseBool(v)
		if err != nil {
			return "", fmt.Errorf("应为 true 或 false")
		}
		return strconv.FormatBool(b), nil
	case SettingNumber:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "", fmt.Errorf("应为数字")
		}
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	case SettingChoice:
		for _, o := range s.Options {
			if v == o {
				return v, nil
			}
		}
		return "", fmt.Errorf("应为 %s 之一", strings.Join(s.Options, "、"))
	}
	return "", fmt.Errorf("未知的设置类型 %q", s.Type)
}
