// Package memory keeps a local record of how Gloss is used, one line per
// use, and distils from it what a user tends to want where: routing reads
// that before it picks a feature. It knows no window and no network. It
// stores no selected text, surrounding text, result or window title: only
// their shape. See docs/decisions/0009-memory-and-routing.md.
package memory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Version is the format of a record. A record of another version is
// skipped, not misread.
const Version = 1

// How a feature came to be chosen.
const (
	ByDefault    = "default"    // nothing better was known
	ByRule       = "rule"       // the routing rules were sure
	ByJev        = "jev"        // Jev chose
	ByPreference = "preference" // what the user tends to choose here
	ByRecent     = "recent"     // the user's last explicit choice
	ByManual     = "manual"     // the user chose
)

// Record is one use: a selection captured, the features run on it, and
// how it ended.
type Record struct {
	V       int       `json:"v"`
	Session string    `json:"session"`
	Time    time.Time `json:"time"`
	// Routing tells whether automatic routing was on.
	Routing  bool   `json:"routing"`
	App      string `json:"app,omitempty"`
	Editable string `json:"editable,omitempty"`
	Shape    Shape  `json:"shape"`

	Candidates []string           `json:"candidates"`
	Initial    string             `json:"initial"`
	InitialBy  string             `json:"initial_by"`
	Reason     string             `json:"reason,omitempty"`
	Scores     map[string]float64 `json:"scores,omitempty"`
	// Corrections are the user's switches to another feature, in order.
	Corrections []Correction `json:"corrections,omitempty"`
	Final       string       `json:"final"`
	Outcome     string       `json:"outcome"` // done, error or cancelled
	Copied      bool         `json:"copied,omitempty"`
	Replaced    bool         `json:"replaced,omitempty"`
	Timings     Timings      `json:"timings"`
	// Confirmed is set only by an explicit label: whether the initial
	// feature was the one the user wanted. Nil is unknown, never right.
	Confirmed *bool `json:"confirmed,omitempty"`
}

// Correction is a switch from one feature to another.
type Correction struct {
	From string `json:"from"`
	To   string `json:"to"`
	AtMs int64  `json:"at_ms"` // since the session began
}

// Timings are in milliseconds.
type Timings struct {
	CaptureMs int64 `json:"capture_ms"`
	RouteMs   int64 `json:"route_ms"`
	FirstMs   int64 `json:"first_ms,omitempty"` // to the first text of the final feature
	TotalMs   int64 `json:"total_ms,omitempty"`
	// ExtraWaitMs is how long the user waited for the corrected feature's
	// first text after the first correction.
	ExtraWaitMs int64 `json:"extra_wait_ms,omitempty"`
	WriteMs     int64 `json:"write_ms,omitempty"` // the previous record's write
}

// Options bound what a store keeps.
type Options struct {
	RetentionDays int
	MaxRecords    int
}

// Store is the record file, usage.jsonl, with what it holds in memory.
type Store struct {
	path string
	opts Options

	mu      sync.Mutex
	records []Record
	enabled bool
}

// Open reads the store in dir, dropping what is past its time or beyond its
// size. A store that cannot be read starts empty; the error says why.
func Open(dir string, opts Options, enabled bool) (*Store, error) {
	if opts.RetentionDays <= 0 {
		opts.RetentionDays = 90
	}
	if opts.MaxRecords <= 0 {
		opts.MaxRecords = 5000
	}
	s := &Store{path: filepath.Join(dir, "usage.jsonl"), opts: opts, enabled: enabled}
	if !enabled {
		return s, nil
	}
	return s, s.load()
}

// Path is where the records are.
func (s *Store) Path() string { return s.path }

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, bad, err := readFile(s.path)
	if err != nil {
		return err
	}
	kept := s.trim(all, time.Now())
	s.records = kept
	if len(kept) != len(all) || bad > 0 {
		return s.compact()
	}
	return nil
}

// readFile reads a record file as it is, a later line of a session
// replacing an earlier one. A missing file holds nothing. bad counts the
// lines that are not records of this version.
func readFile(path string) (all []Record, bad int, err error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	bySession := map[string]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var r Record
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.V != Version || r.Session == "" {
			bad++
			continue
		}
		if i, ok := bySession[r.Session]; ok {
			all[i] = r
			continue
		}
		bySession[r.Session] = len(all)
		all = append(all, r)
	}
	return all, bad, sc.Err()
}

