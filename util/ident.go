package util

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"

	"github.com/ElementumOrg/libtorrent-go"

	"github.com/elgatito/elementum/config"
)

var (
	// Version ...
	Version = "v0.0.1"
)

// GetVersion returns version, provided to compiler
func GetVersion() string {
	if len(Version) > 0 {
		return Version[1:]
	}

	// Return Dummy version if none provided by compiler
	return "0.0.1"
}

// GetTorrentVersion returns version of GoTorrent, provided to compiler
func GetTorrentVersion() string {
	return libtorrent.Version()
}

// DefaultUserAgent ...
func DefaultUserAgent() string {
	return fmt.Sprintf("Elementum/%s", GetVersion())
}

// DefaultPeerID return default PeerID
func DefaultPeerID() string {
	return "-GT0001-"
}

// PeerIDRandom generates random peer id
func PeerIDRandom(peer string) string {
	return peer + getToken(20-len(peer))
}

func getToken(length int) string {
	randomBytes := make([]byte, 32)
	_, err := rand.Read(randomBytes)
	if err != nil {
		panic(err)
	}
	return base32.StdEncoding.EncodeToString(randomBytes)[:length]
}

// GetUserAndPeer returns PeerID and UserAgent, according to Config settings.
// If not set - returns default values
func GetUserAndPeer() (peerID, userAgent string) {
	c := config.Get()

	peerID = DefaultPeerID()
	userAgent = DefaultUserAgent()

	if c.SpoofUserAgent > 0 {
		switch c.SpoofUserAgent {
		case 1:
			userAgent = "Transmission/1.93"
			peerID = "-TR1930-"
			break
		case 2:
			userAgent = "libtorrent (Rasterbar) 1.1.0"
			peerID = "-LT1100-"
			break
		case 3:
			userAgent = "BitTorrent/7.5.0"
			peerID = "-BT7500-"
			break
		case 4:
			userAgent = "BitTorrent/7.4.3"
			peerID = "-BT7430-"
			break
		case 5:
			userAgent = "uTorrent/3.4.9"
			peerID = "-UT3490-"
			break
		case 6:
			userAgent = "uTorrent/3.2.0"
			peerID = "-UT3200-"
			break
		case 7:
			userAgent = "uTorrent/2.2.1"
			peerID = "-UT2210-"
			break
		case 8:
			userAgent = "Transmission/2.92"
			peerID = "-TR2920-"
			break
		case 9:
			userAgent = "Deluge/1.3.6.0"
			peerID = "-DG1360-"
			break
		case 10:
			userAgent = "Deluge/1.3.12.0"
			peerID = "-DG1312-"
			break
		case 11:
			userAgent = "Vuze/5.7.3.0"
			peerID = "-VZ5730-"
			break
		default:
			userAgent = "uTorrent/3.4.9"
			peerID = "-UT3490-"
			break
		}
	}

	return
}
