package api

import (
	"cmp"
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
	for {
		menu := []menuEntry{
			{d.t("Public providers"), d.enabledNames(false), func() error { return d.chooseProviders(false) }},
			{d.t("Private providers (need an account)"), d.enabledNames(true), func() error { return d.chooseProviders(true) }},
			{d.t("Logins, mirrors and cookies"), "", d.editLogins},
		}
		for _, g := range groups {
			if g == "Cookie sync" {
				continue // its own menu, below
			}
			menu = append(menu, menuEntry{d.t(g), d.summary(g), func() error { return d.editOptions(g) }})
		}
		menu = append(menu, menuEntry{"[B]" + d.t("Cookie sync") + "[/B]", d.cookieSyncSummary(), d.cookieSync})
		if burst.HasBurstSettings(providers.BurstSettingsPath()) {
			menu = append(menu, menuEntry{d.t("Import settings from the Burst add-on"), "", d.importBurst})
		}
		choice := d.pick(d.title(""), menu)
		if choice < 0 {
			ctx.String(200, "")
			return
		}
		failed(menu[choice].run())
	}
}

// menuEntry is a line of a menu: what it opens, with its current state below it.
type menuEntry struct {
	label, state string
	run          func() error
}

func (d builtinDialog) pick(title string, menu []menuEntry) int {
	items := make([]xbmc.DetailItem, len(menu))
	for i, e := range menu {
		items[i] = xbmc.DetailItem{Label: e.label, Label2: e.state}
	}
	return d.h.ListDialogDetails(title, items, -1)
}

// shorten keeps a summary to what fits on the line of a dialog.
func shorten(parts []string) string {
	s := strings.Join(parts, " · ")
	if r := []rune(s); len(r) > 110 {
		return string(r[:107]) + "..."
	}
	return s
}

// enabledNames lists the providers of a kind that searches use now, and the private ones switched on
// without a login.
func (d builtinDialog) enabledNames(private bool) string {
	var names []string
	for _, st := range burst.List(d.dir) {
		if st.Private != private || !st.Enabled || st.Unusable != "" {
			continue
		}
		if st.NeedsLogin {
			names = append(names, st.Name+" ([COLOR FF999900]"+d.t("no login yet")+"[/COLOR])")
		} else {
			names = append(names, st.Name)
		}
	}
	return shorten(names)
}

// summary is the state of an option group: the switches that are on and the values.
func (d builtinDialog) summary(group string) string {
	general := burst.LoadGeneral(d.dir)
	var parts []string
	for _, o := range burst.Options {
		if o.Group != group || o.Prefix != "" || o.Secret() {
			continue
		}
		switch v := general.Get(o.ID).(type) {
		case bool:
			if v {
				parts = append(parts, d.t(o.Label))
			}
		case float64:
			value := fmt.Sprint(v)
			if int(v) >= 0 && int(v) < len(o.Choices) {
				value = d.t(o.Choices[int(v)])
			}
			parts = append(parts, d.t(o.Label)+": "+value)
		case string:
			if v != "" {
				parts = append(parts, d.t(o.Label)+": "+v)
			}
		}
	}
	return shorten(parts)
}

func (d builtinDialog) yesNo(on bool) string {
	if on {
		return ui.Translate(d.lang, "LOCALIZE[30413]")
	}
	return ui.Translate(d.lang, "LOCALIZE[30414]")
}

func (d builtinDialog) cookieSyncSummary() string {
	general := burst.LoadGeneral(d.dir)
	parts := []string{d.yesNo(general.Get("cookie_sync_enabled") == true)}
	if user, _ := general.Get("cookie_sync_user").(string); user != "" {
		parts = append(parts, d.t("GitHub user")+": "+user)
	}
	if gist := d.activeGist(general); gist != "" {
		parts = append(parts, d.t("Active Gist")+": "+gist)
	}
	return shorten(parts)
}

// activeGist is the gist the cookie sync uses, as described when it was chosen, else its id.
func (d builtinDialog) activeGist(general burst.General) string {
	info, _ := general.Get("cookie_sync_info").(string)
	id, _ := general.Get("cookie_sync_gist_id").(string)
	return cmp.Or(info, id)
}

