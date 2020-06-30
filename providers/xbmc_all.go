// +build !arm

package providers

import "time"

func providerTimeout() time.Duration {
	return 40 * time.Second
}
