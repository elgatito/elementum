package database

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/goccy/go-json"
	bolt "go.etcd.io/bbolt"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/exit"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

// GetBolt returns common database
func GetBolt() *BoltDatabase {
	return boltDatabase
}

// GetCache returns Cache database
func GetCache() *BoltDatabase {
	return cacheDatabase
}

// InitCacheDB ...
func InitCacheDB(conf *config.Configuration) (*BoltDatabase, error) {
	databasePath := filepath.Join(conf.Info.Profile, cacheFileName)
	backupPath := filepath.Join(conf.Info.Profile, backupCacheFileName)
	compressPath := filepath.Join(conf.Info.Profile, compressCacheFileName)

	db, err := CreateBoltDB(conf, databasePath, backupPath)
	if err != nil || db == nil {
		return nil, errors.New("database not created")
	}

	cacheDatabase = &BoltDatabase{
		db: db,
		Database: Database{
			isCaching: true,

			quit: make(chan struct{}, 5),

			fileName: cacheFileName,
			filePath: databasePath,

			backupFileName: backupCacheFileName,
			backupFilePath: backupPath,

			compressFilePath: compressPath,
		},
	}

	cacheDatabase.mu.Lock()
	defer cacheDatabase.mu.Unlock()

	for _, bucket := range CacheBuckets {
		if err = CheckBucket(cacheDatabase.db, bucket); err != nil {
			if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
				xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
			}
			log.Error(err)
			return cacheDatabase, err
		}
	}

	return cacheDatabase, nil
}

// CreateBoltDB ...
func CreateBoltDB(conf *config.Configuration, databasePath, backupPath string) (*bolt.DB, error) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Got critical error while creating Bolt: %v", r)
			RestoreBackup(databasePath, backupPath)
			exit.Exit(exit.ExitCodeError)
		}
	}()

	db, err := bolt.Open(databasePath, 0600, &bolt.Options{
		ReadOnly: false,
		Timeout:  15 * time.Second,
	})
	if err != nil {
		log.Warningf("Could not open database at %s: %#v", databasePath, err)
		return nil, err
	}
	db.NoSync = true

	return db, nil
}

// CompressBoltDB ...
func CompressBoltDB(conf *config.Configuration, databasePath, compressPath string) error {
	if util.FileExists(compressPath) {
		if err := os.Remove(compressPath); err != nil {
			log.Errorf("Could not remove file %s: %s", compressPath, err)
			return err
		}
	}

	// Ensure source file exists.
	fi, err := os.Stat(databasePath)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	initialSize := fi.Size()

	started := time.Now()
	defer func() {
		log.Infof("Database compress finished in %s", time.Since(started))
	}()

	// Open source database.
	src, err := bolt.Open(databasePath, 0444, &bolt.Options{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() {
		if src != nil {
			src.Close()
		}
	}()

	// Open destination database.
	dst, err := bolt.Open(compressPath, fi.Mode(), nil)
	if err != nil {
		return err
	}
	defer func() {
		if dst != nil {
			dst.Close()
		}
	}()

	// Run compaction.
	if err := bolt.Compact(dst, src, 65536); err != nil {
		return err
	}

	// Report stats on new size.
	fi, err = os.Stat(compressPath)
	if err != nil {
		return err
	} else if fi.Size() == 0 {
		return fmt.Errorf("zero db size")
	}
	log.Infof("Database %s compessed to %s with %d -> %d bytes (gain=%.2fx)\n",
		databasePath, compressPath, initialSize, fi.Size(), float64(initialSize)/float64(fi.Size()))

	src.Close()
	dst.Close()

	// Replace database with compact version
	if err := os.Remove(databasePath); err != nil {
		log.Errorf("Could not remove old database file '%s': %s", databasePath, err)
		return err
	}
	if err := os.Rename(compressPath, databasePath); err != nil {
		log.Errorf("Could not rename old database file '%s' into '%s': %s", compressPath, databasePath, err)
		return err
	}

	return nil
}

// GetFilename returns bolt filename
func (d *BoltDatabase) GetFilename() string {
	if d == nil {
		return ""
	}

	return d.fileName
}

// Close ...
func (d *BoltDatabase) Close() {
	if d == nil {
		return
	}

	log.Info("Closing Bolt Database")

	d.IsClosed = true
	d.quit <- struct{}{}

	// Let it sleep to keep up all the active tasks
	time.Sleep(100 * time.Millisecond)

	d.mu.Lock()
	defer d.mu.Unlock()

	d.db.Close()
}

// CheckBucket ...
func CheckBucket(db *bolt.DB, bucket []byte) error {
	return db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucket)
		return err
	})
}

