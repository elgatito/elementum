package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPlayRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for target, want := range map[string]string{
		"/movie/1/play":                      "plugin://x/play?uri=u",
		"/movie/1/play?resolution_token=a+b": "plugin://x/play?uri=u&resolution_token=a+b",
	} {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest("GET", target, nil)
		playRedirect(ctx, "plugin://x/play?uri=u")
		if got := w.Header().Get("Location"); got != want {
			t.Errorf("playRedirect(%q) Location = %q, want %q", target, got, want)
		}
	}
}
