package api

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/providers/burst"
	"github.com/elgatito/elementum/xbmc"
)

// BuiltinSettings is the Kodi dialog for Elementum's own torrent search (providers/burst): which
// providers are on, their logins and mirrors, and the filters. It is what the Burst add-on's settings
// window was. The same settings are behind /api/providers for other clients.
// shortcut: the dialog texts are English only, until they get ids in the add-on's strings.
func BuiltinSettings(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	dir := providers.ProvidersDir()
	if xbmcHost == nil || dir == "" {
		return
	}
	const title = "Elementum: built-in search"
	failed := func(err error) {
		if err != nil {
			log.Errorf("Saving built-in search settings: %s", err)
			xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
		}
	}
	groups := []string{}
	for _, o := range burst.Options {
		if len(groups) == 0 || groups[len(groups)-1] != o.Group {
			groups = append(groups, o.Group)
		}
	}
	for {
		choice := xbmcHost.ListDialog(title, append([]string{"Providers", "Logins and mirrors"}, groups...)...)
		switch {
		case choice < 0:
			ctx.String(200, "")
			return
		case choice == 0:
			failed(chooseProviders(xbmcHost, dir, title))
		case choice == 1:
			failed(editLogins(xbmcHost, dir, title))
		default:
			failed(editOptions(xbmcHost, dir, title, groups[choice-2]))
		}
	}
}

// chooseProviders switches providers on and off in one multi-select list.
func chooseProviders(h *xbmc.XBMCHost, dir, title string) error {
	var list []burst.Status
	var labels []string
	var on []int
	for _, st := range burst.List(dir) {
		if st.Unusable != "" {
			continue
		}
		label := st.Name
		if st.Private {
			label += " (private)"
			if st.NeedsLogin {
				label += " - no login"
			}
		}
		if st.Enabled {
			on = append(on, len(list))
		}
		list, labels = append(list, st), append(labels, label)
	}
	chosen := h.DialogMultiSelect(title+": providers", labels, on)
	if chosen == nil { // cancelled
		return nil
	}
	want := map[int]bool{}
	for _, i := range chosen {
		want[i] = true
	}
	settings := burst.LoadSettings(dir)
	for i, st := range list {
		if st.Enabled == want[i] {
			continue
		}
		set, enabled := settings[st.ID], want[i]
		set.Enabled = &enabled
		if err := burst.SaveSettings(dir, st.ID, set); err != nil {
			return err
		}
	}
	return nil
}

// editLogins sets the username, password, passkey and mirror of one provider at a time.
func editLogins(h *xbmc.XBMCHost, dir, title string) error {
	for {
		var list []burst.Status
		var labels []string
		for _, st := range burst.List(dir) {
			if st.Unusable != "" || !(st.Enabled || st.Private) {
				continue
			}
			label := st.Name
			if st.Username != "" {
				label += " - " + st.Username
			}
			list, labels = append(list, st), append(labels, label)
		}
		i := h.ListDialog(title+": logins and mirrors", labels...)
		if i < 0 {
			return nil
		}
		set := burst.LoadSettings(dir)[list[i].ID]
		mark := func(set bool) string {
			if set {
				return "(set)"
			}
			return ""
		}
		fields := []string{"Username: " + set.Username, "Password " + mark(set.Password != ""), "Passkey " + mark(set.Passkey != ""), "Mirror (other domain or URL): " + set.Alias}
		switch h.ListDialog(list[i].Name, fields...) {
		case 0:
			set.Username = h.Keyboard(set.Username, "Username", false)
		case 1:
			set.Password = h.Keyboard("", "Password", true)
		case 2:
			set.Passkey = h.Keyboard("", "Passkey", true)
		case 3:
			set.Alias = h.Keyboard(set.Alias, "Mirror", false)
		default:
			continue
		}
		if err := burst.SaveSettings(dir, list[i].ID, set); err != nil {
			return err
		}
	}
}

// editOptions edits the general options of one group: switches in a multi-select list, then the
// numbers and texts one by one.
func editOptions(h *xbmc.XBMCHost, dir, title, group string) error {
	general := burst.LoadGeneral(dir)
	var switches, values []burst.Option
	for _, o := range burst.Options {
		if o.Group != group {
			continue
		}
		if _, ok := o.Default.(bool); ok {
			switches = append(switches, o)
		} else {
			values = append(values, o)
		}
	}
	if len(switches) > 0 {
		var labels []string
		var on []int
		for i, o := range switches {
			labels = append(labels, o.Label)
			if b, _ := general.Get(o.ID).(bool); b {
				on = append(on, i)
			}
		}
		if chosen := h.DialogMultiSelect(title+": "+group, labels, on); chosen != nil {
			want := map[int]bool{}
			for _, i := range chosen {
				want[i] = true
			}
			for i, o := range switches {
				if b, _ := general.Get(o.ID).(bool); b != want[i] {
					if err := burst.SaveOption(dir, o.ID, want[i]); err != nil {
						return err
					}
				}
			}
		}
	}
	for len(values) > 0 {
		general = burst.LoadGeneral(dir)
		var labels []string
		for _, o := range values {
			labels = append(labels, fmt.Sprintf("%s: %v", o.Label, general.Get(o.ID)))
		}
		i := h.ListDialog(title+": "+group, labels...)
		if i < 0 {
			break
		}
		o := values[i]
		answer := h.Keyboard(fmt.Sprint(general.Get(o.ID)), o.Label, false)
		var value any = answer
		if _, isNumber := o.Default.(float64); isNumber {
			n, err := strconv.ParseFloat(answer, 64)
			if err != nil {
				continue // cancelled, or not a number
			}
			value = n
		}
		if err := burst.SaveOption(dir, o.ID, value); err != nil {
			return err
		}
	}
	return nil
}
