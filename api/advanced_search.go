package api

import (
	"cmp"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/gin-gonic/gin"
	"github.com/jmcvetta/napping"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/xbmc"
)

// Filters are kept in the URL query: title=dead&year=2000-2010&genre=28&genre=12&country=US&language=en&cast=287:Brad Pitt&sort=rating
var yearRangeRe = regexp.MustCompile(`^(\d{4})?\s*(-)?\s*(\d{4})?$`)

// advancedSearchSorts lists sort options with their labels, first one is the default
var advancedSearchSorts = []struct {
	key   string
	label int
}{
	{"popularity", 30210},
	{"rating", 30211},
	{"votes", 30212},
	{"newest", 30236},
	{"oldest", 30775},
	{"title", 30721},
}

// parseYearRange accepts "2010", "2000-2010", "2010-" and "-2010", a zero bound means open-ended
func parseYearRange(s string) (from, to int, ok bool) {
	m := yearRangeRe.FindStringSubmatch(strings.TrimSpace(s))
	// reject "", "-" and two years without a dash
	if m == nil || (m[1] == "" && m[3] == "") || (m[2] == "" && m[1] != "" && m[3] != "") {
		return 0, 0, false
	}
	from, _ = strconv.Atoi(m[1])
	to, _ = strconv.Atoi(m[3])
	if m[2] == "" {
		to = from
	}
	if from > 0 && to > 0 && to < from {
		from, to = to, from
	}
	return from, to, true
}

// entryID and entryName split "id:name" cast values
func entryID(entry string) string {
	id, _, _ := strings.Cut(entry, ":")
	return id
}

func entryName(entry string) string {
	_, name, _ := strings.Cut(entry, ":")
	return name
}

func entryIDs(entries []string) string {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, entryID(e))
	}
	return strings.Join(ids, ",")
}

func advancedSearchURL(mediaType, action string, filters url.Values) string {
	filters.Del("page")
	return URLForXBMC("%s", fmt.Sprintf("/%s/advanced%s?%s", mediaType, action, filters.Encode()))
}

func advancedSearchParams(mediaType string, filters url.Values) napping.Params {
	dateField, titleField := "primary_release_date", "title"
	if mediaType == "shows" {
		dateField, titleField = "first_air_date", "name"
	}

	p := napping.Params{"sort_by": "popularity.desc"}
	if from, to, ok := parseYearRange(filters.Get("year")); ok {
		if from > 0 {
			p[dateField+".gte"] = fmt.Sprintf("%d-01-01", from)
		}
		if to > 0 {
			p[dateField+".lte"] = fmt.Sprintf("%d-12-31", to)
		}
	}
	switch filters.Get("sort") {
	case "rating":
		p["sort_by"] = "vote_average.desc"
		// otherwise titles with a couple of 10/10 votes come first
		p["vote_count.gte"] = "100"
	case "votes":
		p["sort_by"] = "vote_count.desc"
	case "newest":
		p["sort_by"] = dateField + ".desc"
		// skip announced titles without a release yet
		if _, ok := p[dateField+".lte"]; !ok {
			p[dateField+".lte"] = time.Now().UTC().Format(time.DateOnly)
		}
	case "oldest":
		p["sort_by"] = dateField + ".asc"
	case "title":
		p["sort_by"] = titleField + ".asc"
	}
	// Comma means AND, pipe means OR in TMDB discover.
	if v := filters["genre"]; len(v) > 0 {
		p["with_genres"] = strings.Join(v, ",")
	}
	if v := filters["country"]; len(v) > 0 {
		p["with_origin_country"] = strings.Join(v, "|")
	}
	if v := filters["language"]; len(v) > 0 {
		p["with_original_language"] = strings.Join(v, "|")
	}
	if v := filters["cast"]; len(v) > 0 && mediaType == "movies" {
		p["with_cast"] = entryIDs(v)
	}
	return p
}

// titleSearchMaxPages caps how many TMDB search pages are filtered locally.
// shortcut: matches past the first 100 search results are never shown, raise if broad titles miss results.
const titleSearchMaxPages = 5

// titleSearchItem holds what discover params are checked against for one search result
type titleSearchItem struct {
	entity     *tmdb.Entity
	date       string
	title      string
	countries  []string
	popularity float64
	index      int
}

