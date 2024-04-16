package bittorrent

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/proxy"
	"github.com/elgatito/elementum/util"
)

const (
	connectionRequestInitialID int64 = 0x041727101980
	defaultTimeout                   = 3 * time.Second
	defaultBufferSize                = 2048 // must be bigger than MTU, which is 1500 most of the time
	maxScrapedHashes                 = 70
)

const (
	// ActionConnect ...
	ActionConnect Action = iota
	// ActionAnnounce ...
	ActionAnnounce
	// ActionScrape ...
	ActionScrape
	// ActionError ...
	ActionError = 50331648 // it's LittleEndian(3), in BigEndian, don't ask
)

// Action ...
type Action int32

// TrackerRequest ...
type TrackerRequest struct {
	ConnectionID  int64
	Action        Action
	TransactionID int32
}

// TrackerResponse ...
type TrackerResponse struct {
	Action        Action
	TransactionID int32
}

// ConnectionResponse ...
type ConnectionResponse struct {
	ConnectionID int64
}

// AnnounceRequest ...
type AnnounceRequest struct {
	InfoHash   [20]byte
	PeerID     [20]byte
	Downloaded int64
	Left       int64
	Uploaded   int64
	Event      int32
	IPAddress  int32
	Key        int32
	NumWant    int32
	Port       int16
}

// Peer ...
type Peer struct {
	IPAddress int32
	Port      int16
}

// AnnounceResponse ...
type AnnounceResponse struct {
	Interval int32
	Leechers int32
	Seeders  int32
}

// ScrapeResponseEntry ...
type ScrapeResponseEntry struct {
	Seeders   int32
	Completed int32
	Leechers  int32
}

// Tracker ...
type Tracker struct {
	connection   net.Conn
	reader       *bufio.Reader
	writer       *bufio.Writer
	connectionID int64
	URL          *url.URL
}

// NewTracker ...
func NewTracker(trackerURL string) (tracker *Tracker, err error) {
	tURL, err := url.Parse(trackerURL)
	if err != nil {
		return
	}
	if tURL.Scheme != "udp" {
		err = errors.New("Only UDP trackers are supported")
		return
	}
	tracker = &Tracker{
		connectionID: connectionRequestInitialID,
		URL:          tURL,
	}
	return
}

func (tracker *Tracker) sendRequest(action Action, request interface{}) error {
	trackerRequest := TrackerRequest{
		ConnectionID:  tracker.connectionID,
		Action:        action,
		TransactionID: rand.Int31(),
	}
	binary.Write(tracker.writer, binary.BigEndian, trackerRequest)
	if request != nil {
		binary.Write(tracker.writer, binary.BigEndian, request)
	}
	tracker.writer.Flush()

	trackerResponse := TrackerResponse{}

	result := make(chan error, 1)
	go func() {
		result <- binary.Read(tracker.reader, binary.BigEndian, &trackerResponse)
	}()
	select {
	case <-time.After(defaultTimeout):
		return errors.New("Request timed out")
	case err := <-result:
		if err != nil {
			return err
		}
	}

	if trackerResponse.TransactionID != trackerRequest.TransactionID {
		return errors.New("Request/Response transaction missmatch")
	}
	if trackerResponse.Action == ActionError {
		msg, err := tracker.reader.ReadString(0)
		if err != nil {
			return err
		}
		return errors.New(msg)
	}

	return nil
}

// Connect ...
func (tracker *Tracker) Connect() error {
	if !strings.Contains(tracker.URL.Host, ":") {
		tracker.URL.Host += ":80"
	}
	var err error
	tracker.connection, err = net.DialTimeout("udp", tracker.URL.Host, defaultTimeout)
	if err != nil {
		return err
	}
	tracker.reader = bufio.NewReaderSize(tracker.connection, defaultBufferSize)
	tracker.writer = bufio.NewWriterSize(tracker.connection, defaultBufferSize)
	if err := tracker.sendRequest(ActionConnect, nil); err != nil {
		return err
	}
	return binary.Read(tracker.reader, binary.BigEndian, &tracker.connectionID)
}

func (tracker *Tracker) doScrape(infoHashes [][]byte) []ScrapeResponseEntry {
	if err := tracker.sendRequest(ActionScrape, bytes.Join(infoHashes, nil)); err != nil {
		return nil
	}

	entries := make([]ScrapeResponseEntry, len(infoHashes))
	binary.Read(tracker.reader, binary.BigEndian, &entries)
	return entries
}

// Scrape ...
func (tracker *Tracker) Scrape(torrents []*TorrentFile) []ScrapeResponseEntry {
	entries := make([]ScrapeResponseEntry, 0, len(torrents))

	infoHashes := make([][]byte, 0, len(torrents))
	for _, torrent := range torrents {
		if bhash, err := hex.DecodeString(torrent.InfoHash); err == nil {
			infoHashes = append(infoHashes, bhash)
		}
	}

	for i := 0; i <= len(infoHashes)/maxScrapedHashes; i++ {
		idx := i * maxScrapedHashes
		max := idx + maxScrapedHashes
		if max > len(infoHashes) {
			max = len(infoHashes)
		}
		entries = append(entries, tracker.doScrape(infoHashes[idx:max])...)
	}

	return entries
}

func (tracker *Tracker) String() string {
	return tracker.URL.String()
}

// UpdateDefaultTrackers fetches extra trackers from predefined page
func UpdateDefaultTrackers() {
	extraTrackers = []string{}
	if config.Get().AddExtraTrackers != addExtraTrackersNone {
		// add Minimum set by default
		extraTrackers = append(extraTrackers, defaultTrackers...)

		if config.Get().AddExtraTrackers != addExtraTrackersMinimum {
			finalExtraTrackersURL := fmt.Sprintf(extraTrackersURLTemplate, addExtraTrackersMap[config.Get().AddExtraTrackers])
			resp, err := proxy.GetClient().Get(finalExtraTrackersURL)
			if err != nil || resp == nil {
				return
			} else if err == nil && resp.StatusCode != 200 {
				return
			}
			defer resp.Body.Close()

			bodyBytes, _ := io.ReadAll(resp.Body)
			scanner := bufio.NewScanner(bytes.NewReader(bodyBytes))
			for scanner.Scan() {
				tracker := strings.TrimSpace(scanner.Text())
				if !util.StringSliceContains(extraTrackers, tracker) {
					extraTrackers = append(extraTrackers, tracker)
				}
			}
		}
	}
}
