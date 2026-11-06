package prompt

import (
	"fmt"
	"strings"
)

// A template is text with {{name}} for a value, {{#name}}…{{/name}} for a
// part kept only when the value is not empty, and {{^name}}…{{/name}} for
// one kept only when it is. Nothing else: templates cannot run code or read
// files, and only the names of vars below exist.
//
// Values come in as they are. Selected text often reads like an instruction,
// so specs keep it between tags that their system prompt calls material.

// vars are the names a template may use, besides setting.<key> for the
// action's own settings.
var vars = map[string]bool{
	"text":        true, // the selection, as the user selected it
	"surrounding": true, // bounded text around it; empty when unknown
	"source_app":  true, // where it was selected, as a bundle id
	"source_lang": true, // empty means detect
	"target_lang": true, // the action's setting when it has one, else the product's
}

type node struct {
	text     string // literal text, when name is empty
	name     string
	section  byte // 0 for a value, '#' or '^' for a section
	children []node
}

// parse checks a template against the names it may use.
func parse(src string, settings map[string]bool) ([]node, error) {
	nodes, rest, err := parseUntil(src, "", settings)
	if err != nil {
		return nil, err
	}
	if rest != "" {
		return nil, fmt.Errorf("多余的内容：%q", rest)
	}
	return nodes, nil
}

func parseUntil(src, closing string, settings map[string]bool) ([]node, string, error) {
	var out []node
	for {
		i := strings.Index(src, "{{")
		if i < 0 {
			if closing != "" {
				return nil, "", fmt.Errorf("{{#%s}} 没有对应的 {{/%s}}", closing, closing)
			}
			if src != "" {
				out = append(out, node{text: src})
			}
			return out, "", nil
		}
		if i > 0 {
			out = append(out, node{text: src[:i]})
		}
		j := strings.Index(src[i:], "}}")
		if j < 0 {
			return nil, "", fmt.Errorf("{{ 没有闭合")
		}
		tag := strings.TrimSpace(src[i+2 : i+j])
		src = src[i+j+2:]
		switch {
		case strings.HasPrefix(tag, "/"):
			name := strings.TrimSpace(tag[1:])
			if name != closing {
				return nil, "", fmt.Errorf("{{/%s}} 没有对应的开头", name)
			}
			return out, src, nil
		case strings.HasPrefix(tag, "#"), strings.HasPrefix(tag, "^"):
			name := strings.TrimSpace(tag[1:])
			if err := known(name, settings); err != nil {
				return nil, "", err
			}
			children, rest, err := parseUntil(src, name, settings)
			if err != nil {
				return nil, "", err
			}
			out = append(out, node{name: name, section: tag[0], children: children})
			src = rest
		default:
			if err := known(tag, settings); err != nil {
				return nil, "", err
			}
			out = append(out, node{name: tag})
		}
	}
}

func known(name string, settings map[string]bool) error {
	if key, ok := strings.CutPrefix(name, "setting."); ok {
		if settings[key] {
			return nil
		}
		return fmt.Errorf("模板用到了未声明的设置 %q", key)
	}
	if vars[name] {
		return nil
	}
	return fmt.Errorf("模板用到了未知的变量 %q", name)
}

func render(nodes []node, values map[string]string) string {
	var b strings.Builder
	write(&b, nodes, values)
	return strings.TrimSpace(b.String())
}

func write(b *strings.Builder, nodes []node, values map[string]string) {
	for _, n := range nodes {
		switch {
		case n.name == "":
			b.WriteString(n.text)
		case n.section == 0:
			b.WriteString(values[n.name])
		case (n.section == '#') == (strings.TrimSpace(values[n.name]) != ""):
			write(b, n.children, values)
		}
	}
}
