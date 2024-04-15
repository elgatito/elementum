package reqapi

import (
	"github.com/elgatito/elementum/cache"
	"github.com/elgatito/elementum/util"
)

type APIIdent string

const (
	TMDBIdent          APIIdent = cache.TMDBKey
	TraktIdent         APIIdent = cache.TraktKey
	FanArtIdent        APIIdent = cache.FanartKey
	OpenSubtitlesIdent APIIdent = cache.OpensubtitlesKey
)

type API struct {
	Ident       APIIdent
	RateLimiter *util.RateLimiter
	Endpoint    string
	RetriesLeft int
}
