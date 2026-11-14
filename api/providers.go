package api

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/anacrolix/missinggo/perf"
	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/providers/burst"
	"github.com/elgatito/elementum/xbmc"
)

const providerPrefix = "plugin://plugin.video.elementum/provider/"

// Addon ...
type Addon struct {
	ID      string
	Name    string
	Version string
	Enabled bool
	Status  int
}

func getProviders(xbmcHost *xbmc.XBMCHost) []Addon {
	defer perf.ScopeTimer()()

	list := make([]Addon, 0)
	for _, addon := range xbmcHost.GetAddons("xbmc.python.script", "executable", "all", []string{"name", "version", "enabled"}).Addons {
		if strings.HasPrefix(addon.ID, "script.elementum.") {
			list = append(list, Addon{
				ID:      addon.ID,
				Name:    addon.Name,
				Version: addon.Version,
				Enabled: addon.Enabled,
				Status:  xbmcHost.AddonCheck(addon.ID),
			})
		}
	}
	// Enabled first, then by status
	slices.SortFunc(list, func(a, b Addon) int {
		if a.Enabled != b.Enabled {
			if a.Enabled {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Status, b.Status)
	})
	return list
}

// ProviderList ...
func ProviderList(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	providers := getProviders(xbmcHost)

	items := make(xbmc.ListItems, 0, len(providers))
	for _, provider := range providers {
		status := "[COLOR FF009900]OK[/COLOR]"
		if provider.Status > 0 {
			status = "[COLOR FF999900]FAILED[/COLOR]"
		}

		enabled := "[COLOR FF009900]Enabled[/COLOR]"
		if !provider.Enabled {
			enabled = "[COLOR FF990000]Disabled[/COLOR]"
		}

		item := &xbmc.ListItem{
			Label:      fmt.Sprintf("%s - %s - %s %s", status, enabled, provider.Name, provider.Version),
			Path:       URLForXBMC("/provider/%s/settings", provider.ID),
			IsPlayable: false,
		}
		item.ContextMenu = [][]string{
			{"LOCALIZE[30242]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/provider/%s/check", provider.ID))},
		}
		if provider.Enabled {
			item.ContextMenu = append(item.ContextMenu,
				[]string{"LOCALIZE[30241]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/provider/%s/disable", provider.ID))},
				[]string{"LOCALIZE[30244]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/provider/%s/settings", provider.ID))},
			)
		} else {
			item.ContextMenu = append(item.ContextMenu,
				[]string{"LOCALIZE[30240]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/provider/%s/enable", provider.ID))},
			)
		}
		item.ContextMenu = append(item.ContextMenu,
			[]string{"LOCALIZE[30274]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/providers/enable"))},
			[]string{"LOCALIZE[30275]", fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/providers/disable"))},
		)
		items = append(items, item)
	}

	items = append(items, builtinProviderItems()...)

	ctx.JSON(200, xbmc.NewView("", items))
}

// builtinProviderItems lists the trackers of Elementum's own search (providers/burst), the ones in use
// first. Clicking one switches it on or off; its login and mirror are in the context menu.
func builtinProviderItems() xbmc.ListItems {
	list := burst.List(providers.ProvidersDir())
	slices.SortStableFunc(list, func(a, b burst.Status) int {
		if a.Enabled != b.Enabled {
			if a.Enabled {
				return -1
			}
			return 1
		}
		return 0
	})
	items := make(xbmc.ListItems, 0, len(list))
	for _, st := range list {
		state := "[COLOR FF990000]Disabled[/COLOR]"
		switch {
		case st.Enabled && st.Unusable != "":
			state = "[COLOR FF999900]Not supported[/COLOR]"
		case st.Enabled && st.NeedsLogin:
			state = "[COLOR FF999900]Needs login[/COLOR]"
		case st.Enabled:
			state = "[COLOR FF009900]Enabled[/COLOR]"
		}
		label := fmt.Sprintf("Built-in - %s - %s", state, st.Name)
		if st.Private {
			label += " (private)"
		}
		action := func(field string) string {
			return fmt.Sprintf("RunPlugin(%s)", URLForXBMC("/providers/builtin/%s/%s", st.ID, field))
		}
		item := &xbmc.ListItem{Label: label, Path: URLForXBMC("/providers/builtin/%s/toggle", st.ID), IsPlayable: false}
		if st.Private {
			item.ContextMenu = [][]string{{"Username", action("username")}, {"Password", action("password")}, {"Passkey", action("passkey")}}
		}
		item.ContextMenu = append(item.ContextMenu, []string{"Mirror (other domain or URL)", action("alias")})
		items = append(items, item)
	}
	return items
}

