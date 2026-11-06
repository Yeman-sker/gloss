// Smoke runs the features against the real model service in the config, to
// measure what a user waits for: the first text and the whole result. It
// prints no key and no result, only sizes and times.
//
//	go run ./cmd/smoke                 every case, every feature
//	go run ./cmd/smoke -feature translate -n 3
//
// The bad-key and unreachable cases need no service and run without one.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gloss/core"
	"gloss/providers/openai"
	"gloss/providers/prompt"
	"gloss/settings"
)

var cases = []struct{ name, text string }{
	{"单词", "serendipity"},
	{"句子", "The committee deferred the vote until the audit is complete."},
	{"段落", "Backpressure is a mechanism by which a slow consumer signals upstream producers to reduce their rate. Without it, buffers grow without bound, latency climbs, and eventually the process runs out of memory. Most streaming systems implement it either by blocking writes or by dropping messages according to a policy."},
}

func main() {
	feature := flag.String("feature", "", "only this feature")
	n := flag.Int("n", 1, "runs per case")
	flag.Parse()
	specs, err := prompt.Builtin()
	if err != nil {
		fail(err)
	}
	var descs []core.Descriptor
	for _, s := range specs {
		descs = append(descs, s.Descriptor)
	}
	path, err := settings.Path("Gloss")
	if err != nil {
		fail(err)
	}
	cfg, cfgErr := settings.Read(path, descs)

	// Failures first: they must say what to fix, and never quote the key.
	fmt.Println("== 失败情形")
	failCase("错误的 Key", openai.Client{BaseURL: settings.DefaultBaseURL, APIKey: "sk-invalid-smoke", Model: "gpt-4o-mini", Idle: 15 * time.Second})
	failCase("无法连接", openai.Client{BaseURL: "http://127.0.0.1:9/v1", APIKey: "x", Model: "m", Idle: 5 * time.Second})

	if cfgErr != nil {
		fmt.Printf("\n配置不可用，跳过真实服务：%v\n", cfgErr)
		return
	}
	fmt.Println("\n== 真实服务（首字 / 总耗时 / 字数）")
	for _, spec := range specs {
		if *feature != "" && spec.ID != *feature || !cfg.Enabled(spec.ID) {
			continue
		}
		svc, err := cfg.Service(spec.ID)
		if err != nil {
			fmt.Printf("%s：%v\n", spec.Name, err)
			continue
		}
		client := openai.Client{BaseURL: svc.BaseURL, APIKey: svc.APIKey, Model: svc.Model}
		for _, c := range cases {
			var firsts, totals []time.Duration
			var chars int
			var lastErr error
			for i := 0; i < *n; i++ {
				req := core.Request{Text: c.text, Context: core.Context{TargetLang: cfg.TargetLang}, Settings: cfg.Values(spec.Descriptor)}
				first, total, size, err := once(client, spec.Messages(req))
				if err != nil {
					lastErr = err
					continue
				}
				firsts, totals, chars = append(firsts, first), append(totals, total), size
			}
			if lastErr != nil {
				fmt.Printf("%-8s %-4s 失败：%v\n", spec.Name, c.name, lastErr)
			}
			if len(firsts) > 0 {
				fmt.Printf("%-8s %-4s 首字 %v  总 %v  %d 字（%s，%d 次）\n", spec.Name, c.name, median(firsts), median(totals), chars, svc.Model, len(firsts))
			}
		}
	}
}

func once(client openai.Client, msgs []prompt.Message) (time.Duration, time.Duration, int, error) {
	out := make([]openai.Message, len(msgs))
	for i, m := range msgs {
		out[i] = openai.Message{Role: m.Role, Content: m.Content}
	}
	start := time.Now()
	var first time.Duration
	var b strings.Builder
	err := client.Stream(context.Background(), out, func(d string) {
		if first == 0 {
			first = time.Since(start)
		}
		b.WriteString(d)
	})
	return first, time.Since(start), len([]rune(b.String())), err
}

func failCase(name string, c openai.Client) {
	start := time.Now()
	err := c.Stream(context.Background(), []openai.Message{{Role: "user", Content: "hi"}}, func(string) {})
	switch {
	case err == nil:
		fmt.Printf("%s：意外成功\n", name)
	case strings.Contains(err.Error(), c.APIKey) && c.APIKey != "x":
		fmt.Printf("%s：错误信息含有 Key！\n", name)
	default:
		var ctxErr = errors.Is(err, context.DeadlineExceeded)
		fmt.Printf("%s（%v）：%v%s\n", name, time.Since(start).Round(time.Millisecond), err, map[bool]string{true: "（超时）"}[ctxErr])
	}
}

func median(d []time.Duration) time.Duration {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[len(d)/2].Round(time.Millisecond)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
