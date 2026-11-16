package tracker

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elgatito/elementum/broadcast"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/ui"
	"github.com/elgatito/elementum/xbmc"
)

var (
	// ErrAuthPending is returned by DeviceFlow.PollToken while the user has not approved the code yet.
	ErrAuthPending = errors.New("authorization pending")
	// ErrSlowDown is returned by DeviceFlow.PollToken when the tracker asks to poll less often.
	ErrSlowDown = errors.New("polling too fast")
)

// AuthStrings are the Kodi string ids a device-code login shows. NoName leaves the tracker name out of the arguments.
type AuthStrings struct {
	Title, Steps, Authorized, Failed, Deauthorized, Reauthorize int
	NoName                                                      bool
}

var defaultAuthStrings = AuthStrings{Title: 30786, Steps: 30788, Authorized: 30789, Failed: 30790, Deauthorized: 30791, Reauthorize: 30794}

// AuthCustomizer is optionally implemented by a Provider that shows its own strings
// or reacts to credential changes (refresh is true when only the token was renewed).
type AuthCustomizer interface {
	AuthStrings() AuthStrings
	AuthChanged(c *config.TrackerCredentials, refresh bool)
}

func authStrings(p Provider) AuthStrings {
	if c, ok := p.(AuthCustomizer); ok {
		return c.AuthStrings()
	}
	return defaultAuthStrings
}

func (s AuthStrings) toast(id int, p Provider) {
	msg := fmt.Sprintf("LOCALIZE[%d]", id)
	if !s.NoName {
		msg += ";;" + p.Name()
	}
	ui.Toast(msg)
}

// DeviceCode is the code the user enters on the tracker website.
type DeviceCode struct {
	DeviceCode      string
	UserCode        string
	VerificationURL string
	ExpiresIn       int // seconds
	Interval        int // seconds
}

// Token is an OAuth token pair.
type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int // seconds
}

// DeviceFlow is the OAuth device-code protocol of a tracker. RunDeviceAuth drives it.
type DeviceFlow interface {
	RequestCode() (*DeviceCode, error)
	PollToken(code *DeviceCode) (*Token, error) // ErrAuthPending or ErrSlowDown while waiting
	RefreshToken(refreshToken string) (*Token, error)
	Revoke(accessToken string) error
	Username(accessToken string) (string, error)
}

// Credentials returns the stored credentials of a device-code tracker.
func Credentials(id string) *config.TrackerCredentials {
	if c := config.Get().TrackerCredentials[id]; c != nil {
		return c
	}
	return &config.TrackerCredentials{}
}

func saveCredentials(p Provider, c *config.TrackerCredentials, refresh bool) {
	id := p.ID()
	if config.Get().TrackerCredentials == nil {
		config.Get().TrackerCredentials = map[string]*config.TrackerCredentials{}
	}
	config.Get().TrackerCredentials[id] = c
	config.SetSetting(id+"_token", c.Token)
	config.SetSetting(id+"_refresh_token", c.RefreshToken)
	config.SetSetting(id+"_token_expiry", strconv.FormatInt(c.TokenExpiry, 10))
	if !refresh { // a refresh writes only the token keys, as Kodi reloads on any other setting change
		config.SetSetting(id+"_username", c.Username)
	}
	if h, ok := p.(AuthCustomizer); ok {
		h.AuthChanged(c, refresh)
	}
}

// DeviceAuthStarter is implemented by trackers that can start a device-code login without Kodi.
// The returned code is for the user to enter; the token is saved in background once approved.
type DeviceAuthStarter interface {
	StartAuth() (*DeviceCode, error)
}

// minPollInterval is a var for tests.
var minPollInterval = 5 * time.Second

var (
	// Serialize generation changes with credential commits, including persistence and provider hooks.
	authCommitMu sync.Mutex
	authMu       sync.Mutex
	authGen      = map[string]uint64{}
	logins       = map[string]int64{}
)

// LoggedIn counts a finished login of the tracker, so a client re-linking an account can tell it is done.
func LoggedIn(id string) {
	authMu.Lock()
	defer authMu.Unlock()
	logins[id]++
}

// Logins is the number of logins of the tracker finished since Elementum started.
func Logins(id string) int64 {
	authMu.Lock()
	defer authMu.Unlock()
	return logins[id]
}

