// +build !arm

package bittorrent

// Nothing to do on regular devices
func getPlatformSpecificConnectionLimit() int {
	return 75
}
