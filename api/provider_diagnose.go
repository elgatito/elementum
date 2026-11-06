package api

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/tmdb"
	"github.com/elgatito/elementum/xbmc"
)

const (
	colorOK   = "FF009900"
	colorWarn = "FF999900"
	colorFail = "FF990000"
	colorOff  = "FF777777"
)

// ProvidersDiagnose sends a test search to every search provider and shows what is wrong with each of them
func ProvidersDiagnose(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	// Dialogs can stay open longer than plugin waits for a response
	go diagnoseProviders(xbmcHost, callbackHost(ctx, xbmcHost))
	ctx.String(200, "")
}

func diagnoseProviders(xbmcHost *xbmc.XBMCHost, cbHost string) {
	// Movie, Episode, Season, Anime episode, Text query
	kinds := []int{30825, 30826, 30827, 30828, 30829}
	defaults := []string{"The Matrix", "Breaking Bad", "Breaking Bad", "Attack on Titan", "The Matrix 1999"}
	labels := make([]string, len(kinds))
	for i, id := range kinds {
		labels[i] = xbmcHost.GetLocalizedString(id)
	}
	kind := xbmcHost.ListDialog("LOCALIZE[30824]", labels...)
	if kind < 0 {
		return
	}
	query := strings.TrimSpace(xbmcHost.Keyboard(defaults[kind], "LOCALIZE[30772]"))
	if query == "" {
		return
	}

	method, subject, build := diagnoseSubject(xbmcHost, kind, query)
	if build == nil {
		xbmcHost.Notify("Elementum", "LOCALIZE[30831]", config.AddonIcon())
		return
	}

	addons := getProviders(xbmcHost)
	if len(addons) == 0 {
		xbmcHost.Notify("Elementum", "LOCALIZE[30204]", config.AddonIcon())
		return
	}

	results := make([]*providers.Diagnosis, len(addons))
	done := make(chan int, len(addons))
	running := 0
	for i, addon := range addons {
		if !addon.Enabled {
			continue
		}
		running++
		go func() {
			results[i] = providers.Diagnose(xbmcHost, cbHost, addon.ID, method, build)
			done <- i
		}()
	}

	if running > 0 {
		dialog := xbmcHost.NewDialogProgress("LOCALIZE[30824]", subject, "", "")
		if dialog == nil {
			return
		}
		finished := map[int]bool{}
		for len(finished) < running {
			select {
			case i := <-done:
				finished[i] = true
			case <-time.After(time.Second):
				if dialog.IsCanceled() {
					dialog.Close()
					return
				}
			}

			waiting := []string{}
			for i, addon := range addons {
				if addon.Enabled && !finished[i] {
					waiting = append(waiting, addon.Name)
				}
			}
			dialog.Update(len(finished)*100/running, subject, fmt.Sprintf("%d / %d", len(finished), running), strings.Join(waiting, ", "))
		}
		dialog.Close()
	}

	items := []string{diagnoseSettingsItem()}
	details := []string{diagnoseSettingsDetails()}
	for i, addon := range addons {
		item, detail := diagnoseReport(addon, results[i])
		items = append(items, item)
		details = append(details, detail)
	}

	for {
		choice := xbmcHost.ListDialogLarge("LOCALIZE[30824]", subject, items...)
		if choice < 0 || choice >= len(details) {
			return
		}
		xbmcHost.DialogTextModal("LOCALIZE[30824]", details[choice])
	}
}

