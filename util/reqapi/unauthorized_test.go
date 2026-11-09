package reqapi_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/reqapi"
)

func TestUnauthorizedIsTyped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	api := &reqapi.API{Ident: reqapi.TraktIdent, Endpoint: srv.URL, RateLimiter: util.NewRateLimiter(10, time.Second, 1)}
	err := (&reqapi.Request{API: api, URL: "x", Result: &struct{}{}}).Do()
	if !errors.Is(err, util.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}
