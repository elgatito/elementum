//go:build !cgo

package config

import "os"

func lookupEnv(key string) string { return os.Getenv(key) }

func cSetenv(key, value string) { os.Setenv(key, value) }
