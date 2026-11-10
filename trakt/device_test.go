package trakt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/ui"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/reqapi"
)

func stubDeviceAPI(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	oldURL, oldAPI := authURL, reqapi.TraktAPI
	authURL = srv.URL
	reqapi.TraktAPI = &reqapi.API{Ident: reqapi.TraktIdent, Endpoint: srv.URL, RateLimiter: util.NewRateLimiter(100, time.Second, 1)}
	t.Cleanup(func() { authURL, reqapi.TraktAPI = oldURL, oldAPI })
}

func TestDeviceUsernameUsesPendingToken(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"valid", `{"user":{"ids":{"slug":"new-user"}}}`, "new-user", 200},
		{"missing identity", `{"user":{}}`, "", 200},
		{"null response", `null`, "", 200},
		{"unauthorized", `{}`, "", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubDeviceAPI(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/users/settings" || r.Header.Get("Authorization") != "Bearer pending-token" {
					t.Errorf("unexpected identity request: %s %s", r.URL, r.Header.Get("Authorization"))
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			username, err := (deviceFlow{}).Username("pending-token")
			if username != tc.want || (err == nil) != (tc.want != "") {
				t.Fatalf("username=%q, err=%v", username, err)
			}
		})
	}
}

func TestDeviceRefreshKeepsTraktCredentialsAndText(t *testing.T) {
	for _, status := range []int{200, 401} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			cfg := config.Get()
			oldConfig, oldPath := *cfg, config.Args.ConfigPath
			t.Cleanup(func() { *cfg, config.Args.ConfigPath = oldConfig, oldPath })
			expiry := time.Now().Unix() + 60
			cfg.TraktToken, cfg.TraktRefreshToken, cfg.TraktUsername, cfg.TraktTokenExpiry = "old", "old-refresh", "old-user", expiry
			cfg.TraktAuthorized = true
			cfg.TrackerCredentials = map[string]*config.TrackerCredentials{"trakt": {Token: "old", RefreshToken: "old-refresh", Username: "old-user", TokenExpiry: expiry}}
			path := filepath.Join(t.TempDir(), "settings.json")
			original := `{"Settings":{"trakt_token":"old","trakt_username":"persisted-user"}}`
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			config.Args.ConfigPath = path
			ui.Default.Poll(context.Background(), "trakt-auth-tests", nil, 0)
			stubDeviceAPI(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/oauth/token" || r.Method != "POST" {
					t.Errorf("unexpected refresh request: %s %s", r.Method, r.URL)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["refresh_token"] != "old-refresh" {
					t.Errorf("refresh body=%v, err=%v", body, err)
				}
				w.WriteHeader(status)
				if status == 200 {
					_, _ = w.Write([]byte(`{"access_token":"renewed","expires_in":7200}`))
				}
			})
			err := NewProvider().RefreshToken()
			b, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if status == 401 {
				if err == nil || cfg.TraktToken != "old" || string(b) != original {
					t.Fatal("failed Trakt refresh changed credentials")
				}
				events := ui.Default.Poll(context.Background(), "trakt-auth-tests", nil, 0)
				if len(events) != 1 || events[0].Message != "LOCALIZE[30576]" {
					t.Fatalf("Trakt reauthorize text=%+v", events)
				}
				return
			}
			if err != nil || cfg.TraktToken != "renewed" || cfg.TraktRefreshToken != "old-refresh" || cfg.TraktUsername != "old-user" || cfg.TraktTokenExpiry < time.Now().Unix()+7100 || !cfg.TraktAuthorized {
				t.Fatalf("bad refresh: %v, token=%q", err, cfg.TraktToken)
			}
			c := cfg.TrackerCredentials["trakt"]
			if c.Token != cfg.TraktToken || c.RefreshToken != cfg.TraktRefreshToken || c.Username != cfg.TraktUsername || c.TokenExpiry != cfg.TraktTokenExpiry {
				t.Fatalf("Trakt credential representations differ: %+v", c)
			}
			var doc struct{ Settings map[string]any }
			if err := json.Unmarshal(b, &doc); err != nil || doc.Settings["trakt_username"] != "persisted-user" {
				t.Fatalf("Trakt refresh rewrote username: %s (%v)", b, err)
			}
		})
	}
}
