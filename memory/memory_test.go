package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	now     = time.Now().UTC() // the clock, as Open trims by it
	enabled = []string{"translate", "explain", "prompt", "agent"}
	claude  = "com.anthropic.claudefordesktop"
	safari  = "com.apple.Safari"
)

func rec(session, app, initial, final string, ago time.Duration, mod ...func(*Record)) Record {
	r := Record{V: Version, Session: session, Time: now.Add(-ago), Routing: true, App: app,
		Shape: ShapeOf("a short sentence to read"), Initial: initial, InitialBy: ByRule, Final: final, Outcome: "done"}
	if initial != final {
		r.Corrections = []Correction{{From: initial, To: final, AtMs: 900}}
		r.Timings.ExtraWaitMs = 400
	}
	for _, m := range mod {
		m(&r)
	}
	return r
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, Options{}, true)
	if err != nil {
		t.Fatal(err)
	}
	r := rec("s1", claude, "translate", "explain", time.Minute)
	if _, err := s.Add(r); err != nil {
		t.Fatal(err)
	}
	r.Copied = true
	s.Add(r) // the same session again replaces it
	s.Add(rec("s2", safari, "translate", "translate", time.Minute))
	s2, err := Open(dir, Options{}, true)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.Records()
	if len(got) != 2 || !got[0].Copied || got[0].Corrections[0].To != "explain" {
		t.Fatalf("records = %+v", got)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "usage.jsonl"))
	if strings.Contains(string(raw), "a short sentence") {
		t.Error("the file holds selected text")
	}
	if err := s2.Label("s2", true); err != nil {
		t.Fatal(err)
	}
	s3, _ := Open(dir, Options{}, true)
	if c := s3.Records()[1].Confirmed; c == nil || !*c {
		t.Error("label lost")
	}
}

func TestRetentionAndLimit(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir, Options{RetentionDays: 30, MaxRecords: 3}, true)
	s.Add(Record{Session: "old", Time: time.Now().AddDate(0, 0, -40)})
	for i := range 4 {
		s.Add(Record{Session: string(rune('a' + i)), Time: time.Now().Add(time.Duration(i) * time.Second)})
	}
	os.WriteFile(filepath.Join(dir, "usage.jsonl"), append(mustRead(t, dir), []byte("garbage\n")...), 0o600)
	s2, err := Open(dir, Options{RetentionDays: 30, MaxRecords: 3}, true)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range s2.Records() {
		ids = append(ids, r.Session)
	}
	if strings.Join(ids, "") != "bcd" {
		t.Errorf("kept %v", ids)
	}
	if strings.Contains(string(mustRead(t, dir)), "garbage") {
		t.Error("did not compact")
	}
}