// BeginAuth marks a new login of the tracker and returns a func telling whether it is still the latest,
// so a poller of an earlier login can stop.
func BeginAuth(id string) (current func() bool) {
	authCommitMu.Lock()
	defer authCommitMu.Unlock()
	authMu.Lock()
	defer authMu.Unlock()
	authGen[id]++
	gen := authGen[id]
	return func() bool {
		authMu.Lock()
		defer authMu.Unlock()
		return authGen[id] == gen
	}
}

// StartDeviceAuth requests a code and polls for the token in background; success or failure is notified when polling ends.
func StartDeviceAuth(p Provider, f DeviceFlow) (*DeviceCode, error) {
	code, _, err := startDeviceAuth(p, f)
	return code, err
}

// startDeviceAuth is StartDeviceAuth that also tells when polling has ended: true when the login succeeded.
func startDeviceAuth(p Provider, f DeviceFlow) (*DeviceCode, <-chan bool, error) {
	current := BeginAuth(p.ID())
	code, err := f.RequestCode()
	if err == nil && (code == nil || code.DeviceCode == "") {
		err = errors.New("empty device code")
	}
	if err != nil {
		log.Errorf("Could not get authorization code from %s: %s", p.Name(), err)
		authStrings(p).toast(authStrings(p).Failed, p)
		return nil, nil, err
	}
	log.Noticef("Got %s code for %s: %s", p.Name(), code.VerificationURL, code.UserCode)

	done := make(chan bool, 1)
	go func() { done <- pollDeviceToken(p, f, code, current) }()
	return code, done, nil
}

var errAuthCanceled = errors.New("Authentication canceled")

// RunDeviceAuth shows the device code to the user and polls for the token in background. The instructions
// stay on screen until the login ends: Kodi closes them by itself when it succeeded or failed, so there
// is nothing to confirm; Cancel stops the login. Success or failure is notified as well.
func RunDeviceAuth(p Provider, f DeviceFlow) error {
	code, done, err := startDeviceAuth(p, f)
	if err != nil {
		return err
	}
	xbmcHost, _ := xbmc.GetLocalXBMCHost()
	if xbmcHost == nil {
		return nil
	}
	str := authStrings(p)
	args := []any{p.Name(), code.VerificationURL, code.UserCode}
	if str.NoName {
		args = args[1:]
	}
	title, message := xbmcHost.GetLocalizedString(str.Title), fmt.Sprintf(xbmcHost.GetLocalizedString(str.Steps), args...)
	dialog := xbmcHost.NewDialogProgress(title, message, "", "")
	if dialog == nil { // an add-on that could not open it: the dialog to confirm, as before
		if !xbmcHost.Dialog(title, message) {
			return errAuthCanceled
		}
		return nil
	}
	defer dialog.Close()
	return waitForLogin(p.ID(), done, dialog, time.Duration(max(code.ExpiresIn, 60))*time.Second, message)
}

// loginDialog is what waitForLogin needs of Kodi's progress dialog.
type loginDialog interface {
	Update(percent int, line1, line2, line3 string)
	IsCanceled() bool
}

// waitForLogin keeps the instructions up, with the time left on the code as the bar, until polling ends
// or the user cancels; a cancel stops the polling.
func waitForLogin(id string, done <-chan bool, dialog loginDialog, lifetime time.Duration, message string) error {
	started := time.Now()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case ok := <-done:
			if !ok {
				return errors.New("Authentication failed")
			}
			return nil
		case <-tick.C:
			if dialog.IsCanceled() {
				BeginAuth(id) // the poller of this login sees it is no longer the latest and stops
				return errAuthCanceled
			}
			left := 100 - int(time.Since(started)*100/lifetime)
			dialog.Update(max(left, 0), message, "", "")
		}
	}
}

