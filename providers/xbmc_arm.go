// +build arm

package providers

import (
	"runtime"
	"time"
)

func providerTimeout() time.Duration {
	if runtime.NumCPU() == 1 {
		return 50 * time.Second
	}
	return 40 * time.Second
}
