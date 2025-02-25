package api

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/xbmc"
)

// type contextMenu []*contextMenuItem
//
// type contextMenuItem []string
//
// // contextMenuRequest ...
// type contextMenuRequest struct {
// }
//
// func makeContextMenu(r contextMenuRequest) *contextMenu {
// 	m := &contextMenu{}
//
// }

func filterListItems(l xbmc.ListItems) xbmc.ListItems {
	t := config.Get().TraktToken != ""

	ret := make(xbmc.ListItems, 0)
	for _, i := range l {
		if i.TraktAuth && !t {
			continue
		}

		ret = append(ret, i)
	}

	return ret
}

// URLForXBMC ...
func URLForXBMC(pattern string, args ...interface{}) string {
	u, _ := url.Parse(fmt.Sprintf(pattern, args...))
	return "plugin://" + config.Get().Info.ID + u.String()
}

// URLQuery ...
func URLQuery(route string, query ...string) string {
	v := url.Values{}
	for i := 0; i < len(query); i += 2 {
		v.Add(query[i], query[i+1])
	}
	return route + "?" + v.Encode()
}

func contextPlayURL(f string, title string, forced bool) string {
	action := "links"
	if strings.Contains(f, "movie") && config.Get().ChooseStreamAutoMovie {
		action = "play"
	} else if strings.Contains(f, "show") && config.Get().ChooseStreamAutoShow {
		action = "play"
	} else if strings.Contains(f, "search") && config.Get().ChooseStreamAutoSearch {
		action = "play"
	}

	if forced {
		action = "force" + action
	}

	return fmt.Sprintf(f, action, url.PathEscape(title))
}

func contextPlayOppositeURL(f string, title string, forced bool) string {
	action := "links"
	if strings.Contains(f, "movie") && !config.Get().ChooseStreamAutoMovie {
		action = "play"
	} else if strings.Contains(f, "show") && !config.Get().ChooseStreamAutoShow {
		action = "play"
	} else if strings.Contains(f, "search") && !config.Get().ChooseStreamAutoSearch {
		action = "play"
	}
	if forced {
		action = "force" + action
	}

	return fmt.Sprintf(f, action, url.PathEscape(title))
}

func detectPlayAction(action, media string) string {
	if !strings.Contains(action, "force") && !strings.Contains(action, "download") && (action == "" || config.Get().ForceLinkType) {
		if media == movieType && config.Get().ChooseStreamAutoMovie {
			return "play"
		} else if media == showType && config.Get().ChooseStreamAutoShow {
			return "play"
		} else if media == searchType && config.Get().ChooseStreamAutoSearch {
			return "play"
		} else {
			return "links"
		}
	}

	return action
}
