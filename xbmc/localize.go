package xbmc

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

// Localize translates "LOCALIZE[30xxx]" texts into lang, with Elementum's own copy of the add-on's strings;
// "" is the language of Kodi's interface from the configuration. The ui package sets it. A text it cannot
// translate comes back unchanged, and the add-on still translates what is left, so a string only the
// add-on has keeps working.
var Localize = func(lang, s string) string { return s }

// LanguageHeader carries the language of Kodi's interface on the add-on's requests, so the texts sent
// back follow Kodi at once when its language changes.
const LanguageHeader = "X-Kodi-Language"

var languageRe = regexp.MustCompile(`^[a-zA-Z]{2,3}(?:[-_][a-zA-Z0-9]{2,4})?$`)

// RequestLanguage returns the language a request asks for with LanguageHeader, "" when it names none.
func RequestLanguage(ctx *gin.Context) string {
	if ctx == nil || ctx.Request == nil { // a context made in code, not for a request
		return ""
	}
	if lang := ctx.GetHeader(LanguageHeader); languageRe.MatchString(lang) {
		return strings.ToLower(lang)
	}
	return ""
}

// NewViewFor is NewView for a request: the view's texts are translated into the language it asks for.
func NewViewFor(ctx *gin.Context, contentType string, items ListItems) *View {
	v := NewView(contentType, items)
	v.lang = RequestLanguage(ctx)
	return v
}

func localize(lang, s string) string {
	if !strings.Contains(s, "LOCALIZE[") {
		return s
	}
	return Localize(lang, s)
}

func localizeAll(lang string, list []string) []string {
	var out []string
	for i, s := range list {
		if t := localize(lang, s); t != s {
			if out == nil {
				out = slices.Clone(list) // the caller's slice stays as it was
			}
			out[i] = t
		}
	}
	if out == nil {
		return list
	}
	return out
}

// localizeArgs translates the texts among the arguments of a call to the add-on: titles, messages and
// lists of choices.
func localizeArgs(lang string, args Args) Args {
	out := make(Args, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case string:
			out[i] = localize(lang, v)
		case []string:
			out[i] = localizeAll(lang, v)
		case [][]string: // details lists: label, label2, icon
			rows := make([][]string, len(v))
			for n, row := range v {
				rows[n] = localizeAll(lang, row)
			}
			out[i] = rows
		default:
			out[i] = a
		}
	}
	return out
}

// MarshalJSON sends a view with its labels and context menu entries translated, the fields the add-on
// used to translate itself.
func (v View) MarshalJSON() ([]byte, error) {
	type plain View
	out := plain(v)
	out.Items = make(ListItems, len(v.Items))
	for i, item := range v.Items {
		out.Items[i] = item
		if item == nil {
			continue
		}
		label, label2 := localize(v.lang, item.Label), localize(v.lang, item.Label2)
		var menu [][]string
		for n, entry := range item.ContextMenu {
			if len(entry) > 0 {
				if t := localize(v.lang, entry[0]); t != entry[0] {
					if menu == nil {
						menu = slices.Clone(item.ContextMenu)
					}
					menu[n] = append([]string{t}, entry[1:]...)
				}
			}
		}
		if label != item.Label || label2 != item.Label2 || menu != nil {
			c := *item // a copy: views can be kept and sent again
			c.Label, c.Label2 = label, label2
			if menu != nil {
				c.ContextMenu = menu
			}
			out.Items[i] = &c
		}
	}
	return json.Marshal(out)
}

// localString returns string id of the add-on from Elementum's own copy, ok false when it has none.
func localString(lang string, id int) (string, bool) {
	raw := fmt.Sprintf("LOCALIZE[%d]", id)
	t := Localize(lang, raw)
	return t, t != raw
}
