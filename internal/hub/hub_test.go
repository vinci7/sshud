package hub

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vinci7/sshud/internal/probeerr"
)

type fakeSource struct {
	calls  atomic.Int32
	mu     sync.Mutex
	levels []Level
	err    error
}

func (f *fakeSource) Probe(ctx context.Context, id string, level Level) (string, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.levels = append(f.levels, level)
	f.mu.Unlock()
	if f.err != nil {
		return "", f.err
	}
	return "@@stat1\ncpu  1 0 1 8 0 0 0 0\n@@stat2\ncpu  2 0 2 14 0 0 0 0\n@@hostname\n" + id + "\n", nil
}

func next(t *testing.T, s *Subscription) Update {
	t.Helper()
	select {
	case u := <-s.C:
		return u
	case <-time.After(2 * time.Second):
		t.Fatal("no update")
	}
	return Update{}
}

func TestSubscribeProbesAndStops(t *testing.T) {
	src := &fakeSource{}
	h := New(src)
	s := h.Subscribe([]string{"a"}, Overview)
	if u := next(t, s); u.Status != StatusConnecting {
		t.Fatalf("first = %+v", u)
	}
	u := next(t, s)
	if u.Status != StatusOK || u.Snapshot == nil || u.Snapshot.Host.Hostname != "a" || u.Snapshot.CPU.Percent != 25 || u.LastOK == 0 {
		t.Fatalf("update = %+v", u)
	}
	// a second subscriber gets the cached frame at once, without a new probe
	s2 := h.Subscribe([]string{"a"}, Overview)
	if u := next(t, s2); u.Status != StatusOK {
		t.Fatalf("cached = %+v", u)
	}
	s.Close()
	s2.Close()
	n := src.calls.Load()
	time.Sleep(50 * time.Millisecond)
	if src.calls.Load() != n {
		t.Error("probing continued after all subscribers left")
	}
}

func TestDetailSubscriberRaisesLevel(t *testing.T) {
	old := Interval
	Interval = map[Level]time.Duration{Overview: time.Hour, Detail: time.Hour}
	defer func() { Interval = old }()

	src := &fakeSource{}
	h := New(src)
	s := h.Subscribe([]string{"a"}, Overview)
	next(t, s)
	next(t, s)
	d := h.Subscribe([]string{"a"}, Detail)
	next(t, d) // cached
	next(t, d) // woken probe
	s.Close()
	d.Close()
	src.mu.Lock()
	defer src.mu.Unlock()
	if len(src.levels) != 2 || src.levels[1] != Detail {
		t.Errorf("levels = %v", src.levels)
	}
}

func TestErrorsAreClassified(t *testing.T) {
	oldDelay := RetryDelay
	RetryDelay = 10 * time.Millisecond
	defer func() { RetryDelay = oldDelay }()
	h := New(&fakeSource{err: &probeerr.Error{Kind: probeerr.ErrAuth, Err: errors.New("denied")}})
	s := h.Subscribe([]string{"a"}, Overview)
	defer s.Close()
	next(t, s)
	u := next(t, s) // reported after the second failed attempt
	if u.Status != StatusError || u.Error.Kind != probeerr.ErrAuth || u.Error.Message != "denied" {
		t.Errorf("update = %+v", u)
	}

	// retry probes again at once, despite the backoff
	if !h.Retry("a") {
		t.Fatal("retry refused for a watched machine")
	}
	if u := next(t, s); u.Status != StatusError {
		t.Errorf("after retry = %+v", u)
	}
	if h.Retry("nobody") {
		t.Error("retry accepted for an unwatched machine")
	}

	h = New(&fakeSource{err: errors.New("boom")})
	s2 := h.Subscribe([]string{"a"}, Overview)
	defer s2.Close()
	next(t, s2)
	if u := next(t, s2); u.Error.Kind != probeerr.ErrInternal {
		t.Errorf("unclassified = %+v", u)
	}
}

// flakySource fails the calls listed in failOn (1-based).
type flakySource struct {
	n      atomic.Int32
	failOn map[int32]bool
}

func (f *flakySource) Probe(ctx context.Context, id string, level Level) (string, error) {
	if f.failOn[f.n.Add(1)] {
		return "", &probeerr.Error{Kind: probeerr.ErrTimeout, Err: errors.New("lost")}
	}
	return "@@hostname\n" + id + "\n", nil
}

