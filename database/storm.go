package database

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/asdine/storm"
	"github.com/asdine/storm/q"

	bolt "go.etcd.io/bbolt"

	"github.com/elgatito/elementum/config"
)

// GetStorm returns common database
func GetStorm() *StormDatabase {
	return stormDatabase
}

// GetStormDB returns common database
func GetStormDB() *storm.DB {
	return stormDatabase.db
}

// InitStormDB ...
func InitStormDB(conf *config.Configuration) (*StormDatabase, error) {
	db, err := CreateStormDB(conf, stormFileName, backupStormFileName)
	if err != nil || db == nil {
		return nil, errors.New("database not created")
	}

	stormDatabase = &StormDatabase{
		db:             db,
		quit:           make(chan struct{}, 2),
		fileName:       stormFileName,
		backupFileName: backupStormFileName,
	}

	return stormDatabase, nil
}

// CreateStormDB ...
func CreateStormDB(conf *config.Configuration, fileName string, backupFileName string) (*storm.DB, error) {
	databasePath := filepath.Join(conf.Info.Profile, fileName)
	backupPath := filepath.Join(conf.Info.Profile, backupFileName)

	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Got critical error while creating Storm: %v", r)
			RestoreBackup(databasePath, backupPath)
			os.Exit(1)
		}
	}()

	db, err := storm.Open(databasePath, storm.BoltOptions(0600, &bolt.Options{
		ReadOnly: false,
		Timeout:  15 * time.Second,
		NoSync:   true,
	}))

	if err != nil {
		log.Warningf("Could not open database at %s: %#v", databasePath, err)
		return nil, err
	}

	return db, nil
}

