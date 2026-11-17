package scrob

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/reqapi"
)

func TestUsername(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"decoded identity", `{"display_name":"alice"}`, "alice", 200},
		{"empty identity", `{}`, "", 200},
		{"unauthorized", `{}`, "", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/proxy/profile/me" || r.Header.Get("Authorization") != "Bearer pending-token" {
					t.Errorf("unexpected identity request: %s %s", r.URL, r.Header.Get("Authorization"))
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			oldAPI, oldURL := reqapi.ScrobAPI, config.Get().ScrobURL
			reqapi.ScrobAPI = &reqapi.API{Ident: reqapi.ScrobIdent, RateLimiter: util.NewRateLimiter(100, time.Second, 1)}
			config.Get().ScrobURL = srv.URL
			defer func() { reqapi.ScrobAPI, config.Get().ScrobURL = oldAPI, oldURL }()
			got, err := NewProvider().username("pending-token")
			if got != tc.want || (tc.status == 200 && err != nil) || (tc.status == 401 && !errors.Is(err, util.ErrUnauthorized)) {
				t.Fatalf("username=%q, err=%v", got, err)
			}
		})
	}
}

// TestLoginPageRedirect covers Scrob's web front end redirecting a device token to its HTML login page (#1217).
func TestLoginPageRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login" {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!DOCTYPE html><html></html>"))
	}))
	defer srv.Close()
	c := config.Get()
	oldAPI, oldURL, oldKey, oldCreds := reqapi.ScrobAPI, c.ScrobURL, c.ScrobAPIKey, c.TrackerCredentials
	reqapi.ScrobAPI = &reqapi.API{Ident: reqapi.ScrobIdent, RateLimiter: util.NewRateLimiter(100, time.Second, 1)}
	c.ScrobURL, c.ScrobAPIKey = srv.URL, ""
	c.TrackerCredentials = map[string]*config.TrackerCredentials{"scrob": {Token: "device-token"}}
	defer func() {
		reqapi.ScrobAPI, c.ScrobURL, c.ScrobAPIKey, c.TrackerCredentials = oldAPI, oldURL, oldKey, oldCreds
	}()

	if _, err := NewProvider().Progress(false); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("device token: err=%v", err)
	}
	c.ScrobAPIKey = "key"
	if _, err := NewProvider().Progress(false); err == nil || !strings.Contains(err.Error(), "address") {
		t.Fatalf("API key: err=%v", err)
	}
}
