package xbmc

import (
	"encoding/json"
	"strings"
	"testing"
)

func useLocalize(t *testing.T) {
	old := Localize
	Localize = func(s string) string { return strings.ReplaceAll(s, "LOCALIZE[30001]", "Filme") }
	t.Cleanup(func() { Localize = old })
}

func TestLocalizeArgs(t *testing.T) {
	useLocalize(t)
	items := []string{"LOCALIZE[30001]", "plain"}
	got := localizeArgs(Args{"LOCALIZE[30001]", items, []int{1}, 5, "LOCALIZE[99999]"})
	if got[0] != "Filme" || got[1].([]string)[0] != "Filme" || got[1].([]string)[1] != "plain" || got[4] != "LOCALIZE[99999]" {
		t.Errorf("%v", got)
	}
	if items[0] != "LOCALIZE[30001]" {
		t.Error("the caller's slice was changed")
	}
	same := []string{"a", "b"}
	if out := localizeAll(same); &out[0] != &same[0] {
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
	if s, ok := localString(30001); !ok || s != "Filme" {
		t.Errorf("%q %v", s, ok)
	}
	if _, ok := localString(30002); ok {
		t.Error("an unknown id must go to the add-on")
	}
}
