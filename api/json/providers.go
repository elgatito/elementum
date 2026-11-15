package jsonapi

import (
	"cmp"
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/providers/burst"
	"github.com/elgatito/elementum/ui"
)

// The providers of the built-in search (providers/burst) and their settings file.

var noProfile = failed(http.StatusInternalServerError, ErrorCodeInternalError, "no profile folder to keep provider settings in")

func toProvider(st burst.Status, set burst.Settings) Provider {
	p := Provider{Id: st.ID, Name: st.Name, Private: st.Private, Languages: st.Languages, Enabled: st.Enabled,
		Searched: st.Enabled && st.Unusable == "" && !st.NeedsLogin, HasSecret: st.HasSecret,
		Username: optional(st.Username), Alias: optional(set.Alias), HasCookies: &st.HasCookies, UserAgent: optional(set.UserAgent),
		Contains: ptr(ProviderContains(cmp.Or(st.Contains, "all")))}
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
	if b.Contains != nil {
		if !b.Contains.Valid() {
			fail(c, bad("contains must be all, movies or shows"))
			return
		}
		if set.Contains = string(*b.Contains); set.Contains == "all" {
			set.Contains = ""
		}
	}
	for _, f := range []struct {
		to   *string
		from *string
	}{{&set.Username, b.Username}, {&set.Password, b.Password}, {&set.Passkey, b.Passkey}, {&set.Alias, b.Alias}, {&set.Cookies, b.Cookies}, {&set.UserAgent, b.UserAgent}} {
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

// toOption maps an option, with its texts in lang; a secret's value is only told as set ("********") or not ("").
func toOption(o burst.Option, general burst.General, lang string) ProviderOption {
	t := func(english string) string { return ui.Plain(ui.Text(lang, english)) }
	value := general.Get(o.ID)
	if o.Secret() && value != "" {
		value = "********"
	}
	out := ProviderOption{Id: o.ID, Group: t(o.Group), Label: t(o.Label), Value: value, Default: o.Default}
	if o.Prefix != "" {
		out.Label = t(o.Prefix) + ": " + out.Label
	}
	if len(o.Choices) > 0 {
		choices := make([]string, len(o.Choices))
		for i, choice := range o.Choices {
			choices[i] = t(choice)
		}
		out.Choices = &choices
	}
	return out
}

func (Server) ListProviderOptions(c *gin.Context, p ListProviderOptionsParams) {
	lang, f := langArg(p.Lang)
	if f != nil {
		fail(c, f)
		return
	}
	general := burst.LoadGeneral(providers.ProvidersDir())
	out := make([]ProviderOption, 0, len(burst.Options))
	for _, o := range burst.Options {
		out = append(out, toOption(o, general, lang))
	}
	c.JSON(http.StatusOK, out)
}

// StartCookieSyncLogin starts GitHub's device login and waits for its end in the background.
func (Server) StartCookieSyncLogin(c *gin.Context) {
	dir := providers.ProvidersDir()
	if dir == "" {
		fail(c, noProfile)
		return
	}
	code, err := burst.StartDeviceLogin(c.Request.Context())
	if err != nil {
		log.Warningf("GitHub device login: %v", err)
		fail(c, failed(http.StatusBadGateway, ErrorCodeInternalError, "GitHub could not be reached"))
		return
	}
	go func() {
		// Not the request's context: the user enters the code long after this request has ended.
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(code.ExpiresIn)*time.Second)
		defer cancel()
		if err := burst.FinishDeviceLogin(ctx, dir, code); err != nil {
			log.Warningf("GitHub device login: %v", err)
		} else {
			ui.Toast(ui.Text(config.Get().Language, "GitHub is connected"))
		}
	}()
	c.JSON(http.StatusOK, DeviceLoginCode{UserCode: code.UserCode, VerificationUrl: code.VerificationURL, ExpiresIn: code.ExpiresIn, Interval: code.Interval})
}

func (Server) UpdateProviderOption(c *gin.Context, id string) {
	b := body[UpdateProviderOptionJSONRequestBody](c)
	if b == nil {
		return
	}
	dir := providers.ProvidersDir()
	if dir == "" {
		fail(c, noProfile)
		return
	}
	// SaveOption refuses unknown ids and values of another type than the default's.
	if err := burst.SaveOption(dir, id, b.Value); err != nil {
		fail(c, bad(err.Error()))
		return
	}
	for _, o := range burst.Options {
		if o.ID == id {
			c.JSON(http.StatusOK, toOption(o, burst.LoadGeneral(dir), config.Get().Language))
		}
	}
}
