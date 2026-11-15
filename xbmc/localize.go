package xbmc

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Localize translates "LOCALIZE[30xxx]" texts into the language of Kodi's interface, with Elementum's own
// copy of the add-on's strings. The ui package sets it. A text it cannot translate comes back unchanged,
// and the add-on still translates what is left, so a string only the add-on has keeps working.
var Localize = func(s string) string { return s }

func localize(s string) string {
	if !strings.Contains(s, "LOCALIZE[") {
		return s
	}
	return Localize(s)
}

func localizeAll(list []string) []string {
	var out []string
	for i, s := range list {
		if t := localize(s); t != s {
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
func localizeArgs(args Args) Args {
	out := make(Args, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case string:
			out[i] = localize(v)
		case []string:
			out[i] = localizeAll(v)
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
		label, label2 := localize(item.Label), localize(item.Label2)
		var menu [][]string
		for n, entry := range item.ContextMenu {
			if len(entry) > 0 {
				if t := localize(entry[0]); t != entry[0] {
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
func localString(id int) (string, bool) {
	raw := fmt.Sprintf("LOCALIZE[%d]", id)
	t := Localize(raw)
	return t, t != raw
}
