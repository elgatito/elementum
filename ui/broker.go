package ui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ringSize     = 50
	queueSize    = 64
	clientGrace  = 10 * time.Second // a client stays connected this long after its last poll ended, plus its wait
	maxClients   = 16
	maxText      = 4096
	tombstoneTTL = 5 * time.Minute
	sweepEvery   = 15 * time.Second
	timeoutAsk   = 60 * time.Second
	timeoutInput = 5 * time.Minute
)

// ClientIDRe is the shape of a client_id.
var ClientIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

var (
	ErrUnknown = errors.New("unknown ui request")
	ErrClosed  = errors.New("ui request is closed")
	ErrInvalid = errors.New("invalid answer")
)

// Event kinds.
const (
	KindNotify    = "notify"
	KindConfirm   = "confirm"
	KindSelect    = "select"
	KindInput     = "input"
	KindText      = "text"
	KindProgress  = "progress"
	KindCancelled = "cancelled"
)

// Event is what a client receives. Title and Message are raw ("LOCALIZE[..]") until Translated is called.
type Event struct {
	ID        string
	Seq       int64
	Kind      string
	Title     string
	Message   string
	Options   []string
	Preselect []int
	Multi     bool
	Hidden    bool
	Default   any
	TimeoutS  int
	Progress  *ProgressEvent
}

// ProgressEvent is the state of a progress dialog.
type ProgressEvent struct {
	ID      string
	Percent int
	Heading string
	Line    string
	Done    bool
}

// Translated returns a copy with every text resolved in lang.
func (e Event) Translated(lang string) Event {
	tr := func(s string) string { return plain(Translate(lang, s)) }
	e.Title, e.Message = tr(e.Title), tr(e.Message)
	if e.Options != nil {
		o := make([]string, len(e.Options))
		for i, s := range e.Options {
			o[i] = tr(s)
		}
		e.Options = o
	}
	if e.Progress != nil {
		p := *e.Progress
		p.Heading, p.Line = tr(p.Heading), tr(p.Line)
		e.Progress = &p
	}
	return e
}

// Answer is a client's reply.
type Answer struct {
	Yes      bool
	Selected []int
	Text     string
	Cancel   bool
}

type request struct {
	ev       Event
	ch       chan Answer // buffered 1; written once, under the broker lock
	deadline time.Time   // zero: no timeout
	closed   bool
	closedAt time.Time
	canceled atomic.Bool // progress: the client asked to stop
}

type client struct {
	until   time.Time // connected until then when no poll is in flight
	polling int
	queue   []Event
	wake    chan struct{}
}

// Broker queues UI events for polling clients and collects their answers.
type Broker struct {
	mu      sync.Mutex
	epoch   string // random per process: clients detect a restart by it
	seq     int64
	ring    []Event
	clients map[string]*client
	reqs    map[string]*request
	now     func() time.Time
	sweeper sync.Once
}

// NewBroker returns an empty broker.
func NewBroker() *Broker {
	return &Broker{epoch: newID(), clients: map[string]*client{}, reqs: map[string]*request{}, now: time.Now}
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// SinceFor returns the since to poll with: a client that names another epoch than this process missed a
// restart, so it starts from 0 and gets the ring replayed.
func (b *Broker) SinceFor(epoch string, since *int64) *int64 {
	if epoch != "" && epoch != b.epoch {
		zero := int64(0)
		return &zero
	}
	return since
}

// Epoch identifies this process; it changes on every restart.
func (b *Broker) Epoch() string { return b.epoch }

// Connected reports whether a client is polling now or its last poll ended a moment ago.
func (b *Broker) Connected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked()
	return len(b.clients) > 0
}

func (b *Broker) sweepLocked() {
	now := b.now()
	for id, c := range b.clients {
		if c.polling == 0 && now.After(c.until) {
			delete(b.clients, id)
		}
	}
	for id, r := range b.reqs {
		if r.closed && now.Sub(r.closedAt) > tombstoneTTL {
			delete(b.reqs, id)
		}
	}
}

// Sweep drops idle clients and old closed requests.
func (b *Broker) Sweep() {
	b.mu.Lock()
	b.sweepLocked()
	b.mu.Unlock()
}

// publishLocked stamps the event and queues it for every client. A queued progress of the same dialog is replaced;
// a full queue loses its oldest toast/progress first, never a question.
func (b *Broker) publishLocked(ev Event) Event {
	b.seq++
	ev.Seq = b.seq
	b.ring = append(b.ring, ev)
	if len(b.ring) > ringSize {
		b.ring = b.ring[len(b.ring)-ringSize:]
	}
	for _, c := range b.clients {
		c.enqueue(ev)
	}
	return ev
}

