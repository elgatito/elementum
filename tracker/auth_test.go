package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/ui"
)

type fakeProvider struct{ Provider }

func (fakeProvider) ID() string   { return "authtest" }
func (fakeProvider) Name() string { return "AuthTest" }

type fakeFlow struct {
	DeviceFlow
	request func() (*DeviceCode, error)
	poll    func() (*Token, error)
	user    func(string) (string, error)
	refresh func(string) (*Token, error)
}

func (f *fakeFlow) RequestCode() (*DeviceCode, error) {
	if f.request != nil {
		return f.request()
	}
	return &DeviceCode{DeviceCode: "device", UserCode: "ABCD", VerificationURL: "https://x/activate", ExpiresIn: 60}, nil
}
func (f *fakeFlow) PollToken(*DeviceCode) (*Token, error) {
	if f.poll != nil {
		return f.poll()
	}
	return &Token{AccessToken: "new", RefreshToken: "new-refresh", ExpiresIn: 7200}, nil
}
func (f *fakeFlow) Username(token string) (string, error) {
	if f.user != nil {
		return f.user(token)
	}
	return "new-user", nil
}
func (f *fakeFlow) RefreshToken(token string) (*Token, error) { return f.refresh(token) }
func (*fakeFlow) Revoke(string) error                         { return nil }