// cookieSync is the menu of the cookie sync: switch it on, the GitHub account, the gist, and the password
// and the name of the cookies file.
func (d builtinDialog) cookieSync() error {
	for {
		general := burst.LoadGeneral(d.dir)
		text := func(id string) string { s, _ := general.Get(id).(string); return s }
		mark := ""
		if text("cookie_sync_password") != "" {
			mark = d.t("(set)")
		}
		enabled := general.Get("cookie_sync_enabled") == true
		menu := []menuEntry{{d.t("Enable Cookie sync"), d.yesNo(enabled), func() error { return burst.SaveOption(d.dir, "cookie_sync_enabled", !enabled) }}}
		if user := text("cookie_sync_user"); user == "" {
			menu = append(menu, menuEntry{d.t("Connect GitHub and select Gist"), "", d.connectGitHub})
		} else { // choosing it signs in again, as to another account
			menu = append(menu, menuEntry{d.t("GitHub user"), user, d.connectGitHub})
		}
		if text("cookie_sync_token") != "" {
			menu = append(menu, menuEntry{d.t("Active Gist"), d.activeGist(general), d.selectGist})
		}
		menu = append(menu,
			menuEntry{d.t("Encryption password (optional)"), mark, func() error {
				password := d.h.Keyboard("", d.t("Encryption password (optional)"), true)
				if password == "" {
					return nil // cancelled: the stored one stays
				}
				return burst.SaveOption(d.dir, "cookie_sync_password", password)
			}},
			menuEntry{d.t("File name (optional)"), text("cookie_sync_filename"), func() error {
				name := d.h.Keyboard(text("cookie_sync_filename"), d.t("File name (optional)"), false)
				if name == "" {
					return nil // cancelled; the name is needed
				}
				return burst.SaveOption(d.dir, "cookie_sync_filename", name)
			}},
		)
		choice := d.pick(d.title(d.t("Cookie sync")), menu)
		if choice < 0 {
			return nil
		}
		if err := menu[choice].run(); err != nil {
			return err
		}
	}
}

// icon is a provider's icon as Kodi can open it.
func (d builtinDialog) icon(id string) string { return config.KodiPath(burst.Icon(d.dir, id)) }

// importBurst takes over the Burst add-on's settings, after asking.
func (d builtinDialog) importBurst() error {
	if d.h.DialogConfirmNonTimed("Elementum", d.t("Import settings of the Burst add-on into the built-in search?")) {
		providers.ImportBurstSettings(d.h)
	}
	return nil
}

// selectGist lists the gists with the cookies file and makes the cookie sync use the chosen one.
func (d builtinDialog) selectGist() error {
	gists, err := burst.ListGists(context.Background(), d.dir)
	if err != nil {
		return err
	}
	general := burst.LoadGeneral(d.dir)
	if len(gists) == 0 {
		d.h.Notify("Elementum", fmt.Sprintf(d.t("No Gists with %s file found"), general.Get("cookie_sync_filename")), config.AddonIcon())
		return nil
	}
	items := make([]xbmc.DetailItem, len(gists))
	current := 0
	for i, g := range gists {
		updated := strings.ReplaceAll(g.Updated, "T", " ")
		updated = updated[:min(len(updated), 16)]
		items[i] = xbmc.DetailItem{
			Label:  cmp.Or(g.Description, g.ID),
			Label2: fmt.Sprintf("ID: %s | %s | %s", g.ID, fmt.Sprintf(d.t("%d cookies for %d sites"), g.Cookies, g.Sites), updated),
		}
		if g.ID == general.Get("cookie_sync_gist_id") {
			current = i
		}
	}
	if i := d.h.ListDialogDetails(d.title(d.t("Select Gist with cookies")), items, current); i >= 0 {
		return burst.SelectGist(d.dir, gists[i].ID, gists[i].Description)
	}
	return nil
}

