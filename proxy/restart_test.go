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

// TestProxyStartStop covers runtime enable/disable: the proxy must bind only
// once, keep serving on repeated starts, and free the port after stopping.
func TestProxyStartStop(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ua=%s", r.UserAgent())
	}))
	defer target.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	oldPort := ProxyPort
	ProxyPort = listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	defer func() { ProxyPort = oldPort }()

	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", ProxyPort))
	client := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}

	get := func() error {
		req, _ := http.NewRequest("GET", target.URL, nil)
		req.Header.Set("User-Agent", "SomeBot/1.0")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		return nil
	}

	first := StartProxy()
	if first.IsErrored || first.Server == nil {
		t.Fatal("proxy did not start")
	}
	if again := StartProxy(); again != first {
		t.Fatal("second start replaced the running proxy")
	}
	if err := get(); err != nil {
		t.Fatalf("request through running proxy: %s", err)
	}

	StopProxy()
	if err := get(); err == nil {
		t.Fatal("proxy still reachable after stop")
	}

	restarted := StartProxy()
	if restarted.IsErrored || restarted.Server == nil {
		t.Fatal("proxy did not restart")
	}
	if err := get(); err != nil {
		t.Fatalf("request through restarted proxy: %s", err)
	}
	StopProxy()
}
