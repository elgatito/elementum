package ui

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func poll(b *Broker, id string, since *int64, wait time.Duration) []Event {
	return b.Poll(context.Background(), id, since, wait)
}

func i64(v int64) *int64 { return &v }

func TestPollReturnsEventsAndTimesOut(t *testing.T) {
	b := NewBroker()
	if ev := poll(b, "tv", nil, 20*time.Millisecond); ev != nil {
		t.Fatalf("expected a timeout, got %v", ev)
	}
	go func() { time.Sleep(30 * time.Millisecond); b.Notify(Notice{Title: "Elementum", Message: "hi"}) }()
	ev := poll(b, "tv", nil, 2*time.Second)
	if len(ev) != 1 || ev[0].Kind != KindNotify || ev[0].Message != "hi" || ev[0].Seq != 1 {
		t.Fatalf("got %+v", ev)
	}
	if ev := poll(b, "tv", nil, 10*time.Millisecond); ev != nil {
		t.Errorf("without since the queue is emptied, got %v", ev)
	}
}

func TestSinceAcknowledgesAndResyncs(t *testing.T) {
	b := NewBroker()
	poll(b, "tv", nil, time.Millisecond) // register
	b.Notify(Notice{Message: "1"})
	b.Notify(Notice{Message: "2"})
	ev := poll(b, "tv", i64(0), time.Second)
	if len(ev) != 2 {
		t.Fatalf("got %d events", len(ev))
	}
	// Not acknowledged yet: delivered again.
	if ev = poll(b, "tv", i64(0), time.Second); len(ev) != 2 {
		t.Fatalf("redelivery: got %d events", len(ev))
	}
	if ev = poll(b, "tv", i64(1), time.Second); len(ev) != 1 || ev[0].Message != "2" {
		t.Fatalf("after ack of 1: %+v", ev)
	}
	if ev = poll(b, "tv", i64(2), 10*time.Millisecond); ev != nil {
		t.Fatalf("after ack of 2: %+v", ev)
	}

	// A client that was forgotten catches up from the ring.
	b.mu.Lock()
	delete(b.clients, "tv")
	b.mu.Unlock()
	b.Notify(Notice{Message: "3"})
	ev = poll(b, "tv", i64(1), time.Second)
	if len(ev) != 2 || ev[0].Seq != 2 || ev[1].Seq != 3 {
		t.Fatalf("resync: %+v", ev)
	}
	// A new client without since gets no old toasts.
	if ev = poll(b, "other", nil, 10*time.Millisecond); ev != nil {
		t.Fatalf("new client got %+v", ev)
	}
}

func TestRingAndQueueBounds(t *testing.T) {
	b := NewBroker()
	poll(b, "tv", nil, time.Millisecond)
	for i := 0; i < 100; i++ {
		b.Notify(Notice{})
	}
	if len(b.ring) != ringSize {
		t.Errorf("ring %d", len(b.ring))
	}
	if n := len(b.clients["tv"].queue); n != queueSize {
		t.Errorf("queue %d", n)
	}
	// A question is never dropped for a toast.
	b2 := NewBroker()
	poll(b2, "tv", nil, time.Millisecond)
	go HTTPHost{b2}.Confirm(context.Background(), Question{Title: "q"})
	ev := poll(b2, "tv", nil, time.Second)
	if len(ev) != 1 || ev[0].Kind != KindConfirm {
		t.Fatalf("got %+v", ev)
	}
	b2.mu.Lock()
	b2.clients["tv"].queue = nil
	b2.clients["tv"].enqueue(ev[0])
	b2.mu.Unlock()
	for i := 0; i < 200; i++ {
		b2.Notify(Notice{})
	}
	if b2.clients["tv"].queue[0].Kind != KindConfirm {
		t.Error("question was dropped")
	}
}

func TestFirstAnswerWins(t *testing.T) {
	b := NewBroker()
	h := HTTPHost{b}
	poll(b, "a", nil, time.Millisecond)
	poll(b, "b", nil, time.Millisecond)

	type res struct{ yes, ok bool }
	done := make(chan res)
	go func() {
		yes, ok := h.Confirm(context.Background(), Question{Title: "t", Message: "m"})
		done <- res{yes, ok}
	}()
	ea, eb := poll(b, "a", nil, time.Second), poll(b, "b", nil, time.Second)
	if len(ea) != 1 || len(eb) != 1 || ea[0].ID != eb[0].ID {
		t.Fatalf("broadcast: %+v %+v", ea, eb)
	}
	id := ea[0].ID
	if err := b.Answer(id, Answer{Yes: true}); err != nil {
		t.Fatal(err)
	}
	if err := b.Answer(id, Answer{Yes: false}); !errors.Is(err, ErrClosed) {
		t.Errorf("second answer: %v", err)
	}
	if r := <-done; !r.yes || !r.ok {
		t.Errorf("result %+v", r)
	}
	if err := b.Answer("nope", Answer{}); !errors.Is(err, ErrUnknown) {
		t.Errorf("unknown: %v", err)
	}
	// The other client is told to close its dialog.
	ev := poll(b, "b", i64(eb[0].Seq), time.Second)
	if len(ev) == 0 || ev[len(ev)-1].Kind != KindCancelled || ev[len(ev)-1].ID != id {
		t.Errorf("no cancelled event: %+v", ev)
	}
}

