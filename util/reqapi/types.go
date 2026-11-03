package reqapi

import (
	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/util"
)

type APIIdent string

const (
	TMDBIdent          APIIdent = cache.TMDBKey
	TraktIdent         APIIdent = cache.TraktKey
	SimklIdent         APIIdent = cache.SimklKey
	MDBListIdent       APIIdent = cache.MDBListKey
	ScrobIdent         APIIdent = cache.ScrobKey
	FanArtIdent        APIIdent = cache.FanartKey
	OpenSubtitlesIdent APIIdent = cache.OpensubtitlesKey
)

type API struct {
	Ident       APIIdent
	RateLimiter *util.RateLimiter
	Endpoint    string
	RetriesLeft int
}
