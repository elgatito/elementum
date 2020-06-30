package bittorrent

import (
	"fmt"
	"net/url"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/util"
)

// URLForHTTP ...
func URLForHTTP(pattern string, args ...interface{}) string {
	u, _ := url.Parse(fmt.Sprintf(pattern, args...))
	return util.GetHTTPHost() + u.String()
}

// URLForXBMC ...
func URLForXBMC(pattern string, args ...interface{}) string {
	u, _ := url.Parse(fmt.Sprintf(pattern, args...))
	return "plugin://" + config.Get().Info.ID + u.String()
}

// URLQuery ...
func URLQuery(route string, query ...string) string {
	v := url.Values{}
	for i := 0; i < len(query); i += 2 {
		v.Add(query[i], query[i+1])
	}
	return route + "?" + v.Encode()
}
