package util

import (
	"fmt"
)

var Version string

func UserAgent() string {
	return fmt.Sprintf("Quasar/%s", Version[1:len(Version) - 1])
}
