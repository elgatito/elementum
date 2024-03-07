package database

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/asdine/storm"
	"github.com/asdine/storm/q"

	bolt "go.etcd.io/bbolt"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/exit"
)

// GetStorm returns common database
func GetStorm() *StormDatabase {
	return stormDatabase
}

// GetStormDB returns common database
func GetStormDB() *storm.DB {
	if stormDatabase == nil {
		return nil
	}
	return stormDatabase.db
}

// InitStormDB ...
func InitStormDB(conf *config.Configuration) (*StormDatabase, error) {
	databasePath := filepath.Join(conf.Info.Profile, stormFileName)
	backupPath := filepath.Join(conf.Info.Profile, backupStormFileName)
	compressPath := filepath.Join(conf.Info.Profile, compressStormFileName)

	db, err := CreateStormDB(conf, databasePath, backupPath)
	if err != nil || db == nil {
		return nil, errors.New("database not created")
	}

	stormDatabase = &StormDatabase{
		db: db,
		Database: Database{
			isCaching: false,

			quit: make(chan struct{}, 5),

			fileName: stormFileName,
			filePath: databasePath,

			backupFileName: backupStormFileName,
			backupFilePath: backupPath,

			compressFilePath: compressPath,
		},
	}

	stormDatabase.mu.Lock()
	defer stormDatabase.mu.Unlock()

	return stormDatabase, nil
}

// CreateStormDB ...
func CreateStormDB(conf *config.Configuration, databasePath, backupPath string) (*storm.DB, error) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("Got critical error while creating Storm: %v", r)
			RestoreBackup(databasePath, backupPath)
			exit.Exit(exit.ExitCodeError)
		}
	}()

	db, err := storm.Open(databasePath, storm.BoltOptions(0600, &bolt.Options{
		ReadOnly: false,
		Timeout:  15 * time.Second,
		NoSync:   true,
	}))

	if err != nil {
		log.Warningf("Could not open database at %s: %s", databasePath, err)
		return nil, err
	}

	return db, nil
}

// MaintenanceRefreshHandler ...
func (d *StormDatabase) MaintenanceRefreshHandler() {
	if d == nil || d.db == nil {
		return
	}

	CreateBackup(d.db.Bolt, d.backupFilePath)

	tickerBackup := time.NewTicker(backupPeriod)
	tickerCleanup := time.NewTicker(cleanupPeriod)

	defer tickerBackup.Stop()
	defer tickerCleanup.Stop()
	defer close(d.quit)

	for {
		select {
		case <-tickerBackup.C:
			go CreateBackup(d.db.Bolt, d.backupFilePath)
		case <-tickerCleanup.C:
			go CacheCleanup(d.db.Bolt)
		case <-d.quit:
			return
		}
	}
}

// Close ...
func (d *StormDatabase) Close() {
	if d == nil || d.db == nil {
		return
	}

	log.Info("Closing Storm Database")

	d.IsClosed = true
	// Let it sleep to keep up all the active tasks
	time.Sleep(100 * time.Millisecond)

	d.mu.Lock()
	defer d.mu.Unlock()

	d.quit <- struct{}{}
	d.db.Close()
}

// GetFilename returns bolt filename
func (d *Database) GetFilename() string {
	if d == nil {
		return ""
	}

	return d.fileName
}

// AddSearchHistory adds query to search history, according to media type
func (d *StormDatabase) AddSearchHistory(historyType, query string) {
	if d == nil || d.db == nil {
		return
	}

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
	if d == nil || d.db == nil {
		return
	}

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
	if d == nil || d.db == nil {
		return
	}

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
	if d == nil || d.db == nil {
		return
	}

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
	if d == nil || d.db == nil {
		return
	}

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
		// we could use just Save() since TorrentAssignMetadata does not have unique field, but better to be explicit
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
	if d == nil || d.db == nil {
		return
	}

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
	if d == nil || d.db == nil {
		return nil
	}

	defer perf.ScopeTimer()()

	item := &BTItem{}
	if err := d.db.One("InfoHash", infoHash, item); err != nil {
		return nil
	}

	return item
}

// UpdateBTItemStatus ...
func (d *StormDatabase) UpdateBTItemStatus(infoHash string, status int) error {
	if d == nil || d.db == nil {
		return errors.New("Database not initialized")
	}

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
	if d == nil || d.db == nil {
		return errors.New("Database not initialized")
	}

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
	if d == nil || d.db == nil {
		return errors.New("Database not initialized")
	}

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
	if d == nil || d.db == nil {
		return errors.New("Database not initialized")
	}

	defer perf.ScopeTimer()()

	return d.db.Delete(BTItemBucket, infoHash)
}

// AddTorrentHistory saves last used torrent
func (d *StormDatabase) AddTorrentHistory(infoHash, name string, b []byte) {
	if d == nil || d.db == nil {
		return
	}

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

// Compress ...
func (d *StormDatabase) Compress() (err error) {
	if d == nil || d.db == nil {
		return errors.New("Database not initialized")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.db.Close()

	if err = CompressBoltDB(config.Get(), d.filePath, d.compressFilePath); err != nil {
		return err
	}

	d.db, err = CreateStormDB(config.Get(), d.filePath, d.backupFilePath)
	return err
}