// Read returns the records in dir as the file holds them, oldest first,
// without trimming or writing anything: for reading the store while the
// app may be running.
func Read(dir string) ([]Record, error) {
	all, _, err := readFile(filepath.Join(dir, "usage.jsonl"))
	sort.SliceStable(all, func(i, j int) bool { return all[i].Time.Before(all[j].Time) })
	return all, err
}

// Label confirms whether a session's initial feature was the right one,
// in the store in dir. It appends the labelled record, which replaces the
// earlier line, so a running app's store loses nothing and keeps the label
// when it next rewrites. Labels are how accuracy becomes known: nothing
// else sets Confirmed.
func Label(dir, session string, right bool) error {
	path := filepath.Join(dir, "usage.jsonl")
	unlock, err := lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	all, _, err := readFile(path)
	if err != nil {
		return err
	}
	for _, r := range all {
		if r.Session == session {
			r.Confirmed = &right
			return appendRecord(path, r)
		}
	}
	return fmt.Errorf("memory: no session %s", session)
}

func appendRecord(path string, r Record) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(r)
	_, err = f.Write(append(b, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func (s *Store) trim(all []Record, now time.Time) []Record {
	cutoff := now.AddDate(0, 0, -s.opts.RetentionDays)
	var kept []Record
	for _, r := range all {
		if r.Time.After(cutoff) {
			kept = append(kept, r)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Time.Before(kept[j].Time) })
	if n := len(kept) - s.opts.MaxRecords; n > 0 {
		kept = kept[n:]
	}
	return kept
}

// compact writes the file again, trimmed, from what the file holds: never
// from this store's snapshot, which may be stale, since another process
// (gloss label, a second Gloss) may have added, labelled or cleared since.
// The store then holds what was written. Called with mu held.
func (s *Store) compact() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	unlock, err := lock(s.path)
	if err != nil {
		return err
	}
	defer unlock()
	disk, _, err := readFile(s.path)
	if err != nil {
		return err
	}
	kept := s.trim(disk, time.Now())
	var buf bytes.Buffer
	for _, r := range kept {
		b, _ := json.Marshal(r)
		buf.Write(b)
		buf.WriteByte('\n')
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.records = kept
	return nil
}

// Enabled says whether the store reads and writes.
func (s *Store) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

// SetEnabled turns the store on or off. Off, it neither records nor
// answers: routing then works as on a first day.
func (s *Store) SetEnabled(on bool) error {
	s.mu.Lock()
	was := s.enabled
	s.enabled = on
	s.mu.Unlock()
	if on && !was {
		return s.load()
	}
	if !on {
		s.mu.Lock()
		s.records = nil
		s.mu.Unlock()
	}
	return nil
}

// Add records a use. It writes one line; the caller does not wait on it for
// anything the user sees. It returns how long the write took.
func (s *Store) Add(r Record) (time.Duration, error) {
	start := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled {
		return 0, nil
	}
	r.V = Version
	if r.Session == "" {
		return 0, errors.New("memory: a record needs a session")
	}
	replaced := false
	for i := range s.records {
		if s.records[i].Session == r.Session {
			s.records[i], replaced = r, true
		}
	}
	if !replaced {
		s.records = append(s.records, r)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return time.Since(start), err
	}
	unlock, err := lock(s.path)
	if err != nil {
		return time.Since(start), err
	}
	err = appendRecord(s.path, r)
	unlock()
	if err == nil && len(s.records) > s.opts.MaxRecords+s.opts.MaxRecords/10 {
		err = s.compact()
	}
	return time.Since(start), err
}

// Records returns a copy of what the store holds, oldest first.
func (s *Store) Records() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Record(nil), s.records...)
}

// Clear forgets every use, and so every preference drawn from them.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = nil
	unlock, err := lock(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil // no directory, so nothing kept
	}
	if err != nil {
		return err
	}
	defer unlock()
	err = os.Remove(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Label confirms whether a session's initial feature was the right one,
// as the package's Label does, and keeps the label in this store too.
func (s *Store) Label(session string, right bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := Label(filepath.Dir(s.path), session, right); err != nil {
		return err
	}
	for i := range s.records {
		if s.records[i].Session == session {
			s.records[i].Confirmed = &right
		}
	}
	return nil
}
