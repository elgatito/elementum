package ip

import "testing"

func TestLocalIPFor(t *testing.T) {
	if got, err := LocalIPFor("127.0.0.1"); err != nil || !got.IsLoopback() {
		t.Errorf("got %v, %v, want loopback", got, err)
	}
}
