// Routebench compares three ways of routing on routing/testdata/samples.json:
// the rules alone, Jev with the state the app sends today, Jev with the
// rich state (text around the selection, app names, rule scores as hints,
// criteria with guidance), and the rich state without the rule scores. It needs the Jev service of the config
// (routing.jev_service) and prints no key. See
// docs/research/2026-11-08-jev-context.md.
//
//	go run ./cmd/routebench [-n 3]
//	go run ./cmd/routebench -show c1   # print what Jev would read, no call
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gloss/core"
	"gloss/memory"
	"gloss/providers/prompt"
	"gloss/routing"
	"gloss/settings"
)

type sample struct {
	ID, Text, Before, After, App, Editable, Expect, Note string
}

// variant is one way to ask Jev, and what it scored.
type variant struct {
	name    string
	jev     routing.Jev
	lat     []time.Duration
	chars   int
	right   int // on clear samples
	ctxOK   int // on samples with text around them
	errs    int
	asked   int
	picks   map[string]int // on ambiguous samples
	confSum float64
}

func main() {
	n := flag.Int("n", 1, "Jev calls per sample and variant, for latency")
	file := flag.String("samples", "routing/testdata/samples.json", "samples")
	show := flag.String("show", "", "print the basic and rich state for this sample id, and call nothing")
	flag.Parse()
	raw, err := os.ReadFile(*file)
	check(err)
	var samples []sample
	check(json.Unmarshal(raw, &samples))
	specs, err := prompt.Builtin()
	check(err)
	var descs []core.Descriptor
	for _, s := range specs {
		descs = append(descs, s.Descriptor)
	}
	input := func(s sample) routing.Input {
		return routing.Input{Text: s.Text, Shape: memory.ShapeOf(s.Text), App: s.App, Editable: core.ParseEditable(s.Editable),
			Before: s.Before, After: s.After, TargetLang: "zh-Hans", Candidates: descs, Default: "translate"}
	}

	if *show != "" {
		for _, s := range samples {
			if s.ID == *show {
				in := input(s)
				fmt.Printf("── basic state ──\n%s\n── rich state ──\n%s\n", routing.Jev{}.State(in), routing.Jev{Rich: true}.State(in))
				return
			}
		}
		check(fmt.Errorf("没有样例 %s", *show))
	}

	path, err := settings.Path("Gloss")
	check(err)
	cfg, err := settings.Read(path, descs)
	check(err)
	svc, ok := cfg.Jev()
	if !ok {
		check(fmt.Errorf("配置里没有可用的 Jev 服务：设置 routing.jev_service，并在 services 里填写它的 api_key"))
	}
	base := routing.Jev{BaseURL: svc.BaseURL, APIKey: svc.APIKey, Model: svc.Model}
	rich := base
	rich.Rich = true
	bare := rich
	bare.NoHints = true
	vs := []*variant{{name: "Jev", jev: base, picks: map[string]int{}}, {name: "Jev+上下文", jev: rich, picks: map[string]int{}},
		{name: "上下文无提示", jev: bare, picks: map[string]int{}}}

	var ruleRight, ruleCtx, clear, withCtx int
	fmt.Printf("%-4s %-10s %-10s %-16s %-16s %-16s\n", "id", "期望", "规则", vs[0].name, vs[1].name, vs[2].name)
	for _, s := range samples {
		in := input(s)
		rule := routing.Router{}.Route(context.Background(), in)
		ruleID := rule.ID
		if rule.By != memory.ByRule {
			ruleID = "-"
		}
		hasCtx := strings.TrimSpace(s.Before+s.After) != ""
		isClear := s.Expect != "ambiguous"
		if isClear {
			clear++
			if ruleID == s.Expect {
				ruleRight++
			}
		}
		if hasCtx {
			withCtx++
			if ruleID == s.Expect {
				ruleCtx++
			}
		}
		cols := make([]string, len(vs))
		for k, v := range vs {
			v.chars += len([]rune(v.jev.State(in)))
			var verdict routing.Verdict
			var jerr error
			for i := 0; i < *n; i++ {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				start := time.Now()
				verdict, jerr = v.jev.Judge(ctx, in)
				cancel()
				v.asked++
				if jerr == nil {
					v.lat = append(v.lat, time.Since(start))
				}
			}
			if jerr != nil {
				v.errs++
				cols[k] = "失败"
				fmt.Fprintf(os.Stderr, "%s %s：%v\n", s.ID, v.name, jerr)
				continue
			}
			v.confSum += verdict.Confidence
			mark := ""
			switch {
			case !isClear:
				v.picks[verdict.Choice]++
			case verdict.Choice == s.Expect:
				v.right++
				if hasCtx {
					v.ctxOK++
				}
			default:
				mark = "✗"
			}
			cols[k] = fmt.Sprintf("%s %.2f%s", verdict.Choice, verdict.Confidence, mark)
		}
		fmt.Printf("%-4s %-10s %-10s %-16s %-16s %-16s\n", s.ID, s.Expect, ruleID, cols[0], cols[1], cols[2])
	}

	fmt.Printf("\n明确样例 %d 条（其中带前后文 %d 条）\n", clear, withCtx)
	fmt.Printf("  %-12s 判对 %d（规则判不了的交给后续环节，不算判对）；带前后文的判对 %d\n", "规则", ruleRight, ruleCtx)
	for _, v := range vs {
		fmt.Printf("  %-12s 判对 %d；带前后文的判对 %d；失败 %d；平均把握 %.2f\n", v.name, v.right, v.ctxOK, v.errs, v.confSum/float64(max(1, len(samples)-v.errs)))
	}
	fmt.Println("歧义样例的选择分布：")
	for _, v := range vs {
		fmt.Printf("  %-12s %v\n", v.name, v.picks)
	}
	fmt.Println("每条样例发给 Jev 的 state 平均字数（粗看 token 和费用）：")
	for _, v := range vs {
		fmt.Printf("  %-12s %d\n", v.name, v.chars/max(1, len(samples)))
	}
	fmt.Println("Jev 延迟：")
	for _, v := range vs {
		lat := v.lat
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		if len(lat) == 0 {
			fmt.Printf("  %-12s 没有成功的调用\n", v.name)
			continue
		}
		fmt.Printf("  %-12s p50 %v，p95 %v，最大 %v（%d 次）\n", v.name, lat[len(lat)/2].Round(time.Millisecond),
			lat[len(lat)*95/100].Round(time.Millisecond), lat[len(lat)-1].Round(time.Millisecond), len(lat))
	}
	fmt.Println("注意：样例由开发时编写，规则在其上调过；c1–c5 的期望是样例作者的假设。以负责人的实测和标注为准。")
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
