package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// Jev asks TypeSafe's Jev, a model that answers choices rather than writes
// text, through POST {BaseURL}/systemone. See docs/research/2026-11-05-jev.md
// for what is sent, and what is known about it.
type Jev struct {
	BaseURL string // like https://api.typesafe.ai/v1
	APIKey  string
	Model   string // jev-latest by default
	// SendTitle lets the window title go to Jev.
	SendTitle bool
	// Rich sends the fuller state and criteria of jevstate.go, text
	// around the selection included. Off in the app: routebench compares
	// the two first.
	Rich bool
	// NoHints leaves the rules' scores out of the rich state, to see
	// whether Jev follows them.
	NoHints bool
	Client  *http.Client
}

// maxSent bounds how much of a selection goes to Jev: its start and end
// say what kind of text it is.
const maxSent = 1200

type jevQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type jevRequest struct {
	State     string                 `json:"state"`
	Model     string                 `json:"model"`
	Questions map[string]jevQuestion `json:"questions"`
}

type jevResponse struct {
	Answers map[string]struct {
		Choice        string             `json:"choice"`
		Confidence    float64            `json:"confidence"`
		Probabilities map[string]float64 `json:"probabilities"`
	} `json:"answers"`
	Error any `json:"error"`
}

// State is what Jev reads: the selection, cut to its start and end, and
// the signals around it. It has no key and, unless allowed, no title.
func (j Jev) State(in Input) string {
	if j.Rich {
		return j.richState(in)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Selected text:\n%s\n\n", clip(in.Text, maxSent))
	fmt.Fprintf(&b, "Selected in app: %s\n", or(in.App, "unknown"))
	if j.SendTitle && in.Title != "" {
		fmt.Fprintf(&b, "Window title: %s\n", clip(in.Title, 120))
	}
	fmt.Fprintf(&b, "Selection is in an editable field: %s\n", in.Editable)
	s := in.Shape
	fmt.Fprintf(&b, "Text shape: %d characters, %d lines, script %s, kind %s", s.Chars, s.Lines, s.Script, s.Class())
	if s.Structured {
		b.WriteString(", has Markdown structure")
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "The user reads and writes: %s\n", or(in.TargetLang, "zh-Hans"))
	if sum := in.Prefs.Summary(); sum != "" {
		fmt.Fprintf(&b, "\nThis user's history (shares of explicit choices):\n%s\n", sum)
	}
	return b.String()
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// clip keeps the start and the end of long text.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	half := n / 2
	return string(r[:half]) + "\n[…]\n" + string(r[len(r)-half:])
}

// Judge asks Jev which candidate the user wants.
func (j Jev) Judge(ctx context.Context, in Input) (Verdict, error) {
	if j.APIKey == "" {
		return Verdict{}, errors.New("Jev 缺少 API Key")
	}
	criteria := j.criteria(in)
	model := j.Model
	if model == "" {
		model = "jev-latest"
	}
	body, err := json.Marshal(jevRequest{
		State: j.State(in),
		Model: model,
		Questions: map[string]jevQuestion{"feature": {
			Type:         "choice",
			Instructions: "The user selected the text and pressed one shortcut. Which feature do they most likely want for it right now?",
			Criteria:     criteria,
		}},
	})
	if err != nil {
		return Verdict{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(j.BaseURL, "/")+"/systemone", bytes.NewReader(body))
	if err != nil {
		return Verdict{}, fmt.Errorf("Jev 地址无效：%w", err)
	}
	req.Header.Set("Authorization", "Bearer "+j.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := j.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Verdict{}, fmt.Errorf("Jev 超时：%w", ctx.Err())
		}
		return Verdict{}, fmt.Errorf("无法连接 Jev：%w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Verdict{}, fmt.Errorf("读取 Jev 结果失败：%w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Verdict{}, fmt.Errorf("Jev 返回 HTTP %d", resp.StatusCode)
	}
	var out jevResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Verdict{}, fmt.Errorf("Jev 返回了无法解析的数据：%w", err)
	}
	a, ok := out.Answers["feature"]
	if !ok {
		return Verdict{}, errors.New("Jev 没有回答")
	}
	return Verdict{Choice: a.Choice, Confidence: a.Confidence, Probabilities: a.Probabilities}, nil
}
