package ui

import "github.com/elgatito/elementum/xbmc"

// background serves code that runs without a request: toasts go to every connected host, questions to the first
// connected one, HTTP clients before Kodi. With nobody connected it logs.
var background = Multi{HTTPHost{Default}, LocalKodi}

// Background returns the host for code without a request. It is never nil.
func Background() Host { return background }

// Toast shows an "Elementum" toast on the background host, with the addon icon for Kodi.
func Toast(message string) { ToastOn(background, message) }

// ToastOn is Toast on a given host.
func ToastOn(h Host, message string) {
	h.Notify(Notice{Title: "Elementum", Message: message, Image: addonIcon()})
}

// For returns the host for code that may hold a request's Kodi host: that Kodi when there is one (output
// unchanged). With nil (an API session, or a remote Kodi that is not registered) only HTTP clients are
// reached, never the local Kodi, which belongs to somebody else; with none connected it only logs.
func For(h *xbmc.XBMCHost) Host {
	if h != nil {
		return Kodi(h)
	}
	return httpOnly
}

var httpOnly = Multi{HTTPHost{Default}}
