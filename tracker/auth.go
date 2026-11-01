package tracker

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/elgatito/elementum/broadcast"
	"github.com/elgatito/elementum/config"
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
	if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
		xbmcHost.SetSetting(id+"_token", c.Token)
		xbmcHost.SetSetting(id+"_refresh_token", c.RefreshToken)
		xbmcHost.SetSetting(id+"_token_expiry", strconv.FormatInt(c.TokenExpiry, 10))
		xbmcHost.SetSetting(id+"_username", c.Username)
	}
}

func notify(format int, args ...any) {
	if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
		xbmcHost.Notify("Elementum", fmt.Sprintf(xbmcHost.GetLocalizedString(format), args...), config.AddonIcon())
	}
}

// RunDeviceAuth shows the device code to the user and polls for the token in background.
// It returns after the dialog is closed; success or failure is notified when polling ends.
func RunDeviceAuth(p Provider, f DeviceFlow) error {
	code, err := f.RequestCode()
	if err != nil {
		log.Errorf("Could not get authorization code from %s: %s", p.Name(), err)
		notify(strAuthFailed, p.Name())
		return err
	}
	log.Noticef("Got %s code for %s: %s", p.Name(), code.VerificationURL, code.UserCode)

	go pollDeviceToken(p, f, code)

	if xbmcHost, _ := xbmc.GetLocalXBMCHost(); xbmcHost != nil {
		if !xbmcHost.Dialog(xbmcHost.GetLocalizedString(strAuthorizeTitle), fmt.Sprintf(xbmcHost.GetLocalizedString(strAuthorizeSteps), p.Name(), code.VerificationURL, code.UserCode)) {
			return errors.New("Authentication canceled")
		}
	}
	return nil
}

func pollDeviceToken(p Provider, f DeviceFlow, code *DeviceCode) {
	interval := time.Duration(max(code.Interval, 5)) * time.Second
	deadline := time.Now().Add(time.Duration(max(code.ExpiresIn, 60)) * time.Second)
	closer := broadcast.Closer.C()

	for time.Now().Before(deadline) {
		select {
		case <-closer:
			return
		case <-time.After(interval):
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
