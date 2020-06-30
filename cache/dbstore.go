package cache

import (
	"bytes"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/klauspost/compress/gzip"
	"github.com/vmihailenco/msgpack"

	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/util"
)

//go:generate msgp -o msgp.go -io=false -tests=false

// DBStore ...
type DBStore struct {
	db *database.BoltDatabase
}

// DBStoreItem ...
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

// Set ...
func (c *DBStore) Set(key string, value interface{}, expires time.Duration) (err error) {
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

	b, err := msgpack.Marshal(item)
	if err != nil {
		return err
	}

	return c.db.SetBytes(database.CommonBucket, key, append([]byte(strconv.FormatInt(time.Now().UTC().Add(expires).Unix(), 10)), b...))
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
	data, errGet := c.db.GetBytes(database.CommonBucket, key)
	if errGet != nil {
		return errGet
	} else if len(data) == 0 {
		return errors.New("data is empty")
	}

	// Recover from unmarshal errors
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("Can't decode into value")
		}
	}()

	if err != nil {
		return err
	}

	item := DBStoreItem{
		Value: value,
	}
	if expires, _ := database.ParseCacheItem(data); expires > 0 && expires < util.NowInt64() {
		go c.db.Delete(database.CommonBucket, key)
		return errors.New("key is expired")
	}

	if errDecode := msgpack.Unmarshal(data[10:], &item); errDecode != nil {
		return errDecode
	}

	return nil
}

// Delete ...
func (c *DBStore) Delete(key string) error {
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
