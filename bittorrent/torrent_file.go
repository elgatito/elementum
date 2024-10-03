package bittorrent

import (
	"compress/gzip"
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/anacrolix/sync"
	"github.com/dustin/go-humanize"
	"github.com/goccy/go-json"
	"github.com/op/go-logging"
	"github.com/valyala/bytebufferpool"
	"github.com/zeebo/bencode"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/proxy"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

var torrentFileLog = logging.MustGetLogger("torrentFile")
var cachedInfoHash sync.Map

// TorrentFile represents a physical torrent file
type TorrentFile struct {
	URI        string   `json:"uri"`
	InfoHash   string   `json:"info_hash"`
	Title      string   `json:"title"`
	Name       string   `json:"name"`
	Trackers   []string `json:"trackers"`
	Size       string   `json:"size"`
	SizeParsed uint64   `jsin:"-"`
	Seeds      int64    `json:"seeds"`
	Peers      int64    `json:"peers"`
	IsPrivate  bool     `json:"is_private"`
	Provider   string   `json:"provider"`
	Icon       string   `json:"icon"`
	Multi      bool

	Resolution  int    `json:"resolution"`
	VideoCodec  int    `json:"video_codec"`
	AudioCodec  int    `json:"audio_codec"`
	Language    string `json:"language"`
	RipType     int    `json:"rip_type"`
	SceneRating int    `json:"scene_rating"`

	hasResolved bool
}

// Used to avoid infinite recursion in UnmarshalJSON
type torrent TorrentFile

// TorrentFileRaw ...
type TorrentFileRaw struct {
	Title        string                 `bencode:"title"`
	Announce     string                 `bencode:"announce"`
	AnnounceList [][]string             `bencode:"announce-list"`
	Info         map[string]interface{} `bencode:"info"`
}

// HasAnnounce checks AnnounceList for specific tracker
func (t TorrentFileRaw) HasAnnounce(tracker string) bool {
	for _, tier := range t.AnnounceList {
		for _, v := range tier {
			if v == tracker {
				return true
			}
		}
	}

	return false
}

const (
	// ResolutionUnknown ...
	ResolutionUnknown = iota
	// Resolution240p ...
	Resolution240p
	// Resolution480p ...
	Resolution480p
	// Resolution720p ...
	Resolution720p
	// Resolution1080p ...
	Resolution1080p
	// Resolution2K ...
	Resolution2K
	// Resolution4k ...
	Resolution4k
)

var (
	// [pр] actually contains "p" from latin and "р" from cyrillic, which looks the same, but it's not.
	resolutionTags = []map[*regexp.Regexp]int{
		{regexp.MustCompile(`(?i)\W+240[pр]\W*`): Resolution240p},
		{regexp.MustCompile(`(?i)\W+480[pр]\W*`): Resolution480p},
		{regexp.MustCompile(`(?i)\W+(720[pр]|1280x720)\W*`): Resolution720p},
		{regexp.MustCompile(`(?i)\W+(1080[piр]|1920x1080)\W*`): Resolution1080p},
		{regexp.MustCompile(`(?i)\W+1440[pр]\W*`): Resolution2K},
		{regexp.MustCompile(`(?i)\W+(4k|2160[pр]|UHD)\W*`): Resolution4k},

		{regexp.MustCompile(`(?i)\W+(vhs\-?rip)\W*`): Resolution240p},
		{regexp.MustCompile(`(?i)\W+(tv\-?rip|sat\-?rip|iptv\-?rip|xvid|dvd|hdtv|web\-(dl)?rip)\W*`): Resolution480p},
		{regexp.MustCompile(`(?i)\W+(hd720p?|hd\-?rip|b[rd]rip)\W*`): Resolution720p},
		{regexp.MustCompile(`(?i)\W+(hd1080p?|fullhd|fhd|blu\W*ray|bd\W*remux)\W*`): Resolution1080p},
		{regexp.MustCompile(`(?i)\W+(2k)\W*`): Resolution2K},
		{regexp.MustCompile(`(?i)\W+(4k|hd4k)\W*`): Resolution4k},
	}
	// Resolutions ...
	Resolutions = []string{"", "240p", "480p", "720p", "1080p", "2K", "4K"}
	// Colors ...
	Colors = []string{"", "FFFC3401", "FFA56F01", "FF539A02", "FF0166FC", "FFF15052", "FF6BB9EC"}
	// Size regexp
	sizeMatcher = regexp.MustCompile(`^\s*([\d\.\,]+)\s*`)
)

const (
	// RipUnknown ...
	RipUnknown = iota
	// RipCam ...
	RipCam
	// RipTS ...
	RipTS
	// RipTC ...
	RipTC
	// RipScr ...
	RipScr
	// RipDVDScr ...
	RipDVDScr
	// RipDVD ...
	RipDVD
	// RipHDTV ...
	RipHDTV
	// RipWeb ...
	RipWeb
	// RipBluRay ...
	RipBluRay
)

var (
	ripTags = map[*regexp.Regexp]int{
		regexp.MustCompile(`(?i)\W+(cam|cam\W*rip|hd\W*cam)\W*`):     RipCam,
		regexp.MustCompile(`(?i)\W+(ts|telesync|hdts)\W*`):           RipTS,
		regexp.MustCompile(`(?i)\W+(tc|telecine)\W*`):                RipTC,
		regexp.MustCompile(`(?i)\W+(scr|screener)\W*`):               RipScr,
		regexp.MustCompile(`(?i)\W+dvd\W*scr\W*`):                    RipDVDScr,
		regexp.MustCompile(`(?i)\W+dvd\W*rip\W*`):                    RipDVD,
		regexp.MustCompile(`(?i)\W+hd(tv|rip)\W*`):                   RipHDTV,
		regexp.MustCompile(`(?i)\W+(web\W*dl|web\W*rip)\W*`):         RipWeb,
		regexp.MustCompile(`(?i)\W+(bluray|b[rd]\W*rip|bdremux)\W*`): RipBluRay,
	}
	// Rips ...
	Rips = []string{"", "CamRip", "TeleSync", "TeleCine", "Screener", "DVD Screener", "DVDRip", "HDTV", "WebDL", "Blu-Ray"}
)

const (
	// RatingUnkown ...
	RatingUnkown = iota
	// RatingProper ...
	RatingProper
	// RatingNuked ...
	RatingNuked
)

var (
	sceneTags = map[*regexp.Regexp]int{
		regexp.MustCompile(`(?i)\W+nuked\W*`):  RatingNuked,
		regexp.MustCompile(`(?i)\W+proper\W*`): RatingProper,
	}
)

const (
	// CodecUnknown ...
	CodecUnknown = iota

	// CodecXVid ...
	CodecXVid
	// CodecH264 ...
	CodecH264
	// CodecH265 ...
	CodecH265

	// CodecMp3 ...
	CodecMp3
	// CodecAAC ...
	CodecAAC
	// CodecAC3 ...
	CodecAC3
	// CodecDTS ...
	CodecDTS
	// CodecDTSHD ...
	CodecDTSHD
	// CodecDTSHDMA ...
	CodecDTSHDMA
)

var (
	videoTags = map[*regexp.Regexp]int{
		regexp.MustCompile(`(?i)\W+xvid\W*`):           CodecXVid,
		regexp.MustCompile(`(?i)\W+([hx]264)\W*`):      CodecH264,
		regexp.MustCompile(`(?i)\W+([hx]265|hevc)\W*`): CodecH265,
	}
	audioTags = map[*regexp.Regexp]int{
		regexp.MustCompile(`(?i)\W+mp3\W*`):              CodecMp3,
		regexp.MustCompile(`(?i)\W+aac\W*`):              CodecAAC,
		regexp.MustCompile(`(?i)\W+(ac3|[Dd]*5\W+1)\W*`): CodecAC3,
		regexp.MustCompile(`(?i)\W+dts\W*`):              CodecDTS,
		regexp.MustCompile(`(?i)\W+dts\W+hd\W*`):         CodecDTSHD,
		regexp.MustCompile(`(?i)\W+dts\W+hd\W+ma\W*`):    CodecDTSHDMA,
	}
	// Codecs ...
	Codecs = []string{"", "Xvid", "H.264", "H.265", "MP3", "AAC", "AC3", "DTS", "DTS HD", "DTS HD MA"}
)

const (
	xtPrefix = "urn:btih:"
	torCache = "http://itorrents.org/torrent/%s.torrent"
)

// UnmarshalJSON ...
func (t *TorrentFile) UnmarshalJSON(b []byte) error {
	tmp := torrent{}
	if err := json.Unmarshal(b, &tmp); err != nil {
		return err
	}
	*t = TorrentFile(tmp)
	t.initialize()
	return nil
}

// MarshalJSON ...
func (t *TorrentFile) MarshalJSON() ([]byte, error) {
	tmp := torrent(*t)
	b, err := json.Marshal(tmp)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// IsMagnet ...
func (t *TorrentFile) IsMagnet() bool {
	return strings.HasPrefix(t.URI, "magnet:")
}

// IsValidMagnet Taken from anacrolix/torrent
func (t *TorrentFile) IsValidMagnet() (err error) {
	u, err := url.Parse(t.URI)
	if err != nil {
		err = fmt.Errorf("error parsing uri: %s", err)
		return
	}
	if u.Scheme != "magnet" {
		err = fmt.Errorf("unexpected scheme: %q", u.Scheme)
		return
	}
	xt := u.Query().Get("xt")
	if !strings.HasPrefix(xt, xtPrefix) {
		err = fmt.Errorf("bad xt parameter")
		return
	}
	infoHash := xt[len(xtPrefix):]

	// BTIH hash can be in HEX or BASE32 encoding
	// will assign appropriate func judging from symbol length
	var decode func(dst, src []byte) (int, error)
	switch len(infoHash) {
	case 40:
		decode = hex.Decode
	case 32:
		decode = base32.StdEncoding.Decode
	}

	if decode == nil {
		err = fmt.Errorf("unhandled xt parameter encoding: encoded length %d", len(infoHash))
		return
	}
	n, err := decode([]byte(t.InfoHash)[:], []byte(infoHash))
	if err != nil {
		err = fmt.Errorf("error decoding xt: %s", err)
		return
	}
	if n != 20 {
		err = fmt.Errorf("invalid magnet length: %d", n)
		return
	}
	return
}

// NewTorrentFile ...
func NewTorrentFile(uri string) *TorrentFile {
	t := &TorrentFile{
		URI: uri,
	}
	t.initialize()
	return t
}

func (t *TorrentFile) initialize() {
	if t.IsMagnet() {
		t.initializeFromMagnet()
	}

	if t.Resolution == ResolutionUnknown {
		t.Resolution = matchLowerTags(t, resolutionTags)
		if t.Resolution == ResolutionUnknown {
			t.Resolution = Resolution480p
		}
	}
	if t.VideoCodec == CodecUnknown {
		t.VideoCodec = matchTags(t, videoTags)
	}
	if t.AudioCodec == CodecUnknown {
		t.AudioCodec = matchTags(t, audioTags)
	}
	if t.RipType == RipUnknown {
		t.RipType = matchTags(t, ripTags)
	}
	if t.SceneRating == RatingUnkown {
		t.SceneRating = matchTags(t, sceneTags)
	}
	t.beautifySize()
	t.parseSize()
}

func (t *TorrentFile) initializeFromMagnet() {
	magnetURI, _ := url.Parse(t.URI)
	vals := magnetURI.Query()
	hash := strings.ToUpper(strings.TrimPrefix(vals.Get("xt"), "urn:btih:"))

	// for backward compatibility
	if unBase32Hash, err := base32.StdEncoding.DecodeString(hash); err == nil {
		hash = hex.EncodeToString(unBase32Hash)
	}

	if t.InfoHash == "" {
		t.InfoHash = strings.ToLower(hash)
	}
	if t.Name == "" {
		t.Name = vals.Get("dn")
	}
	if t.Title == "" {
		t.Title = t.Name
	}

	if len(t.Trackers) == 0 {
		t.Trackers = make([]string, 0)
		for _, tracker := range vals["tr"] {
			if !util.StringSliceContains(t.Trackers, tracker) {
				t.Trackers = append(t.Trackers, strings.Replace(string(tracker), "\\", "", -1))
			}
		}
	}
}

// Magnet ...
func (t *TorrentFile) Magnet(firstTime bool) {
	if !t.hasResolved {
		t.Resolve()
	}

	params := url.Values{}
	params.Set("dn", t.Name)

	if ((config.Get().ModifyTrackersStrategy == modifyTrackersFirstTime && firstTime) || config.Get().ModifyTrackersStrategy == modifyTrackersEveryTime) && config.Get().RemoveOriginalTrackers {
		t.Trackers = []string{}
	} else {
		if len(t.Trackers) != 0 {
			for _, tracker := range t.Trackers {
				params.Add("tr", tracker)
			}
		}
	}

	t.URI = fmt.Sprintf("magnet:?xt=urn:btih:%s&%s", t.InfoHash, params.Encode())

	/*if t.IsValidMagnet() == nil {
		params.Add("as", t.URI)
	} else {
		params.Add("as", fmt.Sprintf(torCache, t.InfoHash))
	}*/
}

// LoadFromBytes ...
func (t *TorrentFile) LoadFromBytes(in []byte) error {

	var torrentFile *TorrentFileRaw

	if err := bencode.DecodeBytes(in, &torrentFile); err != nil {
		return err
	}
	if torrentFile == nil {
		return errors.New("Decoding failed")
	}

	if t.InfoHash == "" {
		hasher := sha1.New()
		bencode.NewEncoder(hasher).Encode(torrentFile.Info)
		t.InfoHash = hex.EncodeToString(hasher.Sum(nil))
	}

	if t.Name == "" {
		t.Name = torrentFile.Info["name"].(string)
	}
	if t.Title == "" {
		if len(torrentFile.Title) > 0 {
			t.Title = torrentFile.Title
		} else {
			t.Title = t.Name
		}
	}

	if torrentFile.Info["private"] != nil {
		if torrentFile.Info["private"].(int64) == 1 {
			// torrentFileLog.Noticef("%s marked as private", t.Name)
			t.IsPrivate = true
		}
	}

	if len(t.Trackers) == 0 {
		t.Trackers = append(t.Trackers, torrentFile.Announce)
		if torrentFile.AnnounceList != nil {
			for _, trackers := range torrentFile.AnnounceList {
				for _, tracker := range trackers {
					if !util.StringSliceContains(t.Trackers, tracker) {
						t.Trackers = append(t.Trackers, tracker)
					}
				}
			}
		}
	}

	fileName := t.GenerateFileName()
	err := t.SaveToFile(in)
	if err != nil {
		return err
	}

	t.URI = fileName
	t.hasResolved = true

	t.initialize()

	return nil
}

// Download takes care about torrent's URI and downloads or reads cached file
func (t *TorrentFile) Download() ([]byte, error) {

	// Try to get file from cache
	if p, ok := cachedInfoHash.Load(t.URI); ok && config.Get().UseCacheTorrents {
		_, errf := os.Stat(p.(string))
		if errf == nil {
			b, err := os.ReadFile(p.(string))
			return b, err
		}

		cachedInfoHash.Delete(t.URI)
	}

	// Try to get local file
	if strings.HasPrefix(t.URI, "/") {
		_, err := os.Stat(t.URI)
		if err == nil {
			return os.ReadFile(t.URI)
		} else {
			torrentFileLog.Errorf("Get local file failed: %s", err)
		}
	}

	// Try to download file
	parts := strings.Split(t.URI, "|")
	uri := parts[0]
	req, err := http.NewRequest("GET", uri, nil)
	if err != nil {
		return nil, err
	}
	if len(parts) > 1 {
		for _, part := range parts[1:] {
			if keyVal := strings.SplitN(part, "=", 2); len(keyVal) > 1 {
				req.Header.Add(keyVal[0], keyVal[1])
			}
		}
	}

	resp, err := proxy.GetClient().Do(req)
	if err != nil || resp == nil {
		return nil, err
	} else if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Request %s failed with code: %d", uri, resp.StatusCode)
	}
	defer resp.Body.Close()

	var reader io.ReadCloser

	switch resp.Header.Get("Content-Encoding") {
	case "gzip":
		reader, err = gzip.NewReader(resp.Body)
		defer reader.Close()
	default:
		reader = resp.Body
	}

	bb := bytebufferpool.Get()
	bb.ReadFrom(reader)

	defer bytebufferpool.Put(bb)
	return bb.Bytes(), err
}

// Resolve ...
func (t *TorrentFile) Resolve() error {
	if t.IsMagnet() {
		t.hasResolved = true
		return nil
	}

	b, err := t.Download()
	if err != nil {
		return err
	}

	err = t.LoadFromBytes(b)
	if err != nil {
		return err
	}

	cachedInfoHash.Store(t.URI, t.GenerateFileName())

	return nil
}

// EnrichTrackers ...
func (t *TorrentFile) EnrichTrackers() {
	for _, trackerURL := range extraTrackers {
		if !util.StringSliceContains(t.Trackers, trackerURL) {
			t.Trackers = append(t.Trackers, trackerURL)
		}
	}
}

// GenerateFileName ...
func (t *TorrentFile) GenerateFileName() string {
	return filepath.Join(config.Get().Info.TempPath, fmt.Sprintf("%s.torrent", t.InfoHash))
}

// SaveToFile will save torrent to .torrent file for libtorrent
func (t *TorrentFile) SaveToFile(b []byte) error {
	// Save torrent file in temp folder
	fileName := t.GenerateFileName()
	out, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err := out.Write(b); err != nil {
		return err
	}

	return nil
}

func matchTags(t *TorrentFile, tokens map[*regexp.Regexp]int) int {
	lowName := strings.ToLower(t.Name)
	codec := 0
	for re, value := range tokens {
		if re.MatchString(lowName) {
			if value > codec {
				codec = value
			}
		}
	}
	return codec
}

func matchLowerTags(t *TorrentFile, tokens []map[*regexp.Regexp]int) int {
	lowName := strings.ToLower(t.Name)
	for _, res := range tokens {
		for re, value := range res {
			if re.MatchString(lowName) {
				return value
			}
		}
	}
	return 0
}

// StreamInfo ...
func (t *TorrentFile) StreamInfo() *xbmc.StreamInfo {
	sie := &xbmc.StreamInfo{
		Video: &xbmc.StreamInfoEntry{
			Codec: Codecs[t.VideoCodec],
		},
		Audio: &xbmc.StreamInfoEntry{
			Codec: Codecs[t.AudioCodec],
		},
	}

	switch t.Resolution {
	case Resolution480p:
		sie.Video.Width = 853
		sie.Video.Height = 480
	case Resolution720p:
		sie.Video.Width = 1280
		sie.Video.Height = 720
	case Resolution1080p:
		sie.Video.Width = 1920
		sie.Video.Height = 1080
	case Resolution2K:
		sie.Video.Width = 2560
		sie.Video.Height = 1440
	case Resolution4k:
		sie.Video.Width = 4096
		sie.Video.Height = 2160
	}

	return sie
}

func (t *TorrentFile) beautifySize() {
	// To upper-case
	t.Size = strings.ToUpper(t.Size)

	// Cyrillic-specific translation
	t.Size = strings.Replace(t.Size, "КБ", "KB", -1)
	t.Size = strings.Replace(t.Size, "МБ", "MB", -1)
	t.Size = strings.Replace(t.Size, "ГБ", "GB", -1)
	t.Size = strings.Replace(t.Size, "ТБ", "TB", -1)
	t.Size = strings.Replace(t.Size, "MO", "MB", -1)
	t.Size = strings.Replace(t.Size, "GO", "GB", -1)

	// Replacing "," with "."
	t.Size = strings.Replace(t.Size, ",", ".", -1)

	// Adding whitespace after number
	t.Size = sizeMatcher.ReplaceAllString(t.Size, "$1 ")
}

func (t *TorrentFile) parseSize() {
	if len(t.Size) == 0 {
		return
	}

	if v, err := humanize.ParseBytes(t.Size); err == nil {
		t.SizeParsed = v
	} else {
		torrentFileLog.Debugf("Could not parse torrent size for '%s': %#v", t.Size, err)
	}
}

// UpdateTorrentTrackers updates raw torrent file trackers
func (t *TorrentFile) UpdateTorrentTrackers() error {
	if t.IsPrivate {
		return nil
	}

	if t.IsMagnet() {
		magnetURI, _ := url.Parse(t.URI)
		vals := magnetURI.Query()
		existing := vals["tr"]

		for _, tracker := range t.Trackers {
			if !util.StringSliceContains(existing, tracker) {
				t.URI += "&tr=" + tracker
			}
		}
	} else {
		_, err := os.Stat(t.URI)
		if err != nil {
			return err
		}

		var b []byte
		b, err = os.ReadFile(t.URI)
		if err != nil {
			return err
		}

		var torrentFile *TorrentFileRaw
		if err := bencode.DecodeBytes(b, &torrentFile); err != nil {
			return err
		}

		for _, tracker := range t.Trackers {
			if torrentFile != nil && torrentFile.AnnounceList != nil && !torrentFile.HasAnnounce(tracker) {
				torrentFile.AnnounceList = append(torrentFile.AnnounceList, []string{tracker})
			}
		}

		b, err = bencode.EncodeBytes(torrentFile)
		if err != nil {
			return err
		}

		return t.SaveToFile(b)
	}

	return nil
}
