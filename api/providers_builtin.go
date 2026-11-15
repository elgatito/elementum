package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/providers"
	"github.com/elgatito/elementum/providers/burst"
	"github.com/elgatito/elementum/ui"
	"github.com/elgatito/elementum/xbmc"
)

// builtinDialog is the Kodi dialog for Elementum's own torrent search (providers/burst): which providers
// are on, their logins and mirrors, the filters and the cookie sync. It is what the Burst add-on's
// settings window was. The same settings are behind /api/providers for other clients.
type builtinDialog struct {
	h    *xbmc.XBMCHost
	dir  string
	lang string
}

// t translates an English text of the built-in search into Kodi's language.
func (d builtinDialog) t(english string) string { return ui.Text(d.lang, english) }

func (d builtinDialog) title(part string) string {
	if part == "" {
		return "Elementum: " + d.t("Built-in search")
	}
	return d.t("Built-in search") + ": " + part
}

// BuiltinSettings opens the dialog.
func BuiltinSettings(ctx *gin.Context) {
	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	dir := providers.ProvidersDir()
	if xbmcHost == nil || dir == "" {
		return
	}
	providers.OfferBurstImport(xbmcHost)
	d := builtinDialog{h: xbmcHost, dir: dir, lang: ui.LanguageOf(xbmcHost)}
	failed := func(err error) {
		if err != nil {
			log.Errorf("Built-in search settings: %s", err)
			xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
		}
	}
	groups := []string{}
	for _, o := range burst.Options {
		if len(groups) == 0 || groups[len(groups)-1] != o.Group {
			groups = append(groups, o.Group)
		}
	}
	menu := []string{d.t("Public providers"), d.t("Private providers (need an account)"), d.t("Logins, mirrors and cookies")}
	for _, g := range groups {
		menu = append(menu, d.t(g))
	}
	menu = append(menu, d.t("Connect GitHub and select Gist"))
	for {
		choice := xbmcHost.ListDialog(d.title(""), menu...)
		switch {
		case choice < 0:
			ctx.String(200, "")
			return
		case choice <= 1:
			failed(d.chooseProviders(choice == 1))
		case choice == 2:
			failed(d.editLogins())
		case choice == len(menu)-1:
			failed(d.connectGitHub())
		default:
			failed(d.editOptions(groups[choice-3]))
		}
	}
}

// chooseProviders switches the public or the private providers on and off in one multi-select list.
func (d builtinDialog) chooseProviders(private bool) error {
	var list []burst.Status
	var labels []string
	var on []int
	for _, st := range burst.List(d.dir) {
		if st.Unusable != "" || st.Private != private {
			continue
		}
		label := st.Name
		if len(st.Languages) > 0 {
			label += " [" + strings.Join(st.Languages, ", ") + "]"
		}
		if st.NeedsLogin {
			label += " - [COLOR FF999900]" + d.t("no login yet") + "[/COLOR]"
		}
		if st.Enabled {
			on = append(on, len(list))
		}
		list, labels = append(list, st), append(labels, label)
	}
	kind := d.t("Public providers")
	if private {
		kind = d.t("Private providers (need an account)")
	}
	chosen := d.h.DialogMultiSelect(d.title(kind), labels, on)
	if chosen == nil { // cancelled
		return nil
	}
	want := map[int]bool{}
	for _, i := range chosen {
		want[i] = true
	}
	settings := burst.LoadSettings(d.dir)
	for i, st := range list {
		if st.Enabled == want[i] {
			continue
		}
		set, enabled := settings[st.ID], want[i]
		set.Enabled = &enabled
		if err := burst.SaveSettings(d.dir, st.ID, set); err != nil {
			return err
		}
	}
	return nil
}

// editLogins sets the login, mirror, content limit and browser cookies of one provider at a time.
func (d builtinDialog) editLogins() error {
	for {
		var list []burst.Status
		var labels []string
		for _, st := range burst.List(d.dir) {
			if st.Unusable != "" || !(st.Enabled || st.Private) {
				continue
			}
			label := st.Name
			if st.Username != "" {
				label += " - " + st.Username
			}
			list, labels = append(list, st), append(labels, label)
		}
		i := d.h.ListDialog(d.title(d.t("Logins, mirrors and cookies")), labels...)
		if i < 0 {
			return nil
		}
		set := burst.LoadSettings(d.dir)[list[i].ID]
		mark := func(set bool) string {
			if set {
				return d.t("(set)")
			}
			return ""
		}
		contains := map[string]string{"": d.t("All"), "movies": d.t("Movies"), "shows": d.t("Shows")}
		fields := []string{
			d.t("Username") + ": " + set.Username,
			d.t("Password") + " " + mark(set.Password != ""),
			d.t("Passkey") + " " + mark(set.Passkey != ""),
			d.t("Domain Alias [Optional]") + ": " + set.Alias,
			d.t("Contains only") + ": " + contains[set.Contains],
			d.t("Browser cookies") + " " + mark(set.Cookies != ""),
			d.t("User Agent") + ": " + set.UserAgent,
		}
		switch d.h.ListDialog(list[i].Name, fields...) {
		case 0:
			set.Username = d.h.Keyboard(set.Username, d.t("Username"), false)
		case 1:
			set.Password = d.h.Keyboard("", d.t("Password"), true)
		case 2:
			set.Passkey = d.h.Keyboard("", d.t("Passkey"), true)
		case 3:
			set.Alias = d.h.Keyboard(set.Alias, d.t("Domain Alias [Optional]"), false)
		case 4:
			if kind := d.h.ListDialog(list[i].Name, d.t("All"), d.t("Movies"), d.t("Shows")); kind >= 0 {
				set.Contains = []string{"", "movies", "shows"}[kind]
			}
		case 5:
			set.Cookies = d.h.Keyboard("", d.t("Browser cookies")+" (name=value; ...)", false)
		case 6:
			set.UserAgent = d.h.Keyboard(set.UserAgent, d.t("User Agent"), false)
		default:
			continue
		}
		if err := burst.SaveSettings(d.dir, list[i].ID, set); err != nil {
			return err
		}
	}
}

