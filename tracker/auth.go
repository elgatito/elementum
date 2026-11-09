package tracker

import (
	"errors"
	"fmt"
	"strconv"
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

// Localized strings shared by device-code trackers.
const (
	strAuthorizeTitle = 30786
	strAuthorizeSteps = 30788
	strAuthorized     = 30789
	strAuthFailed     = 30790
	strDeauthorized   = 30791
	strReauthorize    = 30794
)

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

func saveCredentials(id string, c *config.TrackerCredentials) {
	if config.Get().TrackerCredentials == nil {
		config.Get().TrackerCredentials = map[string]*config.TrackerCredentials{}
	}
	config.Get().TrackerCredentials[id] = c
	config.SetSetting(id+"_token", c.Token)
	config.SetSetting(id+"_refresh_token", c.RefreshToken)
	config.SetSetting(id+"_token_expiry", strconv.FormatInt(c.TokenExpiry, 10))
	config.SetSetting(id+"_username", c.Username)
}

func notify(format int, args ...any) {
	msg := fmt.Sprintf("LOCALIZE[%d]", format)
	for _, a := range args {
		msg += fmt.Sprintf(";;%v", a)
	}
	ui.Toast(msg)
}

// DeviceAuthStarter is implemented by trackers that can start a device-code login without Kodi.
// The returned code is for the user to enter; the token is saved in background once approved.
type DeviceAuthStarter interface {
	StartAuth() (*DeviceCode, error)
}

// minPollInterval is a var for tests.
var minPollInterval = 5 * time.Second

var (
	authMu  sync.Mutex
	authGen = map[string]uint64{}
	logins  = map[string]int64{}
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
	code, err := f.RequestCode()
	if err != nil {
		log.Errorf("Could not get authorization code from %s: %s", p.Name(), err)
		notify(strAuthFailed, p.Name())
		return nil, err
	}
	log.Noticef("Got %s code for %s: %s", p.Name(), code.VerificationURL, code.UserCode)

	go pollDeviceToken(p, f, code, BeginAuth(p.ID()))
	return code, nil
}

// RunDeviceAuth shows the device code to the user and polls for the token in background.
// It returns after the dialog is closed; success or failure is notified when polling ends.
func RunDeviceAuth(p Provider, f DeviceFlow) error {
	code, err := StartDeviceAuth(p, f)
	if err != nil {
		return err
	}

	if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
		if !xbmcHost.Dialog(xbmcHost.GetLocalizedString(strAuthorizeTitle), fmt.Sprintf(xbmcHost.GetLocalizedString(strAuthorizeSteps), p.Name(), code.VerificationURL, code.UserCode)) {
			return errors.New("Authentication canceled")
		}
	}
	return nil
}

func pollDeviceToken(p Provider, f DeviceFlow, code *DeviceCode, current func() bool) {
	interval := max(time.Duration(code.Interval)*time.Second, minPollInterval)
	deadline := time.Now().Add(time.Duration(max(code.ExpiresIn, 60)) * time.Second)
	closer := broadcast.Closer.C()

	for time.Now().Before(deadline) {
		select {
		case <-closer:
			return
		case <-time.After(interval):
		}

		if !current() { // replaced by a newer login
			return
		}
		token, err := f.PollToken(code)
		if errors.Is(err, ErrSlowDown) {
			interval += 5 * time.Second
			continue
		} else if errors.Is(err, ErrAuthPending) {
			continue
		} else if err != nil {
			log.Warningf("%s authorization failed: %s", p.Name(), err)
			notify(strAuthFailed, p.Name())
			return
		}

		c := &config.TrackerCredentials{
			Token:        token.AccessToken,
			RefreshToken: token.RefreshToken,
			TokenExpiry:  expiry(token),
		}
		if c.Username, err = f.Username(token.AccessToken); err != nil {
			log.Warningf("Could not get %s username: %s", p.Name(), err)
		}

		ClearState(p.ID())
		saveCredentials(p.ID(), c)
		LoggedIn(p.ID())
		notify(strAuthorized, p.Name())
		return
	}

	notify(strAuthFailed, p.Name())
}

func expiry(t *Token) int64 {
	if t.ExpiresIn <= 0 {
		return 0
	}
	return time.Now().Unix() + int64(t.ExpiresIn)
}

// DeauthorizeDevice revokes the token when the tracker allows it and clears stored credentials.
func DeauthorizeDevice(p Provider, f DeviceFlow) error {
	if c := Credentials(p.ID()); c.Token != "" {
		if err := f.Revoke(c.Token); err != nil {
			log.Debugf("Failed to revoke %s token: %s", p.Name(), err)
		}
	}

	ClearState(p.ID())
	saveCredentials(p.ID(), &config.TrackerCredentials{})
	notify(strDeauthorized, p.Name())
	return nil
}

// RefreshDeviceToken renews the token of a device-code tracker when it expires within ahead.
func RefreshDeviceToken(p Provider, f DeviceFlow, ahead time.Duration) error {
	c := Credentials(p.ID())
	if c.Token == "" || c.RefreshToken == "" || c.TokenExpiry == 0 || time.Now().Add(ahead).Unix() < c.TokenExpiry {
		return nil
	}

	token, err := f.RefreshToken(c.RefreshToken)
	if err != nil {
		log.Errorf("Failed to refresh %s token: %s", p.Name(), err)
		notify(strReauthorize, p.Name())
		return err
	}

	if token.RefreshToken == "" {
		token.RefreshToken = c.RefreshToken
	}
	saveCredentials(p.ID(), &config.TrackerCredentials{
		Token:        token.AccessToken,
		RefreshToken: token.RefreshToken,
		TokenExpiry:  expiry(token),
		Username:     c.Username,
	})
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