func TestSelectInputAndCancel(t *testing.T) {
	b := NewBroker()
	h := HTTPHost{b}
	poll(b, "a", nil, time.Millisecond)
	// ask runs f in a goroutine and returns the question it raised and a func that waits for f.
	ask := func(f func()) (Event, func()) {
		done := make(chan struct{})
		go func() { f(); close(done) }()
		for {
			for _, e := range poll(b, "a", nil, time.Second) {
				if e.Kind != KindCancelled {
					return e, func() { <-done }
				}
			}
		}
	}
	var idx []int
	var ok bool
	ev, wait := ask(func() { idx, ok = h.Select(context.Background(), Question{Options: []string{"x", "y"}}) })
	if ev.Kind != KindSelect || !reflect.DeepEqual(ev.Options, []string{"x", "y"}) {
		t.Fatalf("%+v", ev)
	}
	b.Answer(ev.ID, Answer{Selected: []int{1}})
	wait()
	if !ok || !reflect.DeepEqual(idx, []int{1}) {
		t.Errorf("select = %v %v", idx, ok)
	}

	var s string
	ev, wait = ask(func() { s, ok = h.Input(context.Background(), Question{Default: "d"}) })
	b.Answer(ev.ID, Answer{Text: "hello"})
	wait()
	if s != "hello" || !ok {
		t.Errorf("input = %q %v", s, ok)
	}

	var yes bool
	ev, wait = ask(func() { yes, ok = h.Confirm(context.Background(), Question{Default: true}) })
	b.Answer(ev.ID, Answer{Cancel: true})
	wait()
	if ok || !yes {
		t.Errorf("cancel should be unanswered with the default: %v %v", yes, ok)
	}
}

func TestTimeoutGivesDefaultAndLateAnswerIs410(t *testing.T) {
	b := NewBroker()
	h := HTTPHost{b}
	poll(b, "a", nil, time.Millisecond)
	done := make(chan [2]bool)
	go func() {
		yes, ok := h.Confirm(context.Background(), Question{Default: true, Timeout: 30 * time.Millisecond})
		done <- [2]bool{yes, ok}
	}()
	ev := poll(b, "a", nil, time.Second)[0]
	if ev.TimeoutS != 0 { // under a second rounds to 0; the field is only informative
		t.Logf("timeout_s=%d", ev.TimeoutS)
	}
	if r := <-done; !r[0] || r[1] {
		t.Errorf("want default true, unanswered; got %v", r)
	}
	if err := b.Answer(ev.ID, Answer{Yes: false}); !errors.Is(err, ErrClosed) {
		t.Errorf("late answer: %v", err)
	}
	// ctx cancellation ends the wait too
	ctx, cancel := context.WithCancel(context.Background())
	end := make(chan bool)
	go func() { _, ok := h.Confirm(ctx, Question{}); end <- ok }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if <-end {
		t.Error("answered after cancel")
	}
}

func TestProgress(t *testing.T) {
	b := NewBroker()
	h := HTTPHost{b}
	poll(b, "a", nil, time.Millisecond)
	p := h.Progress(Progress{Title: "Elementum", Message: "start"})
	p.Update(10, "Elementum", "10")
	p.Update(50, "Elementum", "50")
	ev := poll(b, "a", nil, time.Second)
	if len(ev) != 1 || ev[0].Progress.Percent != 50 { // updates of one dialog are coalesced
		t.Fatalf("%+v", ev)
	}
	id := ev[0].ID
	if p.Canceled() {
		t.Fatal("canceled early")
	}
	if err := b.Answer(id, Answer{Cancel: true}); err != nil || !p.Canceled() {
		t.Fatalf("cancel: %v %v", err, p.Canceled())
	}
	p.Close()
	ev = poll(b, "a", nil, time.Second)
	if len(ev) != 1 || !ev[0].Progress.Done {
		t.Fatalf("close: %+v", ev)
	}
	if err := b.Answer(id, Answer{Cancel: true}); !errors.Is(err, ErrClosed) {
		t.Errorf("answer after close: %v", err)
	}
}

