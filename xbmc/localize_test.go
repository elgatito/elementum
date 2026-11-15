package xbmc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func useLocalize(t *testing.T) {
	old := Localize
	Localize = func(lang, s string) string {
		if lang == "ru" {
			return strings.ReplaceAll(s, "LOCALIZE[30001]", "Фильмы")
		}
		return strings.ReplaceAll(s, "LOCALIZE[30001]", "Filme")
	}
	t.Cleanup(func() { Localize = old })
}

func TestLocalizeArgs(t *testing.T) {
	useLocalize(t)
	items := []string{"LOCALIZE[30001]", "plain"}
	got := localizeArgs("", Args{"LOCALIZE[30001]", items, []int{1}, 5, "LOCALIZE[99999]"})
	if got[0] != "Filme" || got[1].([]string)[0] != "Filme" || got[1].([]string)[1] != "plain" || got[4] != "LOCALIZE[99999]" {
		t.Errorf("%v", got)
	}
	if items[0] != "LOCALIZE[30001]" {
		t.Error("the caller's slice was changed")
	}
	same := []string{"a", "b"}
	if out := localizeAll("", same); &out[0] != &same[0] {
		t.Error("a list without texts to translate was copied")
	}
}

func TestViewJSON(t *testing.T) {
	useLocalize(t)
	item := &ListItem{Label: "LOCALIZE[30001]", Label2: "x LOCALIZE[30001]", ContextMenu: [][]string{{"LOCALIZE[30001]", "RunPlugin(a)"}, {"Keep", "RunPlugin(b)"}}}
	plain := &ListItem{Label: "Plain"}
	view := NewView("movies", ListItems{item, plain, nil})
	b, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		ContentType string `json:"content_type"`
		Items       []struct {
			Label       string     `json:"label"`
			Label2      string     `json:"label2"`
			ContextMenu [][]string `json:"context_menu"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.ContentType != "movies" || len(out.Items) != 3 || out.Items[0].Label != "Filme" || out.Items[0].Label2 != "x Filme" ||
		out.Items[0].ContextMenu[0][0] != "Filme" || out.Items[0].ContextMenu[0][1] != "RunPlugin(a)" || out.Items[0].ContextMenu[1][0] != "Keep" || out.Items[1].Label != "Plain" {
		t.Errorf("%s", b)
	}
	if item.Label != "LOCALIZE[30001]" || item.ContextMenu[0][0] != "LOCALIZE[30001]" {
		t.Error("the view's own items were changed")
	}
	// a value View too, as gin may get either
	if b2, _ := json.Marshal(*view); string(b2) != string(b) {
		t.Errorf("value and pointer differ: %s", b2)
	}
}

func TestLocalString(t *testing.T) {
	useLocalize(t)
	if s, ok := localString("", 30001); !ok || s != "Filme" {
		t.Errorf("%q %v", s, ok)
	}
	if _, ok := localString("", 30002); ok {
		t.Error("an unknown id must go to the add-on")
	}
}

func TestRequestLanguage(t *testing.T) {
	useLocalize(t)
	gin.SetMode(gin.TestMode)
	request := func(lang string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Request.Header.Set("User-Agent", "plugin.video.elementum")
		if lang != "" {
			c.Request.Header.Set(LanguageHeader, lang)
		}
		return c
	}
	for in, want := range map[string]string{"ru": "ru", "pt-BR": "pt-br", "pt_br": "pt_br", "": "", "english": "", "de;q=1": "", "../x": ""} {
		if got := RequestLanguage(request(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}

	// the host of a request is a copy with its language; the registered one is not changed
	old := XBMCHosts
	XBMCHosts = []*XBMCHost{{Host: "192.0.2.1"}}
	t.Cleanup(func() { XBMCHosts = old })
	c := request("ru")
	c.Request.RemoteAddr = "192.0.2.1:5000"
	h, err := GetXBMCHostWithContext(c)
	if err != nil || h == nil || h.Lang != "ru" || h.Host != "192.0.2.1" || XBMCHosts[0].Lang != "" || h == XBMCHosts[0] {
		t.Fatalf("host %+v, %v", h, err)
	}
	if got := localizeArgs(h.Lang, Args{"LOCALIZE[30001]"}); got[0] != "Фильмы" {
		t.Errorf("args in the request's language: %v", got)
	}
	plain := request("")
	plain.Request.RemoteAddr = "192.0.2.1:5000"
	if h, _ := GetXBMCHostWithContext(plain); h != XBMCHosts[0] {
		t.Error("without the header the registered host itself is returned")
	}

	b, _ := json.Marshal(NewViewFor(c, "", ListItems{{Label: "LOCALIZE[30001]"}}))
	if !strings.Contains(string(b), "Фильмы") {
		t.Errorf("view in the request's language: %s", b)
	}
}
