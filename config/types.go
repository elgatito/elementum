package config

import "github.com/elgatito/elementum/xbmc"

type ConfigFormat string

type ConfigBundle struct {
	Info           *xbmc.AddonInfo
	Platform       *xbmc.Platform
	Settings       XbmcSettings
	Language       string
	SecondLanguage string
	Region         string
}

const (
	JSONConfigFormat ConfigFormat = "json"
	YamlConfigFormat ConfigFormat = "yaml"
)

const (
	// StorageFile ...
	StorageFile int = iota
	// StorageMemory ...
	StorageMemory
)

var (
	// Storages ...
	Storages = []string{
		"File",
		"Memory",
	}
)