func TestIdleSweeperAndConnected(t *testing.T) {
	b := NewBroker()
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	b.now = func() time.Time { return time.Unix(0, clock.Load()) }
	advance := func(d time.Duration) { clock.Add(int64(d)) }
	h := HTTPHost{b}
	if h.Connected() {
		t.Fatal("connected with no client")
	}
	poll(b, "a", nil, time.Millisecond)
	if !h.Connected() {
		t.Fatal("not connected after a poll")
	}
	advance(clientGrace - time.Second)
	if !h.Connected() {
		t.Fatal("dropped too early")
	}
	advance(2 * time.Second)
	if h.Connected() {
		t.Fatal("idle client was kept")
	}
	// closed requests are forgotten after the tombstone time
	h.Confirm(context.Background(), Question{Timeout: time.Millisecond})
	count := func() int { b.mu.Lock(); defer b.mu.Unlock(); return len(b.reqs) }
	if count() != 1 {
		t.Fatalf("reqs %d", count())
	}
	advance(tombstoneTTL + time.Second)
	b.Sweep()
	if count() != 0 {
		t.Errorf("tombstone kept: %d", count())
	}
}

func TestReconnectGetsOpenQuestions(t *testing.T) {
	b := NewBroker()
	h := HTTPHost{b}
	go h.Confirm(context.Background(), Question{Title: "open", Timeout: time.Second})
	time.Sleep(20 * time.Millisecond)
	ev := poll(b, "late", nil, time.Second)
	if len(ev) != 1 || ev[0].Title != "open" {
		t.Fatalf("%+v", ev)
	}
}

func TestBrokerRace(t *testing.T) {
	b := NewBroker()
	h := HTTPHost{b}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for _, id := range []string{"a", "b", "c"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var since *int64
			for {
				select {
				case <-stop:
					return
				default:
				}
				for _, e := range poll(b, id, since, 5*time.Millisecond) {
					v := e.Seq
					since = &v
					if e.Kind == KindConfirm {
						b.Answer(e.ID, Answer{Yes: true})
					}
				}
			}
		}()
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Notify(Notice{Message: "x"})
			h.Confirm(context.Background(), Question{Timeout: 200 * time.Millisecond})
			p := h.Progress(Progress{})
			p.Update(1, "", "")
			p.Close()
			b.Connected()
			b.Sweep()
		}()
	}
	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func TestEventTranslated(t *testing.T) {
	useFixture(t)
	e := Event{Title: "Elementum", Message: "LOCALIZE[30287];;Matrix", Options: []string{"LOCALIZE[30204]"},
		Progress: &ProgressEvent{Line: "LOCALIZE[30204]"}}
	d := e.Translated("de")
	if d.Message != "Matrix zur Bibliothek hinzugefügt" || d.Options[0] != "Keine \"Ergebnisse\"" || d.Progress.Line != d.Options[0] {
		t.Errorf("%+v", d)
	}
	if e.Message != "LOCALIZE[30287];;Matrix" || e.Options[0] != "LOCALIZE[30204]" {
		t.Error("original was modified")
	}
}

func TestConnectedFollowsWait(t *testing.T) {
	b := NewBroker()
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	b.now = func() time.Time { return time.Unix(0, clock.Load()) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Poll(ctx, "a", nil, 25*time.Second); close(done) }()
	for !b.Connected() {
		time.Sleep(time.Millisecond)
	}
	clock.Add(int64(time.Hour)) // a poll in flight stays connected however long it takes
	if !b.Connected() {
		t.Fatal("in-flight poll dropped")
	}
	cancel()
	<-done
	clock.Add(int64(25*time.Second + clientGrace - time.Second))
	if !b.Connected() {
		t.Fatal("dropped before wait+grace")
	}
	clock.Add(int64(2 * time.Second))
	if b.Connected() {
		t.Fatal("kept after wait+grace")
	}
}

func TestRestartResyncAndEpoch(t *testing.T) {
	b := NewBroker()
	if b.Epoch() == "" || b.Epoch() == NewBroker().Epoch() {
		t.Fatal("epoch must be random per broker")
	}
	b.Notify(Notice{Message: "after restart"})
	ev := poll(b, "tv", i64(500), time.Second) // client remembers seq 500 from the old process
	if len(ev) != 1 || ev[0].Message != "after restart" {
		t.Fatalf("got %+v", ev)
	}
}

