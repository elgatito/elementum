package config

import "github.com/elgatito/elementum/xbmc"

type ConfigFormat string

type ConfigBundle struct {
	// Version is the settings file format version; absent (0) is a legacy -exportConfig file.
	Version        int
	Info           *xbmc.AddonInfo
	Platform       *xbmc.Platform
	Settings       XbmcSettings
	Language       string
	SecondLanguage string
	Region         string
	// KodiBufferSize is Kodi's advancedsettings cache size, since a file-mode binary cannot ask Kodi.
	KodiBufferSize int
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

const (
	ImageQualityOriginal int = iota
	ImageQualityHigh
	ImageQualityMedium
	ImageQualityLow
)

var (
	// Storages ...
	Storages = []string{
		"File",
		"Memory",
	}
)
