package reqapi

import (
	"fmt"
	"strings"
	"time"

	"github.com/elgatito/elementum/proxy"
	"github.com/elgatito/elementum/util"

	"github.com/jmcvetta/napping"
	"github.com/op/go-logging"
)

var (
	locker = util.NewLocker()
)

var (
	TMDBAPI = &API{
		Ident:       TMDBIdent,
		Endpoint:    "https://api.themoviedb.org/3",
		RetriesLeft: 3,
		RateLimiter: util.NewRateLimiter(50, 1*time.Second, 50),
	}

	TraktAPI = &API{
		Ident:       TraktIdent,
		Endpoint:    "https://api.trakt.tv",
		RetriesLeft: 3,
		RateLimiter: util.NewRateLimiter(100, 10*time.Second, 25),
	}

	FanartAPI = &API{
		Ident:       FanArtIdent,
		Endpoint:    "http://webservice.fanart.tv/v3",
		RetriesLeft: 3,
		RateLimiter: util.NewRateLimiter(100, 10*time.Second, 25),
	}

	OpenSubtitlesAPI = &API{
		Ident:       OpenSubtitlesIdent,
		Endpoint:    "https://api.opensubtitles.com/api/v1",
		RetriesLeft: 3,
		RateLimiter: util.NewRateLimiter(100, 10*time.Second, 25),
	}
)

var log = logging.MustGetLogger("reqapi")

func GetAPI(ident APIIdent) *API {
	switch ident {
	case TMDBIdent:
		return TMDBAPI
	case TraktIdent:
		return TraktAPI
	case FanArtIdent:
		return FanartAPI
	default:
		return nil
	}
}

func (api *API) GetURL(url string) string {
	if strings.HasPrefix(url, "http") {
		return url
	} else if strings.HasPrefix(url, "/") && strings.HasSuffix(api.Endpoint, "/") {
		return fmt.Sprintf("%s%s", api.Endpoint, url[1:])
	} else if strings.HasPrefix(url, "/") || strings.HasSuffix(api.Endpoint, "/") {
		return fmt.Sprintf("%s%s", api.Endpoint, url)
	} else {
		return fmt.Sprintf("%s/%s", api.Endpoint, url)
	}
}

func (api *API) GetSession() *napping.Session {
	return &napping.Session{
		Client: proxy.GetClient(),
	}
}
