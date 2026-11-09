package ui

import (
	"context"
	"encoding/json"
	"net"
	"reflect"
	"sync"
	"testing"

	"github.com/elgatito/elementum/xbmc"
)

type call struct {
	Method string
	Params []any
}

// fakeAddon is a JSON-RPC server standing in for the add-on's Python side.
type fakeAddon struct {
	mu      sync.Mutex
	calls   []call
	results map[string]any
}

func startFakeAddon(t *testing.T, results map[string]any) (*fakeAddon, *xbmc.XBMCHost) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	f := &fakeAddon{results: results}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				var req struct {
					Method string `json:"method"`
					Params []any  `json:"params"`
					ID     any    `json:"id"`
				}
				if json.NewDecoder(c).Decode(&req) != nil {
					return
				}
				f.mu.Lock()
				f.calls = append(f.calls, call{req.Method, req.Params})
				res := f.results[req.Method]
				f.mu.Unlock()
				_ = json.NewEncoder(c).Encode(map[string]any{"id": req.ID, "result": res, "error": nil})
			}()
		}
	}()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	old := xbmc.XBMCExJSONRPCPort
	xbmc.XBMCExJSONRPCPort = port
	t.Cleanup(func() { xbmc.XBMCExJSONRPCPort = old })
	return f, &xbmc.XBMCHost{Host: "127.0.0.1"}
}

func (f *fakeAddon) take() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := f.calls
	f.calls = nil
	return c
}

// KodiHost must send the same JSON-RPC calls as the direct xbmc.XBMCHost methods it replaces.
func TestKodiHostMatchesDirectCalls(t *testing.T) {
	fake, h := startFakeAddon(t, map[string]any{
		"Dialog_Confirm_With_Timeout": 1, "Dialog_Confirm": 1, "Dialog": 1,
		"Dialog_Select": 2, "Dialog_Select_With_Options": 1, "Dialog_Select_Large": 0,
		"Dialog_MultiSelect": []int{0, 2}, "Keyboard": "abc",
	})
	old := xbmc.DialogAutoclose
	xbmc.DialogAutoclose = 30
	defer func() { xbmc.DialogAutoclose = old }()
	k := Kodi(h)
	ctx := context.Background()
	cases := []struct {
		name   string
		viaUI  func()
		direct func()
	}{
		{"notify", func() { k.Notify(Notice{"Elementum", "LOCALIZE[30204]", "icon.png"}) },
			func() { h.Notify("Elementum", "LOCALIZE[30204]", "icon.png") }},
		{"confirm", func() { k.Confirm(ctx, Question{Title: "t", Message: "m"}) }, func() { h.DialogConfirm("t", "m") }},
		{"confirm focused", func() { k.Confirm(ctx, Question{Title: "t", Message: "m", Style: ConfirmFocused}) }, func() { h.DialogConfirmFocused("t", "m") }},
		{"confirm nontimed", func() { k.Confirm(ctx, Question{Title: "t", Message: "m", Style: ConfirmNonTimed}) }, func() { h.DialogConfirmNonTimed("t", "m") }},
		{"confirm plain", func() { k.Confirm(ctx, Question{Title: "t", Message: "m", Style: ConfirmPlain}) }, func() { h.Dialog("t", "m") }},
		{"select", func() { k.Select(ctx, Question{Title: "t", Options: []string{"a", "b", "c"}}) }, func() { h.ListDialog("t", "a", "b", "c") }},
		{"select options", func() {
			k.Select(ctx, Question{Title: "t", Options: []string{"a", "b"}, Preselect: []int{1}, Timeout: 5e9})
		},
			func() { h.ListDialogWithOptions(5, 1, "t", "a", "b") }},
		{"select large", func() { k.Select(ctx, Question{Title: "t", Message: "s", Options: []string{"a"}, Large: true}) }, func() { h.ListDialogLarge("t", "s", "a") }},
		{"multi", func() {
			k.Select(ctx, Question{Title: "t", Options: []string{"a", "b"}, Preselect: []int{1}, Multi: true})
		},
			func() { h.DialogMultiSelect("t", []string{"a", "b"}, []int{1}) }},
		{"input", func() { k.Input(ctx, Question{Title: "t", Default: "d"}) }, func() { h.Keyboard("d", "t", false) }},
	}
	for _, c := range cases {
		fake.take()
		c.viaUI()
		got := fake.take()
		c.direct()
		want := fake.take()
		if len(want) == 0 || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: via ui %v, direct %v", c.name, got, want)
		}
	}

	if yes, ok := k.Confirm(ctx, Question{Style: ConfirmPlain}); !yes || !ok {
		t.Errorf("confirm answer = %v %v", yes, ok)
	}
	if idx, ok := k.Select(ctx, Question{Options: []string{"a", "b", "c"}}); !ok || !reflect.DeepEqual(idx, []int{2}) {
		t.Errorf("select answer = %v %v", idx, ok)
	}
	if s, ok := k.Input(ctx, Question{}); s != "abc" || !ok {
		t.Errorf("input answer = %q %v", s, ok)
	}
}

func TestKodiHostWithoutKodi(t *testing.T) {
	k := Kodi(nil)
	if k.Connected() {
		t.Fatal("nil host is connected")
	}
	k.Notify(Notice{})
	if _, ok := k.Confirm(context.Background(), Question{}); ok {
		t.Error("answered without Kodi")
	}
	k.Progress(Progress{}).Close() // no panic
}

func TestLogHostNeverAnswers(t *testing.T) {
	var h Host = LogHost{}
	ctx := context.Background()
	if _, ok := h.Confirm(ctx, Question{}); ok {
		t.Error("confirm answered")
	}
	if _, ok := h.Select(ctx, Question{}); ok {
		t.Error("select answered")
	}
	if _, ok := h.Input(ctx, Question{}); ok {
		t.Error("input answered")
	}
	p := h.Progress(Progress{})
	p.Update(1, "", "")
	if p.Canceled() {
		t.Error("canceled")
	}
	p.Close()
}

// stub is a host that records, for the routing tests.
type stub struct {
	LogHost
	on      bool
	notes   int
	confirm int
}

func (s *stub) Connected() bool { return s.on }
func (s *stub) Notify(Notice)   { s.notes++ }
func (s *stub) Confirm(context.Context, Question) (bool, bool) {
	s.confirm++
	return true, true
}

func TestMultiRouting(t *testing.T) {
	httpH, kodi := &stub{on: true}, &stub{on: true}
	m := Multi{httpH, kodi}
	m.Notify(Notice{})
	if httpH.notes != 1 || kodi.notes != 1 {
		t.Errorf("toast should reach both, got %d %d", httpH.notes, kodi.notes)
	}
	if yes, ok := m.Confirm(context.Background(), Question{}); !yes || !ok || httpH.confirm != 1 || kodi.confirm != 0 {
		t.Errorf("question should go to the HTTP host only: %d %d", httpH.confirm, kodi.confirm)
	}
	httpH.on = false
	m.Confirm(context.Background(), Question{})
	if kodi.confirm != 1 {
		t.Error("no HTTP client: Kodi should be asked")
	}
	kodi.on = false
	if _, ok := m.Confirm(context.Background(), Question{}); ok || kodi.confirm != 1 {
		t.Error("nobody connected: unanswered")
	}
	m.Notify(Notice{}) // logs, no panic
	if m.Connected() {
		t.Error("connected")
	}
	if Background() == nil {
		t.Error("Background is nil")
	}
}
