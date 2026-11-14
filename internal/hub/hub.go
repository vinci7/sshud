// Package hub schedules probes on demand: a machine is only probed while at
// least one subscriber is watching it, at the fastest interval any of them
// asked for. It knows nothing about SSH (see Source) or the UI.
package hub

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/vinci7/sshud/internal/derive"
	"github.com/vinci7/sshud/internal/parse"
	"github.com/vinci7/sshud/internal/probeerr"
)

// Level selects how much a probe collects.
type Level int

const (
	Overview Level = iota
	Detail
)

func (l Level) String() string {
	if l == Detail {
		return "detail"
	}
	return "overview"
}

// FailThreshold is how many probes in a row must fail before a machine that
// was reachable is reported as down. Lossy links drop single probes; flipping
// a card to "offline" for each of them would make the dashboard untrustworthy.
var FailThreshold = 3

// RetryDelay is the pause before retrying a tolerated failure.
var RetryDelay = 2 * time.Second

// Interval is how often a watched machine is probed at each level.
var Interval = map[Level]time.Duration{Overview: 5 * time.Second, Detail: 2 * time.Second}

// Window of recent points kept in memory so a chart opens with a curve. Only
// frames probed for a watcher are kept; nothing is probed to fill it. histCap
// bounds the ring: one point per detail interval (2 s) over Window.
const (
	Window  = 5 * time.Minute
	histCap = 150
)

// Point is the part of a frame the live charts draw.
type Point struct {
	Time int64    `json:"time"`
	CPU  *float64 `json:"cpu"`
	Rx   *float64 `json:"rx"`
	Tx   *float64 `json:"tx"`
}

// Source runs the probe on a machine and returns its raw output.
type Source interface {
	Probe(ctx context.Context, machineID string, level Level) (string, error)
}

// Status of a machine as seen by subscribers.
type Status string

const (
	StatusConnecting Status = "connecting"
	StatusOK         Status = "ok"
	StatusError      Status = "error"
)

// Update is one message to subscribers about one machine.
type Update struct {
	Machine  string           `json:"machine"`
	Status   Status           `json:"status"`
	Error    *ErrorInfo       `json:"error,omitempty"`
	LastOK   int64            `json:"lastOk,omitempty"` // unix ms of the last good frame
	Snapshot *derive.Snapshot `json:"snapshot,omitempty"`
	// History: the last Window, oldest first; first detail update only.
	History []Point `json:"history,omitempty"`
}

// ErrorInfo describes the last failure.
type ErrorInfo struct {
	Kind    probeerr.Kind `json:"kind"`
	Message string        `json:"message"`
}

// Hub fans probe results out to subscribers.
type Hub struct {
	src Source
	now func() time.Time

	mu       sync.Mutex
	machines map[string]*worker
}

// New returns a hub probing through src.
func New(src Source) *Hub {
	return &Hub{src: src, now: time.Now, machines: map[string]*worker{}}
}

type worker struct {
	id      string
	subs    map[*Subscription]Level
	cancel  context.CancelFunc
	wake    chan struct{}
	last    *Update
	lastOK  int64
	state   derive.State
	backoff time.Duration
	fails   int // consecutive failures
	hist    [histCap]Point
	written int // points ever recorded; hist[written%histCap] is next to go
}

// record adds p to the ring, overwriting the oldest point when it is full.
func (w *worker) record(p Point) {
	w.hist[w.written%histCap] = p
	w.written++
}

// history returns the points from the last Window before now, oldest first.
func (w *worker) history(now time.Time) []Point {
	cut := now.Add(-Window).UnixMilli()
	var out []Point
	for i := max(0, w.written-histCap); i < w.written; i++ {
		if p := w.hist[i%histCap]; p.Time >= cut {
			out = append(out, p)
		}
	}
	return out
}

// Subscription receives updates for the machines it watches.
type Subscription struct {
	C   chan Update
	hub *Hub
	ids []string
}

