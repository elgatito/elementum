package bittorrent

import lt "github.com/ElementumOrg/libtorrent-go"

// Alert is a Go copy of a libtorrent alert.
// libtorrent frees alerts on the next PopAlerts(), so consumers must not get the raw pointer.
type Alert struct {
	Type     int
	Category int
	What     string
	Message  string
	Name     string
	Entry    lt.Entry
	InfoHash string
	State    int
}
