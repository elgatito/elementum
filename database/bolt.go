package database

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/boltdb/bolt"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

// InitCacheDB ...
func InitCacheDB(conf *config.Configuration) (*BoltDatabase, error) {
	db, err := CreateBoltDB(conf, cacheFileName, backupCacheFileName)
	if err != nil || db == nil {
		return nil, errors.New("database not created")
	}

	cacheDatabase = &BoltDatabase{
		db:             db,
		quit:           make(chan struct{}, 2),
		fileName:       cacheFileName,
		backupFileName: backupCacheFileName,
	}

	for _, bucket := range CacheBuckets {
		if err = cacheDatabase.CheckBucket(bucket); err != nil {
			xbmc.Notify("Elementum", err.Error(), config.AddonIcon())
			log.Error(err)
			return cacheDatabase, err
		}
	}

	return cacheDatabase, nil
}

// CreateBoltDB ...
func CreateBoltDB(conf *config.Configuration, fileName string, backupFileName string) (*bolt.DB, error) {
	databasePath := filepath.Join(conf.Info.Profile, fileName)
	backupPath := filepath.Join(conf.Info.Profile, backupFileName)

	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Got critical error while creating Bolt: %v", r)
			RestoreBackup(databasePath, backupPath)
			os.Exit(1)
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

// GetBolt returns common database
func GetBolt() *BoltDatabase {
	return boltDatabase
}

// GetCache returns Cache database
func GetCache() *BoltDatabase {
	return cacheDatabase
}

// GetFilename returns bolt filename
func (d *BoltDatabase) GetFilename() string {
	return d.fileName
}

// Close ...
func (d *BoltDatabase) Close() {
	log.Debug("Closing Bolt Database")
	d.quit <- struct{}{}
	d.db.Close()
}

// CheckBucket ...
func (d *BoltDatabase) CheckBucket(bucket []byte) error {
	return d.db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucket)
		return err
	})
}

// BucketExists checks if bucket already exists in the database
func (d *BoltDatabase) BucketExists(bucket []byte) (res bool) {
	d.db.View(func(tx *bolt.Tx) error {
		res = tx.Bucket(bucket) != nil
		return nil
	})

	return
}

// RecreateBucket ...
func (d *BoltDatabase) RecreateBucket(bucket []byte) error {
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
	backupPath := filepath.Join(config.Get().Info.Profile, d.backupFileName)

	d.CreateBackup(backupPath)
	d.CacheCleanup()

	tickerBackup := time.NewTicker(2 * time.Hour)

	defer tickerBackup.Stop()
	defer close(d.quit)

	for {
		select {
		case <-tickerBackup.C:
			go func() {
				d.CreateBackup(backupPath)
			}()
			// case <-tickerCache.C:
			// 	go d.CacheCleanup()
		case <-d.quit:
			return
		}
	}
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

// CreateBackup ...
func (d *BoltDatabase) CreateBackup(backupPath string) {
	d.db.View(func(tx *bolt.Tx) error {
		tx.CopyFile(backupPath, 0600)
		log.Debugf("Database backup saved at: %s", backupPath)
		return nil
	})
}

// CacheCleanup ...
func (d *BoltDatabase) CacheCleanup() {
	defer perf.ScopeTimer()()

	now := util.NowInt64()
	for _, bucket := range CacheBuckets {
		if !d.BucketExists(bucket) {
			continue
		}

		toRemove := []string{}
		d.ForEach(bucket, func(key []byte, value []byte) error {
			expire, _ := ParseCacheItem(value)
			if (expire > 0 && expire < now) || expire == 0 {
				toRemove = append(toRemove, string(key))
			}

			return nil
		})

		if len(toRemove) > 0 {
			log.Debugf("Removing %d invalidated items from cache", len(toRemove))
			d.BatchDelete(bucket, toRemove)
		}
	}
}

// DeleteWithPrefix ...
func (d *BoltDatabase) DeleteWithPrefix(bucket []byte, prefix []byte) {
	toRemove := []string{}
	d.ForEach(bucket, func(key []byte, v []byte) error {
		if bytes.HasPrefix(key, prefix) {
			toRemove = append(toRemove, string(key))
		}

		return nil
	})

	if len(toRemove) > 0 {
		log.Debugf("Deleting %d items from cache", len(toRemove))
		d.BatchDelete(bucket, toRemove)
	}
}

//
//	Callback operations
//

// Seek ...
func (d *BoltDatabase) Seek(bucket []byte, prefix string, callback callBack) error {
	return d.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucket).Cursor()
		bytePrefix := []byte(prefix)
		for k, v := c.Seek(bytePrefix); k != nil && bytes.HasPrefix(k, bytePrefix); k, v = c.Next() {
			callback(k, v)
		}
		return nil
	})
}

