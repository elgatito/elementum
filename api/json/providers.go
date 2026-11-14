package jsonapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/providers/burst"
)

// The providers of the built-in search (providers/burst) and their settings file.

var noProfile = failed(http.StatusInternalServerError, ErrorCodeInternalError, "no profile folder to keep provider settings in")

func toProvider(st burst.Status, set burst.Settings) Provider {
	p := Provider{Id: st.ID, Name: st.Name, Private: st.Private, Languages: st.Languages, Enabled: st.Enabled,
		Searched: st.Enabled && st.Unusable == "" && !st.NeedsLogin, HasSecret: st.HasSecret,
		Username: optional(st.Username), Alias: optional(set.Alias)}
	if p.Languages == nil {
		p.Languages = []string{}
	}
	switch {
	case !st.Enabled:
	case st.Unusable != "":
		p.Reason = &st.Unusable
	case st.NeedsLogin:
		p.Reason = ptr("no username and password, or passkey")
	}
	return p
}

func (Server) ListProviders(c *gin.Context) {
	dir := providers.ProvidersDir()
	settings := burst.LoadSettings(dir)
	out := []Provider{}
	for _, st := range burst.List(dir) {
		out = append(out, toProvider(st, settings[st.ID]))
	}
	c.JSON(http.StatusOK, out)
}

func (Server) UpdateProvider(c *gin.Context, id string) {
	b := body[ProviderSettings](c)
	if b == nil {
		return
	}
	dir := providers.ProvidersDir()
	if dir == "" {
		fail(c, noProfile)
		return
	}
	if _, known := burst.Load(dir)[id]; !known {
		fail(c, failed(http.StatusNotFound, ErrorCodeNotFound, "unknown provider"))
		return
	}
	set := burst.LoadSettings(dir)[id]
	if b.Enabled != nil {
		set.Enabled = b.Enabled
	}
	for _, f := range []struct {
		to   *string
		from *string
	}{{&set.Username, b.Username}, {&set.Password, b.Password}, {&set.Passkey, b.Passkey}, {&set.Alias, b.Alias}} {
		if f.from != nil {
			*f.to = *f.from
		}
	}
	if err := burst.SaveSettings(dir, id, set); err != nil {
		log.Errorf("Saving provider settings: %v", err)
		fail(c, failed(http.StatusInternalServerError, ErrorCodeInternalError, "provider settings could not be saved"))
		return
	}
	for _, st := range burst.List(dir) {
		if st.ID == id {
			c.JSON(http.StatusOK, toProvider(st, set))
			return
		}
	}
}
