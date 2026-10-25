package api

import (
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/xbmc"
)

// KodiHost is a connected Kodi instance
type KodiHost struct {
	Host  string `json:"host"`
	Name  string `json:"name"`
	Local bool   `json:"local"`
	// Kodi UI language, like "pl_pl", Web UI uses it by default
	Language string `json:"language"`
}

// KodiHosts lists connected Kodi instances, so Web UI can choose which one to use
func KodiHosts(ctx *gin.Context) {
	hosts := xbmc.GetXBMCHosts()
	ret := make([]KodiHost, len(hosts))

	var wg sync.WaitGroup
	for i, h := range hosts {
		ret[i] = KodiHost{Host: h.Host, Local: h == xbmc.XBMCLocalHost}
		// Unreachable Kodi delays the response by connection timeout, so ask all at once
		wg.Go(func() {
			ret[i].Name = h.InfoLabel("System.FriendlyName")
			// Language is set as an add-on id, like "resource.language.pl_pl"
			ret[i].Language = strings.TrimPrefix(h.SettingsGetSettingValue("locale.language"), "resource.language.")
		})
	}
	wg.Wait()

	ctx.JSON(200, ret)
}
