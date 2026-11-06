package proxy

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestRecording checks that requests are recorded by the proxy user name, for plain HTTP and for MITM'd HTTPS
func TestRecording(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) })
	plain := httptest.NewServer(handler)
	defer plain.Close()
	secure := httptest.NewTLSServer(handler)
	defer secure.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	oldPort := ProxyPort
	ProxyPort = listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	defer func() { ProxyPort = oldPort }()

	if p := StartProxy(); p.IsErrored {
		t.Fatal("proxy did not start")
	}
	defer StopProxy()

	get := func(user, target string) {
		proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", ProxyPort))
		if user != "" {
			proxyURL.User = url.UserPassword(user, "")
		}
		client := &http.Client{Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}}
		resp, err := client.Get(target)
		if err != nil {
			t.Fatalf("request to %s: %s", target, err)
		}
		resp.Body.Close()
	}

	StartRecording("tag1")
	get("tag1", plain.URL+"/plain")
	get("tag1", secure.URL+"/secure")
	get("other", plain.URL+"/other")
	get("", plain.URL+"/anonymous")

	got := StopRecording("tag1")
	if len(got) != 2 {
		t.Fatalf("expected 2 recorded requests, got %+v", got)
	}
	for i, path := range []string{plain.URL + "/plain", secure.URL + "/secure"} {
		if got[i].URL != path || got[i].Status != http.StatusForbidden || got[i].Method != "GET" {
			t.Errorf("unexpected request %d: %+v, expected GET 403 %s", i, got[i], path)
		}
	}
	if again := StopRecording("tag1"); again != nil {
		t.Errorf("recording was not removed: %+v", again)
	}
}