// MaintenanceRefreshHandler ...
func (d *StormDatabase) MaintenanceRefreshHandler() {
	backupPath := filepath.Join(config.Get().Info.Profile, d.backupFileName)

	d.CreateBackup(backupPath)

	tickerBackup := time.NewTicker(backupPeriod)

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

// CreateBackup ...
func (d *StormDatabase) CreateBackup(backupPath string) {
	if config.Args.DisableBackup {
		return
	}
	if stat, err := os.Stat(backupPath); err == nil && time.Now().Sub(stat.ModTime()) < backupPeriod {
		log.Infof("Skipping backup due to newer modification date of %s", backupPath)
		return
	}

	defer perf.ScopeTimer()()

	d.db.Bolt.View(func(tx *bolt.Tx) error {
		tx.CopyFile(backupPath, 0600)
		log.Infof("Storm Database backup saved at: %s", backupPath)
		return nil
	})
}

// Close ...
func (d *StormDatabase) Close() {
	log.Debug("Closing Storm Database")
	d.quit <- struct{}{}
	d.db.Close()
}

// GetFilename returns bolt filename
func (d *StormDatabase) GetFilename() string {
	return d.fileName
}

// AddSearchHistory adds query to search history, according to media type
func (d *StormDatabase) AddSearchHistory(historyType, query string) {
	defer perf.ScopeTimer()()

	var qh QueryHistory

	if err := d.db.One("ID", fmt.Sprintf("%s|%s", historyType, query), &qh); err == nil {
		qh.Dt = time.Now()
		d.db.Update(&qh)
		return
	}

	qh = QueryHistory{
		ID:    fmt.Sprintf("%s|%s", historyType, query),
		Dt:    time.Now(),
		Type:  historyType,
		Query: query,
	}

	d.db.Save(&qh)

	var qhs []QueryHistory
	d.db.Select(q.Eq("Type", historyType)).Skip(historyMaxSize).Find(&qhs)
	for _, qh := range qhs {
		d.db.DeleteStruct(&qh)
	}
}

// CleanSearchHistory cleans search history for selected media type
func (d *StormDatabase) CleanSearchHistory(historyType string) {
	defer perf.ScopeTimer()()

	var qs []QueryHistory
	d.db.Select(q.Eq("Type", historyType)).Find(&qs)
	for _, q := range qs {
		d.db.DeleteStruct(&q)
	}
	d.db.ReIndex(&QueryHistory{})
}

// RemoveSearchHistory removes query from the history
func (d *StormDatabase) RemoveSearchHistory(historyType, query string) {
	defer perf.ScopeTimer()()

	var qs []QueryHistory
	d.db.Select(q.Eq("Type", historyType), q.Eq("Query", query)).Find(&qs)
	for _, q := range qs {
		d.db.DeleteStruct(&q)
	}
	d.db.ReIndex(&QueryHistory{})
}

// CleanupTorrentLink ...
func (d *StormDatabase) CleanupTorrentLink(infoHash string) {
	defer perf.ScopeTimer()()

	var oldTi TorrentAssignItem
	// check that there is no TorrentAssignItem left and only then delete TorrentAssignMetadata
	if err := d.db.Select(q.Eq("InfoHash", infoHash)).First(&oldTi); err != nil {
		if err := d.db.Delete(TorrentAssignMetadataBucket, infoHash); err != nil {
			log.Errorf("Could not delete old torrent metadata: %s", err)
		}
	}
}

// AddTorrentLink saves link between torrent file and tmdbID entry
func (d *StormDatabase) AddTorrentLink(tmdbID, infoHash string, b []byte, force bool) {
	// Dummy check if infohash is real
	if len(infoHash) == 0 || infoHash == "0000000000000000000000000000000000000000" {
		return
	}

	defer perf.ScopeTimer()()

	log.Debugf("Saving torrent entry for TMDB %s with infohash %s", tmdbID, infoHash)

	var tm TorrentAssignMetadata
	if err := d.db.One("InfoHash", infoHash, &tm); err != nil || force {
		tm = TorrentAssignMetadata{
			InfoHash: infoHash,
			Metadata: b,
		}
		// we could use just Save() since TorrentAssignMetadata does not have unique field, but bettert to be explicit
		if err == nil {
			d.db.Update(&tm)
		} else {
			d.db.Save(&tm)
		}
	}

	tmdbInt, _ := strconv.Atoi(tmdbID)

	var ti TorrentAssignItem
	if err := d.db.One("TmdbID", tmdbInt, &ti); err == nil {
		oldInfoHash := ti.InfoHash
		// check that old torrent is not equal to new torrent
		if oldInfoHash != infoHash {
			ti.InfoHash = infoHash
			log.Infof("Update torrent info, old %s, new %s", oldInfoHash, infoHash)
			if err := d.db.Update(&ti); err != nil {
				log.Errorf("Could not update torrent info: %s", err)
			}

			d.CleanupTorrentLink(oldInfoHash)

			// make old torrent disappear from "found in active torrents" dialog after restart
			oldBTItem := d.GetBTItem(oldInfoHash)
			if oldBTItem != nil {
				if err := d.db.UpdateField(oldBTItem, "ID", 0); err != nil {
					log.Errorf("Could not update old BTItem's ID: %s", err)
				}
				if err := d.db.UpdateField(oldBTItem, "ShowID", 0); err != nil {
					log.Errorf("Could not update old BTItem's ShowID: %s", err)
				}
			}
		}
		return
	}

	ti = TorrentAssignItem{
		InfoHash: infoHash,
		TmdbID:   tmdbInt,
	}
	if err := d.db.Save(&ti); err != nil {
		log.Errorf("Could not insert torrent info: %s", err)
	}
}

// UpdateTorrentMetadata updates bytes for specific InfoHash
func (d *StormDatabase) UpdateTorrentMetadata(infoHash string, b []byte) {
	// Dummy check if infohash is real
	if len(infoHash) == 0 || infoHash == "0000000000000000000000000000000000000000" {
		return
	}

	defer perf.ScopeTimer()()

	log.Debugf("Updating torrent metadata for infohash %s", infoHash)

	var tm TorrentAssignMetadata
	if err := d.db.One("InfoHash", infoHash, &tm); err != nil {
		tm = TorrentAssignMetadata{
			InfoHash: infoHash,
			Metadata: b,
		}
		d.db.Save(&tm)
	} else {
		tm = TorrentAssignMetadata{
			InfoHash: infoHash,
			Metadata: b,
		}
		d.db.Update(&tm)
	}
}

// Bittorrent Database handlers

// GetBTItem ...
func (d *StormDatabase) GetBTItem(infoHash string) *BTItem {
	defer perf.ScopeTimer()()

	item := &BTItem{}
	if err := d.db.One("InfoHash", infoHash, item); err != nil {
		return nil
	}

	return item
}

// UpdateBTItemStatus ...
func (d *StormDatabase) UpdateBTItemStatus(infoHash string, status int) error {
	defer perf.ScopeTimer()()

	item := BTItem{}
	if err := d.db.One("InfoHash", infoHash, &item); err != nil {
		return err
	}

	item.State = status
	return d.db.Update(&item)
}

// UpdateBTItem ...
func (d *StormDatabase) UpdateBTItem(infoHash string, mediaID int, mediaType string, files []string, query string, infos ...int) error {
	defer perf.ScopeTimer()()

	item := BTItem{
		ID:       mediaID,
		Type:     mediaType,
		InfoHash: infoHash,
		State:    StateActive,
		Files:    files,
		Query:    query,
	}

	if len(infos) >= 3 {
		item.ShowID = infos[0]
		item.Season = infos[1]
		item.Episode = infos[2]
	}

	var oldItem BTItem
	if err := d.db.One("InfoHash", infoHash, &oldItem); err == nil {
		d.db.DeleteStruct(&oldItem)
	}
	if err := d.db.Save(&item); err != nil {
		log.Debugf("UpdateBTItem failed: %s", err)
	}

	return nil
}

// UpdateBTItemFiles ...
func (d *StormDatabase) UpdateBTItemFiles(infoHash string, files []string) error {
	defer perf.ScopeTimer()()

	item := BTItem{}
	if err := d.db.One("InfoHash", infoHash, &item); err != nil {
		return err
	}

	item.Files = files
	return d.db.Update(&item)
}

// DeleteBTItem ...
func (d *StormDatabase) DeleteBTItem(infoHash string) error {
	defer perf.ScopeTimer()()

	return d.db.Delete(BTItemBucket, infoHash)
}

// AddTorrentHistory saves last used torrent
func (d *StormDatabase) AddTorrentHistory(infoHash, name string, b []byte) {
	defer perf.ScopeTimer()()

	if !config.Get().UseTorrentHistory {
		return
	}

	log.Debugf("Saving torrent %s with infohash %s to the history", name, infoHash)

	var oldItem TorrentHistory
	if err := d.db.One("InfoHash", infoHash, &oldItem); err == nil {
		oldItem.Dt = time.Now()
		if err := d.db.Update(&oldItem); err != nil {
			log.Warningf("Error updating item in the history: %s", err)
		}

		return
	}

	item := TorrentHistory{
		InfoHash: infoHash,
		Name:     name,
		Dt:       time.Now(),
		Metadata: b,
	}

	if err := d.db.Save(&item); err != nil {
		log.Warningf("Error inserting item to the history: %s", err)
		return
	}

	var ths []TorrentHistory
	d.db.AllByIndex("Dt", &ths, storm.Reverse(), storm.Skip(config.Get().TorrentHistorySize))
	for _, th := range ths {
		d.db.DeleteStruct(&th)
	}
	d.db.ReIndex(&TorrentHistory{})
}
