package cache

import (
	"bytes"
	"errors"
	"strconv"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/anacrolix/sync"
	"github.com/klauspost/compress/gzip"
	msgpack "github.com/vmihailenco/msgpack/v4"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/trace"
)

type DBStore struct {
	db *database.BoltDatabase
}

type DBStoreItem struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

var (
	bufferPool = &sync.Pool{
		New: func() interface{} {
			return &bytes.Buffer{}
		},
	}

	zipWriters = sync.Pool{
		New: func() interface{} {
			return &gzip.Writer{}
		}}
	zipReaders = sync.Pool{
		New: func() interface{} {
			return &gzip.Reader{}
		}}
)

var dbStore *DBStore

// NewDBStore Returns instance of BoltDB backed cache store
func NewDBStore() *DBStore {
	if dbStore == nil {
		dbStore = &DBStore{database.GetCache()}
	}

	return dbStore
}

// SetBytes stores []byte into cache instance
func (c *DBStore) SetBytes(key string, value []byte, expires time.Duration) (err error) {
	defer perf.ScopeTimer()()

	if c == nil || c.db == nil || c.db.IsClosed {
		return errors.New("database is closed")
	}
	if config.Args.DisableCache || config.Args.DisableCacheSet {
		return errors.New("caching is disabled")
	}

	t := trace.Cache{
		Action: "SetBytes",
		Key:    key,
	}

	t.Create()
	if config.Args.EnableCacheTracing {
		defer func() {
			t.Stage("SetBytes")
			log.Debugf(t.String())
		}()
	}

	t.Size(uint64(len(value)))

	return c.db.SetBytes(database.CommonBucket, key, append([]byte(strconv.FormatInt(time.Now().UTC().Add(expires).Unix(), 10)), value...))
}

// Set ...
func (c *DBStore) Set(key string, value interface{}, expires time.Duration) (err error) {
	defer perf.ScopeTimer()()

	if c == nil || c.db == nil || c.db.IsClosed {
		return errors.New("database is closed")
	}
	if config.Args.DisableCache || config.Args.DisableCacheSet {
		return errors.New("caching is disabled")
	}

	t := trace.Cache{
		Action: "Set",
		Key:    key,
	}

	item := DBStoreItem{
		Key:   key,
		Value: value,
	}

	// Recover from marshal errors
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("Can't encode the value")
		}
	}()

	t.Create()
	if config.Args.EnableCacheTracing {
		defer func() {
			t.Stage("SetBytes")
			log.Debugf(t.String())
		}()
	}

	b, err := msgpack.Marshal(item)
	if err != nil {
		t.Stage("Unmarshal")
		return err
	}
	t.Stage("Unmarshal")
	t.Size(uint64(len(b)))

	return c.SetBytes(key, b, expires)
}

// Add ...
func (c *DBStore) Add(key string, value interface{}, expires time.Duration) error {
	return c.Set(key, value, expires)
}

// Replace ...
func (c *DBStore) Replace(key string, value interface{}, expires time.Duration) error {
	return c.Set(key, value, expires)
}

// Get ...
func (c *DBStore) Get(key string, value interface{}) (err error) {
	defer perf.ScopeTimer()()

	t := trace.Cache{
		Action: "Get",
		Key:    key,
	}
	t.Create()
	if config.Args.EnableCacheTracing {
		defer func() {
			log.Debugf(t.String())
		}()
	}

	data, err := c.GetBytes(key)
	t.Stage("GetBytes")
	t.Size(uint64(len(data)))
	if err != nil {
		return err
	}

	// Recover from unmarshal errors
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("Can't decode into value")
		}
	}()

	item := DBStoreItem{
		Value: value,
	}

	if errDecode := msgpack.Unmarshal(data, &item); errDecode != nil {
		t.Stage("Unmarshal")
		return errDecode
	}
	t.Stage("Unmarshal")

	return nil
}

// GetBytes gets []byte from cache instance
func (c *DBStore) GetBytes(key string) (ret []byte, err error) {
	if c == nil || c.db == nil || c.db.IsClosed {
		return nil, errors.New("database is closed")
	}
	if config.Args.DisableCache || config.Args.DisableCacheGet {
		return nil, errors.New("caching is disabled")
	}

	defer perf.ScopeTimer()()
	t := trace.Cache{
		Action: "GetBytes",
		Key:    key,
	}
	t.Create()
	if config.Args.EnableCacheTracing {
		defer func() {
			log.Debugf(t.String())
		}()
	}

	data, err := c.db.GetBytes(database.CommonBucket, key)
	t.Stage("GetBytes")
	t.Size(uint64(len(data)))
	if data != nil {
		if len(data) == 0 {
			return nil, errors.New("data is empty")
		} else if len(data) < 10 {
			return nil, errors.New("not enough data")
		}
	} else if data == nil {
		return nil, errors.New("no data found")
	}

	// Check if item is expired
	if expires, _ := database.ParseCacheItem(data); expires > 0 && expires < util.NowInt64() {
		t.Stage("Parse")
		if c != nil && c.db != nil {
			go c.db.Delete(database.CommonBucket, key)
		}
		return nil, errors.New("key is expired")
	}

	// Return data without date part
	return data[10:], err
}

// Delete ...
func (c *DBStore) Delete(key string) error {
	defer perf.ScopeTimer()()

	return c.db.Delete(database.CommonBucket, key)
}

// Increment ...
func (c *DBStore) Increment(key string, delta uint64) (uint64, error) {
	return 0, errNotSupported
}

// Decrement ...
func (c *DBStore) Decrement(key string, delta uint64) (uint64, error) {
	return 0, errNotSupported
}

// Flush ...
func (c *DBStore) Flush() error {
	return errNotSupported
}