// ForEach ...
func (d *BoltDatabase) ForEach(bucket []byte, callback callBackWithError) error {
	return d.db.View(func(tx *bolt.Tx) error {
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
	value, err := d.GetCachedBytes(bucket, key)
	return string(value), err
}

// GetCachedBool ...
func (d *BoltDatabase) GetCachedBool(bucket []byte, key string) (bool, error) {
	value, err := d.GetCachedBytes(bucket, key)
	if err != nil {
		return false, err
	}

	return strconv.ParseBool(string(value))
}

// GetCachedObject ...
func (d *BoltDatabase) GetCachedObject(bucket []byte, key string, item interface{}) (err error) {
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
	d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		ret = len(b.Get([]byte(key))) > 0
		return nil
	})

	return
}

// GetBytes ...
func (d *BoltDatabase) GetBytes(bucket []byte, key string) (value []byte, err error) {
	err = d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		value = b.Get([]byte(key))
		return nil
	})

	return
}

// Get ...
func (d *BoltDatabase) Get(bucket []byte, key string) (string, error) {
	value, err := d.GetBytes(bucket, key)
	return string(value), err
}

// GetObject ...
func (d *BoltDatabase) GetObject(bucket []byte, key string, item interface{}) (err error) {
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
	return d.db.Update(func(tx *bolt.Tx) error {
		value = append([]byte(strconv.Itoa(util.NowPlusSecondsInt(seconds))+"|"), value...)
		return tx.Bucket(bucket).Put([]byte(key), value)
	})
}

// SetCached ...
func (d *BoltDatabase) SetCached(bucket []byte, seconds int, key string, value string) error {
	return d.SetCachedBytes(bucket, seconds, key, []byte(value))
}

// SetCachedBool ...
func (d *BoltDatabase) SetCachedBool(bucket []byte, seconds int, key string, value bool) error {
	return d.SetCachedBytes(bucket, seconds, key, []byte(strconv.FormatBool(value)))
}

// SetCachedObject ...
func (d *BoltDatabase) SetCachedObject(bucket []byte, seconds int, key string, item interface{}) error {
	if buf, err := json.Marshal(item); err != nil {
		return err
	} else if err := d.SetCachedBytes(bucket, seconds, key, buf); err != nil {
		return err
	}

	return nil
}

// SetBytes ...
func (d *BoltDatabase) SetBytes(bucket []byte, key string, value []byte) error {
	return d.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucket).Put([]byte(key), value)
	})
}

// Set ...
func (d *BoltDatabase) Set(bucket []byte, key string, value string) error {
	return d.SetBytes(bucket, key, []byte(value))
}

// SetObject ...
func (d *BoltDatabase) SetObject(bucket []byte, key string, item interface{}) error {
	if buf, err := json.Marshal(item); err != nil {
		return err
	} else if err := d.SetBytes(bucket, key, buf); err != nil {
		return err
	}

	return nil
}

// BatchSet ...
func (d *BoltDatabase) BatchSet(bucket []byte, objects map[string]string) error {
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
	return d.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucket).Delete([]byte(key))
	})
}

// BatchDelete ...
func (d *BoltDatabase) BatchDelete(bucket []byte, keys []string) error {
	return d.db.Batch(func(tx *bolt.Tx) error {
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

// Write ...
func (w *DBWriter) Write(b []byte) (n int, err error) {
	return len(b), w.database.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(w.bucket).Put(w.key, b)
	})
}