// BucketExists checks if bucket already exists in the database
func BucketExists(db *bolt.DB, bucket []byte) (res bool) {
	db.View(func(tx *bolt.Tx) error {
		res = tx.Bucket(bucket) != nil
		return nil
	})

	return
}

// RecreateBucket ...
func (d *BoltDatabase) RecreateBucket(bucket []byte) error {
	if d == nil {
		return nil
	}

	return d.db.Update(func(tx *bolt.Tx) error {
		errDrop := tx.DeleteBucket(bucket)
		if errDrop != nil {
			return errDrop
		}

		_, errCreate := tx.CreateBucketIfNotExists(bucket)
		return errCreate
	})
}

// MaintenanceRefreshHandler ...
func (d *BoltDatabase) MaintenanceRefreshHandler() {
	CreateBackup(d.db, d.backupFilePath)
	CacheCleanup(d.db)

	tickerBackup := time.NewTicker(backupPeriod)
	tickerCleanup := time.NewTicker(cleanupPeriod)

	defer tickerBackup.Stop()
	defer tickerCleanup.Stop()
	defer close(d.quit)

	for {
		select {
		case <-tickerBackup.C:
			go CreateBackup(d.db, d.backupFilePath)
		case <-tickerCleanup.C:
			go CacheCleanup(d.db)
		case <-d.quit:
			return
		}
	}
}

// CreateBackup ...
func CreateBackup(db *bolt.DB, backupPath string) {
	if config.Args.DisableBackup {
		return
	}
	if stat, err := os.Stat(backupPath); err == nil && time.Since(stat.ModTime()) < backupPeriod {
		log.Infof("Skipping backup due to newer modification date of %s", backupPath)
		return
	}

	defer perf.ScopeTimer()()

	db.View(func(tx *bolt.Tx) error {
		tx.CopyFile(backupPath, 0600)
		log.Infof("Database backup saved at: %s", backupPath)
		return nil
	})
}

// RestoreBackup ...
func RestoreBackup(databasePath string, backupPath string) {
	log.Warningf("Restoring backup from '%s' to '%s'", backupPath, databasePath)

	// Remove existing library.db if needed
	if _, err := os.Stat(databasePath); err == nil {
		if err := os.Remove(databasePath); err != nil {
			log.Warningf("Could not delete existing library file (%s): %s", databasePath, err)
			return
		}
	}

	// Restore backup if exists
	if _, err := os.Stat(backupPath); err == nil {
		errorMsg := fmt.Sprintf("Could not restore backup from '%s' to '%s': ", backupPath, databasePath)

		srcFile, err := os.Open(backupPath)
		if err != nil {
			log.Warning(errorMsg, err)
			return
		}
		defer srcFile.Close()

		destFile, err := os.Create(databasePath)
		if err != nil {
			log.Warning(errorMsg, err)
			return
		}
		defer destFile.Close()

		if _, err := io.Copy(destFile, srcFile); err != nil {
			log.Warning(errorMsg, err)
			return
		}

		if err := destFile.Sync(); err != nil {
			log.Warning(errorMsg, err)
			return
		}

		log.Warningf("Restored backup to %s", databasePath)
	}
}

// CacheCleanup ...
func CacheCleanup(db *bolt.DB) {
	defer perf.ScopeTimer()()

	now := util.NowInt64()
	for _, bucket := range CacheBuckets {
		if !BucketExists(db, bucket) {
			continue
		}

		toRemove := []string{}
		ForEach(db, bucket, func(key []byte, value []byte) error {
			expire, _ := ParseCacheItem(value)
			if (expire > 0 && expire < now) || expire == 0 {
				toRemove = append(toRemove, string(key))
			}

			return nil
		})

		if len(toRemove) > 0 {
			log.Debugf("Removing %d invalidated items from cache", len(toRemove))
			BatchDelete(db, bucket, toRemove)
		}
	}
}

