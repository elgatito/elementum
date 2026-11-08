package config

import (
	"os"
	"runtime"
	"testing"
)

func TestWindowsToWSLPath(t *testing.T) {
	if got := WindowsToWSLPath("/home/user/"); got != "/home/user/" {
		t.Errorf("linux path changed: %s", got)
	}
	if got := WindowsToWSLPath(`Z:\missing\dir\`); got != `Z:\missing\dir\` {
		t.Errorf("missing path changed: %s", got)
	}

	if _, err := os.Stat("/mnt/c/"); err != nil {
		t.Skip("not running in WSL")
	}
	if got := WindowsToWSLPath(`C:\`); got != "/mnt/c/" {
		t.Errorf("expected /mnt/c/, got %s", got)
	}
}

func TestSplitKodiDir(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("WSL mapping is linux only")
	}
	mounted := func(p string) bool { return p == "/mnt/d/Torrents/" }
	for _, c := range []struct{ raw, kodi, local string }{
		{`D:\Torrents\`, `D:\Torrents`, "/mnt/d/Torrents"},
		{"/home/x/Torrents/", "/home/x/Torrents", "/home/x/Torrents"},
		{"", ".", "."},
	} {
		kodi, local := splitKodiDir(c.raw, mounted)
		if kodi != c.kodi || local != c.local {
			t.Errorf("%q: got %q, %q; want %q, %q", c.raw, kodi, local, c.kodi, c.local)
		}
	}
}
