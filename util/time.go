package util

import (
	"fmt"
	"os"
	"strconv"
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

// AirDateWithAiredCheck returns the aired date and flag if it was already aired
func AirDateWithAiredCheck(dt string, dateFormat string, allowSameDay bool) (airDate time.Time, isAired bool) {
	airDate, _ = time.Parse(dateFormat, dt)
	now := UTCBod()
	if airDate.After(now) || (!allowSameDay && airDate.Equal(now)) {
		return airDate, false
	}

	return airDate, true
}

func GetTimeFromFile(timeFile string) (time.Time, error) {
	stamp, err := os.ReadFile(timeFile)
	if err != nil {
		return time.Time{}, err
	}

	val, err := strconv.ParseInt(string(stamp), 10, 64)
	if err != nil {
		return time.Time{}, err
	}

	return time.Unix(val, 0).UTC(), nil
}

func SetTimeIntoFile(timeFile string) (time.Time, error) {
	t := time.Now().UTC()
	err := os.WriteFile(timeFile, []byte(fmt.Sprintf("%d", t.Unix())), 0666)

	return t, err
}

func IsTimePassed(last time.Time, period time.Duration) bool {
	return time.Since(last) > period
}
