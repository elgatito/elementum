package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/config"
)

func TestTopComponent(t *testing.T) {
	for in, want := range map[string]string{
		"movie.mkv":        "movie.mkv",
		"Show/S01/e01.mkv": "Show",
		".abcdef.parts":    ".abcdef.parts",
	} {
		if got := topComponent(in); got != want {
			t.Errorf("topComponent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEntrySizeAndRoot(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(dir, "a", "x"), make([]byte, 10), 0o644)
	os.WriteFile(filepath.Join(dir, "a", "b", "y"), make([]byte, 5), 0o644)

	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if got := entrySize(root.FS(), "a"); got != 15 {
		t.Errorf("entrySize = %d, want 15", got)
	}
	if err := root.RemoveAll("../outside"); err == nil {
		t.Error("RemoveAll escaped the download root")
	}
}

func TestLeftoversFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a #1.mkv"), []byte("0123456789"), 0o644)
	os.WriteFile(filepath.Join(filepath.Dir(dir), "secret"), []byte("x"), 0o644)
	config.Get().DownloadPath = dir

	r := gin.New()
	r.GET("/cmd/leftovers/files/*path", LeftoversFile)
	get := func(target, rng string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", target, nil)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := get("/cmd/leftovers/files/a%20%231.mkv", "bytes=2-4"); w.Code != http.StatusPartialContent || w.Body.String() != "234" {
		t.Errorf("range request = %d %q", w.Code, w.Body.String())
	}
	if w := get("/cmd/leftovers/files/..%2fsecret", ""); w.Code == http.StatusOK {
		t.Errorf("escaped download path: %d %q", w.Code, w.Body.String())
	}
}