// movieSearchItem uses production countries, as Movie has no origin_country
func movieSearchItem(m *tmdb.Movie) titleSearchItem {
	countries := make([]string, 0, len(m.ProductionCountries))
	for _, c := range m.ProductionCountries {
		countries = append(countries, c.Iso31661)
	}
	return titleSearchItem{entity: &m.Entity, date: m.ReleaseDate, title: m.Title, countries: countries, popularity: m.Popularity}
}

func showSearchItem(s *tmdb.Show) titleSearchItem {
	return titleSearchItem{entity: &s.Entity, date: s.FirstAirDate, title: s.Name, countries: s.OriginCountry, popularity: s.Popularity}
}

func containsAll(have, want []string) bool {
	for _, w := range want {
		if !slices.Contains(have, w) {
			return false
		}
	}
	return true
}

// matches checks discover params from advancedSearchParams against a search result
func (it titleSearchItem) matches(p napping.Params) bool {
	e := it.entity
	for k, v := range p {
		ok := true
		switch {
		case strings.HasSuffix(k, "_date.gte"):
			ok = it.date != "" && it.date >= v
		case strings.HasSuffix(k, "_date.lte"):
			ok = it.date != "" && it.date <= v
		case k == "vote_count.gte":
			n, _ := strconv.Atoi(v)
			ok = e.VoteCount >= n
		case k == "with_genres":
			genres := make([]string, 0, len(e.Genres))
			for _, g := range e.Genres {
				genres = append(genres, strconv.Itoa(g.ID))
			}
			ok = containsAll(genres, strings.Split(v, ","))
		case k == "with_cast":
			cast := []string{}
			if e.Credits != nil {
				for _, c := range e.Credits.Cast {
					cast = append(cast, strconv.Itoa(c.ID))
				}
			}
			ok = containsAll(cast, strings.Split(v, ","))
		case k == "with_origin_country":
			ok = slices.ContainsFunc(strings.Split(v, "|"), func(c string) bool { return slices.Contains(it.countries, c) })
		case k == "with_original_language":
			ok = slices.Contains(strings.Split(v, "|"), e.OriginalLanguage)
		}
		if !ok {
			return false
		}
	}
	return true
}

func sortTitleSearch(items []titleSearchItem, sortBy string) {
	field, dir, _ := strings.Cut(sortBy, ".")
	slices.SortStableFunc(items, func(a, b titleSearchItem) int {
		var c int
		switch field {
		case "popularity":
			c = cmp.Compare(a.popularity, b.popularity)
		case "vote_average":
			c = cmp.Compare(a.entity.VoteAverage, b.entity.VoteAverage)
		case "vote_count":
			c = cmp.Compare(a.entity.VoteCount, b.entity.VoteCount)
		case "title", "name":
			c = cmp.Compare(strings.ToLower(a.title), strings.ToLower(b.title))
		default: // release or first air date
			c = cmp.Compare(a.date, b.date)
		}
		if dir == "desc" {
			return -c
		}
		return c
	})
}

// titleSearch runs TMDB text search and applies discover params locally, as search endpoints only take a query
func titleSearch[T any](search func(page int) ([]*T, int), describe func(*T) titleSearchItem, p napping.Params, page, perPage int) ([]*T, int) {
	var all []*T
	for pg := 1; pg <= titleSearchMaxPages; pg++ {
		results, total := search(pg)
		all = append(all, results...)
		if len(results) == 0 || pg*tmdb.TMDBResultsPerPage >= total {
			break
		}
	}

	items := make([]titleSearchItem, 0, len(all))
	for i, r := range all {
		if r == nil {
			continue
		}
		it := describe(r)
		it.index = i
		if it.matches(p) {
			items = append(items, it)
		}
	}
	sortTitleSearch(items, p["sort_by"])

	start := min(max(page-1, 0)*perPage, len(items))
	end := min(start+perPage, len(items))
	ret := make([]*T, 0, end-start)
	for _, it := range items[start:end] {
		ret = append(ret, all[it.index])
	}
	return ret, len(items)
}