func mustRead(t *testing.T, dir string) []byte {
	b, err := os.ReadFile(filepath.Join(dir, "usage.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDisabledAndClear(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir, Options{}, false)
	s.Add(rec("x", claude, "explain", "explain", 0))
	if len(s.Records()) != 0 {
		t.Error("recorded while off")
	}
	if _, err := os.Stat(filepath.Join(dir, "usage.jsonl")); err == nil {
		t.Error("wrote while off")
	}
	s.SetEnabled(true)
	s.Add(rec("y", claude, "explain", "explain", 0))
	if len(s.Records()) != 1 {
		t.Error("did not record once on")
	}
	if err := s.Clear(); err != nil || len(s.Records()) != 0 {
		t.Errorf("clear: %v", err)
	}
	if snap := Prefer(s.Records(), Situation{App: claude}, enabled, now); snap.All.Top != "" {
		t.Error("preferences survive a clear")
	}
}

func TestWriteFailureIsReported(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, nil, 0o600)
	s, _ := Open(filepath.Join(file, "sub"), Options{}, true) // a directory under a file
	if _, err := s.Add(rec("x", claude, "explain", "explain", 0)); err == nil {
		t.Error("no error")
	}
	if len(s.Records()) != 1 {
		t.Error("lost the record in memory")
	}
}

func TestPreferNoMemory(t *testing.T) {
	snap := Prefer(nil, Situation{App: claude}, enabled, now)
	if !snap.All.Insufficient || snap.All.Top != "" || len(snap.Recent) != 0 {
		t.Errorf("snap = %+v", snap)
	}
	if _, ok := snap.Similar.Lean(0.6); ok {
		t.Error("leans with no evidence")
	}
	if !strings.Contains(snap.Summary(), "没有记录") {
		t.Errorf("summary = %s", snap.Summary())
	}
}

func TestPreferFewRecordsIsInsufficient(t *testing.T) {
	recs := []Record{rec("1", claude, "translate", "explain", time.Hour)}
	snap := Prefer(recs, Situation{App: claude, Shape: ShapeOf("a short sentence to read")}, enabled, now)
	if snap.App.Top != "explain" || !snap.App.Insufficient {
		t.Errorf("app = %+v", snap.App)
	}
	if len(snap.Corrections) != 1 || snap.Recent[0] != "explain" {
		t.Errorf("snap = %+v", snap)
	}
}

// The same text leans differently in different apps.
func TestPreferDependsOnApp(t *testing.T) {
	var recs []Record
	for i := range 4 {
		recs = append(recs, rec("c"+string(rune('0'+i)), claude, "translate", "explain", time.Duration(i)*time.Hour))
		recs = append(recs, rec("s"+string(rune('0'+i)), safari, "explain", "translate", time.Duration(i)*time.Hour))
	}
	sh := ShapeOf("a short sentence to read")
	c := Prefer(recs, Situation{App: claude, Shape: sh}, enabled, now)
	s := Prefer(recs, Situation{App: safari, Shape: sh}, enabled, now)
	if id, ok := c.Similar.Lean(0.6); !ok || id != "explain" {
		t.Errorf("claude leans %q %v: %+v", id, ok, c.Similar)
	}
	if id, ok := s.Similar.Lean(0.6); !ok || id != "translate" {
		t.Errorf("safari leans %q %v", id, ok)
	}
	if !c.All.Conflicting {
		t.Errorf("all should conflict: %+v", c.All)
	}
	if c.Summary() == s.Summary() {
		t.Error("same summary in different apps")
	}
}

// Recent choices outweigh old ones, and routed results the user only saw
// count for nothing.
func TestPreferDriftsAndDoesNotReinforceItself(t *testing.T) {
	var recs []Record
	for i := range 5 {
		recs = append(recs, rec("old"+string(rune('0'+i)), claude, "explain", "translate", 90*24*time.Hour))
	}
	for i := range 3 {
		recs = append(recs, rec("new"+string(rune('0'+i)), claude, "translate", "explain", time.Hour))
	}
	for i := range 20 {
		recs = append(recs, rec("auto"+string(rune('a'+i)), claude, "agent", "agent", time.Minute))
	}
	snap := Prefer(recs, Situation{App: claude, Shape: ShapeOf("a short sentence to read")}, enabled, now)
	if id, ok := snap.App.Lean(0.6); !ok || id != "explain" {
		t.Errorf("leans %q %v: %+v", id, ok, snap.App)
	}
	if snap.App.Shares["agent"] != 0 {
		t.Error("unconfirmed routing reinforced itself")
	}
	// A disabled feature is left out.
	snap = Prefer(recs, Situation{App: claude}, []string{"translate", "agent"}, now)
	if _, ok := snap.App.Shares["explain"]; ok {
		t.Error("disabled feature counted")
	}
}

func TestStats(t *testing.T) {
	yes, no := true, false
	recs := []Record{
		rec("1", claude, "translate", "explain", time.Hour, func(r *Record) { r.Timings.RouteMs = 2 }),
		rec("2", claude, "explain", "explain", time.Hour, func(r *Record) { r.Confirmed = &yes; r.Timings.RouteMs = 1 }),
		rec("3", claude, "explain", "explain", time.Hour, func(r *Record) { r.InitialBy = ByDefault; r.Timings.RouteMs = 1 }),
		rec("4", claude, "agent", "explain", time.Hour, func(r *Record) { r.Confirmed = &no; r.Timings.RouteMs = 700; r.InitialBy = ByJev }),
		rec("5", claude, "explain", "explain", time.Hour, func(r *Record) { r.Routing = false }),
	}
	s := Summarize(recs, now.Add(-24*time.Hour))
	if s.Uses != 5 || s.Routed != 4 || s.Corrected != 2 || s.Labelled != 2 || s.Right != 1 || s.Unknown != 1 || s.Fallback != 1 {
		t.Fatalf("stats = %+v", s)
	}
	if s.CorrectionRate() != 0.5 || s.UnknownRate() != 0.25 || s.FallbackRate() != 0.25 {
		t.Errorf("rates %v %v %v", s.CorrectionRate(), s.UnknownRate(), s.FallbackRate())
	}
	// Known: two labels and one unlabelled correction; right: one.
	if got := s.ConfirmedAccuracy(); got < 0.333 || got > 0.334 {
		t.Errorf("confirmed accuracy = %v", got)
	}
	if s.RouteP95 != 2*time.Millisecond || s.ExtraWaitP50 != 400*time.Millisecond {
		t.Errorf("latency %v %v", s.RouteP95, s.ExtraWaitP50)
	}
	if !strings.Contains(s.String(), "未知比例") {
		t.Error(s.String())
	}
}

func TestShape(t *testing.T) {
	for text, want := range map[string]string{
		"serendipity":                              "term latin",
		"注意力机制":                                    "term cjk",
		"The cat sat on the mat today.":            "sentence latin",
		"我们在 Go 里用 channel。":                       "sentence mixed",
		"## Summary\n- fixed `a.go`\n- tests pass": "structured latin",
	} {
		s := ShapeOf(text)
		if got := s.Class() + " " + s.Script; got != want {
			t.Errorf("%q = %s, want %s (%+v)", text, got, want, s)
		}
	}
	if s := ShapeOf("I've updated app/panel.go and verified the tests pass. Next steps: run make check."); s.AgentMarks < 3 {
		t.Errorf("agent marks = %d", s.AgentMarks)
	}
	if s := ShapeOf("请帮我写一个函数，要求不要用递归"); s.AskMarks < 2 {
		t.Errorf("ask marks = %d", s.AskMarks)
	}
}

// The app's store and the gloss command share the file: a label or a
// session one writes survives the other's rewrite, and reading trims
// nothing.
func TestTwoWritersShareTheFile(t *testing.T) {
	dir := t.TempDir()
	app, _ := Open(dir, Options{MaxRecords: 4}, true)
	// A record past the default retention, as a longer one would keep.
	// Times from the clock, as trimming reads it.
	at := func(ago time.Duration) func(*Record) { return func(r *Record) { r.Time = time.Now().Add(-ago) } }
	if err := appendRecord(app.Path(), rec("old", claude, "translate", "translate", 0, at(200*24*time.Hour))); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if rs, _ := Read(dir); len(rs) != 1 {
			t.Fatalf("read %d: %d records", i, len(rs))
		}
	}
	for i, id := range []string{"a1", "a2", "a3", "a4"} {
		app.Add(rec(id, claude, "translate", "translate", 0, at(time.Duration(10-i)*time.Minute)))
	}
	if err := Label(dir, "a4", false); err != nil {
		t.Fatal(err)
	}
	// Past the limit: the app's store rewrites the file from its snapshot,
	// which has no label.
	app.Add(rec("a5", claude, "translate", "translate", 0, at(time.Minute)))
	rs, _ := Read(dir)
	found := map[string]Record{}
	for _, r := range rs {
		found[r.Session] = r
	}
	if c := found["a4"].Confirmed; c == nil || *c {
		t.Errorf("label = %v, records %d", c, len(rs))
	}
	if _, ok := found["a5"]; !ok || len(rs) != 4 {
		t.Errorf("kept %d records, newest %v", len(rs), ok)
	}
}

