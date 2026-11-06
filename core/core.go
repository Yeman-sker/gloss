// Package core is what Gloss is, with no window in it: an Action takes text
// and its context, and streams Blocks back. See docs/CORE.md. It must not
// import mygo, app, capture or any provider; arch_test.go checks that.
package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Context is what surrounds the text. Sentences and memes need it to be
// explained well, so it travels with every request from the start.
type Context struct {
	SourceApp   string // a stable identity, the bundle id on macOS
	Surrounding string // bounded text around the selection; empty when unknown
	SourceLang  string // empty means detect
	TargetLang  string
	// WindowTitle is for routing only: hosts do not pass it to actions'
	// models unless the user allows it.
	WindowTitle string
	Editable    Editable
}

// Request is one thing the user asked about.
type Request struct {
	Text    string
	Context Context
	// Settings are the action's own settings, checked and defaulted by the
	// host, keyed by Setting.Key. An action sees no other action's.
	Settings map[string]string
}

// BlockKind says what a block means. Actions give meaning, app gives looks.
type BlockKind string

const (
	BlockText       BlockKind = "text"
	BlockPhonetic   BlockKind = "phonetic"
	BlockDefinition BlockKind = "definition"
	BlockExample    BlockKind = "example"
	BlockTag        BlockKind = "tag"
)

// Emit streams one piece of the result: delta is appended to the block at
// index, which is created with kind when it does not exist yet.
type Emit func(index int, kind BlockKind, delta string)

// Action turns a request into a stream of blocks. Run stops when ctx ends.
type Action interface {
	ID() string
	Name() string
	Run(ctx context.Context, req Request, emit Emit) error
}

// EventType tells a Sink what happened to a run. EventDone means the result
// is complete and may be copied or written back; a run that ends in
// EventError has no complete result, whatever it emitted before. Neither
// says anything about the panel's animation, which finishes on its own.
type EventType int

const (
	EventDelta EventType = iota
	EventDone
	EventError
)

// Event is what a Sink receives. Run identifies the run it belongs to.
type Event struct {
	Run   uint64
	Type  EventType
	Index int
	Kind  BlockKind
	Delta string
	Err   error
}

// Sink receives events on the run's goroutine; a UI must hand them over to
// its own thread. A cancelled run sends nothing more, not even EventDone.
type Sink func(Event)

// ErrUnknownAction is returned for an action that was never registered.
var ErrUnknownAction = errors.New("core: unknown action")

// Engine holds the actions and runs one at a time: a new request cancels the
// one before it, as selecting new text makes the old answer pointless.
type Engine struct {
	mu      sync.Mutex
	actions map[string]Action
	order   []string
	run     uint64
	cancel  context.CancelFunc
}

// NewEngine returns an Engine without actions.
func NewEngine() *Engine { return &Engine{actions: map[string]Action{}} }

// Register adds an action. Built-in and plugin actions come through here.
func (e *Engine) Register(a Action) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, dup := e.actions[a.ID()]; dup {
		return fmt.Errorf("core: action %q registered twice", a.ID())
	}
	e.actions[a.ID()] = a
	e.order = append(e.order, a.ID())
	return nil
}

// Actions returns the registered actions in the order they were added.
func (e *Engine) Actions() []Action {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Action, len(e.order))
	for i, id := range e.order {
		out[i] = e.actions[id]
	}
	return out
}

// Descriptors describes the registered actions in the order they were added.
func (e *Engine) Descriptors() []Descriptor {
	actions := e.Actions()
	out := make([]Descriptor, len(actions))
	for i, a := range actions {
		out[i] = Describe(a)
	}
	return out
}

// Start cancels the current run and starts the action. It returns the new
// run's number at once; the result arrives at sink.
func (e *Engine) Start(id string, req Request, sink Sink) (uint64, error) {
	e.mu.Lock()
	a, ok := e.actions[id]
	if !ok {
		e.mu.Unlock()
		return 0, fmt.Errorf("%w: %s", ErrUnknownAction, id)
	}
	if e.cancel != nil {
		e.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.run++
	run := e.run
	e.mu.Unlock()

	// send drops whatever a replaced action still sends after its cancel.
	send := func(ev Event) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if run != e.run || ctx.Err() != nil {
			return
		}
		ev.Run = run
		sink(ev)
	}
	go func() {
		defer cancel()
		err := a.Run(ctx, req, func(i int, k BlockKind, d string) {
			send(Event{Type: EventDelta, Index: i, Kind: k, Delta: d})
		})
		if err != nil {
			send(Event{Type: EventError, Err: err})
			return
		}
		send(Event{Type: EventDone})
	}()
	return run, nil
}

// Cancel stops the current run, if any.
func (e *Engine) Cancel() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
	}
}