// advancedSearchOptions returns ids and labels for a multi-value filter
func advancedSearchOptions(mediaType, field string) (ids, names []string) {
	language := config.Get().Language
	switch field {
	case "genre":
		genres := tmdb.GetMovieGenres(language)
		if mediaType == "shows" {
			genres = tmdb.GetTVGenres(language)
		}
		for _, g := range genres {
			ids = append(ids, strconv.Itoa(g.ID))
			names = append(names, g.Name)
		}
	case "country":
		for _, c := range tmdb.GetCountries(language) {
			ids = append(ids, c.Iso31661)
			names = append(names, c.EnglishName)
		}
	case "language":
		for _, l := range tmdb.GetLanguages(language) {
			ids = append(ids, l.Iso639_1)
			names = append(names, l.Name)
		}
	}
	return
}

// AdvancedSearch lists filters for TMDB discover, with current values in labels
func AdvancedSearch(mediaType string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		filters := ctx.Request.URL.Query()
		filters.Del("page")

		label := func(title string, values []string) string {
			if len(values) == 0 {
				return title + ": LOCALIZE[30766]"
			}
			return title + ": " + strings.Join(values, ", ")
		}
		selected := func(field string) []string {
			ids, names := advancedSearchOptions(mediaType, field)
			ret := []string{}
			for _, v := range filters[field] {
				if i := slices.Index(ids, v); i >= 0 {
					ret = append(ret, names[i])
				}
			}
			return ret
		}
		// Edits run as actions, a folder would load and fail before the listing is updated
		edit := func(text, field string) *xbmc.ListItem {
			return &xbmc.ListItem{Label: text, Path: advancedSearchURL(mediaType, "/edit/"+field, filters), IsAction: true}
		}

		single := func(field string) []string {
			if v := filters.Get(field); v != "" {
				return []string{v}
			}
			return nil
		}
		items := xbmc.ListItems{
			edit(label("LOCALIZE[30772]", single("title")), "title"),
			edit(label("LOCALIZE[30767]", single("year")), "year"),
			edit(label("LOCALIZE[30289]", selected("genre")), "genre"),
			edit(label("LOCALIZE[30374]", selected("country")), "country"),
			edit(label("LOCALIZE[30373]", selected("language")), "language"),
		}
		// TMDB discover/tv has no cast filter
		if mediaType == "movies" {
			cast := []string{}
			for _, c := range filters["cast"] {
				cast = append(cast, entryName(c))
			}
			items = append(items, edit(label("LOCALIZE[30768]", cast), "cast"))
		}
		sortLabel := advancedSearchSorts[0].label
		for _, s := range advancedSearchSorts {
			if s.key == filters.Get("sort") {
				sortLabel = s.label
			}
		}
		items = append(items, edit(label("LOCALIZE[30774]", []string{fmt.Sprintf("LOCALIZE[%d]", sortLabel)}), "sort"))
		items = append(items, &xbmc.ListItem{
			Label:     "LOCALIZE[30209]",
			Path:      advancedSearchURL(mediaType, "/results", filters),
			Thumbnail: config.AddonResource("img", "search.png"),
		})
		if len(filters) > 0 {
			items = append(items, edit("LOCALIZE[30770]", "reset"))
		}

		ctx.JSON(200, xbmc.NewView("", items))
	}
}

