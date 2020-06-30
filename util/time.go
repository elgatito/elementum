package util

import (
	"time"
)

// NowInt ...
func NowInt() int {
	return int(time.Now().UTC().Unix())
}

// NowInt64 ...
func NowInt64() int64 {
	return time.Now().UTC().Unix()
}

// NowPlusSecondsInt ..
func NowPlusSecondsInt(seconds int) int {
	return int(time.Now().UTC().Add(time.Duration(seconds) * time.Second).Unix())
}

// Bod returns the start of a day for specific date
func Bod(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}

// UTCBod returns the start of a day for Now().UTC()
func UTCBod() time.Time {
	t := time.Now().UTC()
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, t.Location())
}
