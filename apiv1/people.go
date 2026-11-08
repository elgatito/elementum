package apiv1

import (
	"context"
	"errors"
	"net/http"

	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/util"
)

func personArgs(lang *string) (string, *Error) {
	_, _, l, e := args(nil, nil, lang)
	return l, e
}

func toPerson(p *tmdb.Person) Person {
	return Person{
		TmdbId: p.ID, Name: p.Name, Profile: imageURL(p.ProfilePath, profileSize), Biography: p.Biography,
		Birthday: date(p.Birthday), Deathday: date(p.Deathday), PlaceOfBirth: optional(p.PlaceOfBirth),
		KnownForDepartment: optional(p.KnownForDepartment),
	}
}

func (Server) GetPerson(_ context.Context, req GetPersonRequestObject) (GetPersonResponseObject, error) {
	lang, e := personArgs(req.Params.Lang)
	if e != nil {
		return GetPerson400JSONResponse{badRequest(e)}, nil
	}
	p := tmdb.GetPerson(req.TmdbId, lang)
	if p == nil {
		return GetPerson404JSONResponse{NotFoundJSONResponse(tmdbNotFound)}, nil
	}
	return GetPerson200JSONResponse(toPerson(p)), nil
}

func (Server) GetPersonCredits(_ context.Context, req GetPersonCreditsRequestObject) (GetPersonCreditsResponseObject, error) {
	lang, e := personArgs(req.Params.Lang)
	if e != nil {
		return GetPersonCredits400JSONResponse{badRequest(e)}, nil
	}
	movies, shows, err := tmdb.PersonCredits(req.TmdbId, lang)
	if errors.Is(err, util.ErrNotFound) {
		return GetPersonCredits404JSONResponse{NotFoundJSONResponse(tmdbNotFound)}, nil
	} else if err != nil {
		return GetPersonCreditsdefaultJSONResponse{Body: tmdbFailed, StatusCode: http.StatusBadGateway}, nil
	}
	return GetPersonCredits200JSONResponse{Movies: annotate(movieItems(movies)), Shows: annotate(showItems(shows))}, nil
}
