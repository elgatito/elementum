package ui

import (
	"context"

	"github.com/elgatito/elementum/xbmc"
)

// KodiHost forwards to Kodi through the same xbmc.XBMCHost calls the code used before, so Kodi output is unchanged.
type KodiHost struct {
	get func() *xbmc.XBMCHost
}

// Kodi wraps one Kodi host (a request's host). A nil h is a host that is not connected.
func Kodi(h *xbmc.XBMCHost) KodiHost { return KodiHost{get: func() *xbmc.XBMCHost { return h }} }

// LocalKodi follows xbmc.GetLocalXBMCHost at each call, for code that runs without a request.
var LocalKodi = KodiHost{get: func() *xbmc.XBMCHost { h, _ := xbmc.GetLocalXBMCHost(); return h }}

func (k KodiHost) host() *xbmc.XBMCHost {
	if k.get == nil {
		return nil
	}
	return k.get()
}

// Connected reports whether a Kodi host is registered.
func (k KodiHost) Connected() bool { return k.host() != nil }

func (k KodiHost) Notify(n Notice) {
	if h := k.host(); h != nil {
		h.Notify(n.Title, n.Message, n.Image)
	}
}

func (k KodiHost) Confirm(_ context.Context, q Question) (bool, bool) {
	h := k.host()
	if h == nil {
		return false, false
	}
	switch q.Style {
	case ConfirmFocused:
		return h.DialogConfirmFocused(q.Title, q.Message), true
	case ConfirmNonTimed:
		return h.DialogConfirmNonTimed(q.Title, q.Message), true
	case ConfirmPlain:
		return h.Dialog(q.Title, q.Message), true
	}
	return h.DialogConfirm(q.Title, q.Message), true
}

func (k KodiHost) Select(_ context.Context, q Question) ([]int, bool) {
	h := k.host()
	if h == nil {
		return nil, false
	}
	if q.Multi {
		idx := h.DialogMultiSelect(q.Title, q.Options, q.Preselect)
		return idx, idx != nil
	}
	var i int
	switch {
	case q.Large:
		i = h.ListDialogLarge(q.Title, q.Message, q.Options...)
	case q.Timeout > 0 || len(q.Preselect) > 0:
		pre := 0
		if len(q.Preselect) > 0 {
			pre = q.Preselect[0]
		}
		i = h.ListDialogWithOptions(int(q.Timeout.Seconds()), pre, q.Title, q.Options...)
	default:
		i = h.ListDialog(q.Title, q.Options...)
	}
	if i < 0 {
		return nil, false
	}
	return []int{i}, true
}

func (k KodiHost) Input(_ context.Context, q Question) (string, bool) {
	h := k.host()
	if h == nil {
		return "", false
	}
	def, _ := q.Default.(string)
	s := h.Keyboard(def, q.Title, q.Hidden)
	return s, s != ""
}

func (k KodiHost) Text(_ context.Context, title, body string) bool {
	h := k.host()
	if h == nil {
		return false
	}
	h.DialogTextModal(title, body)
	return true
}

func (k KodiHost) Progress(p Progress) ProgressHandle {
	h := k.host()
	if h == nil {
		return noProgress{}
	}
	if p.Background {
		if d := h.NewDialogProgressBG(p.Title, p.Message); d != nil {
			return kodiBG{d}
		}
	} else if d := h.NewDialogProgress(p.Title, p.Message, "", ""); d != nil {
		return kodiModal{d}
	}
	return noProgress{}
}

type kodiBG struct{ d *xbmc.DialogProgressBG }

func (p kodiBG) Update(pct int, heading, msg string) { p.d.Update(pct, heading, msg) }
func (p kodiBG) Canceled() bool                      { return false }
func (p kodiBG) Close()                              { p.d.Close() }

type kodiModal struct{ d *xbmc.DialogProgress }

func (p kodiModal) Update(pct int, heading, msg string) { p.d.Update(pct, heading, msg, "") }
func (p kodiModal) Canceled() bool                      { return p.d.IsCanceled() }
func (p kodiModal) Close()                              { p.d.Close() }