// DeleteWithPrefix ...
func (d *BoltDatabase) DeleteWithPrefix(bucket []byte, prefix []byte) {
	if d == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	toRemove := []string{}
	ForEach(d.db, bucket, func(key []byte, v []byte) error {
		if bytes.HasPrefix(key, prefix) {
			toRemove = append(toRemove, string(key))
		}

		return nil
	})

	if len(toRemove) > 0 {
		log.Debugf("Deleting %d items from cache", len(toRemove))
		BatchDelete(d.db, bucket, toRemove)
	}
}

//
//	Callback operations
//

// Seek ...
func Seek(db *bolt.DB, bucket []byte, prefix string, callback callBack) error {
	return db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucket).Cursor()
		bytePrefix := []byte(prefix)
		for k, v := c.Seek(bytePrefix); k != nil && bytes.HasPrefix(k, bytePrefix); k, v = c.Next() {
			callback(k, v)
		}
		return nil
	})
}

// ForEach ...
func ForEach(db *bolt.DB, bucket []byte, callback callBackWithError) error {
	return db.View(func(tx *bolt.Tx) error {
		tx.Bucket(bucket).ForEach(callback)
		return nil
	})
}

//
// Cache operations
//

// ParseCacheItem ...
func ParseCacheItem(item []byte) (int64, []byte) {
	if len(item) < 11 {
		return 0, nil
	}

	expire, _ := strconv.ParseInt(string(item[0:10]), 10, 64)
	return expire, item[11:]
}

// GetCachedBytes ...
func (d *BoltDatabase) GetCachedBytes(bucket []byte, key string) (cacheValue []byte, err error) {
	if d == nil {
		return nil, nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	var value []byte
	err = d.db.View(func(tx *bolt.Tx) error {
		value = tx.Bucket(bucket).Get([]byte(key))
		return nil
	})

	if err != nil || len(value) == 0 {
		return
	}

	expire, v := ParseCacheItem(value)
	if expire > 0 && expire < util.NowInt64() {
		d.Delete(bucket, key)
		return nil, errors.New("Key Expired")
	} else if expire == 0 {
		d.Delete(bucket, key)
		return nil, errors.New("Invalid Key")
	}

	return v, nil
}

// GetCached ...
func (d *BoltDatabase) GetCached(bucket []byte, key string) (string, error) {
	if d == nil {
		return "", nil
	}

	value, err := d.GetCachedBytes(bucket, key)
	return string(value), err
}

// GetCachedBool ...
func (d *BoltDatabase) GetCachedBool(bucket []byte, key string) (bool, error) {
	if d == nil {
		return false, nil
	}

	value, err := d.GetCachedBytes(bucket, key)
	if err != nil {
		return false, err
	}

	return strconv.ParseBool(string(value))
}

// GetCachedObject ...
func (d *BoltDatabase) GetCachedObject(bucket []byte, key string, item interface{}) (err error) {
	if d == nil {
		return nil
	}

	v, err := d.GetCachedBytes(bucket, key)
	if err != nil || len(v) == 0 {
		return err
	}

	if err = json.Unmarshal(v, &item); err != nil {
		log.Warningf("Could not unmarshal object for key: '%s', in bucket '%s': %s; Value: %#v", key, bucket, err, string(v))
		return err
	}

	return
}

//
// Get/Set operations
//

// Has checks for existence of a key
func (d *BoltDatabase) Has(bucket []byte, key string) (ret bool) {
	if d == nil {
		return
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		ret = len(b.Get([]byte(key))) > 0
		return nil
	})

	return
}

// GetBytes ...
func (d *BoltDatabase) GetBytes(bucket []byte, key string) (value []byte, err error) {
	if d == nil {
		return
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	err = d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		value = b.Get([]byte(key))
		return nil
	})

	return
}

// Get ...
func (d *BoltDatabase) Get(bucket []byte, key string) (string, error) {
	if d == nil {
		return "", nil
	}

	value, err := d.GetBytes(bucket, key)
	return string(value), err
}

// GetObject ...
func (d *BoltDatabase) GetObject(bucket []byte, key string, item interface{}) (err error) {
	if d == nil {
		return
	}

	v, err := d.GetBytes(bucket, key)
	if err != nil {
		return err
	}

	if len(v) == 0 {
		return errors.New("Bytes empty")
	}

	if err = json.Unmarshal(v, &item); err != nil {
		log.Warningf("Could not unmarshal object for key: '%s', in bucket '%s': %s", key, bucket, err)
		return err
	}

	return
}

