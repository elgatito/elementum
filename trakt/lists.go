package trakt

import (
	"fmt"

	"github.com/anacrolix/missinggo/perf"
	"github.com/jmcvetta/napping"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/util/reqapi"
)

func (l *List) IsPersonal() bool {
	return l.Type == "personal"
}

func (l *List) IsOfficial() bool {
	return l.Type == "official"
}

func (l *List) IsPrivate() bool {
	return l.Privacy == "private"
}

func (l *List) IsPublic() bool {
	return l.Privacy == "public"
}

func (l *List) IsOur() bool {
	return l.User != nil && l.User.Ids.Slug == config.Get().TraktUsername
}

func (l *List) Username() string {
	if l.User != nil && l.User.Ids.Slug != "" {
		return l.User.Ids.Slug
	}
	return l.User.Username
}

func (l *List) ID() int {
	return l.IDs.Trakt
}

func GetList(listID string) (list *List, err error) {
	defer perf.ScopeTimer()()

	req := &reqapi.Request{
		API:         reqapi.TraktAPI,
		URL:         fmt.Sprintf("/lists/%s", listID),
		Header:      GetAvailableHeader(),
		Params:      napping.Params{}.AsUrlValues(),
		Result:      &list,
		Description: "user list information",
	}

	err = req.Do()
	return list, err
}

type ListActivities struct {
	source string
	listID string

	Previous *List
	Current  *List
}

func GetListActivities(source string, listID string) (*ListActivities, error) {

	current, err := GetList(listID)

	var previous List
	_ = cache.NewDBStore().Get(fmt.Sprintf(cache.TraktListActivitiesKey, source, listID), &previous)

	saveListActivity(source, listID, current)

	return &ListActivities{
		source: source,
		listID: listID,

		Previous: &previous,
		Current:  current,
	}, err
}

func (a *ListActivities) SaveCurrent() error {
	return saveListActivity(a.source, a.listID, a.Current)
}

func saveListActivity(source string, listID string, activity *List) error {
	return cache.NewDBStore().Set(fmt.Sprintf(cache.TraktListActivitiesKey, source, listID), activity, cache.TraktListActivitiesExpire)
}

func (a *ListActivities) HasPrevious() bool {
	return a.Previous != nil && !a.Previous.CreatedAt.IsZero()
}

func (a *ListActivities) HasCurrent() bool {
	return a.Current != nil && !a.Current.CreatedAt.IsZero()
}

func (a *ListActivities) IsUpdated() bool {
	return !a.HasPrevious() || !a.HasCurrent() || a.Current.UpdatedAt.After(a.Previous.UpdatedAt)
}
