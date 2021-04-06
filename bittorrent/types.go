package bittorrent

const (
	movieType   = "movie"
	showType    = "show"
	episodeType = "episode"
	searchType  = "search"
)

const (
	// StorageFile ...
	StorageFile int = iota
	// StorageMemory ...
	StorageMemory
)

const (
	// DownloadFilePlaying ...
	DownloadFilePlaying int = iota
	// DownloadFileSeason ...
	DownloadFileSeason
	// DownloadFileAll ...
	DownloadFileAll
)

var (
	// Storages ...
	Storages = []string{
		"File",
		"Memory",
	}
)

const (
	// StatusQueued ...
	StatusQueued = iota
	// StatusChecking ...
	StatusChecking
	// StatusFinding ...
	StatusFinding
	// StatusDownloading ...
	StatusDownloading
	// StatusFinished ...
	StatusFinished
	// StatusSeeding ...
	StatusSeeding
	// StatusAllocating ...
	StatusAllocating
	// StatusStalled ...
	StatusStalled
	// StatusPaused ...
	StatusPaused
	// StatusBuffering ...
	StatusBuffering
	// StatusPlaying ...
	StatusPlaying
)

// StatusStrings ...
var StatusStrings = []string{
	"LOCALIZE[30621]",
	"LOCALIZE[30622]",
	"LOCALIZE[30623]",
	"LOCALIZE[30624]",
	"LOCALIZE[30625]",
	"LOCALIZE[30626]",
	"LOCALIZE[30627]",
	"LOCALIZE[30628]",
	"LOCALIZE[30629]",
	"LOCALIZE[30630]",
	"LOCALIZE[30631]",
}

const (
	// Remove ...
	Remove = iota
	// Active ...
	Active
)

const (
	profileDefault = iota
	profileMinMemory
	profileHighSpeed
)

const (
	addExtraTrackersNone = iota
	addExtraTrackersAll
	addExtraTrackersBest
	addExtraTrackersMinimum
)

const (
	ipToSDefault     = iota
	ipToSLowDelay    = 1 << iota
	ipToSReliability = 1 << iota
	ipToSThroughput  = 1 << iota
	ipToSLowCost     = 1 << iota
)

var dhtBootstrapNodes = []string{
	"router.bittorrent.com:6881",
	"router.utorrent.com:6881",
	"dht.transmissionbt.com:6881",
	"dht.aelitis.com:6881",     // Vuze
	"dht.libtorrent.org:25401", // Libtorrent
}

var (
	extraTrackersURLTemplate = "https://ngosang.github.io/trackerslist/trackers_%s.txt"
	addExtraTrackersMap      = map[int]string{
		addExtraTrackersAll:  "all",
		addExtraTrackersBest: "best"}
	defaultTrackers = []string{
		"http://bt4.t-ru.org/ann",
		"http://retracker.mgts.by:80/announce",
		"http://tracker.city9x.com:2710/announce",
		"http://tracker.electro-torrent.pl:80/announce",
		"http://tracker.internetwarriors.net:1337/announce",
		"http://bt.svao-ix.ru/announce",

		"udp://tracker.opentrackr.org:1337/announce",
		"udp://tracker.coppersurfer.tk:6969/announce",
		"udp://tracker.leechers-paradise.org:6969/announce",
		"udp://tracker.openbittorrent.com:80/announce",
		"udp://public.popcorn-tracker.org:6969/announce",
		"udp://explodie.org:6969",
		"udp://46.148.18.250:2710",
		"udp://opentor.org:2710",
	}
	extraTrackers = [][]string{}
)

const (
	ltAlertWaitTime = 1 // 1 second
)

const (
	// ProxyTypeNone ...
	ProxyTypeNone = iota
	// ProxyTypeSocks4 ...
	ProxyTypeSocks4
	// ProxyTypeSocks5 ...
	ProxyTypeSocks5
	// ProxyTypeSocks5Password ...
	ProxyTypeSocks5Password
	// ProxyTypeSocksHTTP ...
	ProxyTypeSocksHTTP
	// ProxyTypeSocksHTTPPassword ...
	ProxyTypeSocksHTTPPassword
	// ProxyTypeI2PSAM ...
	ProxyTypeI2PSAM
)

const (
	storedResumeExpiration      = 60 * 60 * 60 * 24
	storedWatchedFileExpiration = 60 * 60 * 60 * 24
)

func byPath(l, r *File) bool {
	return l.Path < r.Path
}