// SetCachedBytes ...
func (d *BoltDatabase) SetCachedBytes(bucket []byte, seconds int, key string, value []byte) error {
	if d == nil {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	return d.db.Update(func(tx *bolt.Tx) error {
		value = append([]byte(strconv.Itoa(util.NowPlusSecondsInt(seconds))+"|"), value...)
		return tx.Bucket(bucket).Put([]byte(key), value)
	})
}

// SetCached ...
func (d *BoltDatabase) SetCached(bucket []byte, seconds int, key string, value string) error {
	if d == nil {
		return nil
	}

	return d.SetCachedBytes(bucket, seconds, key, []byte(value))
}

// SetCachedBool ...
func (d *BoltDatabase) SetCachedBool(bucket []byte, seconds int, key string, value bool) error {
	if d == nil {
		return nil
	}

	return d.SetCachedBytes(bucket, seconds, key, []byte(strconv.FormatBool(value)))
}

// SetCachedObject ...
func (d *BoltDatabase) SetCachedObject(bucket []byte, seconds int, key string, item interface{}) error {
	if d == nil {
		return nil
	}

	if buf, err := json.Marshal(item); err != nil {
		return err
	} else if err := d.SetCachedBytes(bucket, seconds, key, buf); err != nil {
		return err
	}

	return nil
}

// SetBytes ...
func (d *BoltDatabase) SetBytes(bucket []byte, key string, value []byte) error {
	if d == nil {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	return d.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucket).Put([]byte(key), value)
	})
}

// Set ...
func (d *BoltDatabase) Set(bucket []byte, key string, value string) error {
	if d == nil {
		return nil
	}

	return d.SetBytes(bucket, key, []byte(value))
}

// SetObject ...
func (d *BoltDatabase) SetObject(bucket []byte, key string, item interface{}) error {
	if d == nil {
		return nil
	}

	if buf, err := json.Marshal(item); err != nil {
		return err
	} else if err := d.SetBytes(bucket, key, buf); err != nil {
		return err
	}

	return nil
}

// BatchSet ...
func (d *BoltDatabase) BatchSet(bucket []byte, objects map[string]string) error {
	if d == nil {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	return d.db.Batch(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		for key, value := range objects {
			if err := b.Put([]byte(key), []byte(value)); err != nil {
				return err
			}
		}
		return nil
	})
}

// BatchSetBytes ...
func (d *BoltDatabase) BatchSetBytes(bucket []byte, objects map[string][]byte) error {
	if d == nil {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	return d.db.Batch(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		for key, value := range objects {
			if err := b.Put([]byte(key), value); err != nil {
				return err
			}
		}
		return nil
	})
}

// BatchSetObject ...
func (d *BoltDatabase) BatchSetObject(bucket []byte, objects map[string]interface{}) error {
	if d == nil {
		return nil
	}

	serialized := map[string][]byte{}
	for k, item := range objects {
		buf, err := json.Marshal(item)
		if err != nil {
			return err
		}
		serialized[k] = buf
	}

	return d.BatchSetBytes(bucket, serialized)
}

// Delete ...
func (d *BoltDatabase) Delete(bucket []byte, key string) error {
	if d == nil {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	return d.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucket).Delete([]byte(key))
	})
}

// BatchDelete ...
func BatchDelete(db *bolt.DB, bucket []byte, keys []string) error {
	return db.Batch(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		for _, key := range keys {
			b.Delete([]byte(key))
		}
		return nil
	})
}

// AsWriter ...
func (d *BoltDatabase) AsWriter(bucket []byte, key string) *DBWriter {
	return &DBWriter{
		database: d,
		bucket:   bucket,
		key:      []byte(key),
	}
}

// Compress ...
func (d *BoltDatabase) Compress() (err error) {
	if d == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.db.Close()

	if err = CompressBoltDB(config.Get(), d.filePath, d.compressFilePath); err != nil {
		return err
	}

	d.db, err = CreateBoltDB(config.Get(), d.filePath, d.backupFilePath)
	return err
}

// Write ...
func (w *DBWriter) Write(b []byte) (n int, err error) {
	return len(b), w.database.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(w.bucket).Put(w.key, b)
	})
}
