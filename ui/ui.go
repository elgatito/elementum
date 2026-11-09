// Package ui is the seam between Elementum and whatever shows its dialogs: Kodi, HTTP clients (the TV app)
// or nothing. Every blocking call returns answered=false on timeout, cancel or no listener; the caller then
// keeps the default it uses today when there is no Kodi.
package ui

import (
	"context"
	"time"

	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("ui")

// Host shows notifications and asks questions.
type Host interface {
	Notify(n Notice)
	Confirm(ctx context.Context, q Question) (yes, answered bool)
	// Select returns the chosen indexes: one for a single choice.
	Select(ctx context.Context, q Question) (idx []int, answered bool)
	Input(ctx context.Context, q Question) (s string, answered bool)
	// Text shows a modal text and returns when it is dismissed.
	Text(ctx context.Context, title, body string) (answered bool)
	// Progress never returns nil.
	Progress(p Progress) ProgressHandle
}

// Connector is implemented by hosts that know whether anyone is listening. Hosts without it count as connected.
type Connector interface{ Connected() bool }

// ProgressHandle updates and closes a progress dialog.
type ProgressHandle interface {
	Update(percent int, heading, message string)
	Canceled() bool
	Close()
}

// Messages may use Kodi's form "LOCALIZE[30xxx];;arg;;arg"; every host resolves it.

// Notice is a toast.
type Notice struct {
	Title, Message string
	Image          string // Kodi only
}

// ConfirmStyle picks which Kodi dialog a Confirm uses.
type ConfirmStyle int

const (
	ConfirmTimed    ConfirmStyle = iota // Yes/No that closes itself after xbmc.DialogAutoclose
	ConfirmFocused                      // as above, with "Yes" focused (and the timeout answer)
	ConfirmNonTimed                     // Yes/No without a time limit
	ConfirmPlain                        // Kodi's Dialog
)

// Question is the input of Confirm, Select and Input.
type Question struct {
	Title, Message string
	Options        []string
	Preselect      []int
	Default        any // timeout answer: bool for Confirm, initial text for Input
	Timeout        time.Duration
	Multi          bool
	Style          ConfirmStyle
	Large          bool // Select: Kodi's large list (Message is its subject)
	Hidden         bool // Input: password
}

// Progress describes a progress dialog.
type Progress struct {
	Title, Message string
	Background     bool
}

type noProgress struct{}

func (noProgress) Update(int, string, string) {}
func (noProgress) Canceled() bool             { return false }
func (noProgress) Close()                     {}

// LogHost is the host when nobody is listening. It only logs.
type LogHost struct{}

func (LogHost) Notify(n Notice) { log.Infof("notify: %s: %s", n.Title, n.Message) }
func (LogHost) Confirm(_ context.Context, q Question) (bool, bool) {
	log.Infof("confirm (no UI): %s: %s", q.Title, q.Message)
	return false, false
}
func (LogHost) Select(_ context.Context, q Question) ([]int, bool) {
	log.Infof("select (no UI): %s", q.Title)
	return nil, false
}
func (LogHost) Input(_ context.Context, q Question) (string, bool) {
	log.Infof("input (no UI): %s", q.Title)
	return "", false
}
func (LogHost) Text(_ context.Context, title, _ string) bool {
	log.Infof("text (no UI): %s", title)
	return false
}
func (LogHost) Progress(Progress) ProgressHandle { return noProgress{} }

// Multi routes to several hosts in priority order. Toasts go to all connected hosts; a question goes to the
// first connected one only (so Kodi is asked only when no HTTP client is listening). With none connected it logs.
type Multi []Host

func connected(h Host) bool {
	c, ok := h.(Connector)
	return !ok || c.Connected()
}

func (m Multi) first() Host {
	for _, h := range m {
		if connected(h) {
			return h
		}
	}
	return LogHost{}
}

// Connected reports whether any member is.
func (m Multi) Connected() bool {
	for _, h := range m {
		if connected(h) {
			return true
		}
	}
	return false
}

func (m Multi) Notify(n Notice) {
	sent := false
	for _, h := range m {
		if connected(h) {
			h.Notify(n)
			sent = true
		}
	}
	if !sent {
		LogHost{}.Notify(n)
	}
}
func (m Multi) Confirm(ctx context.Context, q Question) (bool, bool) {
	return m.first().Confirm(ctx, q)
}
func (m Multi) Select(ctx context.Context, q Question) ([]int, bool) { return m.first().Select(ctx, q) }
func (m Multi) Input(ctx context.Context, q Question) (string, bool) { return m.first().Input(ctx, q) }
func (m Multi) Text(ctx context.Context, title, body string) bool {
	return m.first().Text(ctx, title, body)
}
func (m Multi) Progress(p Progress) ProgressHandle { return m.first().Progress(p) }
