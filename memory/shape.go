package memory

import (
	"regexp"
	"strings"
	"unicode"
)

// Shape is what a selection looks like, without what it says. Routing
// reads it; memory keeps it instead of the text.
type Shape struct {
	Chars  int    `json:"chars"`
	Words  int    `json:"words"` // Latin words; each Han character counts as one
	Lines  int    `json:"lines"`
	Script string `json:"script"` // latin, cjk, mixed or other
	// Markdown or code: headings, lists, fences, tables.
	Structured bool `json:"structured,omitempty"`
	// Phrases an agent ends a task with: what it did, what it checked,
	// what is left, file paths, commands.
	AgentMarks int `json:"agent_marks,omitempty"`
	// Phrases that ask an assistant to do something.
	AskMarks int  `json:"ask_marks,omitempty"`
	Question bool `json:"question,omitempty"`
}

// Class sorts shapes into a few kinds, for preferences by situation.
func (s Shape) Class() string {
	switch {
	case s.Chars == 0:
		return "empty"
	case s.Lines <= 1 && (s.Script == "cjk" && s.Chars <= 8 || s.Script != "cjk" && s.Words <= 3):
		return "term"
	case s.Chars <= 300 && s.Lines <= 3 && !s.Structured:
		return "sentence"
	case s.Structured || s.AgentMarks >= 2:
		return "structured"
	}
	return "passage"
}

var (
	reStructure = regexp.MustCompile("(?m)^(#{1,6} |\\s*[-*+] |\\s*\\d+[.)] |```|\\|.*\\|)")
	reAgent     = regexp.MustCompile(`(?i)\b(i'?ve|i have|i (added|updated|changed|fixed|created|removed|ran|verified)|done|summary|changes|next steps?|tests? (pass|passed|fail)|verified|not verified|committed|let me know|you (can|may|should|need)|run ` + "`" + `|PR|diff)\b|已(完成|修改|添加|修复|验证|提交)|改动|验证|下一步|需要你|测试(通过|失败)|✅|❌|[\w./-]+\.(go|rs|ts|tsx|js|py|md|json|swift|toml|yaml|yml)\b`)
	reAsk       = regexp.MustCompile(`(?i)^\s*(please|can you|could you|help me|write|create|make|generate|rewrite|translate|summari[sz]e|explain|you are|act as|i want|i need|give me|list|draft|fix|implement|design)\b|\b(you are|act as|step by step|make sure|do not|don't|must|should)\b|请|帮我|你是|给我|写一|生成|改写|总结|列出|要求|需要你|不要|务必`)
)

// ShapeOf measures text.
func ShapeOf(text string) Shape {
	text = strings.TrimSpace(text)
	var s Shape
	if text == "" {
		return s
	}
	var latin, han, other int
	inWord := false
	for _, r := range text {
		s.Chars++
		switch {
		case r >= 0x3400 && r <= 0x9FFF, r >= 0x3040 && r <= 0x30FF, r >= 0xAC00 && r <= 0xD7AF:
			han++
			s.Words++
			inWord = false
		case r < 0x250 && unicode.IsLetter(r):
			latin++
			if !inWord {
				s.Words++
				inWord = true
			}
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if r < 0x250 && !inWord {
				s.Words++
				inWord = true
			}
			if unicode.IsLetter(r) {
				other++
			}
		case r == '\'' || r == '-':
		default:
			inWord = false
		}
	}
	s.Lines = strings.Count(text, "\n") + 1
	letters := latin + han + other
	switch {
	case letters == 0:
		s.Script = "other"
	case float64(han)/float64(letters) > 0.8:
		s.Script = "cjk"
	case float64(latin)/float64(letters) > 0.8:
		s.Script = "latin"
	case han > 0 && latin > 0:
		s.Script = "mixed"
	default:
		s.Script = "other"
	}
	s.Structured = len(reStructure.FindAllStringIndex(text, 3)) >= 2
	s.AgentMarks = min(len(reAgent.FindAllStringIndex(text, 20)), 20)
	s.AskMarks = min(len(reAsk.FindAllStringIndex(text, 10)), 10)
	s.Question = strings.ContainsAny(text[max(0, len(text)-4):], "?？")
	return s
}
