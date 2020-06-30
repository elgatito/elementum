package cache

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"os"
	"path"
	"time"
)

// FileStore ...
type FileStore struct {
	path string
}

type fileStoreItem struct {
	Key     string      `json:"key"`
	Value   interface{} `json:"value"`
	Expires time.Time   `json:"expires"`
}

// NewFileStore ...
func NewFileStore(path string) *FileStore {
	os.MkdirAll(path, 0777)
	return &FileStore{path}
}

// Set ...
func (c *FileStore) Set(key string, value interface{}, expires time.Duration) error {
	filename := path.Join(c.path, key)
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	gzWriter := zipWriters.Get().(*gzip.Writer)
	gzWriter.Reset(file)

	defer func() {
		gzWriter.Close()
		zipWriters.Put(gzWriter)
	}()

	item := fileStoreItem{
		Key:     key,
		Value:   value,
		Expires: time.Now().UTC().Add(expires),
	}

	return json.NewEncoder(gzWriter).Encode(item)
}

// Add ...
func (c *FileStore) Add(key string, value interface{}, expires time.Duration) error {
	if _, err := os.Stat(path.Join(c.path, key)); os.IsExist(err) {
		return os.ErrExist
	}
	return c.Set(key, value, expires)
}

// Replace ...
func (c *FileStore) Replace(key string, value interface{}, expires time.Duration) error {
	if _, err := os.Stat(path.Join(c.path, key)); os.IsNotExist(err) {
		return os.ErrNotExist
	}
	return c.Set(key, value, expires)
}

// Get ...
func (c *FileStore) Get(key string, value interface{}) error {
	file, err := os.Open(path.Join(c.path, key))
	if err != nil {
		return err
	}
	defer file.Close()

	gzReader := zipReaders.Get().(*gzip.Reader)
	gzReader.Reset(file)

	defer func() {
		gzReader.Close()
		zipReaders.Put(gzReader)
	}()

	item := fileStoreItem{
		Value: value,
	}
	if err = json.NewDecoder(gzReader).Decode(&item); err != nil {
		return err
	}
	if item.Expires.Before(time.Now().UTC()) {
		return errors.New("key is expired")
	}
	return nil
}

// Delete ...
func (c *FileStore) Delete(key string) error {
	return nil
}

// Increment ...
func (c *FileStore) Increment(key string, delta uint64) (uint64, error) {
	return 0, errNotSupported
}

// Decrement ...
func (c *FileStore) Decrement(key string, delta uint64) (uint64, error) {
	return 0, errNotSupported
}

// Flush ...
func (c *FileStore) Flush() error {
	return errNotSupported
}
