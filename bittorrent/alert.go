package bittorrent

import lt "github.com/ElementumOrg/libtorrent-go"

type ltAlert struct {
	lt.Alert
}

// Alert ...
type Alert struct {
	Type     int
	Category int
	What     string
	Message  string
	Pointer  uintptr
	Name     string
	Entry    lt.Entry
	InfoHash string
}
