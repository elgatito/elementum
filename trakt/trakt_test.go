package trakt

import (
	"errors"
	"testing"
	"time"
)

func TestPollTokenSuperseded(t *testing.T) {
	start := time.Now()
	_, err := PollToken(&Code{Interval: 1, ExpiresIn: 60}, func() bool { return false })
	if !errors.Is(err, errSuperseded) || time.Since(start) > 3*time.Second {
		t.Fatalf("err = %v after %s", err, time.Since(start))
	}
}
