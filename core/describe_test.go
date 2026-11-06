package core

import (
	"context"
	"testing"
)

type plain struct{}

func (plain) ID() string                               { return "plain" }
func (plain) Name() string                             { return "Plain" }
func (plain) Run(context.Context, Request, Emit) error { return nil }

type described struct{ plain }

func (described) Describe() Descriptor {
	return Descriptor{ID: "explain", Name: "释义", Purpose: "explain", Output: BlockMarkdown}
}

func TestDescribeFillsPlainActions(t *testing.T) {
	d := Describe(plain{})
	if d.ID != "plain" || d.Name != "Plain" || d.Output != BlockText || d.Replaceable {
		t.Errorf("plain = %+v", d)
	}
	if d := Describe(described{}); d.Output != BlockMarkdown || d.Purpose != "explain" {
		t.Errorf("described = %+v", d)
	}
}

func TestSettingCheck(t *testing.T) {
	for _, tt := range []struct {
		s       Setting
		in, out string
		bad     bool
	}{
		{Setting{Type: SettingText}, " hi ", "hi", false},
		{Setting{Type: SettingBool}, "TRUE", "true", false},
		{Setting{Type: SettingBool}, "yes", "", true},
		{Setting{Type: SettingNumber}, "1.50", "1.5", false},
		{Setting{Type: SettingNumber}, "x", "", true},
		{Setting{Type: SettingChoice, Options: []string{"short", "long"}}, "long", "long", false},
		{Setting{Type: SettingChoice, Options: []string{"short", "long"}}, "mid", "", true},
		{Setting{Type: "color"}, "red", "", true},
	} {
		got, err := tt.s.Check(tt.in)
		if (err != nil) != tt.bad || got != tt.out {
			t.Errorf("%s %q = %q, %v", tt.s.Type, tt.in, got, err)
		}
	}
}

func TestEditableRoundTrip(t *testing.T) {
	for _, e := range []Editable{EditableUnknown, EditableYes, EditableNo} {
		if ParseEditable(e.String()) != e {
			t.Errorf("%v does not round-trip", e)
		}
	}
	if ParseEditable("maybe") != EditableUnknown {
		t.Error("unknown text is not Unknown")
	}
}