// diagnoseSubject resolves what is searched for, build is nil if it was not found
func diagnoseSubject(xbmcHost *xbmc.XBMCHost, kind int, query string) (method, subject string, build func(*providers.AddonSearcher) providers.SearchObject) {
	language := config.Get().Language

	if kind == 0 {
		found, _ := tmdb.SearchMovies(query, language, 1)
		if len(found) == 0 {
			return
		}
		movie := tmdb.GetMovieByID(strconv.Itoa(found[0].ID), language)
		if movie == nil {
			return
		}
		return "search_movie", movie.GetSearchTitle(), func(as *providers.AddonSearcher) providers.SearchObject {
			return as.GetMovieSearchObject(movie)
		}
	}

	if kind == 4 {
		return "search", query, func(as *providers.AddonSearcher) providers.SearchObject {
			return as.GetQuerySearchObject(query)
		}
	}

	found, _ := tmdb.SearchShows(query, language, 1)
	if len(found) == 0 {
		return
	}
	show := tmdb.GetShow(found[0].ID, language)
	if show == nil {
		return
	}

	defaultNumber := "S01E01"
	if kind == 2 {
		defaultNumber = "S01"
	}
	seasonNumber, episodeNumber := 1, 1
	fmt.Sscanf(strings.ToUpper(xbmcHost.Keyboard(defaultNumber, "LOCALIZE[30830]")), "S%dE%d", &seasonNumber, &episodeNumber)

	season := tmdb.GetSeason(show.ID, seasonNumber, language, len(show.Seasons), true)
	if season == nil {
		return
	}
	if kind == 2 {
		return "search_season", fmt.Sprintf("%s S%02d", show.GetName(), seasonNumber), func(as *providers.AddonSearcher) providers.SearchObject {
			return as.GetSeasonSearchObject(show, season)
		}
	}

	if !season.HasEpisode(episodeNumber) {
		return
	}
	episode := season.GetEpisode(episodeNumber)
	return "search_episode", fmt.Sprintf("%s S%02dE%02d", show.GetName(), seasonNumber, episodeNumber), func(as *providers.AddonSearcher) providers.SearchObject {
		// GetEpisodeSearchObject changes episode number, and providers run in parallel
		e := *episode
		return as.GetEpisodeSearchObject(show, season, &e)
	}
}

func colored(color, text string) string {
	return fmt.Sprintf("[COLOR %s]%s[/COLOR]", color, text)
}

func diagnoseSettingsItem() string {
	if !config.Get().InternalProxyEnabled {
		return colored(colorFail, "Elementum internal proxy is disabled") + "\nProviders, that use internal proxy, can't reach their sites"
	}
	return colored(colorOK, "Elementum internal proxy is enabled") + "\n" + fmt.Sprintf("Providers timeout: %s", providers.SearchTimeout())
}

func diagnoseSettingsDetails() string {
	c := config.Get()
	lines := []string{
		fmt.Sprintf("Internal proxy enabled: %v", c.InternalProxyEnabled),
		fmt.Sprintf("Internal proxy logging: %v", c.InternalProxyLogging),
		fmt.Sprintf("Proxy for providers (proxy_url in payload): %q", c.ProxyURL),
		fmt.Sprintf("Providers timeout: %s", providers.SearchTimeout()),
		fmt.Sprintf("Language: %s", c.Language),
	}
	if !c.InternalProxyEnabled {
		lines = append(lines, "", colored(colorFail, "Enable internal proxy in Elementum settings, providers rely on it to bypass site protections."))
	}
	return strings.Join(lines, "\n")
}

