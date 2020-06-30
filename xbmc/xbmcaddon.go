package xbmc

import "strconv"

// AddonInfo ...
type AddonInfo struct {
	Author      string `xml:"id,attr"`
	Changelog   string
	Description string
	Disclaimer  string
	Fanart      string
	Home        string
	Icon        string
	ID          string
	Name        string
	Path        string
	Profile     string
	TempPath    string
	Stars       string
	Summary     string
	Type        string
	Version     string
	Xbmc        string
}

// Setting ...
type Setting struct {
	Key    string `json:"key"`
	Type   string `json:"type"`
	Value  string `json:"value"`
	Option string `json:"option"`
}

// GetAddonInfo ...
func GetAddonInfo() *AddonInfo {
	retVal := AddonInfo{}
	executeJSONRPCEx("GetAddonInfo", &retVal, nil)
	return &retVal
}

// AddonSettings ...
func AddonSettings(addonID string) (retVal string) {
	executeJSONRPCEx("AddonSettings", &retVal, Args{addonID})
	return
}

// AddonSettingsOpened ...
func AddonSettingsOpened() bool {
	retVal := 0
	executeJSONRPCEx("AddonSettingsOpened", &retVal, nil)
	return retVal != 0
}

// AddonFailure ...
func AddonFailure(addonID string) (failures int) {
	executeJSONRPCEx("AddonFailure", &failures, Args{addonID})
	return
}

// AddonCheck ...
func AddonCheck(addonID string) (failures int) {
	executeJSONRPCEx("AddonCheck", &failures, Args{addonID})
	return
}

// GetLocalizedString ...
func GetLocalizedString(id int) (retVal string) {
	executeJSONRPCEx("GetLocalizedString", &retVal, Args{id})
	return
}

// GetAllSettings ...
func GetAllSettings() (retVal []*Setting) {
	executeJSONRPCEx("GetAllSettings", &retVal, nil)
	return
}

// GetSettingString ...
func GetSettingString(id string) (retVal string) {
	executeJSONRPCEx("GetSetting", &retVal, Args{id})
	return
}

// GetSettingInt ...
func GetSettingInt(id string) int {
	val, _ := strconv.Atoi(GetSettingString(id))
	return val
}

// GetSettingBool ...
func GetSettingBool(id string) bool {
	return GetSettingString(id) == "true"
}

// SetSetting ...
func SetSetting(id string, value interface{}) {
	retVal := 0
	executeJSONRPCEx("SetSetting", &retVal, Args{id, value})
}

// GetCurrentView ...
func GetCurrentView() (viewMode string) {
	executeJSONRPCEx("GetCurrentView", &viewMode, nil)
	return
}

// OpenDirectory ...
func OpenDirectory(path string) {
	retVal := 0
	executeJSONRPCEx("OpenDirectory", &retVal, Args{path})
}
