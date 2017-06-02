package util

import (
	"time"
)

func NowInt() int {
	return int(time.Now().Unix())
}

func NowPlusSecondsInt(seconds int) int {
	return int(time.Now().Add(time.Duration(seconds)*time.Second).Unix())
}
