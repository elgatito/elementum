//go:build !cgo || windows

// On Windows os.Getenv already reads the live process environment, so no libc lookup is needed (and MinGW has no setenv).

package config

import "os"

func lookupEnv(key string) string { return os.Getenv(key) }

func cSetenv(key, value string) { os.Setenv(key, value) }
