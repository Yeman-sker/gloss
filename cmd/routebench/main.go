// Routebench compares the rules with Jev on routing/testdata/samples.json:
// what each picks, and how long Jev takes. It needs the Jev service of the
// config (routing.jev_service) and prints no key.
//
//	go run ./cmd/routebench [-n 3]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"gloss/core"
	"gloss/memory"
	"gloss/providers/prompt"
	"gloss/routing"
	"gloss/settings"
)

type sample struct {
	ID, Text, App, Editable, Expect, Note string
}

func main() {
	n := flag.Int("n", 1, "Jev calls per sample, for latency")
	file := flag.String("samples", "routing/testdata/samples.json", "samples")
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
	path, err := settings.Path("Gloss")
	check(err)
	cfg, err := settings.Read(path, descs)
	check(err)
	svc, ok := cfg.Jev()
	if !ok {
		check(fmt.Errorf("配置里没有可用的 Jev 服务：设置 routing.jev_service，并在 services 里填写它的 api_key"))
	}
	jev := routing.Jev{BaseURL: svc.BaseURL, APIKey: svc.APIKey, Model: svc.Model}
	var lat []time.Duration
	var ruleRight, jevRight, clear, jevErrs int
	fmt.Printf("%-4s %-10s %-10s %-10s %s\n", "id", "期望", "规则", "Jev", "把握")
	for _, s := range samples {
		in := routing.Input{Text: s.Text, Shape: memory.ShapeOf(s.Text), App: s.App, Editable: core.ParseEditable(s.Editable),
			TargetLang: "zh-Hans", Candidates: descs, Default: "translate"}
		rule := routing.Router{}.Route(context.Background(), in)
		ruleID := rule.ID
		if rule.By != memory.ByRule {
			ruleID = "-"
		}
		var v routing.Verdict
		var jerr error
		for i := 0; i < *n; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			start := time.Now()
			v, jerr = jev.Judge(ctx, in)
			cancel()
			if jerr == nil {
				lat = append(lat, time.Since(start))
			}
		}
		if jerr != nil {
			jevErrs++
			fmt.Printf("%-4s %-10s %-10s 失败：%v\n", s.ID, s.Expect, ruleID, jerr)
			continue
		}
		if s.Expect != "ambiguous" {
			clear++
			if ruleID == s.Expect {
				ruleRight++
			}
			if v.Choice == s.Expect {
				jevRight++
			}
		}
		fmt.Printf("%-4s %-10s %-10s %-10s %.2f\n", s.ID, s.Expect, ruleID, v.Choice, v.Confidence)
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	fmt.Printf("\n明确样本 %d：规则判对 %d（其余交给后续环节），Jev 判对 %d；Jev 失败 %d\n", clear, ruleRight, jevRight, jevErrs)
	if len(lat) > 0 {
		fmt.Printf("Jev 延迟：p50 %v，p95 %v，最大 %v（%d 次）\n", lat[len(lat)/2].Round(time.Millisecond),
			lat[len(lat)*95/100].Round(time.Millisecond), lat[len(lat)-1].Round(time.Millisecond), len(lat))
	}
	fmt.Println("注意：样本由开发时编写，规则在其上调过，判对率会偏高；以实测记录为准。")
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
