package api

import (
	"github.com/elgatito/elementum/database"
	"github.com/elgatito/elementum/xbmc"
	"github.com/gin-gonic/gin"
)

var (
	// MovieMenu represents Custom Movie menu
	MovieMenu = Menu{Name: "MovieMenu"}

	// TVMenu represents Custom TV menu
	TVMenu = Menu{Name: "TVMenu"}

	addAction    = 0
	removeAction = 1
)

// Menu ...
type Menu struct {
	Name        string      `json:"name"`
	AddItems    []*MenuItem `json:"add_items"`
	RemoveItems []*MenuItem `json:"remove_items"`
}

// MenuItem ...
type MenuItem struct {
	Link string `json:"link"`
	Name string `json:"name"`
}

// Load ...
func (m *Menu) Load() {
	database.GetCache().GetObject(database.CommonBucket, m.Name, m)
}

// Save ...
func (m *Menu) Save() {
	database.GetCache().SetObject(database.CommonBucket, m.Name, m)
}

// Add ...
func (m *Menu) Add(action int, i *MenuItem) {
	if m.Has(action, i) != -1 {
		return
	}

	if action == addAction {
		m.AddItems = append(m.AddItems, i)
	} else {
		m.RemoveItems = append(m.RemoveItems, i)
	}

	m.Save()
}

// Remove ...
func (m *Menu) Remove(action int, i *MenuItem) {
	ei := m.Has(action, i)
	if ei == -1 {
		return
	}

	if action == addAction {
		m.AddItems = append(m.AddItems[:ei], m.AddItems[ei+1:]...)
	} else {
		m.RemoveItems = append(m.RemoveItems[:ei], m.RemoveItems[ei+1:]...)
	}

	m.Save()
}

// Contains ...
func (m *Menu) Contains(action int, i *MenuItem) bool {
	return m.Has(action, i) != -1
}

// Has ...
func (m *Menu) Has(action int, i *MenuItem) int {
	if action == addAction {
		for index, ii := range m.AddItems {
			if ii.Link == i.Link {
				return index
			}
		}
		return -1
	}

	for index, ii := range m.RemoveItems {
		if ii.Link == i.Link {
			return index
		}
	}
	return -1
}

// MenuAdd ...
func MenuAdd(ctx *gin.Context) {
	mediaType := ctx.Params.ByName("type")
	name := ctx.Query("name")
	link := ctx.Query("link")

	i := &MenuItem{Name: name, Link: link}
	log.Debugf("Adding menu item: %#v", i)

	if mediaType == "movie" {
		MovieMenu.Add(addAction, i)
	} else {
		TVMenu.Add(addAction, i)
	}

	ctx.String(200, "")
	xbmc.Refresh()
}

// MenuRemove ...
func MenuRemove(ctx *gin.Context) {
	mediaType := ctx.Params.ByName("type")
	name := ctx.Query("name")
	link := ctx.Query("link")

	i := &MenuItem{Name: name, Link: link}
	log.Debugf("Deleting menu item: %#v", i)

	if mediaType == "movie" {
		MovieMenu.Remove(addAction, i)
	} else {
		TVMenu.Remove(addAction, i)
	}

	ctx.String(200, "")
	xbmc.Refresh()
}
