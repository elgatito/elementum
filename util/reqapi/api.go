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

	// SimklAPI allows 10 GET/s and 1 POST/s and wants sequential requests, so stay at 1 request per second.
	SimklAPI = &API{
		Ident:       SimklIdent,
		Endpoint:    "https://api.simkl.com",
		RetriesLeft: 3,
		RateLimiter: util.NewRateLimiter(1, 1*time.Second, 1),
	}

	// MDBListAPI allows 1000 reads and 300 writes per 5 minutes; the daily quota is the real limit.
	MDBListAPI = &API{
		Ident:       MDBListIdent,
		Endpoint:    "https://api.mdblist.com",
		RetriesLeft: 3,
		RateLimiter: util.NewRateLimiter(60, 1*time.Minute, 2),
	}

	// ScrobAPI is a self-hosted server: requests carry the full URL from settings, so there is no endpoint.
	ScrobAPI = &API{
		Ident:       ScrobIdent,
		RetriesLeft: 3,
		RateLimiter: util.NewRateLimiter(10, 1*time.Second, 2),
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
	case SimklIdent:
		return SimklAPI
	case MDBListIdent:
		return MDBListAPI
	case ScrobIdent:
		return ScrobAPI
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
