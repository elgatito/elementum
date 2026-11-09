package tracker

import (
	"testing"
	"time"
)

type fakeProvider struct{ Provider }

func (fakeProvider) ID() string   { return "authtest" }
func (fakeProvider) Name() string { return "AuthTest" }

type fakeFlow struct {
	DeviceFlow
	polls chan struct{}
	user  chan string
	n     int
}

func (*fakeFlow) RequestCode() (*DeviceCode, error) {
	return &DeviceCode{UserCode: "ABCD", VerificationURL: "https://x/activate", ExpiresIn: 60, Interval: 1}, nil
}
func (f *fakeFlow) PollToken(*DeviceCode) (*Token, error) {
	f.polls <- struct{}{}
	if f.n++; f.n == 1 {
		return nil, ErrAuthPending
	}
	return &Token{AccessToken: "tok", ExpiresIn: 100}, nil
}
func (f *fakeFlow) Username(tok string) (string, error) { f.user <- tok; return "me", nil }

func TestStartDeviceAuth(t *testing.T) {
	old := minPollInterval
	minPollInterval = 0
	t.Cleanup(func() { minPollInterval = old })

	f := &fakeFlow{polls: make(chan struct{}, 10), user: make(chan string, 1)}
	code, err := StartDeviceAuth(fakeProvider{}, f)
	if err != nil || code.UserCode != "ABCD" || code.VerificationURL != "https://x/activate" {
		t.Fatalf("code = %+v, %v", code, err)
	}
	// the interval is 1s: pending, then success
	for range 2 {
		select {
		case <-f.polls:
		case <-time.After(5 * time.Second):
			t.Fatal("poller did not poll")
		}
	}
	// success reaches Username with the new token, right before the credentials are saved. Reading them back
	// would race with the poller: config is not synchronized.
	select {
	case tok := <-f.user:
		if tok != "tok" {
			t.Errorf("token = %q", tok)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("token not accepted")
	}
	for start := time.Now(); Logins(fakeProvider{}.ID()) != 1; time.Sleep(10 * time.Millisecond) {
		if time.Since(start) > 5*time.Second {
			t.Fatal("login not counted")
		}
	}
}

func TestBeginAuthSupersedes(t *testing.T) {
	first := BeginAuth("x")
	second := BeginAuth("x")
	if first() || !second() {
		t.Error("only the latest login is current")
	}
}
