package jsonapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/util"
)

func toPerson(p *tmdb.Person) Person {
	return Person{
		TmdbId: p.ID, Name: p.Name, Profile: imageURL(p.ProfilePath, profileSize), Biography: p.Biography,
		Birthday: date(p.Birthday), Deathday: date(p.Deathday), PlaceOfBirth: optional(p.PlaceOfBirth),
		KnownForDepartment: optional(p.KnownForDepartment),
	}
}

func (Server) GetPerson(c *gin.Context, id TmdbIdParam, params GetPersonParams) {
	lang, ok := detailArgs(c, id, params.Lang)
	if !ok {
		return
	}
	p := tmdb.GetPerson(id, lang)
	if p == nil {
		fail(c, tmdbNotFound)
		return
	}
	c.JSON(http.StatusOK, toPerson(p))
}

func (Server) GetPersonCredits(c *gin.Context, id TmdbIdParam, params GetPersonCreditsParams) {
	lang, ok := detailArgs(c, id, params.Lang)
	if !ok {
		return
	}
	movies, shows, err := tmdb.PersonCredits(id, lang)
	if errors.Is(err, util.ErrNotFound) {
		fail(c, tmdbNotFound)
		return
	} else if err != nil {
		fail(c, tmdbFailed)
		return
	}
	c.JSON(http.StatusOK, PersonCredits{Movies: annotate(movieItems(movies)), Shows: annotate(showItems(shows))})
}
