package bittorrent

import (
	"testing"

	lt "github.com/ElementumOrg/libtorrent-go"
)

func testSettings(downloadLimit int) lt.SettingsPack {
	settings := lt.DefaultSettings()
	settings.SetStr("listen_interfaces", "127.0.0.1:0")
	settings.SetBool("enable_dht", false)
	settings.SetBool("enable_lsd", false)
	settings.SetBool("enable_upnp", false)
	settings.SetBool("enable_natpmp", false)
	settings.SetInt("download_rate_limit", downloadLimit)
	return settings
}

func TestSessionReloadAndClose(t *testing.T) {
	s := &Service{}
	s.q = NewQueue(s)

	if !s.setSessionSettings(testSettings(1000)) {
		t.Fatal("session was not created")
	}
	session := s.SessionGlobal

	// Reload must keep the session, torrents belong to it
	if !s.setSessionSettings(testSettings(2000)) {
		t.Fatal("settings were not applied on reload")
	}
	if s.SessionGlobal != session {
		t.Fatal("reload replaced the session")
	}

	s.SetUploadLimit(3000)
	var download, upload int
	s.withSession(func(sh lt.SessionHandle) {
		pack := sh.GetSettings()
		defer lt.DeleteSettingsPack(pack)
		download, upload = pack.GetInt("download_rate_limit"), pack.GetInt("upload_rate_limit")
	})
	if download != 2000 || upload != 3000 {
		t.Fatalf("session has download %d, upload %d, want 2000, 3000", download, upload)
	}

	s.Closer.Set()
	s.CloseSession()

	if s.withSession(func(lt.SessionHandle) { t.Fatal("called on closed session") }) {
		t.Fatal("expected false for closed session")
	}
	// Late config watcher event must not create a new session
	if s.setSessionSettings(testSettings(0)) || s.SessionGlobal != nil {
		t.Fatal("session was created after close")
	}
	s.SetDownloadLimit(1)
	s.Pause()
	if s.IsPaused() {
		t.Fatal("closed session reported as paused")
	}
}
