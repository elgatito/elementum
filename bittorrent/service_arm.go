// +build arm

package bittorrent

import (
	"runtime"
)

const (
	maxSingleCoreConnections = 50
)

// On Raspberry Pi, we need to limit the number of active connections
// because otherwise it fries. So here we need to detect that we are on RPi
// (or, rather, a single cpu arm machine, no need to be specific to RPi) and
// set those limits.
// See https://github.com/steeve/plugin.video.pulsar/issues/24
func setPlatformSpecificSettings(settings *BTConfiguration) {
	if runtime.NumCPU() == 1 { // single core?
		settings.ConnectionsLimit = maxSingleCoreConnections
	}
}