func setupAuth(t *testing.T) string {
	t.Helper()
	cfg := config.Get()
	oldCreds, oldPath, oldInterval := cfg.TrackerCredentials, config.Args.ConfigPath, minPollInterval
	cfg.TrackerCredentials = map[string]*config.TrackerCredentials{
		fakeProvider{}.ID(): {Token: "old", RefreshToken: "old-refresh", TokenExpiry: time.Now().Unix() + 60, Username: "old-user"},
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"Settings":{"authtest_token":"old","authtest_refresh_token":"old-refresh","authtest_username":"old-user"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	config.Args.ConfigPath, minPollInterval = path, 0
	ui.Default.Poll(context.Background(), "tracker-auth-tests", nil, 0)
	t.Cleanup(func() {
		cfg.TrackerCredentials, config.Args.ConfigPath, minPollInterval = oldCreds, oldPath, oldInterval
	})
	return path
}
func readAuthFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func runAuth(f *fakeFlow, current func() bool) bool {
	return pollDeviceToken(fakeProvider{}, f, &DeviceCode{ExpiresIn: 60}, current)
}
func waitAuth(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("auth request did not finish")
	}
}

func TestStartDeviceAuthRelink(t *testing.T) {
	setupAuth(t)
	before := Logins(fakeProvider{}.ID())
	for i := range 2 {
		token := fmt.Sprintf("new-%d", i)
		f := &fakeFlow{poll: func() (*Token, error) { return &Token{AccessToken: token}, nil }}
		code, err := StartDeviceAuth(fakeProvider{}, f)
		if err != nil || code.UserCode != "ABCD" || code.VerificationURL != "https://x/activate" {
			t.Fatalf("code = %+v, %v", code, err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for Logins(fakeProvider{}.ID()) != before+int64(i)+1 {
			if time.Now().After(deadline) {
				t.Fatal("login was not counted")
			}
			time.Sleep(time.Millisecond)
		}
		// The commit mutex also waits for the success notification to finish before reading global state.
		authCommitMu.Lock()
		got := *Credentials(fakeProvider{}.ID())
		authCommitMu.Unlock()
		if got.Token != token || got.Username != "new-user" {
			t.Fatalf("credentials = %+v", got)
		}
	}
}

func TestBeginAuthSupersedes(t *testing.T) {
	first, second := BeginAuth("x"), BeginAuth("x")
	if first() || !second() {
		t.Error("only the latest login is current")
	}
}

func TestStartDeviceAuthInvalidCode(t *testing.T) {
	for _, code := range []*DeviceCode{nil, {UserCode: "ABCD"}} {
		path := setupAuth(t)
		before, stored := Logins(fakeProvider{}.ID()), readAuthFile(t, path)
		f := &fakeFlow{request: func() (*DeviceCode, error) { return code, nil }}
		if _, err := StartDeviceAuth(fakeProvider{}, f); err == nil {
			t.Fatal("empty device code accepted")
		}
		if Logins(fakeProvider{}.ID()) != before || readAuthFile(t, path) != stored {
			t.Fatal("invalid code changed credentials")
		}
	}
}

func TestLoginFailsWithoutUsername(t *testing.T) {
	for _, tc := range []struct {
		name, user string
		err        error
	}{
		{"lookup error", "", errors.New("lookup failed")}, {"empty", "", nil}, {"blank", " \t", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := setupAuth(t)
			stored, creds, before := readAuthFile(t, path), *Credentials(fakeProvider{}.ID()), Logins(fakeProvider{}.ID())
			if runAuth(&fakeFlow{user: func(string) (string, error) { return tc.user, tc.err }}, BeginAuth(fakeProvider{}.ID())) {
				t.Error("a failed login reported success")
			}
			if *Credentials(fakeProvider{}.ID()) != creds || readAuthFile(t, path) != stored || Logins(fakeProvider{}.ID()) != before {
				t.Fatal("failed identity lookup must preserve stored credentials and the login counter")
			}
			events := ui.Default.Poll(context.Background(), "tracker-auth-tests", nil, 0)
			if len(events) != 1 || events[0].Message != "LOCALIZE[30790];;AuthTest" {
				t.Fatalf("identity failure was not notified: %+v", events)
			}
		})
	}
}

func TestLoginSupersededInFlight(t *testing.T) {
	for _, stage := range []string{"token", "username"} {
		t.Run(stage, func(t *testing.T) {
			path := setupAuth(t)
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			f := &fakeFlow{}
			block := func() { close(entered); <-release }
			f.poll = func() (*Token, error) {
				if stage == "token" {
					block()
				}
				return &Token{AccessToken: "superseded"}, nil
			}
			f.user = func(string) (string, error) {
				if stage == "username" {
					block()
				}
				return "superseded-user", nil
			}
			current := BeginAuth(fakeProvider{}.ID())
			var stale bool
			go func() { defer close(done); stale = runAuth(f, current) }()
			waitAuth(t, entered)
			before := Logins(fakeProvider{}.ID())
			if !runAuth(&fakeFlow{}, BeginAuth(fakeProvider{}.ID())) {
				t.Error("a successful login reported failure")
			}
			stored := readAuthFile(t, path)
			close(release)
			waitAuth(t, done)
			if stale {
				t.Error("a superseded login reported success")
			}
			if Credentials(fakeProvider{}.ID()).Token != "new" || readAuthFile(t, path) != stored || Logins(fakeProvider{}.ID()) != before+1 {
				t.Fatal("superseded in-flight login overwrote the newer login")
			}
		})
	}
}

func TestRefreshDeviceToken(t *testing.T) {
	path := setupAuth(t)
	// A refresh must not rewrite the username setting, which triggers a Kodi reload.
	if err := os.WriteFile(path, []byte(`{"Settings":{"authtest_username":"persisted-user"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	before := Logins(fakeProvider{}.ID())
	f := &fakeFlow{refresh: func(token string) (*Token, error) {
		if token != "old-refresh" {
			t.Errorf("refresh token = %q", token)
		}
		return &Token{AccessToken: "refreshed", ExpiresIn: 7200}, nil
	}}
	if err := RefreshDeviceToken(fakeProvider{}, f, 90*time.Minute); err != nil {
		t.Fatal(err)
	}
	c := Credentials(fakeProvider{}.ID())
	if c.Token != "refreshed" || c.RefreshToken != "old-refresh" || c.Username != "old-user" || c.TokenExpiry < time.Now().Unix()+7100 || Logins(fakeProvider{}.ID()) != before {
		t.Fatalf("bad refresh: %+v", c)
	}
	stored := readAuthFile(t, path)
	var settings struct{ Settings map[string]any }
	if err := json.Unmarshal([]byte(stored), &settings); err != nil || settings.Settings["authtest_username"] != "persisted-user" {
		t.Fatalf("refresh rewrote username: %s (%v)", stored, err)
	}
	f.refresh = func(string) (*Token, error) { t.Fatal("fresh token should not refresh"); return nil, nil }
	if err := RefreshDeviceToken(fakeProvider{}, f, 90*time.Minute); err != nil || readAuthFile(t, path) != stored {
		t.Fatalf("unneeded refresh: %v", err)
	}
}

func TestRefreshSupersededInFlight(t *testing.T) {
	path := setupAuth(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	f := &fakeFlow{refresh: func(string) (*Token, error) {
		close(entered)
		<-release
		return &Token{AccessToken: "stale-refresh"}, nil
	}}
	go func() { defer close(done); _ = RefreshDeviceToken(fakeProvider{}, f, 90*time.Minute) }()
	waitAuth(t, entered)
	runAuth(&fakeFlow{}, BeginAuth(fakeProvider{}.ID()))
	stored := readAuthFile(t, path)
	close(release)
	waitAuth(t, done)
	if Credentials(fakeProvider{}.ID()).Token != "new" || readAuthFile(t, path) != stored {
		t.Fatal("old refresh overwrote re-linked account")
	}
}

func TestDeauthorizeSupersedesLogin(t *testing.T) {
	setupAuth(t)
	current := BeginAuth(fakeProvider{}.ID())
	if err := DeauthorizeDevice(fakeProvider{}, &fakeFlow{}); err != nil {
		t.Fatal(err)
	}
	runAuth(&fakeFlow{}, current)
	if Credentials(fakeProvider{}.ID()).Token != "" {
		t.Fatal("old login restored deauthorized credentials")
	}
}

func TestDeviceAuthPendingAndSlowdown(t *testing.T) {
	setupAuth(t)
	var polls []time.Time
	f := &fakeFlow{poll: func() (*Token, error) {
		polls = append(polls, time.Now())
		switch len(polls) {
		case 1:
			return nil, ErrAuthPending
		case 2:
			return nil, ErrSlowDown
		}
		return &Token{AccessToken: "after-slowdown"}, nil
	}}
	runAuth(f, BeginAuth(fakeProvider{}.ID()))
	if len(polls) != 3 || polls[2].Sub(polls[1]) < 5*time.Second || Credentials(fakeProvider{}.ID()).Token != "after-slowdown" {
		t.Fatal("pending/slowdown polling failed")
	}
}

func TestRefreshFailurePreservesCredentials(t *testing.T) {
	for _, tc := range []struct {
		name  string
		token *Token
		err   error
	}{
		{name: "upstream error", err: errors.New("refresh failed")},
		{name: "nil token"}, {name: "empty token", token: &Token{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := setupAuth(t)
			stored, creds := readAuthFile(t, path), *Credentials(fakeProvider{}.ID())
			f := &fakeFlow{refresh: func(string) (*Token, error) { return tc.token, tc.err }}
			if err := RefreshDeviceToken(fakeProvider{}, f, 90*time.Minute); err == nil {
				t.Fatal("invalid refresh accepted")
			}
			if *Credentials(fakeProvider{}.ID()) != creds || readAuthFile(t, path) != stored {
				t.Fatal("failed refresh changed credentials")
			}
		})
	}
}

type fakeLoginDialog struct {
	canceled atomic.Bool
	updates  atomic.Int32
}

func (d *fakeLoginDialog) Update(int, string, string, string) { d.updates.Add(1) }
func (d *fakeLoginDialog) IsCanceled() bool                   { return d.canceled.Load() }

func TestWaitForLogin(t *testing.T) {
	// the login ends while the instructions are up: nothing to confirm
	done := make(chan bool, 1)
	done <- true
	if err := waitForLogin("x", done, &fakeLoginDialog{}, time.Minute, "m"); err != nil {
		t.Errorf("success: %v", err)
	}
	done <- false
	if err := waitForLogin("x", done, &fakeLoginDialog{}, time.Minute, "m"); err == nil {
		t.Error("a failed login returned no error")
	}

	// a cancel makes this login stale, so its poller stops
	current := BeginAuth("cancel-test")
	d := &fakeLoginDialog{}
	d.canceled.Store(true)
	if err := waitForLogin("cancel-test", make(chan bool), d, time.Minute, "m"); err != errAuthCanceled {
		t.Errorf("cancel: %v", err)
	}
	if current() {
		t.Error("the poller of a canceled login would go on")
	}

	// the bar follows the time left while waiting
	d = &fakeLoginDialog{}
	slow := make(chan bool, 1)
	go func() { time.Sleep(1200 * time.Millisecond); slow <- true }()
	if err := waitForLogin("x", slow, d, time.Minute, "m"); err != nil || d.updates.Load() == 0 {
		t.Errorf("waiting: %v, %d updates", err, d.updates.Load())
	}
}
