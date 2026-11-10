package trakt

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tracker"
)

func TestAuthorizeFileModeWithoutKodi(t *testing.T) {
	old := config.Args.ConfigPath
	config.Args.ConfigPath = "/nonexistent/settings.json"
	defer func() { config.Args.ConfigPath = old }()
	if err := NewProvider().Authorize(); err == nil {
		t.Fatal("expected an error without Kodi")
	}
}

func TestPollTokenStatus(t *testing.T) {
	status := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		if status == 200 {
			w.Write([]byte(`{"access_token":"a","refresh_token":"r","expires_in":60}`))
		}
	}))
	defer srv.Close()
	old := authURL
	authURL = srv.URL
	defer func() { authURL = old }()

	for _, c := range []struct {
		status int
		want   error
	}{{400, tracker.ErrAuthPending}, {429, tracker.ErrSlowDown}, {200, nil}} {
		status = c.status
		tok, err := deviceFlow{}.PollToken(&tracker.DeviceCode{DeviceCode: "d"})
		if !errors.Is(err, c.want) {
			t.Fatalf("status %d: err = %v, want %v", c.status, err, c.want)
		}
		if c.status == 200 && (tok == nil || tok.AccessToken != "a" || tok.RefreshToken != "r" || tok.ExpiresIn != 60) {
			t.Fatalf("token = %+v", tok)
		}
	}
	for _, s := range []int{404, 409, 410, 418} {
		status = s
		if _, err := (deviceFlow{}).PollToken(&tracker.DeviceCode{}); err == nil || errors.Is(err, tracker.ErrAuthPending) {
			t.Fatalf("status %d: err = %v", s, err)
		}
	}
}
