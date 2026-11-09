package tmdb

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/jmcvetta/napping"

	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/util/reqapi"
)

// Person is TMDB's /person/{id} answer.
type Person struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	ProfilePath        string `json:"profile_path"`
	Biography          string `json:"biography"`
	Birthday           string `json:"birthday"`
	Deathday           string `json:"deathday"`
	PlaceOfBirth       string `json:"place_of_birth"`
	KnownForDepartment string `json:"known_for_department"`
}

// GetPerson returns a person's details, nil when TMDB has none.
func GetPerson(id int, language string) *Person {
	var p *Person
	req := reqapi.Request{
		API:         reqapi.TMDBAPI,
		URL:         fmt.Sprintf("/person/%d", id),
		Params:      napping.Params{"api_key": apiKey, "language": language}.AsUrlValues(),
		Result:      &p,
		Description: "person",

		Cache:       true,
		CacheExpire: cache.CacheExpireLong,
	}
	if req.Do(); p == nil || p.ID == 0 {
		return nil
	}
	return p
}

type personCredit struct {
	Entity
	MediaType  string  `json:"media_type"`
	Popularity float64 `json:"popularity"`
	Character  string  `json:"character"`
}

type combinedCredits struct {
	Cast []*personCredit `json:"cast"`
	Crew []*personCredit `json:"crew"`
}

func fetchCredits(id int, language string) (*combinedCredits, error) {
	var raw *combinedCredits
	req := reqapi.Request{
		API:         reqapi.TMDBAPI,
		URL:         fmt.Sprintf("/person/%d/combined_credits", id),
		Params:      napping.Params{"api_key": apiKey, "language": language}.AsUrlValues(),
		Result:      &raw,
		Description: "person credits",

		Cache:       true,
		CacheExpire: cache.CacheExpireMedium,
	}
	if err := req.Do(); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, util.ErrNotFound
	}
	return &combinedCredits{Cast: raw.Cast, Crew: raw.Crew}, nil
}

// PersonCredits returns the movies and shows a person played in or worked on (cast first, then crew, one
// entry per id), most popular first, then by vote count. Entries are Lite: built from the credits alone, with
// overviews missing in the language filled from en-US (best effort, cached). The error is util.ErrNotFound for an unknown person.
func PersonCredits(id int, language string) (Movies, Shows, error) {
	raw, err := fetchCredits(id, language)
	if err != nil {
		return nil, nil, err
	}
	if !strings.HasPrefix(language, "en") && hasEmptyOverview(raw) {
		if en, err := fetchCredits(id, "en-US"); err == nil {
			fillCreditOverviews(raw, en)
		}
	}
	movies, shows := splitCredits(raw, liteGenres([]ListOption{Lite}, func() []*Genre { return GetMovieGenres(language) }), liteGenres([]ListOption{Lite}, func() []*Genre { return GetTVGenres(language) }))
	return movies, shows, nil
}

func hasEmptyOverview(c *combinedCredits) bool {
	for _, e := range append(slices.Clone(c.Cast), c.Crew...) {
		if e != nil && e.Overview == "" {
			return true
		}
	}
	return false
}

func fillCreditOverviews(dst, en *combinedCredits) {
	byID := map[string]string{}
	for _, e := range append(slices.Clone(en.Cast), en.Crew...) {
		if e != nil {
			byID[fmt.Sprint(e.MediaType, e.ID)] = e.Overview
		}
	}
	for _, e := range append(slices.Clone(dst.Cast), dst.Crew...) {
		if e != nil && e.Overview == "" {
			e.Overview = byID[fmt.Sprint(e.MediaType, e.ID)]
		}
	}
}

// TV talk, news and reality shows are mostly guest appearances as themselves (as in PersonShows).
func isTalkOrNews(ids []int) bool {
	return slices.ContainsFunc(ids, func(g int) bool { return g == 10767 || g == 10763 || g == 10764 })
}

// isSelf reports a cast character like "Self", "Himself - Guest" (crew entries have no character).
func isSelf(character string) bool {
	c := strings.ToLower(strings.TrimSpace(character))
	for _, p := range []string{"self", "himself", "herself", "themselves"} {
		if c == p || strings.HasPrefix(c, p+" -") || strings.HasPrefix(c, p+" (") {
			return true
		}
	}
	return false
}

// splitCredits skips adult titles, de-duplicates by media type and id (cast wins over crew) and sorts each
// part by popularity, then vote count.
func splitCredits(c *combinedCredits, movieGenres, showGenres map[int]string) (Movies, Shows) {
	byPopularity := func(a, b *personCredit) int {
		return cmp.Or(cmp.Compare(b.Popularity, a.Popularity), cmp.Compare(b.VoteCount, a.VoteCount))
	}
	cast, crew := slices.Clone(c.Cast), slices.Clone(c.Crew)
	slices.SortStableFunc(cast, byPopularity)
	slices.SortStableFunc(crew, byPopularity)
	seen := map[string]bool{}
	movies, shows := Movies{}, Shows{}
	for _, c := range append(cast, crew...) {
		if c == nil || c.ID == 0 || c.IsAdult || isSelf(c.Character) || seen[fmt.Sprint(c.MediaType, c.ID)] {
			continue
		}
		seen[fmt.Sprint(c.MediaType, c.ID)] = true
		switch c.MediaType {
		case "movie":
			movies = append(movies, &Movie{Entity: *c.Entity.lite(movieGenres)})
		case "tv":
			if len(c.GenreIDs) > 0 && !isTalkOrNews(c.GenreIDs) {
				shows = append(shows, &Show{Entity: *c.Entity.lite(showGenres)})
			}
		}
	}
	return movies, shows
}
