package database

import (
	"database/sql"
	"sync"
	"time"

	"github.com/asdine/storm"
	"github.com/op/go-logging"
	bolt "go.etcd.io/bbolt"
)

// StormDatabase ...
type StormDatabase struct {
	db             *storm.DB
	quit           chan struct{}
	fileName       string
	backupFileName string
}

// BoltDatabase ...
type BoltDatabase struct {
	db             *bolt.DB
	quit           chan struct{}
	fileName       string
	backupFileName string
}

// SqliteDatabase ...
type SqliteDatabase struct {
	*sql.DB
	quit           chan struct{}
	fileName       string
	backupFileName string
}

type schemaChange func(*int, *SqliteDatabase) (bool, error)

type callBack func([]byte, []byte)
type callBackWithError func([]byte, []byte) error

// DBWriter ...
type DBWriter struct {
	bucket   []byte
	key      []byte
	database *BoltDatabase
}

// BTItem ...
type BTItem struct {
	InfoHash string   `json:"infoHash" storm:"id"`
	ID       int      `json:"id"`
	State    int      `json:"state"`
	Type     string   `json:"type"`
	Files    []string `json:"files"`
	ShowID   int      `json:"showid"`
	Season   int      `json:"season"`
	Episode  int      `json:"episode"`
	Query    string   `json:"query"`
}

// LibraryItem ...
type LibraryItem struct {
	ID        int `storm:"id"`
	MediaType int `storm:"index"`
	State     int `storm:"index"`
	ShowID    int `storm:"index"`
}

// QueryHistory ...
type QueryHistory struct {
	ID    string    `storm:"id"`
	Type  string    `storm:"index"`
	Query string    `storm:"index"`
	Dt    time.Time `storm:"index"`
}

// TorrentAssignMetadata ...
type TorrentAssignMetadata struct {
	InfoHash string `storm:"id"`
	Metadata []byte
}

// TorrentAssignItem ...
type TorrentAssignItem struct {
	Pk       int    `storm:"id,increment"`
	InfoHash string `storm:"index"`
	TmdbID   int    `storm:"unique"`
}

// TorrentHistory ...
type TorrentHistory struct {
	InfoHash string `storm:"id"`
	Name     string
	Dt       time.Time `storm:"index"`
	// Dt       int64 `storm:"index"`
	Metadata []byte
}

var (
	stormFileName        = "storm.db"
	backupStormFileName  = "storm-backup.db"
	sqliteFileName       = "app.db"
	backupSqliteFileName = "app-backup.db"
	boltFileName         = "library.db"
	backupBoltFileName   = "library-backup.db"
	cacheFileName        = "cache.db"
	backupCacheFileName  = "cache-backup.db"

	log = logging.MustGetLogger("database")

	boltDatabase  *BoltDatabase
	cacheDatabase *BoltDatabase
	stormDatabase *StormDatabase

	once sync.Once
)

const (
	// StateDeleted ...
	StateDeleted = iota
	// StateActive ...
	StateActive
)

const (
	historyMaxSize = 50
	backupPeriod   = 5 * time.Hour
)

var (
	// CommonBucket ...
	CommonBucket = []byte("Common")
)

// CacheBuckets represents buckets in Cache database
var CacheBuckets = [][]byte{
	CommonBucket,
}

const (
	// BTItemBucket ...
	BTItemBucket = "BTItem"

	// TorrentHistoryBucket ...
	TorrentHistoryBucket = "TorrentHistory"

	// TorrentAssignMetadataBucket ...
	TorrentAssignMetadataBucket = "TorrentAssignMetadata"
	// TorrentAssignItemBucket ...
	TorrentAssignItemBucket = "TorrentAssignItem"

	// QueryHistoryBucket ...
	QueryHistoryBucket = "QueryHistory"
)