// Subscribe starts watching ids at level. The last known update of each
// machine is delivered immediately. Call Close when done.
func (h *Hub) Subscribe(ids []string, level Level) *Subscription {
	s := &Subscription{C: make(chan Update, 4*len(ids)+4), hub: h, ids: ids}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, id := range ids {
		w := h.machines[id]
		if w == nil {
			w = &worker{id: id, subs: map[*Subscription]Level{}, wake: make(chan struct{}, 1)}
			h.machines[id] = w
		}
		prev := h.level(w)
		w.subs[s] = level
		if w.last != nil {
			u := *w.last
			// a frame from before anyone was watching is shown, but not as
			// live: it says "connecting" until a fresh probe arrives
			if u.Status == StatusOK && u.Snapshot != nil && h.now().Sub(time.UnixMilli(u.Snapshot.Time)) > 3*Interval[level] {
				u.Status = StatusConnecting
			}
			if level == Detail {
				u.History = w.history(h.now())
			}
			s.send(u)
		} else {
			s.send(Update{Machine: id, Status: StatusConnecting})
		}
		if w.cancel == nil {
			ctx, cancel := context.WithCancel(context.Background())
			w.cancel = cancel
			go h.run(ctx, w)
		} else if level > prev {
			// a more detailed level was requested: probe now
			select {
			case w.wake <- struct{}{}:
			default:
			}
		}
	}
	return s
}

// Retry probes a watched machine now instead of waiting out its backoff.
// It reports false when nobody is watching the machine.
func (h *Hub) Retry(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	w := h.machines[id]
	if w == nil || w.cancel == nil {
		return false
	}
	w.backoff = 0
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return true
}

// Forget drops what is known about a deleted machine and stops probing it,
// even while someone still watches it.
func (h *Hub) Forget(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if w := h.machines[id]; w != nil && w.cancel != nil {
		w.cancel()
	}
	delete(h.machines, id)
}

// Close stops the subscription; machines nobody watches stop being probed.
func (s *Subscription) Close() {
	h := s.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, id := range s.ids {
		w := h.machines[id]
		if w == nil {
			continue
		}
		delete(w.subs, s)
		if len(w.subs) == 0 && w.cancel != nil {
			w.cancel()
			w.cancel = nil
		}
	}
}

// send never blocks: a slow subscriber drops frames instead of stalling probes.
func (s *Subscription) send(u Update) {
	select {
	case s.C <- u:
	default:
	}
}

// level returns the most detailed level any subscriber of w wants.
func (h *Hub) level(w *worker) Level {
	l := Overview
	for _, sl := range w.subs {
		if sl > l {
			l = sl
		}
	}
	return l
}

func (h *Hub) run(ctx context.Context, w *worker) {
	for ctx.Err() == nil {
		h.mu.Lock()
		level := h.level(w)
		h.mu.Unlock()

		started := time.Now()
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		raw, err := h.src.Probe(pctx, w.id, level)
		cancel()
		if ctx.Err() != nil {
			return
		}
		// the interval is measured from the start of the probe, so slow
		// links still get a frame every Interval
		wait := Interval[level] - time.Since(started)

		h.mu.Lock()
		u := Update{Machine: w.id}
		threshold := FailThreshold
		if w.lastOK == 0 {
			// never connected: nothing to keep showing, so give up sooner,
			// but still forgive one lost first attempt
			threshold = 2
		}
		if err != nil && w.fails+1 < threshold {
			// tolerate a transient failure: keep the last frame (or the
			// connecting state), retry soon
			w.fails++
			h.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-w.wake:
			case <-time.After(RetryDelay):
			}
			continue
		}
		if err != nil {
			w.fails++
			var pe *probeerr.Error
			if !errors.As(err, &pe) {
				pe = &probeerr.Error{Kind: probeerr.ErrInternal, Err: err}
			}
			u.Status = StatusError
			u.Error = &ErrorInfo{Kind: pe.Kind, Message: pe.Err.Error()}
			// keep showing the last good numbers next to the error
			if w.last != nil {
				u.Snapshot = w.last.Snapshot
			}
			w.backoff = min(max(2*w.backoff, 2*time.Second), 60*time.Second)
			wait = w.backoff
		} else {
			snap := w.state.Update(parse.Split(raw), h.now())
			u.Status = StatusOK
			u.Snapshot = &snap
			w.lastOK = snap.Time
			p := Point{Time: snap.Time}
			if snap.CPU != nil {
				p.CPU = &snap.CPU.Percent
			}
			if snap.Net != nil {
				p.Rx, p.Tx = snap.Net.RxRate, snap.Net.TxRate
			}
			w.record(p)
			w.backoff = 0
			w.fails = 0
		}
		u.LastOK = w.lastOK
		w.last = &u
		for s := range w.subs {
			s.send(u)
		}
		h.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-time.After(wait):
		}
	}
}