// pollDeviceToken polls until the user approved the code, it expired or failed, or a newer login replaced
// this one. It returns true when the credentials were saved.
func pollDeviceToken(p Provider, f DeviceFlow, code *DeviceCode, current func() bool) bool {
	interval := max(time.Duration(code.Interval)*time.Second, minPollInterval)
	deadline := time.Now().Add(time.Duration(max(code.ExpiresIn, 60)) * time.Second)
	closer := broadcast.Closer.C()

	for time.Now().Before(deadline) {
		select {
		case <-closer:
			return false
		case <-time.After(interval):
		}

		if !current() { // replaced by a newer login
			return false
		}
		token, err := f.PollToken(code)
		if !current() {
			return false
		}
		if errors.Is(err, ErrSlowDown) {
			interval += 5 * time.Second
			continue
		} else if errors.Is(err, ErrAuthPending) {
			continue
		} else if err != nil {
			log.Warningf("%s authorization failed: %s", p.Name(), err)
			authStrings(p).toast(authStrings(p).Failed, p)
			return false
		}

		if token == nil || token.AccessToken == "" {
			authStrings(p).toast(authStrings(p).Failed, p)
			return false
		}
		c := &config.TrackerCredentials{
			Token:        token.AccessToken,
			RefreshToken: token.RefreshToken,
			TokenExpiry:  expiry(token),
		}
		c.Username, err = f.Username(token.AccessToken)
		if !current() {
			return false
		}
		if err == nil && strings.TrimSpace(c.Username) == "" {
			err = errors.New("empty username")
		}
		if err != nil {
			log.Warningf("Could not get %s username: %s", p.Name(), err)
			authStrings(p).toast(authStrings(p).Failed, p)
			return false
		}

		authCommitMu.Lock()
		defer authCommitMu.Unlock()
		if !current() {
			return false
		}
		ClearState(p.ID())
		saveCredentials(p, c, false)
		LoggedIn(p.ID())
		authStrings(p).toast(authStrings(p).Authorized, p)
		return true
	}

	authStrings(p).toast(authStrings(p).Failed, p)
	return false
}

func expiry(t *Token) int64 {
	if t.ExpiresIn <= 0 {
		return 0
	}
	return time.Now().Unix() + int64(t.ExpiresIn)
}

// DeauthorizeDevice revokes the token when the tracker allows it and clears stored credentials.
func DeauthorizeDevice(p Provider, f DeviceFlow) error {
	current := BeginAuth(p.ID())
	authCommitMu.Lock()
	if !current() {
		authCommitMu.Unlock()
		return nil
	}
	token := Credentials(p.ID()).Token
	authCommitMu.Unlock()
	if token != "" {
		if err := f.Revoke(token); err != nil {
			log.Debugf("Failed to revoke %s token: %s", p.Name(), err)
		}
	}

	authCommitMu.Lock()
	defer authCommitMu.Unlock()
	if !current() {
		return nil
	}
	ClearState(p.ID())
	saveCredentials(p, &config.TrackerCredentials{}, false)
	authStrings(p).toast(authStrings(p).Deauthorized, p)
	return nil
}

// RefreshDeviceToken renews the token of a device-code tracker when it expires within ahead.
func RefreshDeviceToken(p Provider, f DeviceFlow, ahead time.Duration) error {
	authCommitMu.Lock()
	c := Credentials(p.ID())
	authMu.Lock()
	gen := authGen[p.ID()]
	authMu.Unlock()
	authCommitMu.Unlock()
	if c.Token == "" || c.RefreshToken == "" || c.TokenExpiry == 0 || time.Now().Add(ahead).Unix() < c.TokenExpiry {
		return nil
	}

	token, err := f.RefreshToken(c.RefreshToken)
	authCommitMu.Lock()
	defer authCommitMu.Unlock()
	authMu.Lock()
	current := authGen[p.ID()] == gen
	authMu.Unlock()
	if !current || Credentials(p.ID()) != c {
		return nil
	}
	if err != nil {
		log.Errorf("Failed to refresh %s token: %s", p.Name(), err)
		authStrings(p).toast(authStrings(p).Reauthorize, p)
		return err
	}
	if token == nil || token.AccessToken == "" {
		authStrings(p).toast(authStrings(p).Reauthorize, p)
		return errors.New("empty refreshed access token")
	}

	if token.RefreshToken == "" {
		token.RefreshToken = c.RefreshToken
	}
	saveCredentials(p, &config.TrackerCredentials{
		Token:        token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenExpiry:  expiry(token),
		Username:     c.Username,
	}, true)
	log.Noticef("%s token refreshed", p.Name())
	return nil
}

// TokenRefreshHandler renews tokens of every registered tracker on start and every hour.
func TokenRefreshHandler() {
	refresh := func() {
		for _, p := range All() {
			_ = p.RefreshToken()
		}
	}
	refresh()

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	closer := broadcast.Closer.C()
	for {
		select {
		case <-closer:
			return
		case <-ticker.C:
			refresh()
		}
	}
}
