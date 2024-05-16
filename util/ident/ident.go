package ident

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"

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

// GetCleanVersion returns version, provided to compiler, but without possible commit information
func GetCleanVersion() string {
	v := GetVersion()
	if strings.Contains(v, "-") {
		return strings.Split(v, "-")[0]
	}
	return v
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
	return "-LT1100-"
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
			return
		case 2:
			userAgent = "libtorrent/1.1.0.0"
			peerID = "-LT1100-"
			return
		case 3:
			userAgent = "BitTorrent/7.5.0"
			peerID = "-BT7500-"
			return
		case 4:
			userAgent = "BitTorrent/7.9.9"
			peerID = "-BT7990-"
			return
		case 5:
			userAgent = "uTorrent/3.5.5"
			peerID = "-UT3550-"
			return
		case 6:
			userAgent = "uTorrent/3.6.0"
			peerID = "-UT3600-"
			return
		case 7:
			userAgent = "uTorrent/2.2.1"
			peerID = "-UT2210-"
			return
		case 8:
			userAgent = "Transmission/2.92"
			peerID = "-TR2920-"
			return
		case 9:
			userAgent = "Deluge/1.3.6"
			peerID = "-DE1360-"
			return
		case 10:
			userAgent = "Deluge/2.1.1 libtorrent/1.2.15.0"
			peerID = "-DE211s-"
			return
		case 11:
			userAgent = "Vuze/5.7.3.0"
			peerID = "-AZ5730-"
			return
		case 12:
			userAgent = "Transmission/4.05"
			peerID = "-TR4050-"
			return
		case 13:
			userAgent = "qBittorrent/3.3.9"
			peerID = "-qB3390-"
			return
		case 14:
			userAgent = "qBittorrent/4.6.4"
			peerID = "-qB4640-"
			return
		default:
			userAgent = "Transmission/1.93"
			peerID = "-TR1930-"
			return
		}
	}

	return
}
