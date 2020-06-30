// +build !darwin,!freebsd,!dragonfly

package bittorrent

import "os"

func unlockFile(file *os.File) error {
	return nil
}
