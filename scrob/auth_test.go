package scrob

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
