package config

import (
	"os"
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