func (c *client) enqueue(ev Event) {
	if ev.Kind == KindProgress {
		for i, q := range c.queue {
			if q.Kind == KindProgress && q.ID == ev.ID {
				c.queue = append(c.queue[:i], c.queue[i+1:]...)
				break
			}
		}
	}
	if len(c.queue) >= queueSize {
		// Oldest toast/progress first, then the oldest question; cancelled events are never dropped.
		drop := -1
		for pass := 0; pass < 2 && drop < 0; pass++ {
			for i, q := range c.queue {
				if q.Kind == KindCancelled || (pass == 0 && q.Kind != KindNotify && q.Kind != KindProgress) {
					continue
				}
				drop = i
				break
			}
		}
		if drop >= 0 {
			c.queue = append(c.queue[:drop], c.queue[drop+1:]...)
		}
	}
	c.queue = append(c.queue, ev)
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Poll returns the events for a client, waiting up to wait for the first one. since is the last seq the client
// has handled (nil: the queue is emptied by this call). A since above the broker's seq comes from before a
// restart and counts as 0. An unknown client with since set gets the ring's events after it; a client that
// registers always gets every open question and progress dialog.
func (b *Broker) Poll(ctx context.Context, id string, since *int64, wait time.Duration) []Event {
	b.sweeper.Do(func() {
		go func() {
			for range time.Tick(sweepEvery) {
				b.Sweep()
			}
		}()
	})
	b.mu.Lock()
	if since != nil && *since > b.seq {
		zero := int64(0)
		since = &zero
	}
	c := b.clients[id]
	isNew := c == nil
	if isNew {
		c = b.registerLocked(id, since)
	}
	c.polling++
	defer func() {
		b.mu.Lock()
		c.polling--
		c.until = b.now().Add(wait + clientGrace)
		b.mu.Unlock()
	}()
	if since != nil { // acknowledge
		n := 0
		for _, ev := range c.queue {
			if ev.Seq > *since {
				c.queue[n] = ev
				n++
			}
		}
		c.queue = c.queue[:n]
	}
	if isNew { // after the acknowledgement: open requests always reach a (re)registering client
		b.addOpenLocked(c)
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		if len(c.queue) > 0 {
			out := append([]Event(nil), c.queue...)
			if since == nil {
				c.queue = nil
			}
			b.mu.Unlock()
			return out
		}
		b.mu.Unlock()
		select {
		case <-c.wake:
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return nil
		}
		b.mu.Lock()
	}
}

// registerLocked adds a client, evicting the oldest one when there are too many.
func (b *Broker) registerLocked(id string, since *int64) *client {
	if len(b.clients) >= maxClients {
		var oldest string
		var oc *client
		for k, c := range b.clients {
			if oc == nil || (c.polling == 0 && (oc.polling > 0 || c.until.Before(oc.until))) {
				oldest, oc = k, c
			}
		}
		delete(b.clients, oldest)
	}
	c := &client{wake: make(chan struct{}, 1), until: b.now().Add(clientGrace)}
	b.clients[id] = c
	if since != nil {
		for _, ev := range b.ring {
			if ev.Seq > *since {
				c.queue = append(c.queue, ev)
			}
		}
	}
	return c
}

func (b *Broker) addOpenLocked(c *client) {
	now := b.now()
	for _, r := range b.reqs {
		if r.closed {
			continue
		}
		ev := r.ev
		if !r.deadline.IsZero() {
			ev.TimeoutS = max(1, int((r.deadline.Sub(now)+time.Second-1)/time.Second))
		}
		dup := false
		for i, q := range c.queue {
			if q.ID == ev.ID && q.Kind == ev.Kind {
				if ev.Kind == KindProgress {
					c.queue[i] = ev // the latest state, not an old ring copy
				}
				dup = true
				break
			}
		}
		if !dup {
			c.queue = append(c.queue, ev)
		}
	}
	sortBySeq(c.queue)
}

func sortBySeq(evs []Event) {
	for i := 1; i < len(evs); i++ {
		for j := i; j > 0 && evs[j].Seq < evs[j-1].Seq; j-- {
			evs[j], evs[j-1] = evs[j-1], evs[j]
		}
	}
}

// Seq is the sequence number of the latest event.
func (b *Broker) Seq() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.seq
}

// Notify queues a toast.
func (b *Broker) Notify(n Notice) {
	b.mu.Lock()
	b.publishLocked(Event{ID: newID(), Kind: KindNotify, Title: n.Title, Message: n.Message})
	b.mu.Unlock()
}