// label is the translated name of an option, with what it is about when several share the name.
func (d builtinDialog) label(o burst.Option) string {
	if o.Prefix != "" {
		return d.t(o.Prefix) + ": " + d.t(o.Label)
	}
	return d.t(o.Label)
}

// editOptions edits the general options of one group: switches in a multi-select list, then the numbers
// and texts one by one.
func (d builtinDialog) editOptions(group string) error {
	general := burst.LoadGeneral(d.dir)
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
			labels = append(labels, d.label(o))
			if b, _ := general.Get(o.ID).(bool); b {
				on = append(on, i)
			}
		}
		if chosen := d.h.DialogMultiSelect(d.title(d.t(group)), labels, on); chosen != nil {
			want := map[int]bool{}
			for _, i := range chosen {
				want[i] = true
			}
			for i, o := range switches {
				if b, _ := general.Get(o.ID).(bool); b != want[i] {
					if err := burst.SaveOption(d.dir, o.ID, want[i]); err != nil {
						return err
					}
				}
			}
		}
	}
	for len(values) > 0 {
		general = burst.LoadGeneral(d.dir)
		var labels []string
		for _, o := range values {
			shown := fmt.Sprint(general.Get(o.ID))
			if n, isNumber := general.Get(o.ID).(float64); isNumber && int(n) >= 0 && int(n) < len(o.Choices) {
				shown = d.t(o.Choices[int(n)])
			}
			if o.Secret() && shown != "" {
				shown = d.t("(set)")
			}
			labels = append(labels, d.label(o)+": "+shown)
		}
		i := d.h.ListDialog(d.title(d.t(group)), labels...)
		if i < 0 {
			break
		}
		o := values[i]
		var value any
		switch {
		case len(o.Choices) > 0: // a number with named values
			names := make([]string, len(o.Choices))
			for n, c := range o.Choices {
				names[n] = d.t(c)
			}
			picked := d.h.ListDialog(d.label(o), names...)
			if picked < 0 {
				continue
			}
			value = float64(picked)
		default:
			current := fmt.Sprint(general.Get(o.ID))
			if o.Secret() {
				current = ""
			}
			answer := d.h.Keyboard(current, d.label(o), o.Secret())
			value = answer
			if _, isNumber := o.Default.(float64); isNumber {
				n, err := strconv.ParseFloat(answer, 64)
				if err != nil {
					continue // cancelled, or not a number
				}
				value = n
			}
		}
		if err := burst.SaveOption(d.dir, o.ID, value); err != nil {
			return err
		}
	}
	return nil
}

// connectGitHub signs in to GitHub for the cookie sync: Kodi shows the page and the code to enter on
// any device, until the user has done so, cancels, or the code expires.
func (d builtinDialog) connectGitHub() error {
	code, err := burst.StartDeviceLogin(context.Background())
	if err != nil {
		return err
	}
	// the text has Kodi's [B] and [CR], which the dialog shows as bold and a line break
	message := fmt.Sprintf(d.t("Open [B]%s[/B] on any device and enter the code:[CR][B]%s[/B]"), code.VerificationURL, code.UserCode)
	dialog := d.h.NewDialogProgress(d.title(d.t("Cookie sync")), message, "", "")
	if dialog == nil {
		return nil
	}
	defer dialog.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- burst.FinishDeviceLogin(ctx, d.dir, code) }()
	started := time.Now()
	for {
		select {
		case err := <-done:
			if err == nil {
				d.h.Notify("Elementum", d.t("GitHub is connected"), config.AddonIcon())
			}
			return err
		case <-time.After(500 * time.Millisecond):
			if dialog.IsCanceled() {
				return nil // the deferred cancel stops the waiting
			}
			left := 100 - int(time.Since(started).Seconds()*100/float64(max(code.ExpiresIn, 1)))
			dialog.Update(max(left, 0), message, "", "")
		}
	}
}
