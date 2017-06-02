package bittorrent

import (
	"net"
	"time"
	"bufio"
	"bytes"
	"errors"
	"strings"
	"net/url"
	"math/rand"
	"encoding/hex"
	"encoding/binary"
)

const (
	connectionRequestInitialId int64 = 0x041727101980
	defaultTimeout                   = 3 * time.Second
	defaultBufferSize                = 2048 // must be bigger than MTU, which is 1500 most of the time
	maxScrapedHashes                 = 70
)

const (
	ActionConnect Action = iota
	ActionAnnounce
	ActionScrape
	ActionError = 50331648 // it's LittleEndian(3), in BigEndian, don't ask
)

type Action int32

type TrackerRequest struct {
	ConnectionId  int64
	Action        Action
	TransactionId int32
}

type TrackerResponse struct {
	Action        Action
	TransactionId int32
}

type ConnectionResponse struct {
	ConnectionId int64
}

type AnnounceRequest struct {
	InfoHash   [20]byte
	PeerId     [20]byte
	Downloaded int64
	Left       int64
	Uploaded   int64
	Event      int32
	IPAddress  int32
	Key        int32
	NumWant    int32
	Port       int16
}

type Peer struct {
	IPAddress int32
	Port      int16
}

type AnnounceResponse struct {
	Interval int32
	Leechers int32
	Seeders  int32
}

type ScrapeResponseEntry struct {
	Seeders   int32
	Completed int32
	Leechers  int32
}

type Tracker struct {
	connection   net.Conn
	reader       *bufio.Reader
	writer       *bufio.Writer
	connectionId int64
	URL          *url.URL
}

func NewTracker(trackerUrl string) (tracker *Tracker, err error) {
	tURL, err := url.Parse(trackerUrl)
	if err != nil {
		return
	}
	if tURL.Scheme != "udp" {
		err = errors.New("Only UDP trackers are supported.")
		return
	}
	tracker = &Tracker{
		connectionId: connectionRequestInitialId,
		URL:          tURL,
	}
	return
}

func (tracker *Tracker) sendRequest(action Action, request interface{}) error {
	trackerRequest := TrackerRequest{
		ConnectionId:  tracker.connectionId,
		Action:        action,
		TransactionId: rand.Int31(),
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
		return errors.New("Request timed out.")
	case err := <-result:
		if err != nil {
			return err
		}
	}

	if trackerResponse.TransactionId != trackerRequest.TransactionId {
		return errors.New("Request/Response transaction missmatch.")
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

func (tracker *Tracker) Connect() error {
	if strings.Index(tracker.URL.Host, ":") < 0 {
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
	return binary.Read(tracker.reader, binary.BigEndian, &tracker.connectionId)
}

func (tracker *Tracker) doScrape(infoHashes [][]byte) []ScrapeResponseEntry {
	if err := tracker.sendRequest(ActionScrape, bytes.Join(infoHashes, nil)); err != nil {
		return nil
	}

	entries := make([]ScrapeResponseEntry, len(infoHashes))
	binary.Read(tracker.reader, binary.BigEndian, &entries)
	return entries
}

func (tracker *Tracker) Scrape(torrents []*TorrentFile) []ScrapeResponseEntry {
	entries := make([]ScrapeResponseEntry, 0, len(torrents))

	infoHashes := make([][]byte, 0, len(torrents))
	for _, torrent := range torrents {
		bhash, _ := hex.DecodeString(torrent.InfoHash)
		infoHashes = append(infoHashes, bhash)
	}

	for i := 0; i <= len(infoHashes) / maxScrapedHashes; i++ {
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