// AdvancedSearchEdit asks for a new filter value and replaces the filters listing with the updated one
func AdvancedSearchEdit(mediaType string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
		if xbmcHost == nil {
			return
		}

		filters := ctx.Request.URL.Query()
		switch field := ctx.Params.ByName("field"); field {
		case "title":
			// As with year, cancel and cleared input both drop the filter.
			if title := strings.TrimSpace(xbmcHost.Keyboard(filters.Get("title"), "LOCALIZE[30772]")); title != "" {
				filters.Set("title", title)
			} else {
				filters.Del("title")
			}
		case "year":
			// Keyboard returns "" both on cancel and on cleared input, either way we drop the filter.
			year := strings.TrimSpace(xbmcHost.Keyboard(filters.Get("year"), "LOCALIZE[30767] (2010, 2000-2010, 2010-, -2010)"))
			if year == "" {
				filters.Del("year")
			} else if _, _, ok := parseYearRange(year); ok {
				filters.Set("year", year)
			} else {
				xbmcHost.Notify("Elementum", "LOCALIZE[30771]", config.AddonIcon())
				return
			}
		case "genre", "country", "language":
			ids, names := advancedSearchOptions(mediaType, field)
			preselect := []int{}
			for _, v := range filters[field] {
				if i := slices.Index(ids, v); i >= 0 {
					preselect = append(preselect, i)
				}
			}
			titles := map[string]string{"genre": "LOCALIZE[30289]", "country": "LOCALIZE[30374]", "language": "LOCALIZE[30373]"}
			chosen := xbmcHost.DialogMultiSelect(titles[field], names, preselect)
			if chosen == nil {
				return
			}
			filters.Del(field)
			for _, i := range chosen {
				if i >= 0 && i < len(ids) {
					filters.Add(field, ids[i])
				}
			}
		case "cast":
			cast := filters["cast"]
			options := []string{xbmcHost.GetLocalizedString(30769)}
			for _, c := range cast {
				options = append(options, "- "+entryName(c))
			}
			choice := xbmcHost.ListDialog("LOCALIZE[30768]", options...)
			if choice < 0 {
				return
			} else if choice > 0 {
				cast = slices.Delete(cast, choice-1, choice)
			} else {
				name := xbmcHost.Keyboard("", "LOCALIZE[30769]")
				if name == "" {
					return
				}
				people := tmdb.SearchPeople(name, config.Get().Language)
				if len(people) == 0 {
					xbmcHost.Notify("Elementum", "LOCALIZE[30754]", config.AddonIcon())
					return
				}
				names := make([]string, 0, len(people))
				for _, p := range people {
					names = append(names, p.Name)
				}
				pick := xbmcHost.ListDialog("LOCALIZE[30769]", names...)
				if pick < 0 {
					return
				}
				cast = append(cast, fmt.Sprintf("%d:%s", people[pick].ID, people[pick].Name))
			}
			filters.Del("cast")
			for _, c := range cast {
				filters.Add("cast", c)
			}
		case "sort":
			options := make([]string, 0, len(advancedSearchSorts))
			preselect := 0
			for i, s := range advancedSearchSorts {
				options = append(options, xbmcHost.GetLocalizedString(s.label))
				if s.key == filters.Get("sort") {
					preselect = i
				}
			}
			choice := xbmcHost.ListDialogWithOptions(0, preselect, "LOCALIZE[30774]", options...)
			if choice < 0 || choice >= len(advancedSearchSorts) {
				return
			} else if choice == 0 {
				filters.Del("sort")
			} else {
				filters.Set("sort", advancedSearchSorts[choice].key)
			}
		case "reset":
			filters = url.Values{}
		default:
			return
		}

		// replace keeps Back from walking through every intermediate filter state
		updateURL := advancedSearchURL(mediaType, "", filters)
		log.Debugf("Advanced search filters updated: %s", updateURL)
		go xbmcHost.UpdatePath(updateURL + ",replace")
		ctx.String(200, "")
	}
}

// AdvancedSearchResults runs TMDB discover, or text search when title is set, with filters from the URL query
func AdvancedSearchResults(mediaType string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		defer perf.ScopeTimer()()

		page, _ := strconv.Atoi(ctx.DefaultQuery("page", "1"))
		filters := ctx.Request.URL.Query()
		params := advancedSearchParams(mediaType, filters)
		title := filters.Get("title")
		language, perPage := config.Get().Language, config.Get().ResultsPerPage
		if mediaType == "movies" {
			var movies tmdb.Movies
			var total int
			if title != "" {
				movies, total = titleSearch(func(pg int) ([]*tmdb.Movie, int) { return tmdb.SearchMovies(title, language, pg) }, movieSearchItem, params, page, perPage)
			} else {
				movies, total = tmdb.DiscoverMovies(params, language, page)
			}
			renderMovies(ctx, movies, page, total, false)
		} else {
			var shows tmdb.Shows
			var total int
			if title != "" {
				shows, total = titleSearch(func(pg int) ([]*tmdb.Show, int) { return tmdb.SearchShows(title, language, pg) }, showSearchItem, params, page, perPage)
			} else {
				shows, total = tmdb.DiscoverShows(params, language, page)
			}
			renderShows(ctx, shows, page, total, false)
		}
	}
}
