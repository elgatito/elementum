package bittorrent

import (
	"testing"
	"time"
)

func TestBufferWithoutKodiNeedsExternal(t *testing.T) {
	// Fails before touching the service, so a nil one is fine.
	if err := NewPlayer(nil, PlayerParams{}, nil).Buffer(); err != errNoXBMCHost {
		t.Fatalf("got %v, want errNoXBMCHost", err)
	}
}

func TestGetActivePlayerSkipsExternal(t *testing.T) {
	s := &Service{Players: map[string]*Player{
		"api": {t: &Torrent{}, p: &PlayerParams{Playing: true, External: true}},
	}}
	if p := s.GetActivePlayer(); p != nil {
		t.Fatal("external player returned")
	}
	kodi := &Player{t: &Torrent{}, p: &PlayerParams{Playing: true}}
	s.Players["kodi"] = kodi
	if p := s.GetActivePlayer(); p != kodi {
		t.Fatal("Kodi player not returned")
	}
}

// An API session stopped while its reused torrent still waits for metadata: Close releases the torrent
// once, and Buffer must not release it again.
func TestStopReusedTorrentBeforeBuffer(t *testing.T) {
	s := &Service{Players: map[string]*Player{}}
	tor := &Torrent{infoHash: "abc"}
	kodi := &Player{s: s, t: tor, p: &PlayerParams{Playing: true}}
	s.Players["abc"] = kodi
	tor.PlayerAttached.Store(1) // Kodi plays it too, so nobody removes it

	p := NewPlayer(s, PlayerParams{External: true, ResumeHash: "abc"}, nil)
	p.SetTorrent(tor)
	p.Close()
	if err := p.Buffer(); err != errPlayerClosed {
		t.Fatalf("Buffer: %v, want errPlayerClosed", err)
	}
	p.Close()

	for i := 0; i < 100 && tor.PlayerAttached.Load() != 1; i++ {
		time.Sleep(10 * time.Millisecond) // Close detaches in a goroutine
	}
	time.Sleep(50 * time.Millisecond)
	if n := tor.PlayerAttached.Load(); n != 1 {
		t.Fatalf("PlayerAttached %d, want 1", n)
	}
	s.mu.Lock()
	if s.Players["abc"] != kodi {
		t.Fatal("API player detach dropped Kodi's player entry")
	}
	delete(s.Players, "abc")
	s.mu.Unlock()

	s.AttachPlayer(p) // stopped during processMetadata, attached afterwards
	if len(s.Players) != 0 {
		t.Fatal("closed player was registered")
	}
}
