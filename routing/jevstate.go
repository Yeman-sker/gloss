package routing

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"gloss/core"
	"gloss/memory"
)

// The rich state is an experiment: what Jev reads when it, not the rules,
// is the judge. It adds the text around the selection, the app's name and
// kind, the rules' scores as hints, preferences in English, and criteria
// that say when to choose each built-in feature and when not. Only
// routebench uses it for now; docs/research/2026-11-08-jev-context.md
// records why and what is measured.

// maxAround bounds the text kept on each side of the selection.
const maxAround = 200

func (j Jev) richState(in Input) string {
	s := in.Shape
	if s.Chars == 0 && in.Text != "" {
		s = memory.ShapeOf(in.Text)
		in.Shape = s
	}
	lang := or(in.TargetLang, "zh-Hans")
	var b strings.Builder
	b.WriteString("The user selected text in another app and pressed Gloss's single shortcut. Gloss must pick the feature they want, without asking them.\n\n")
	fmt.Fprintf(&b, "Selected text:\n<<<\n%s\n>>>\n\n", clip(in.Text, maxSent))
	before, after := tail(in.Before, maxAround), head(in.After, maxAround)
	if strings.TrimSpace(before+after) != "" {
		fmt.Fprintf(&b, "Text around it in the same field or page, the selection shown as [SELECTION]:\n<<<\n%s[SELECTION]%s\n>>>\n\n", before, after)
	} else {
		b.WriteString("Text around it: not available.\n\n")
	}
	fmt.Fprintf(&b, "App: %s\n", appLabel(in.App))
	if j.SendTitle && in.Title != "" {
		fmt.Fprintf(&b, "Window title: %s\n", clip(in.Title, 120))
	}
	fmt.Fprintf(&b, "Where: %s\n", where(in.Editable))
	fmt.Fprintf(&b, "Language: the user reads and writes %s; the selection is %s.\n", lang, script(s, lang))
	fmt.Fprintf(&b, "Shape: %d characters, %d lines, %s", s.Chars, s.Lines, kind(s.Class()))
	if s.Structured {
		b.WriteString(", Markdown or code structure")
	}
	if s.AgentMarks > 0 {
		fmt.Fprintf(&b, ", %d phrases typical of an agent's report", s.AgentMarks)
	}
	if s.AskMarks > 0 {
		fmt.Fprintf(&b, ", %d phrases typical of a request to an assistant", s.AskMarks)
	}
	if s.Question {
		b.WriteString(", ends with a question mark")
	}
	b.WriteString("\n")
	var hints []string
	if !j.NoHints {
		scores, _ := Rules(in)
		for _, c := range in.Candidates {
			hints = append(hints, fmt.Sprintf("%s %.2f", c.ID, scores[c.ID]))
		}
	}
	if len(hints) > 0 {
		fmt.Fprintf(&b, "Scores from simple keyword rules (0 to 1; hints only, often wrong when the text is ambiguous): %s\n", strings.Join(hints, ", "))
	}
	fmt.Fprintf(&b, "\nThis user's history (explicit choices only; no text is kept):\n%s\n", history(in.Prefs))
	return b.String()
}

// guides say when to choose each built-in feature, and when not, for the
// rich criteria. A feature without one is described by its purpose alone.
var guides = map[string]string{
	"translate": "Choose when the selection is in a language the user does not read well and they want it in their own language: foreign words, sentences or passages they are reading. Not when a foreign term sits inside text in the user's own language (they want what it means: explain), not when it is an agent's report (agent), and not when it is a request they are writing to an AI (prompt).",
	"explain":   "Choose when the user can read the words but wants to know what they mean here: a term, name, slang, meme or sentence in their own language, or a foreign term used inside text in their own language. Usually short, usually in something they are reading.",
	"prompt":    "Choose when the selection is a request or instruction the user is writing to an AI assistant, usually in an input box and not yet sent, in any language. Not ordinary writing such as notes, emails or documents, even when editable.",
	"agent":     "Choose when the selection is a long report an AI agent wrote after a task: what it changed, files, commands, tests, what is left. Usually read-only, in a chat with an assistant, a terminal or a code editor. Not a short phrase picked out of such a report.",
}

func (j Jev) criteria(in Input) map[string]string {
	out := map[string]string{}
	for _, c := range in.Candidates {
		text := c.Name + "：" + c.Purpose
		if g := guides[c.ID]; j.Rich && g != "" {
			text += "\n" + g
		}
		out[c.ID] = text
	}
	return out
}

