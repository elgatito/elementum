package simkl

import (
	"bytes"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/tracker"
)

const (
	statusPlanToWatch = "plantowatch"
	statusCompleted   = "completed"

	libraryKey    = "com.tracker.simkl.library"
	libraryExpire = 365 * 24 * time.Hour
)

var (
	libraryTypes    = []string{"shows", "movies", "anime"}
	libraryStatuses = []string{"watching", "plantowatch", "hold", "completed", "dropped"}
	// episodeParams load every watched episode with its date, also for completed and dropped shows.
	episodeParams = url.Values{"extended": {"full"}, "episode_watched_at": {"yes"}, "include_all_episodes": {"yes"}}
)

// entry is one title of the user's Simkl library.
type entry struct {
	IsMovie       bool
	Status        string
	Item          tracker.Item
	LastWatchedAt time.Time
	Episodes      []tracker.WatchedEpisode
}

func (e *entry) matches(t tracker.MediaType) bool {
	return e.IsMovie == (t == tracker.MovieType)
}

// library is the local copy of the user's Simkl library, kept current through date_from deltas.
type library struct {
	Owner    string            // username the copy belongs to
	SyncedAt string            // activities "all" the copy is current with, sent as the next date_from
	Removed  string            // removedMark at the last sync
	Entries  map[string]*entry // by Simkl id
}

type rawItem struct {
	AddedToWatchlistAt time.Time `json:"added_to_watchlist_at"`
	LastWatchedAt      time.Time `json:"last_watched_at"`
	Status             string    `json:"status"`
	AnimeType          string    `json:"anime_type"`
	Movie              *rawMedia `json:"movie"`
	Show               *rawMedia `json:"show"`
	Seasons            []struct {
		Number   int `json:"number"`
		Episodes []struct {
			Number    int       `json:"number"`
			WatchedAt time.Time `json:"watched_at"`
		} `json:"episodes"`
	} `json:"seasons"`
}

type allItems struct {
	Movies []*rawItem `json:"movies"`
	Shows  []*rawItem `json:"shows"`
	Anime  []*rawItem `json:"anime"`
}

// decodeAllItems accepts the empty array or null Simkl returns for an empty library.
func decodeAllItems(body []byte) (*allItems, error) {
	ret := &allItems{}
	body = bytes.TrimSpace(body)
	if len(body) == 0 || body[0] != '{' {
		return ret, nil
	}
	return ret, json.Unmarshal(body, ret)
}

// entries converts a response to library entries by Simkl id. Titles without a Simkl id are skipped.
func (a *allItems) entries() map[string]*entry {
	ret := map[string]*entry{}
	add := func(items []*rawItem, isMovie bool) {
		for _, r := range items {
			if r == nil {
				continue
			}
			m := r.Movie
			if m == nil {
				m = r.Show
			}
			if m == nil || m.IDs.simkl() == 0 {
				continue
			}

			// anime movies come wrapped in show, like other anime
			movie := isMovie || r.AnimeType == "movie"
			t := tracker.ShowType
			if movie {
				t = tracker.MovieType
			}
			e := &entry{
				IsMovie:       movie,
				Status:        r.Status,
				Item:          tracker.Item{Type: t, IDs: m.IDs.tracker(), Title: m.Title, Year: m.Year, ListedAt: r.AddedToWatchlistAt},
				LastWatchedAt: r.LastWatchedAt,
			}
			for _, s := range r.Seasons {
				for _, ep := range s.Episodes {
					at := ep.WatchedAt
					if at.IsZero() {
						at = r.LastWatchedAt
					}
					e.Episodes = append(e.Episodes, tracker.WatchedEpisode{Season: s.Number, Number: ep.Number, Plays: 1, LastWatchedAt: at})
				}
			}
			ret[strconv.Itoa(m.IDs.simkl())] = e
		}
	}
	add(a.Movies, true)
	add(a.Shows, false)
	add(a.Anime, false)
	return ret
}

