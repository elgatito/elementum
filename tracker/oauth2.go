package tracker

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/goccy/go-json"

	"github.com/elgatito/elementum/util/reqapi"
)

// OAuth2Device implements DeviceFlow for trackers following RFC 8628 (device grant) with form-encoded requests.
type OAuth2Device struct {
	API       *reqapi.API
	ClientID  string
	Scope     string
	CodeURL   string
	TokenURL  string
	RevokeURL string // empty when the tracker cannot revoke tokens
	// RequiredScope must be in the granted scope, as some trackers silently downgrade unknown scopes to read-only.
	RequiredScope string
	// Params are added to every request URL.
	Params url.Values
	// UsernameFunc returns the account name for a token.
	UsernameFunc func(accessToken string) (string, error)
}

type oauth2Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

type oauth2Error struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

func (o *OAuth2Device) post(endpoint string, form url.Values, result any) error {
	params := url.Values{}
	for k, v := range o.Params {
		params[k] = v
	}

	req := &reqapi.Request{
		API:         o.API,
		Method:      "POST",
		URL:         endpoint,
		Params:      params,
		Header:      http.Header{"Content-Type": []string{"application/x-www-form-urlencoded"}},
		Payload:     bytes.NewBufferString(form.Encode()),
		Result:      result,
		Description: "oauth2 " + endpoint,
	}
	err := req.Do()
	if err != nil && req.ResponseBody != nil {
		var e oauth2Error
		if json.Unmarshal(req.ResponseBody.Bytes(), &e) == nil && e.Error != "" {
			switch e.Error {
			case "authorization_pending":
				return ErrAuthPending
			case "slow_down":
				return ErrSlowDown
			}
			return fmt.Errorf("%s: %s", e.Error, e.Description)
		}
	}
	return err
}

func (o *OAuth2Device) token(form url.Values) (*Token, error) {
	form.Set("client_id", o.ClientID)

	var t oauth2Token
	if err := o.post(o.TokenURL, form, &t); err != nil {
		return nil, err
	}
	if t.AccessToken == "" {
		return nil, errors.New("empty access token")
	}
	if o.RequiredScope != "" && !strings.Contains(" "+t.Scope+" ", " "+o.RequiredScope+" ") {
		return nil, fmt.Errorf("token was granted without %q scope (got %q)", o.RequiredScope, t.Scope)
	}
	return &Token{AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, ExpiresIn: t.ExpiresIn}, nil
}

// RequestCode starts the device flow.
func (o *OAuth2Device) RequestCode() (*DeviceCode, error) {
	var c struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := o.post(o.CodeURL, url.Values{"client_id": {o.ClientID}, "scope": {o.Scope}}, &c); err != nil {
		return nil, err
	}
	if c.DeviceCode == "" {
		return nil, errors.New("empty device code")
	}
	return &DeviceCode{DeviceCode: c.DeviceCode, UserCode: c.UserCode, VerificationURL: c.VerificationURI, ExpiresIn: c.ExpiresIn, Interval: c.Interval}, nil
}

// PollToken asks for the token once.
func (o *OAuth2Device) PollToken(code *DeviceCode) (*Token, error) {
	return o.token(url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {code.DeviceCode}})
}

// RefreshToken trades a refresh token for a new token pair.
func (o *OAuth2Device) RefreshToken(refreshToken string) (*Token, error) {
	return o.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
}

// Revoke invalidates an access token.
func (o *OAuth2Device) Revoke(accessToken string) error {
	if o.RevokeURL == "" {
		return nil
	}
	return o.post(o.RevokeURL, url.Values{"token": {accessToken}, "client_id": {o.ClientID}}, nil)
}

// Username returns the account name for a token.
func (o *OAuth2Device) Username(accessToken string) (string, error) {
	if o.UsernameFunc == nil {
		return "", nil
	}
	return o.UsernameFunc(accessToken)
}