func TestRewriteKeepsAnotherWritersSession(t *testing.T) {
	dir := t.TempDir()
	app, _ := Open(dir, Options{}, true)
	app.Add(rec("a1", claude, "translate", "translate", time.Minute))
	other, _ := Open(dir, Options{}, true)
	other.Add(rec("b1", safari, "explain", "explain", time.Minute))
	if err := app.Label("a1", true); err != nil { // a rewrite from the app's snapshot
		t.Fatal(err)
	}
	rs, _ := Read(dir)
	if len(rs) != 2 {
		t.Fatalf("records = %+v", rs)
	}
}

// A store's stale snapshot never comes back: not after another clears the
// file, and not over a newer label.
func TestStaleSnapshotDoesNotReturn(t *testing.T) {
	at := func(ago time.Duration) func(*Record) { return func(r *Record) { r.Time = time.Now().Add(-ago) } }
	dir := t.TempDir()
	app, _ := Open(dir, Options{MaxRecords: 2}, true)
	app.Add(rec("a1", claude, "translate", "translate", 0, at(3*time.Minute)))
	app.Add(rec("a2", claude, "translate", "translate", 0, at(2*time.Minute)))
	other, _ := Open(dir, Options{MaxRecords: 2}, true)
	if err := other.Label("a2", true); err != nil {
		t.Fatal(err)
	}
	if err := Label(dir, "a2", false); err != nil { // a newer label
		t.Fatal(err)
	}
	if err := other.Label("a2", true); err != nil { // then the store's own, newest
		t.Fatal(err)
	}
	if err := Label(dir, "a2", false); err != nil { // and the command's, newest of all
		t.Fatal(err)
	}
	app.Add(rec("a3", claude, "translate", "translate", 0, at(time.Minute))) // compacts
	for _, r := range mustRecords(t, dir) {
		if r.Session == "a2" && (r.Confirmed == nil || *r.Confirmed) {
			t.Errorf("a2 label = %v, want the newest (wrong)", r.Confirmed)
		}
	}
	if err := other.Clear(); err != nil {
		t.Fatal(err)
	}
	app.Add(rec("a4", claude, "translate", "translate", 0, at(0)))
	app.Add(rec("a5", claude, "translate", "translate", 0, at(0))) // compacts again
	var ids []string
	for _, r := range mustRecords(t, dir) {
		ids = append(ids, r.Session)
	}
	if strings.Join(ids, ",") != "a4,a5" {
		t.Errorf("after another cleared: %v", ids)
	}
}

func mustRecords(t *testing.T, dir string) []Record {
	t.Helper()
	rs, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}