// ask publishes a question and waits for the first answer, the timeout or ctx.
func (b *Broker) ask(ctx context.Context, ev Event, timeout time.Duration) (Answer, bool) {
	ev.ID = newID()
	ev.TimeoutS = int(timeout.Seconds())
	r := &request{ev: ev, ch: make(chan Answer, 1)}
	b.mu.Lock()
	r.deadline = b.now().Add(timeout)
	b.reqs[ev.ID] = r
	r.ev = b.publishLocked(ev)
	b.mu.Unlock()

	var a Answer
	var ok bool
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case a = <-r.ch:
		ok = !a.Cancel
	case <-timer.C:
	case <-ctx.Done():
	}

	b.mu.Lock()
	if !r.closed {
		r.closed, r.closedAt = true, b.now()
	} else if !ok && len(r.ch) > 0 { // answered just as the timer fired
		a = <-r.ch
		ok = !a.Cancel
	}
	// Tell the other clients to close their dialogs.
	b.publishLocked(Event{ID: ev.ID, Kind: KindCancelled})
	b.mu.Unlock()
	return a, ok
}

// Answer delivers a client's reply. The first valid one wins; later ones get ErrClosed, unknown ids ErrUnknown,
// and an invalid answer ErrInvalid (the question stays open).
func (b *Broker) Answer(id string, a Answer) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.reqs[id]
	if r == nil {
		return ErrUnknown
	}
	if r.closed {
		return ErrClosed
	}
	if r.ev.Kind == KindProgress {
		if a.Cancel {
			r.canceled.Store(true)
		}
		return nil
	}
	if !a.Cancel {
		switch r.ev.Kind {
		case KindSelect:
			if len(a.Selected) == 0 || (!r.ev.Multi && len(a.Selected) != 1) {
				return ErrInvalid
			}
			seen := map[int]bool{}
			for _, i := range a.Selected {
				if i < 0 || i >= len(r.ev.Options) || seen[i] {
					return ErrInvalid
				}
				seen[i] = true
			}
		case KindInput:
			if len(a.Text) > maxText {
				return ErrInvalid
			}
		}
	}
	r.closed, r.closedAt = true, b.now()
	r.ch <- a
	return nil
}

// HTTPHost is the Host that serves HTTP clients through a Broker.
type HTTPHost struct{ B *Broker }

// Default is the broker behind /api/v1/ui and Background().
var Default = NewBroker()

func (h HTTPHost) Connected() bool { return h.B.Connected() }
func (h HTTPHost) Notify(n Notice) { h.B.Notify(n) }

func timeoutOr(q Question, def time.Duration) time.Duration {
	if q.Timeout > 0 {
		return q.Timeout
	}
	return def
}

func (h HTTPHost) Confirm(ctx context.Context, q Question) (bool, bool) {
	def, _ := q.Default.(bool)
	a, ok := h.B.ask(ctx, Event{Kind: KindConfirm, Title: q.Title, Message: q.Message, Default: def}, timeoutOr(q, timeoutAsk))
	if !ok {
		return def, false
	}
	return a.Yes, true
}

func (h HTTPHost) Select(ctx context.Context, q Question) ([]int, bool) {
	a, ok := h.B.ask(ctx, Event{Kind: KindSelect, Title: q.Title, Message: q.Message, Options: q.Options, Preselect: q.Preselect, Multi: q.Multi}, timeoutOr(q, timeoutAsk))
	if !ok {
		return nil, false
	}
	return a.Selected, true
}

func (h HTTPHost) Input(ctx context.Context, q Question) (string, bool) {
	def, _ := q.Default.(string)
	a, ok := h.B.ask(ctx, Event{Kind: KindInput, Title: q.Title, Message: q.Message, Default: def, Hidden: q.Hidden}, timeoutOr(q, timeoutInput))
	if !ok {
		return "", false
	}
	return a.Text, true
}

func (h HTTPHost) Text(ctx context.Context, title, body string) bool {
	_, ok := h.B.ask(ctx, Event{Kind: KindText, Title: title, Message: body}, timeoutAsk)
	return ok
}

func (h HTTPHost) Progress(p Progress) ProgressHandle {
	id := newID()
	r := &request{ev: Event{ID: id, Kind: KindProgress}}
	ph := &httpProgress{b: h.B, r: r, title: p.Title}
	h.B.mu.Lock()
	h.B.reqs[id] = r
	h.B.mu.Unlock()
	ph.Update(0, p.Title, p.Message)
	return ph
}

type httpProgress struct {
	b     *Broker
	r     *request
	title string
}

func (p *httpProgress) emit(pct int, heading, line string, done bool) {
	p.b.mu.Lock()
	defer p.b.mu.Unlock()
	if p.r.closed {
		return
	}
	id := p.r.ev.ID
	if done {
		p.r.closed, p.r.closedAt = true, p.b.now()
	}
	p.r.ev = p.b.publishLocked(Event{ID: id, Kind: KindProgress, Title: p.title, Message: line,
		Progress: &ProgressEvent{ID: id, Percent: pct, Heading: heading, Line: line, Done: done}})
}

func (p *httpProgress) Update(pct int, heading, message string) { p.emit(pct, heading, message, false) }
func (p *httpProgress) Canceled() bool                          { return p.r.canceled.Load() }
func (p *httpProgress) Close()                                  { p.emit(100, "", "", true) }
