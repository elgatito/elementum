package api

import (
	"fmt"
	"sort"
	"strings"

	"github.com/anacrolix/missinggo/perf"
	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/config"
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

// ByEnabled ...
type ByEnabled []Addon

func (a ByEnabled) Len() int           { return len(a) }
func (a ByEnabled) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByEnabled) Less(i, j int) bool { return a[i].Enabled }

// ByStatus ...
type ByStatus []Addon

func (a ByStatus) Len() int           { return len(a) }
func (a ByStatus) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByStatus) Less(i, j int) bool { return a[i].Status < a[j].Status }

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
	sort.Sort(ByStatus(list))
	sort.Sort(ByEnabled(list))
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

	ctx.JSON(200, xbmc.NewView("", items))
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