// apps names well-known apps and what they are, so Jev need not guess
// from a bundle id.
var apps = map[string]string{
	"com.anthropic.claudefordesktop": "Claude (a chat with an AI assistant)",
	"com.openai.chat":                "ChatGPT (a chat with an AI assistant)",
	"com.openai.codex":               "Codex (a coding agent)",
	"com.apple.Terminal":             "Terminal (where coding agents often run)",
	"com.googlecode.iterm2":          "iTerm2 (a terminal, where coding agents often run)",
	"com.mitchellh.ghostty":          "Ghostty (a terminal, where coding agents often run)",
	"dev.warp.Warp-Stable":           "Warp (a terminal, where coding agents often run)",
	"com.microsoft.VSCode":           "VS Code (a code editor)",
	"com.todesktop.230313mzl4w4u92":  "Cursor (a code editor with an AI agent)",
	"dev.zed.Zed":                    "Zed (a code editor)",
	"com.apple.Safari":               "Safari (a web browser)",
	"com.google.Chrome":              "Chrome (a web browser)",
	"company.thebrowser.Browser":     "Arc (a web browser)",
	"com.apple.mail":                 "Mail",
	"com.apple.Notes":                "Notes",
	"com.apple.TextEdit":             "TextEdit (a text editor)",
	"com.apple.Preview":              "Preview (a PDF and image viewer)",
	"com.apple.iBooksX":              "Books (an e-book reader)",
	"com.tencent.xinWeChat":          "WeChat (a chat app)",
	"com.tinyspeck.slackmacgap":      "Slack (a chat app)",
	"ru.keepcoder.Telegram":          "Telegram (a chat app)",
	"com.ss.iphone.ugc.Aweme":        "Douyin (short videos and comments)",
	"md.obsidian":                    "Obsidian (a notes app)",
	"notion.id":                      "Notion (notes and documents)",
}

func appLabel(id string) string {
	if id == "" {
		return "unknown"
	}
	if name, ok := apps[id]; ok {
		return name + ", " + id
	}
	return id
}

func where(e core.Editable) string {
	switch e {
	case core.EditableYes:
		return "an editable field: the user is writing here (an input box, editor or chat composer)"
	case core.EditableNo:
		return "read-only content the user is reading"
	}
	return "not known whether the selection is editable"
}

func script(s memory.Shape, lang string) string {
	switch {
	case isForeign(s, lang):
		return "in a language other than the user's"
	case s.Script == "mixed":
		return "a mix of the user's language and a foreign one"
	case s.Script == "other":
		return "mostly symbols, numbers or another script"
	}
	return "in the user's own language"
}

func kind(class string) string {
	switch class {
	case "term":
		return "a single word or term"
	case "sentence":
		return "a sentence or two"
	case "structured":
		return "structured text"
	}
	return "a passage"
}

// history says what the snapshot says, in English for Jev.
func history(p memory.Snapshot) string {
	var lines []string
	for _, sc := range []struct {
		name string
		p    memory.Preference
	}{{"same app and same kind of text", p.Similar}, {"same app", p.App}, {"everywhere", p.All}} {
		if len(sc.p.Shares) == 0 {
			continue
		}
		var parts []string
		for _, id := range sortedShares(sc.p.Shares) {
			parts = append(parts, fmt.Sprintf("%s %.0f%%", id, 100*sc.p.Shares[id]))
		}
		note := fmt.Sprintf("%d explicit choices", sc.p.Samples)
		if sc.p.Insufficient {
			note += ", too few to rely on"
		}
		if sc.p.Conflicting {
			note += ", mixed"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s (%s)", sc.name, strings.Join(parts, ", "), note))
	}
	if len(p.Recent) > 0 {
		lines = append(lines, "- latest explicit choices for this kind of text in this app, newest first: "+strings.Join(p.Recent, ", "))
	}
	if len(p.Corrections) > 0 {
		var cs []string
		for _, c := range p.Corrections {
			cs = append(cs, c.From+" → "+c.To)
		}
		lines = append(lines, "- the user switched features in this app, newest first: "+strings.Join(cs, ", "))
	}
	if len(lines) == 0 {
		return "none yet"
	}
	return strings.Join(lines, "\n")
}

func sortedShares(m map[string]float64) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		for k := i; k > 0 && (m[ids[k]] > m[ids[k-1]] || m[ids[k]] == m[ids[k-1]] && ids[k] < ids[k-1]); k-- {
			ids[k], ids[k-1] = ids[k-1], ids[k]
		}
	}
	return ids
}

// tail keeps the last n runes of s, head the first n.
func tail(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return "…" + string(r[len(r)-n:])
}

func head(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
