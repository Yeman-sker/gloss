// Package demo is an action with a fixed answer, so the whole path runs
// without a network or a key. Copy it to start a new built-in action.
package demo

import (
	"context"
	"time"

	"gloss/core"
)

const pause = 38 * time.Millisecond

var answer = []struct {
	kind core.BlockKind
	text string
}{
	{core.BlockText, "敏捷的棕色狐狸跳过了那只懒狗。"},
	{core.BlockPhonetic, "/ðə kwɪk braʊn fɒks/ · 这是演示数据，尚未接入真实翻译服务。"},
	{core.BlockDefinition, "quick：敏捷的、迅速的；lazy：懒惰的。"},
}

// Action streams answer whatever it is asked.
type Action struct{}

func (Action) ID() string   { return "demo" }
func (Action) Name() string { return "演示" }

func (Action) Run(ctx context.Context, _ core.Request, emit core.Emit) error {
	for i, b := range answer {
		runes := []rune(b.text)
		for at := 0; at < len(runes); at += 3 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pause):
			}
			emit(i, b.kind, string(runes[at:min(at+3, len(runes))]))
		}
	}
	return nil
}
