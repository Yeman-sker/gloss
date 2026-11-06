// Package routing picks the feature for a selection, between capture and
// the action: rules first, then Jev when the rules are unsure and it is
// configured, then what the user tends to choose, then the user's last
// explicit choice, then the default. It only picks. It does not start the
// engine, touch the clipboard or write back: app does those. See
// docs/decisions/0009-memory-and-routing.md.
package routing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gloss/core"
	"gloss/memory"
)

// Input is everything routing may read about one selection.
type Input struct {
	Text       string
	Shape      memory.Shape
	App        string
	Title      string // for Jev only when the user allows it
	Editable   core.Editable
	TargetLang string
	// Candidates are the features that are on, in their order.
	Candidates []core.Descriptor
	// Default is the feature to use when nothing else is known; the
	// first candidate when it is not one of them.
	Default string
	Prefs   memory.Snapshot
}

// Decision is the feature picked, how, and why.
type Decision struct {
	ID     string
	By     string // memory.ByRule and the like
	Reason string
	// Scores are the rules' scores, or Jev's probabilities when Jev chose.
	Scores  map[string]float64
	Elapsed time.Duration
	// JevError says why Jev was asked and not used, when it was: a kind,
	// never what Jev or its service returned, which may quote the text.
	JevError string
}

// Judge is a model that picks a feature: Jev, in this app.
type Judge interface {
	Judge(ctx context.Context, in Input) (Verdict, error)
}

// Verdict is a judge's answer.
type Verdict struct {
	Choice        string
	Confidence    float64
	Probabilities map[string]float64
}

// Router decides. The zero Router decides by rules and memory alone.
type Router struct {
	Judge Judge
	// Wait bounds Jev: past it, routing goes on without.
	Wait time.Duration
	// MinConfidence is the confidence a verdict needs to be taken. It is
	// the model's score, not a measured probability of being right.
	MinConfidence float64
	// PreferenceShare is the share a preference needs to decide.
	PreferenceShare float64
}

// Thresholds of the rules: a rule decides only when its feature scores at
// least ruleScore and ruleMargin ahead of the next.
const (
	ruleScore  = 0.7
	ruleMargin = 0.25
)

// Route decides for in. It never returns a feature that is not a
// candidate, and returns the default when there are no others.
func (r Router) Route(ctx context.Context, in Input) Decision {
	start := time.Now()
	d := r.route(ctx, in)
	d.Elapsed = time.Since(start)
	return d
}

func (r Router) route(ctx context.Context, in Input) Decision {
	ok := map[string]bool{}
	for _, c := range in.Candidates {
		ok[c.ID] = true
	}
	def := in.Default
	if !ok[def] && len(in.Candidates) > 0 {
		def = in.Candidates[0].ID
	}
	if len(in.Candidates) <= 1 {
		return Decision{ID: def, By: memory.ByDefault, Reason: "只有一个可用功能"}
	}
	if in.Shape.Chars == 0 && in.Text != "" {
		in.Shape = memory.ShapeOf(in.Text)
	}

	scores, why := Rules(in)
	best, second := top2(scores, ok)
	if best != "" && scores[best] >= ruleScore && scores[best]-scores[second] >= ruleMargin {
		return Decision{ID: best, By: memory.ByRule, Reason: why[best], Scores: scores}
	}

	var jevErr string
	if r.Judge != nil {
		wait := r.Wait
		if wait <= 0 {
			wait = 800 * time.Millisecond
		}
		jctx, cancel := context.WithTimeout(ctx, wait)
		v, err := r.Judge.Judge(jctx, in)
		cancel()
		switch {
		// Kinds only: what Jev or its service says may echo the text.
		case errors.Is(err, context.DeadlineExceeded):
			jevErr = "Jev 超时"
		case err != nil:
			jevErr = "Jev 请求失败"
		case !ok[v.Choice]:
			jevErr = "Jev 返回了不在候选中的功能"
		case v.Confidence < r.MinConfidence:
			jevErr = fmt.Sprintf("Jev 的把握 %.2f 低于 %.2f", v.Confidence, r.MinConfidence)
		default:
			return Decision{ID: v.Choice, By: memory.ByJev, Reason: fmt.Sprintf("Jev 选择，把握 %.2f", v.Confidence), Scores: v.Probabilities}
		}
	}

	share := r.PreferenceShare
	if share <= 0 {
		share = 0.6
	}
	for _, p := range []memory.Preference{in.Prefs.Similar, in.Prefs.App} {
		if id, sure := p.Lean(share); sure && ok[id] {
			return Decision{ID: id, By: memory.ByPreference, Reason: fmt.Sprintf("%s里通常选它（%d 次明确选择）", p.Scope, p.Samples), Scores: scores, JevError: jevErr}
		}
	}
	for _, id := range in.Prefs.Recent {
		if ok[id] {
			return Decision{ID: id, By: memory.ByRecent, Reason: "最近一次明确的选择", Scores: scores, JevError: jevErr}
		}
	}
	return Decision{ID: def, By: memory.ByDefault, Reason: "信号不明确，用默认功能", Scores: scores, JevError: jevErr}
}