// BuiltinProviderSet changes one setting of a built-in provider: "toggle" flips it on or off, the other
// fields are asked for with Kodi's keyboard (an empty answer leaves the value as it is).
func BuiltinProviderSet(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	dir, id, field := providers.ProvidersDir(), ctx.Params.ByName("id"), ctx.Params.ByName("field")
	if xbmcHost == nil || dir == "" {
		return
	}
	var current *burst.Status
	for _, st := range burst.List(dir) {
		if st.ID == id {
			current = &st
		}
	}
	if current == nil {
		ctx.String(404, "unknown provider")
		return
	}
	set := burst.LoadSettings(dir)[id]
	ask := func(value string, hidden bool) string {
		if answer := xbmcHost.Keyboard(value, current.Name+": "+field, hidden); answer != "" {
			return answer
		}
		return value
	}
	switch field {
	case "toggle":
		on := !current.Enabled
		set.Enabled = &on
	case "username":
		set.Username = ask(set.Username, false)
	case "password":
		set.Password = ask("", true)
	case "passkey":
		set.Passkey = ask("", true)
	case "alias":
		set.Alias = ask(set.Alias, false)
	default:
		ctx.String(404, "unknown setting")
		return
	}
	if err := burst.SaveSettings(dir, id, set); err != nil {
		log.Errorf("Saving provider settings: %s", err)
		xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
	}
	if xbmcHost.InfoLabel("Container.FolderPath") == providerPrefix {
		xbmcHost.Refresh()
	}
	ctx.String(200, "")
}

// ProviderSettings ...
func ProviderSettings(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	addonID := ctx.Params.ByName("provider")
	xbmcHost.AddonSettings(addonID)
	ctx.String(200, "")
}

// ProviderCheck ...
func ProviderCheck(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	addonID := ctx.Params.ByName("provider")
	failures := xbmcHost.AddonCheck(addonID)
	translated := xbmcHost.GetLocalizedString(30243)
	xbmcHost.Notify("Elementum", fmt.Sprintf("%s: %d", translated, failures), config.AddonIcon())
	ctx.String(200, "")
}

// ProviderFailure ...
func ProviderFailure(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	addonID := ctx.Params.ByName("provider")
	xbmcHost.AddonFailure(addonID)
	ctx.String(200, "")
}

// ProviderEnable ...
func ProviderEnable(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	addonID := ctx.Params.ByName("provider")
	xbmcHost.SetAddonEnabled(addonID, true)
	path := xbmcHost.InfoLabel("Container.FolderPath")
	if path == providerPrefix {
		xbmcHost.Refresh()
	}
	ctx.String(200, "")
}

// ProviderDisable ...
func ProviderDisable(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	addonID := ctx.Params.ByName("provider")
	xbmcHost.SetAddonEnabled(addonID, false)
	path := xbmcHost.InfoLabel("Container.FolderPath")
	if path == providerPrefix {
		xbmcHost.Refresh()
	}
	ctx.String(200, "")
}

// ProvidersEnableAll ...
func ProvidersEnableAll(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	providers := getProviders(xbmcHost)

	for _, addon := range providers {
		xbmcHost.SetAddonEnabled(addon.ID, true)
	}
	path := xbmcHost.InfoLabel("Container.FolderPath")
	if path == providerPrefix {
		xbmcHost.Refresh()
	}
	ctx.String(200, "")
}

// ProvidersDisableAll ...
func ProvidersDisableAll(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	providers := getProviders(xbmcHost)

	for _, addon := range providers {
		xbmcHost.SetAddonEnabled(addon.ID, false)
	}
	path := xbmcHost.InfoLabel("Container.FolderPath")
	if path == providerPrefix {
		xbmcHost.Refresh()
	}
	ctx.String(200, "")
}
