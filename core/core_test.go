package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fn struct {
	id  string
	run func(context.Context, Request, Emit) error
}

func (f fn) ID() string   { return f.id }
func (f fn) Name() string { return f.id }
func (f fn) Run(ctx context.Context, r Request, e Emit) error {
	return f.run(ctx, r, e)
}

func collect() (Sink, chan Event) {
	ch := make(chan Event, 64)
	return func(ev Event) { ch <- ev }, ch
}

func next(t *testing.T, ch chan Event) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
		return Event{}
	}
}

func TestStreamsThenDone(t *testing.T) {
	e := NewEngine()
	e.Register(fn{"a", func(_ context.Context, r Request, emit Emit) error {
		emit(0, BlockText, r.Text)
		return nil
	}})
	sink, ch := collect()
	run, err := e.Start("a", Request{Text: "hi"}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if ev := next(t, ch); ev.Type != EventDelta || ev.Delta != "hi" || ev.Kind != BlockText || ev.Run != run {
		t.Errorf("delta: %+v", ev)
	}
	if ev := next(t, ch); ev.Type != EventDone {
		t.Errorf("done: %+v", ev)
	}
}

func TestActionErrorArrives(t *testing.T) {
	boom := errors.New("boom")
	e := NewEngine()
	e.Register(fn{"a", func(context.Context, Request, Emit) error { return boom }})
	sink, ch := collect()
	e.Start("a", Request{}, sink)
	if ev := next(t, ch); ev.Type != EventError || !errors.Is(ev.Err, boom) {
		t.Errorf("event: %+v", ev)
	}
}

func TestNewRunSilencesTheOldOne(t *testing.T) {
	release := make(chan struct{})
	cancelled := make(chan struct{})
	e := NewEngine()
	e.Register(fn{"slow", func(ctx context.Context, _ Request, emit Emit) error {
		<-ctx.Done()
		close(cancelled)
		<-release
		emit(0, BlockText, "late") // a badly behaved action
		return nil
	}})
	e.Register(fn{"fast", func(_ context.Context, _ Request, emit Emit) error {
		emit(0, BlockText, "new")
		return nil
	}})
	sink, ch := collect()
	e.Start("slow", Request{}, sink)
	run, _ := e.Start("fast", Request{}, sink)
	<-cancelled
	if ev := next(t, ch); ev.Run != run || ev.Delta != "new" {
		t.Errorf("first: %+v", ev)
	}
	if ev := next(t, ch); ev.Type != EventDone || ev.Run != run {
		t.Errorf("second: %+v", ev)
	}
	close(release)
	select {
	case ev := <-ch:
		t.Errorf("cancelled run spoke: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestUnknownAndDuplicate(t *testing.T) {
	e := NewEngine()
	if _, err := e.Start("x", Request{}, func(Event) {}); !errors.Is(err, ErrUnknownAction) {
		t.Errorf("unknown: %v", err)
	}
	a := fn{id: "a"}
	if err := e.Register(a); err != nil {
		t.Fatal(err)
	}
	if err := e.Register(a); err == nil {
		t.Error("duplicate accepted")
	}
}