func TestOpenRequestsResentAfterAck(t *testing.T) {
	b := NewBroker()
	h := HTTPHost{b}
	go h.Confirm(context.Background(), Question{Title: "open", Timeout: 30 * time.Second})
	p := h.Progress(Progress{Title: "T", Message: "m"})
	p.Update(40, "head", "line")
	for {
		b.mu.Lock()
		n := len(b.reqs)
		b.mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	// Even with a since above both events, a new client gets them (restart of the client, not the server).
	ev := poll(b, "fresh", i64(b.Seq()), time.Second)
	if len(ev) != 2 {
		t.Fatalf("got %+v", ev)
	}
	for _, e := range ev {
		switch e.Kind {
		case KindConfirm:
			if e.TimeoutS < 28 || e.TimeoutS > 30 {
				t.Errorf("remaining timeout %d", e.TimeoutS)
			}
		case KindProgress:
			if e.Progress == nil || e.Progress.Percent != 40 || e.Title != "T" {
				t.Errorf("progress state lost: %+v", e)
			}
		default:
			t.Errorf("unexpected %s", e.Kind)
		}
	}
}

func TestCancelledSurvivesOverflow(t *testing.T) {
	c := &client{wake: make(chan struct{}, 1)}
	c.enqueue(Event{ID: "x", Kind: KindCancelled, Seq: 1})
	for i := 0; i < 3*queueSize; i++ {
		c.enqueue(Event{ID: "n", Kind: KindNotify, Seq: int64(i + 2)})
	}
	if c.queue[0].Kind != KindCancelled || len(c.queue) != queueSize {
		t.Errorf("cancelled lost or queue unbounded: %d", len(c.queue))
	}
}

func TestInvalidSelectKeepsQuestionOpen(t *testing.T) {
	b := NewBroker()
	h := HTTPHost{b}
	poll(b, "a", nil, time.Millisecond)
	done := make(chan []int)
	go func() {
		idx, _ := h.Select(context.Background(), Question{Options: []string{"x", "y"}, Timeout: 5 * time.Second})
		done <- idx
	}()
	var ev Event
	for ev.ID == "" {
		for _, e := range poll(b, "a", nil, time.Second) {
			if e.Kind == KindSelect {
				ev = e
			}
		}
	}
	for _, bad := range [][]int{nil, {5}, {-1}, {0, 1}, {0, 0}} {
		if err := b.Answer(ev.ID, Answer{Selected: bad}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	if err := b.Answer(ev.ID, Answer{Selected: []int{1}}); err != nil {
		t.Fatal(err)
	}
	if got := <-done; !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("got %v", got)
	}
}

func TestClientCap(t *testing.T) {
	b := NewBroker()
	for i := 0; i < maxClients+5; i++ {
		poll(b, "c"+string(rune('a'+i)), nil, time.Millisecond)
	}
	b.mu.Lock()
	n := len(b.clients)
	b.mu.Unlock()
	if n > maxClients {
		t.Errorf("%d clients", n)
	}
}

func TestPlainMarkup(t *testing.T) {
	useFixture(t)
	in := "[B]Bold[/B][CR][COLOR red]red[/COLOR] [I]it[/I] [UPPERCASE]up[/UPPERCASE] [keep]"
	if got := plain(in); got != "Bold\nred it up [keep]" {
		t.Errorf("got %q", got)
	}
	if got := (Event{Message: in}).Translated("en").Message; got != "Bold\nred it up [keep]" {
		t.Errorf("translated %q", got)
	}
}

func TestOldEpochReplaysRing(t *testing.T) {
	oldProcess := NewBroker()
	b := NewBroker() // "restarted"
	for i := 0; i < 40; i++ {
		b.Notify(Notice{})
	}
	// The client acked 30 in the old process; without the epoch, since=30 would hide events 1-30.
	if ev := poll(b, "x", i64(30), time.Millisecond); len(ev) != 10 {
		t.Fatalf("same-epoch control: %d events", len(ev))
	}
	ev := poll(b, "tv", b.SinceFor(oldProcess.Epoch(), i64(30)), time.Second)
	if len(ev) != 40 || ev[0].Seq != 1 || ev[39].Seq != 40 {
		t.Fatalf("got %d events", len(ev))
	}
	if got := b.SinceFor(b.Epoch(), i64(30)); *got != 30 {
		t.Error("same epoch must keep since")
	}
}

func TestPOUnknownLangNotCached(t *testing.T) {
	useFixture(t)
	poMu.Lock()
	n := len(poCache)
	poMu.Unlock()
	for _, l := range []string{"zz", "yy-XX", "..", "de_xx"} {
		Translate(l, "LOCALIZE[30204]")
	}
	poMu.Lock()
	defer poMu.Unlock()
	if len(poCache) > n+1 { // de_xx falls back to the real de_de file
		t.Errorf("poCache grew from %d to %d", n, len(poCache))
	}
}