func top2(scores map[string]float64, ok map[string]bool) (string, string) {
	ids := make([]string, 0, len(scores))
	for id := range scores {
		if ok[id] {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		return scores[ids[i]] > scores[ids[j]] || scores[ids[i]] == scores[ids[j]] && ids[i] < ids[j]
	})
	switch len(ids) {
	case 0:
		return "", ""
	case 1:
		return ids[0], ""
	}
	return ids[0], ids[1]
}

// Apps whose selections are often an agent's output: chats with agents,
// terminals and editors where agents run.
var agentApps = map[string]bool{
	"com.anthropic.claudefordesktop": true,
	"com.openai.chat":                true,
	"com.openai.codex":               true,
	"com.apple.Terminal":             true,
	"com.googlecode.iterm2":          true,
	"com.mitchellh.ghostty":          true,
	"dev.warp.Warp-Stable":           true,
	"com.microsoft.VSCode":           true,
	"com.todesktop.230313mzl4w4u92":  true, // Cursor
	"dev.zed.Zed":                    true,
}

// Rules scores the built-in features from the selection's signals. No
// single signal reaches the threshold: "from Claude" is not agent output,
// "editable" is not a prompt, "foreign" is not a wish to translate. It
// returns the scores and, for each, why.
func Rules(in Input) (map[string]float64, map[string]string) {
	s := in.Shape
	scores := map[string]float64{}
	why := map[string][]string{}
	add := func(id string, v float64, reason string) {
		scores[id] += v
		if v > 0 {
			why[id] = append(why[id], reason)
		}
	}
	foreign := isForeign(s, in.TargetLang)
	short := s.Chars <= 400 && s.Lines <= 4
	long := s.Chars >= 200 || s.Chars >= 60 && s.Lines >= 4
	agentLike := s.AgentMarks >= 2 || s.Structured && s.AgentMarks >= 1
	editable := in.Editable == core.EditableYes
	readOnly := in.Editable == core.EditableNo
	class := s.Class()

	// translate
	if foreign {
		add("translate", 0.45, "外文")
		if short {
			add("translate", 0.2, "篇幅短")
		}
		if !editable {
			add("translate", 0.15, "不在输入框里")
		}
		if class == "term" {
			add("translate", 0.1, "单个词")
		}
		if agentLike {
			add("translate", -0.3, "")
			if agentApps[in.App] {
				add("translate", -0.15, "")
			}
		}
		if editable && s.AskMarks > 0 {
			add("translate", -0.3, "")
		}
	}

	// explain
	if !foreign && s.Chars > 0 {
		switch class {
		case "term":
			add("explain", 0.6, "母语里的一个词")
		case "sentence":
			add("explain", 0.45, "母语里的一句话")
		}
		if readOnly {
			add("explain", 0.2, "在阅读的内容里")
		}
		if !editable {
			add("explain", 0.1, "不在输入框里")
		}
		if agentLike {
			add("explain", -0.3, "")
		}
		if s.Question {
			add("explain", 0.05, "")
		}
		if editable {
			add("explain", -0.2, "")
		}
	}

	// prompt
	if editable {
		add("prompt", 0.35, "在输入框里")
	}
	if s.AskMarks > 0 {
		add("prompt", 0.2+0.05*float64(min(s.AskMarks, 4)), "像是给助手的请求")
	}
	if s.Chars >= 20 && editable {
		add("prompt", 0.1, "有一定篇幅")
	}
	if agentLike {
		add("prompt", -0.2, "")
	}

	// agent
	if long {
		add("agent", 0.25, "篇幅长")
	}
	if agentLike {
		add("agent", 0.3, "有 Agent 汇报的特征")
	}
	if s.AgentMarks >= 4 {
		add("agent", 0.15, "汇报特征很多")
	}
	if s.Structured {
		add("agent", 0.1, "有结构")
	}
	if agentApps[in.App] {
		add("agent", 0.15, "来自常运行 Agent 的应用")
	}
	if editable {
		add("agent", -0.3, "")
	}

	reasons := map[string]string{}
	for id, rs := range why {
		reasons[id] = "规则：" + strings.Join(rs, "、")
	}
	for id, v := range scores {
		scores[id] = max(0, min(1, v))
	}
	return scores, reasons
}

// isForeign says the text is not in the user's language: Latin text for a
// reader of Chinese, Japanese or Korean, Han text for a reader of a Latin
// language.
func isForeign(s memory.Shape, target string) bool {
	t := strings.ToLower(target)
	cjkReader := t == "" || strings.HasPrefix(t, "zh") || strings.HasPrefix(t, "ja") || strings.HasPrefix(t, "ko")
	if cjkReader {
		return s.Script == "latin"
	}
	return s.Script == "cjk"
}
