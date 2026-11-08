package trakt

import (
	"testing"

	"github.com/elgatito/elementum/config"
)

func TestAuthorizeFileModeWithoutKodi(t *testing.T) {
	old := config.Args.ConfigPath
	config.Args.ConfigPath = "/nonexistent/settings.json"
	defer func() { config.Args.ConfigPath = old }()
	if err := Authorize(false); err == nil {
		t.Fatal("expected an error without Kodi")
	}
}