func diagnoseReport(addon Addon, d *providers.Diagnosis) (item, details string) {
	title := fmt.Sprintf("%s %s", addon.Name, addon.Version)
	lines := []string{fmt.Sprintf("%s (%s)", title, addon.ID), fmt.Sprintf("Failures counted by Elementum: %d", addon.Status), ""}

	if d == nil {
		return colored(colorOff, "DISABLED") + " " + title + "\nNot tested, addon is disabled in Kodi",
			strings.Join(append(lines, "Addon is disabled in Kodi, enable it to use it for searching."), "\n")
	}

	failed := 0
	for _, r := range d.Requests {
		if r.Error != "" || r.Status == 0 || r.Status >= 400 {
			failed++
		}
	}
	proxyInfo := fmt.Sprintf("proxy requests: %d, failed: %d", len(d.Requests), failed)
	if len(d.Requests) == 0 {
		proxyInfo = "internal proxy not used"
	}

	status := colored(colorOK, "OK")
	summary := fmt.Sprintf("Results: %d, usable: %d, time: %.1fs, %s", len(d.Results), d.Usable, d.Elapsed.Seconds(), proxyInfo)
	switch {
	case !d.Responded:
		status = colored(colorFail, "NO RESPONSE")
		summary = fmt.Sprintf("No response in %s, %s", providers.SearchTimeout(), proxyInfo)
	case d.ParseError != nil:
		status = colored(colorFail, "INVALID RESPONSE")
	case len(d.Results) == 0:
		status = colored(colorWarn, "NO RESULTS")
	case d.Usable == 0:
		status = colored(colorFail, "NO USABLE RESULTS")
	case len(d.Problems) > 0 || failed > 0 || len(d.Requests) == 0:
		status = colored(colorWarn, "WARNING")
	}
	item = status + " " + title + "\n" + summary

	if d.Responded {
		lines = append(lines, fmt.Sprintf("Response received in %.1fs", d.Elapsed.Seconds()))
	} else {
		lines = append(lines, colored(colorFail, fmt.Sprintf("No response in %s: addon failed, is not configured, or is too slow. Check Kodi log for its errors.", providers.SearchTimeout())))
	}
	if d.ParseError != nil {
		lines = append(lines, colored(colorFail, "Response is not a valid list of results: "+d.ParseError.Error()))
	}
	if d.Responded && d.ParseError == nil {
		lines = append(lines, fmt.Sprintf("Results: %d, usable by Elementum: %d", len(d.Results), d.Usable))
	}
	for _, p := range d.Problems {
		lines = append(lines, colored(colorWarn, fmt.Sprintf(" - %s: %d", p.Text, p.Count)))
		if p.Example != "" {
			lines = append(lines, "   e.g. "+p.Example)
		}
	}

	lines = append(lines, "")
	if len(d.Requests) == 0 {
		lines = append(lines, colored(colorWarn, "No requests went through Elementum internal proxy: addon does not use it, or uses own proxy settings."))
	} else {
		lines = append(lines, fmt.Sprintf("Requests through Elementum internal proxy: %d, failed: %d", len(d.Requests), failed))
		auth := false
		for _, r := range d.Requests {
			switch {
			case r.Error != "":
				lines = append(lines, colored(colorFail, "ERR")+fmt.Sprintf(" %s %s: %s", r.Method, r.URL, r.Error))
			case r.Status == 0 || r.Status >= 400:
				auth = auth || r.Status == 401 || r.Status == 403
				lines = append(lines, colored(colorFail, strconv.Itoa(r.Status))+fmt.Sprintf(" %s %s", r.Method, r.URL))
			default:
				lines = append(lines, colored(colorOK, strconv.Itoa(r.Status))+fmt.Sprintf(" %s %s", r.Method, r.URL))
			}
		}
		if auth {
			lines = append(lines, colored(colorWarn, "Some sites answered 401/403: login is failing, or site protection blocks requests."))
		}
	}

	if len(d.Results) > 0 {
		lines = append(lines, "", "Results by site ('provider' field): total / usable")
		sites := []string{}
		total, usable := map[string]int{}, map[string]int{}
		for _, r := range d.Results {
			if _, ok := total[r.Provider]; !ok {
				sites = append(sites, r.Provider)
			}
			total[r.Provider]++
			if r.Problem == "" {
				usable[r.Provider]++
			}
		}
		for _, site := range sites {
			lines = append(lines, fmt.Sprintf(" - %s: %d / %d", site, total[site], usable[site]))
		}

		lines = append(lines, "", "Results: seeds / peers, size, site, name")
		for _, r := range d.Results {
			status := colored(colorOK, "OK")
			if r.Problem != "" {
				status = colored(colorWarn, r.Problem)
			}
			lines = append(lines, fmt.Sprintf("%s %d / %d, %s, %s, %s", status, r.Seeds, r.Peers, r.Size, r.Provider, r.Name))
			if r.Problem != "" {
				lines = append(lines, "   "+r.URI)
			}
		}
	}

	if payload, err := json.MarshalIndent(d.Payload, "", "  "); err == nil {
		lines = append(lines, "", "Search payload:", string(payload))
	}
	return item, strings.Join(lines, "\n")
}
