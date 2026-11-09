package apiv1_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/elgatito/elementum/apiv1"
	"github.com/elgatito/elementum/ui"
)

func TestUIEventsAndAnswers(t *testing.T) {
	setCreds(t, "user", "secret")
	r := newEngine()
	cid := fmt.Sprintf("tv%d", time.Now().UnixNano()) // ui.Default outlives the test
	get := func(path string) *httptest.ResponseRecorder {
		return do(r, request{path: path, user: "user", pass: "secret"})
	}

	if w := do(r, request{path: "/api/v1/ui/events?client_id=" + cid}); w.Code != http.StatusUnauthorized {
		t.Errorf("no auth: %d", w.Code)
	}
	for _, q := range []string{"", "client_id=bad%20id", "client_id=" + cid + "&wait=61", "client_id=" + cid + "&since=-1", "client_id=" + cid + "&lang=deutsch"} {
		if w := get("/api/v1/ui/events?" + q); w.Code != http.StatusBadRequest || errorCode(t, w) != apiv1.ErrorCodeBadRequest {
			t.Errorf("%q: %d %s", q, w.Code, w.Body)
		}
	}
	w204 := get("/api/v1/ui/events?client_id=" + cid + "&wait=0")
	if w204.Code != http.StatusNoContent || w204.Header().Get("X-UI-Epoch") == "" {
		t.Fatalf("idle poll: %d epoch %q", w204.Code, w204.Header().Get("X-UI-Epoch"))
	}

	// A toast and a question reach the poll; the first answer wins, the second gets 410.
	ui.Default.Notify(ui.Notice{Title: "Elementum", Message: "hello"})
	done := make(chan bool)
	go func() {
		yes, ok := ui.HTTPHost{B: ui.Default}.Confirm(context.Background(), ui.Question{Title: "t", Message: "m", Timeout: 5 * time.Second})
		done <- yes && ok
	}()
	var batch apiv1.UIEventBatch
	var q apiv1.UIEvent
	for q.Id == "" {
		w := get("/api/v1/ui/events?client_id=" + cid + "&wait=2")
		if w.Code != http.StatusOK {
			t.Fatalf("poll: %d %s", w.Code, w.Body)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &batch); err != nil {
			t.Fatal(err)
		}
		if batch.Epoch == "" || batch.Epoch != w.Header().Get("X-UI-Epoch") {
			t.Errorf("epoch %q vs header %q", batch.Epoch, w.Header().Get("X-UI-Epoch"))
		}
		for _, e := range batch.Events {
			if e.Kind == apiv1.UIEventKindConfirm {
				q = e
			}
		}
	}
	answer := func(id, client string) *httptest.ResponseRecorder {
		return do(r, request{method: http.MethodPost, path: "/api/v1/ui/requests/" + id, body: `{"client_id":"` + client + `","yes":true}`, user: "user", pass: "secret"})
	}
	if w := answer(q.Id, "bad id"); w.Code != http.StatusBadRequest {
		t.Errorf("bad client id: %d", w.Code)
	}
	if w := answer("00000000000000000000000000000000", "tv"); w.Code != http.StatusNotFound {
		t.Errorf("unknown: %d", w.Code)
	}
	if w := answer(q.Id, "tv"); w.Code != http.StatusNoContent {
		t.Fatalf("answer: %d %s", w.Code, w.Body)
	}
	if w := answer(q.Id, "other-tv"); w.Code != http.StatusGone || errorCode(t, w) != apiv1.ErrorCodeUiRequestExpired {
		t.Errorf("second answer: %d %s", w.Code, w.Body)
	}
	if !<-done {
		t.Error("the question did not get the yes")
	}
}