// fetchAllItems reads /sync/all-items with params.
func (p *Provider) fetchAllItems(path string, params url.Values) (map[string]*entry, error) {
	req, err := p.request("GET", path, params, nil, nil, false, false)
	if err != nil {
		if req != nil && req.ResponseStatusCode == 400 && req.ResponseBody != nil {
			if b := req.ResponseBody.String(); strings.Contains(b, "max_items") || strings.Contains(b, "Too many") {
				return nil, errMaxItems
			}
		}
		return nil, err
	}

	var body []byte
	if req.ResponseBody != nil {
		body = req.ResponseBody.Bytes()
	}
	items, err := decodeAllItems(body)
	if err != nil {
		return nil, err
	}
	return items.entries(), nil
}

// fullPull reads the whole library one type after another, never in parallel as Simkl asks.
// A type too big for one response is split by status.
func (p *Provider) fullPull() (map[string]*entry, error) {
	ret := map[string]*entry{}
	for _, t := range libraryTypes {
		items, err := p.fetchAllItems("/sync/all-items/"+t, episodeParams)
		if err == errMaxItems {
			items = map[string]*entry{}
			for _, status := range libraryStatuses {
				part, err := p.fetchAllItems("/sync/all-items/"+t+"/"+status, episodeParams)
				if err != nil {
					return nil, err
				}
				for k, v := range part {
					items[k] = v
				}
			}
		} else if err != nil {
			return nil, err
		}
		for k, v := range items {
			ret[k] = v
		}
	}
	return ret, nil
}

// applyDelta merges titles changed after lib.SyncedAt, and drops removed titles when a removal happened.
func (p *Provider) applyDelta(lib *library, acts *activities) error {
	params := url.Values{}
	for k, v := range episodeParams {
		params[k] = v
	}
	if lib.SyncedAt != "" {
		params.Set("date_from", lib.SyncedAt)
	}
	changed, err := p.fetchAllItems("/sync/all-items", params)
	if err != nil {
		return err
	}

	// Deltas never return removed titles, only an id-only snapshot can tell what is gone
	var present map[string]*entry
	if acts.removedMark() != lib.Removed {
		if present, err = p.fetchAllItems("/sync/all-items", url.Values{"extended": {"simkl_ids_only"}}); err != nil {
			return err
		}
	}

	for k, v := range changed {
		lib.Entries[k] = v
	}
	if present != nil {
		for k := range lib.Entries {
			if _, ok := present[k]; !ok {
				delete(lib.Entries, k)
			}
		}
	}
	return nil
}

// library returns the local library copy, bringing it up to date when refresh is set and activities moved.
// The copy is saved only after every fetch succeeded, so a failed sync is retried with the same date_from.
func (p *Provider) library(refresh bool) (*library, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	owner := tracker.Credentials(p.ID()).Username
	if p.lib == nil || p.lib.Owner != owner {
		p.lib = nil
		lib := &library{}
		if err := cache.NewDBStore().Get(libraryKey, lib); err == nil && lib.Owner == owner && lib.Entries != nil {
			p.lib = lib
		}
	}
	if p.lib != nil && !refresh {
		return p.lib, nil
	}

	// Activities must be read before the fetch they anchor, LastActivities does that at the start of a sync
	acts := p.acts
	if acts == nil {
		var err error
		if acts, err = p.fetchActivities(); err != nil {
			return nil, err
		}
		p.acts = acts
	}

	if p.lib == nil {
		entries, err := p.fullPull()
		if err != nil {
			return nil, err
		}
		p.lib = &library{Owner: owner, Entries: entries}
	} else if acts.All == p.lib.SyncedAt && acts.removedMark() == p.lib.Removed {
		return p.lib, nil
	} else {
		// work on a copy, so a failed delta leaves the saved state untouched
		next := &library{Owner: p.lib.Owner, SyncedAt: p.lib.SyncedAt, Removed: p.lib.Removed, Entries: make(map[string]*entry, len(p.lib.Entries))}
		for k, v := range p.lib.Entries {
			next.Entries[k] = v
		}
		if err := p.applyDelta(next, acts); err != nil {
			return p.lib, err
		}
		p.lib = next
	}

	p.lib.SyncedAt = acts.All
	p.lib.Removed = acts.removedMark()
	if err := cache.NewDBStore().Set(libraryKey, p.lib, libraryExpire); err != nil {
		log.Warningf("Could not save Simkl library: %s", err)
	}
	return p.lib, nil
}
