package apiv1

import (
	"strconv"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/elgatito/elementum/tmdb"
)

// The mapping reads TMDB's raw fields, which TMDB already localizes to the requested language,
// not the GetTitle()-style helpers that follow Elementum's global language.

const (
	posterSize   = "w342"
	backdropSize = "w1280"
	logoSize     = "original"
	profileSize  = "w185"
	stillSize    = "w300"
)

func imageURL(path string, size tmdb.ImageQualityIdentifier) *string {
	if path == "" {
		return nil
	}
	u := tmdb.ImageURL(path, size)
	return &u
}

func date(s string) *openapi_types.Date {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil
	}
	return &openapi_types.Date{Time: t}
}

func year(s string) *int {
	if d := date(s); d != nil {
		y := d.Year()
		return &y
	}
	return nil
}

func positive(n int) *int {
	if n <= 0 {
		return nil
	}
	return &n
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// overview falls back to the English translation when TMDB has no text in the requested language.
func overview(e *tmdb.Entity) string {
	if e.Overview != "" || e.Translations == nil {
		return e.Overview
	}
	for _, t := range e.Translations.Translations {
		if t != nil && t.Iso639_1 == "en" && t.Data != nil {
			return t.Data.Overview
		}
	}
	return ""
}

func mediaItem(t MediaType, e *tmdb.Entity, title, released string) MediaItem {
	genres := make([]string, 0, len(e.Genres))
	for _, g := range e.Genres {
		if g != nil {
			genres = append(genres, g.Name)
		}
	}
	return MediaItem{
		Type:     t,
		TmdbId:   e.ID,
		Title:    title,
		Year:     year(released),
		Overview: overview(e),
		Poster:   imageURL(e.PosterPath, posterSize),
		Backdrop: imageURL(e.BackdropPath, backdropSize),
		Rating:   e.VoteAverage,
		Genres:   genres,
	}
}

func movieItem(m *tmdb.Movie) MediaItem {
	return mediaItem(MediaTypeMovie, &m.Entity, m.Title, m.ReleaseDate)
}

func showItem(s *tmdb.Show) MediaItem {
	return mediaItem(MediaTypeShow, &s.Entity, s.Name, s.FirstAirDate)
}

// movieItems skips the nil entries that list fetchers leave for missing or failed items.
func movieItems(movies tmdb.Movies) []MediaItem {
	items := make([]MediaItem, 0, len(movies))
	for _, m := range movies {
		if m != nil {
			items = append(items, movieItem(m))
		}
	}
	return items
}

func showItems(shows tmdb.Shows) []MediaItem {
	items := make([]MediaItem, 0, len(shows))
	for _, s := range shows {
		if s != nil {
			items = append(items, showItem(s))
		}
	}
	return items
}

// TMDB serves at most 500 pages of 20, whatever total_results says.
const tmdbMaxResults = tmdb.TMDBMaxPages * tmdb.TMDBResultsPerPage

// pastLastPage reports whether page starts beyond what TMDB serves, i.e. (page-1)*limit >= tmdbMaxResults,
// written so a huge page can't overflow.
func pastLastPage(page, limit int) bool { return page > (tmdbMaxResults+limit-1)/limit }

// emptyPage answers a page past TMDB's last one without fetching; the total is unknown there.
func emptyPage(page, limit int) MediaItemPage {
	return MediaItemPage{Items: []MediaItem{}, Page: page, Limit: limit, TotalPages: (tmdbMaxResults + limit - 1) / limit}
}

func mediaPage(items []MediaItem, total, page, limit int) MediaItemPage {
	return MediaItemPage{
		Items:        items,
		Page:         page,
		Limit:        limit,
		TotalPages:   (min(total, tmdbMaxResults) + limit - 1) / limit,
		TotalResults: &total,
	}
}

func genres(gs []*tmdb.Genre) []Genre {
	out := make([]Genre, 0, len(gs))
	for _, g := range gs {
		if g != nil {
			out = append(out, Genre{Id: g.ID, Name: g.Name})
		}
	}
	return out
}

func images(list []*tmdb.Image, size tmdb.ImageQualityIdentifier) []Image {
	out := make([]Image, 0, len(list))
	for _, i := range list {
		if i != nil && i.FilePath != "" {
			out = append(out, Image{Url: *imageURL(i.FilePath, size), Width: i.Width, Height: i.Height, Language: optional(i.Iso639_1)})
		}
	}
	return out
}

func details(e *tmdb.Entity, imdbID string) DetailsCommon {
	d := DetailsCommon{
		Cast:   []CastMember{},
		Crew:   []CrewMember{},
		Images: Images{Posters: []Image{}, Backdrops: []Image{}, Logos: []Image{}},
		Videos: []Video{},
	}
	if e.Credits != nil {
		for _, c := range e.Credits.Cast {
			if c != nil {
				d.Cast = append(d.Cast, CastMember{TmdbId: c.ID, Name: c.Name, Character: c.Character, Profile: imageURL(c.ProfilePath, profileSize)})
			}
		}
		for _, c := range e.Credits.Crew {
			if c != nil {
				d.Crew = append(d.Crew, CrewMember{TmdbId: c.ID, Name: c.Name, Job: c.Job, Department: c.Department, Profile: imageURL(c.ProfilePath, profileSize)})
			}
		}
	}
	if e.Images != nil {
		d.Images = Images{
			Posters:   images(e.Images.Posters, posterSize),
			Backdrops: images(e.Images.Backdrops, backdropSize),
			Logos:     images(e.Images.Logos, logoSize),
		}
	}
	if e.Videos != nil {
		for _, v := range e.Videos.Videos {
			if v != nil {
				d.Videos = append(d.Videos, Video{Name: v.Name, Site: v.Site, Key: v.Key, Type: v.Type})
			}
		}
	}
	if e.ExternalIDs != nil {
		d.ExternalIds.TvdbId = tvdbID(e.ExternalIDs.TVDBID)
		if imdbID == "" {
			imdbID = e.ExternalIDs.IMDBId
		}
	}
	d.ExternalIds.ImdbId = optional(imdbID)
	return d
}

// tvdbID reads TMDB's untyped tvdb_id: a JSON number, or whatever the msgpack cache decoded it to.
func tvdbID(v any) *int {
	switch n := v.(type) {
	case float64:
		return positive(int(n))
	case int64:
		return positive(int(n))
	case uint64:
		return positive(int(n))
	case int:
		return positive(n)
	case string:
		i, _ := strconv.Atoi(n)
		return positive(i)
	}
	return nil
}

func movieDetails(m *tmdb.Movie) MovieDetails {
	i, d := movieItem(m), details(&m.Entity, m.IMDBId)
	return MovieDetails{
		Type: i.Type, TmdbId: i.TmdbId, Title: i.Title, Year: i.Year, Overview: i.Overview,
		Poster: i.Poster, Backdrop: i.Backdrop, Rating: i.Rating, Genres: i.Genres,
		OriginalTitle: m.OriginalTitle, Tagline: m.TagLine, Cast: d.Cast, Crew: d.Crew,
		Images: d.Images, Videos: d.Videos, ExternalIds: d.ExternalIds,
		Runtime:     positive(m.Runtime),
		ReleaseDate: date(m.ReleaseDate),
	}
}

func showDetails(s *tmdb.Show) ShowDetails {
	i, d := showItem(s), details(&s.Entity, "")
	var runtime *int
	if len(s.EpisodeRunTime) > 0 {
		runtime = positive(s.EpisodeRunTime[0])
	} else if s.LastEpisodeToAir != nil { // TMDB leaves episode_run_time empty for most shows now
		runtime = positive(s.LastEpisodeToAir.Runtime)
	}
	seasons := make([]SeasonSummary, 0, len(s.Seasons))
	for _, se := range s.Seasons {
		if se != nil {
			seasons = append(seasons, SeasonSummary{
				SeasonNumber: se.Season, Name: se.Name, EpisodeCount: se.EpisodeCount,
				AirDate: date(se.AirDate), Poster: imageURL(se.PosterPath, posterSize),
			})
		}
	}
	return ShowDetails{
		Type: i.Type, TmdbId: i.TmdbId, Title: i.Title, Year: i.Year, Overview: i.Overview,
		Poster: i.Poster, Backdrop: i.Backdrop, Rating: i.Rating, Genres: i.Genres,
		OriginalTitle: s.OriginalName, Tagline: s.TagLine, Cast: d.Cast, Crew: d.Crew,
		Images: d.Images, Videos: d.Videos, ExternalIds: d.ExternalIds,
		Status:           s.Status,
		FirstAirDate:     date(s.FirstAirDate),
		EpisodeRuntime:   runtime,
		NumberOfSeasons:  s.NumberOfSeasons,
		NumberOfEpisodes: s.NumberOfEpisodes,
		Seasons:          seasons,
	}
}

func season(s *tmdb.Season) Season {
	episodes := make([]Episode, 0, len(s.Episodes))
	for _, e := range s.Episodes {
		if e != nil {
			episodes = append(episodes, Episode{
				TmdbId: e.ID, SeasonNumber: e.SeasonNumber, EpisodeNumber: e.EpisodeNumber,
				Name: e.Name, Overview: e.Overview, AirDate: date(e.AirDate),
				Still: imageURL(e.StillPath, stillSize), Runtime: positive(e.Runtime), Rating: e.VoteAverage,
			})
		}
	}
	return Season{
		SeasonNumber: s.Season, Name: s.Name, Overview: s.Overview,
		AirDate: date(s.AirDate), Poster: imageURL(s.PosterPath, posterSize), Episodes: episodes,
	}
}