// chooseProviders switches the public or the private providers on and off in one multi-select list.
func (d builtinDialog) chooseProviders(private bool) error {
	var list []burst.Status
	var items []xbmc.DetailItem
	var on []int
	for _, st := range burst.List(d.dir) {
		if st.Unusable != "" || st.Private != private {
			continue
		}
		label2 := strings.Join(st.Languages, ", ")
		if st.NeedsLogin {
			label2 += " - [COLOR FF999900]" + d.t("no login yet") + "[/COLOR]"
		}
		if st.Enabled {
			on = append(on, len(list))
		}
		list, items = append(list, st), append(items, xbmc.DetailItem{Label: st.Name, Label2: label2, Icon: d.icon(st.ID)})
	}
	kind := d.t("Public providers")
	if private {
		kind = d.t("Private providers (need an account)")
	}
	chosen := d.h.DialogMultiSelectDetails(d.title(kind), items, on)
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
		var items []xbmc.DetailItem
		for _, st := range burst.List(d.dir) {
			if st.Unusable != "" || !(st.Enabled || st.Private) {
				continue
			}
			list, items = append(list, st), append(items, xbmc.DetailItem{Label: st.Name, Label2: st.Username, Icon: d.icon(st.ID)})
		}
		i := d.h.ListDialogDetails(d.title(d.t("Logins, mirrors and cookies")), items, -1)
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
		fields := []xbmc.DetailItem{
			{Label: d.t("Username"), Label2: set.Username},
			{Label: d.t("Password"), Label2: mark(set.Password != "")},
			{Label: d.t("Passkey"), Label2: mark(set.Passkey != "")},
			{Label: d.t("Domain Alias [Optional]"), Label2: set.Alias},
			{Label: d.t("Contains only"), Label2: contains[set.Contains]},
			{Label: d.t("Browser cookies"), Label2: mark(set.Cookies != "")},
			{Label: d.t("User Agent"), Label2: set.UserAgent},
		}
		for n := range fields {
			fields[n].Icon = d.icon(list[i].ID)
		}
		switch d.h.ListDialogDetails(list[i].Name, fields, -1) {
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
		var items []xbmc.DetailItem
		for _, o := range values {
			shown := fmt.Sprint(general.Get(o.ID))
			if n, isNumber := general.Get(o.ID).(float64); isNumber && int(n) >= 0 && int(n) < len(o.Choices) {
				shown = d.t(o.Choices[int(n)])
			}
			if o.Secret() && shown != "" {
				shown = d.t("(set)")
			}
			items = append(items, xbmc.DetailItem{Label: d.label(o), Label2: shown})
		}
		i := d.h.ListDialogDetails(d.title(d.t(group)), items, -1)
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

// connectGitHub signs in to GitHub for the cookie sync, then lets the user choose the gist.
func (d builtinDialog) connectGitHub() error {
	connected, err := d.waitForGitHub()
	if err != nil || !connected {
		return err
	}
	d.h.Notify("Elementum", d.t("GitHub is connected"), config.AddonIcon())
	return d.selectGist()
}

// waitForGitHub shows the page and the code to enter on any device until the user has done so (true),
// cancels or the code expires.
func (d builtinDialog) waitForGitHub() (bool, error) {
	code, err := burst.StartDeviceLogin(context.Background())
	if err != nil {
		return false, err
	}
	// the text has Kodi's [B] and [CR], which the dialog shows as bold and a line break
	message := fmt.Sprintf(d.t("Open [B]%s[/B] on any device and enter the code:[CR][B]%s[/B]"), code.VerificationURL, code.UserCode)
	dialog := d.h.NewDialogProgress(d.title(d.t("Cookie sync")), message, "", "")
	if dialog == nil {
		return false, nil
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
			return err == nil, err
		case <-time.After(500 * time.Millisecond):
			if dialog.IsCanceled() {
				return false, nil // the deferred cancel stops the waiting
			}
			left := 100 - int(time.Since(started).Seconds()*100/float64(max(code.ExpiresIn, 1)))
			dialog.Update(max(left, 0), message, "", "")
		}
	}
}