func TestTransientFailuresKeepLastFrame(t *testing.T) {
	old := Interval
	Interval = map[Level]time.Duration{Overview: 10 * time.Millisecond, Detail: 10 * time.Millisecond}
	oldDelay := RetryDelay
	RetryDelay = 10 * time.Millisecond
	defer func() { Interval, RetryDelay = old, oldDelay }()

	// probes 2 and 3 fail (below the threshold of 3), 5–7 fail (reaches it)
	h := New(&flakySource{failOn: map[int32]bool{2: true, 3: true, 5: true, 6: true, 7: true}})
	s := h.Subscribe([]string{"a"}, Overview)
	defer s.Close()
	var statuses []Status
	for len(statuses) < 3 {
		u := next(t, s)
		if u.Status != StatusConnecting {
			statuses = append(statuses, u.Status)
		}
	}
	// ok (1), ok (4: failures 2–3 were swallowed), error (7)
	if statuses[0] != StatusOK || statuses[1] != StatusOK || statuses[2] != StatusError {
		t.Errorf("statuses = %v", statuses)
	}
}

func TestStaleCachedFrameIsNotLive(t *testing.T) {
	old := Interval
	Interval = map[Level]time.Duration{Overview: time.Hour, Detail: time.Hour}
	defer func() { Interval = old }()
	h := New(&fakeSource{})
	s := h.Subscribe([]string{"a"}, Overview)
	next(t, s)
	next(t, s) // ok frame cached
	s.Close()

	// much later (more than three intervals) someone opens the page again
	h.now = func() time.Time { return time.Now().Add(4 * time.Hour) }
	s2 := h.Subscribe([]string{"a"}, Overview)
	defer s2.Close()
	u := next(t, s2)
	if u.Status != StatusConnecting || u.Snapshot == nil {
		t.Errorf("stale frame = %+v", u)
	}
}

func TestHistoryRingOverwritesOldest(t *testing.T) {
	var w worker
	base := time.UnixMilli(1_000_000)
	for i := range histCap + 10 { // 160 points, 1 s apart: all inside Window
		w.record(Point{Time: base.Add(time.Duration(i) * time.Second).UnixMilli()})
	}
	got := w.history(base.Add(159 * time.Second))
	if len(got) != 150 {
		t.Fatalf("len = %d, want 150", len(got))
	}
	// the 10 oldest (0..9 s) were overwritten: 10 s .. 159 s remain, in order
	if got[0].Time != 1_010_000 || got[149].Time != 1_159_000 {
		t.Fatalf("range = %d..%d", got[0].Time, got[149].Time)
	}
}

func TestHistoryDropsOlderThanWindow(t *testing.T) {
	var w worker
	for _, sec := range []int64{0, 100, 200, 400, 500} {
		w.record(Point{Time: sec * 1000})
	}
	// at 600 s the window starts at 300 s: only 400 s and 500 s are left
	got := w.history(time.UnixMilli(600_000))
	if len(got) != 2 || got[0].Time != 400_000 || got[1].Time != 500_000 {
		t.Fatalf("history = %+v", got)
	}
}

func TestDetailSubscriberGetsBackfill(t *testing.T) {
	old := Interval
	Interval = map[Level]time.Duration{Overview: time.Hour, Detail: time.Hour}
	defer func() { Interval = old }()

	h := New(&fakeSource{})
	var mu sync.Mutex
	now := time.UnixMilli(10_000_000)
	h.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	s := h.Subscribe([]string{"a"}, Overview)
	next(t, s)
	if u := next(t, s); u.History != nil {
		t.Fatalf("live frames carry no history: %+v", u.History)
	}
	mu.Lock()
	now = now.Add(2 * time.Second)
	mu.Unlock()
	h.Retry("a") // second probe, 2 s later
	next(t, s)

	d := h.Subscribe([]string{"a"}, Detail)
	u := next(t, d)
	// fakeSource's CPU is 25%; it has no net section, so rx is null
	if len(u.History) != 2 || u.History[0].Time != 10_000_000 || u.History[1].Time != 10_002_000 ||
		u.History[1].CPU == nil || *u.History[1].CPU != 25 || u.History[1].Rx != nil {
		t.Fatalf("backfill = %+v", u.History)
	}
	s.Close()
	d.Close()
}

func TestForgetDropsDeletedMachine(t *testing.T) {
	old := Interval
	Interval = map[Level]time.Duration{Overview: time.Millisecond, Detail: time.Millisecond}
	defer func() { Interval = old }()
	src := &fakeSource{}
	h := New(src)

	// deleted while someone watches it: probing stops at once
	s := h.Subscribe([]string{"a"}, Overview)
	next(t, s)
	next(t, s)
	h.Forget("a")
	time.Sleep(20 * time.Millisecond)
	n := src.calls.Load()
	time.Sleep(50 * time.Millisecond)
	if src.calls.Load() != n {
		t.Error("probing continued after Forget")
	}
	s.Close()

	// deleted after everyone left
	s = h.Subscribe([]string{"b"}, Overview)
	next(t, s)
	s.Close()
	h.Forget("b")
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.machines) != 0 {
		t.Errorf("%d machines left after Forget", len(h.machines))
	}
}
