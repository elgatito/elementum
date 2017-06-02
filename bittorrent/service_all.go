// +build !arm

package bittorrent

import (
	gotorrent "github.com/anacrolix/torrent"
	// "github.com/scakemyer/libtorrent-go"
)

// Nothing to do on regular devices
func setPlatformSpecificSettings(settings gotorrent.Config) {
}
