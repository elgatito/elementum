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
func (h *XBMCHost) GetAddonInfo() *AddonInfo {
	retVal := AddonInfo{}
	h.executeJSONRPCEx("GetAddonInfo", &retVal, nil)
	return &retVal
}

// AddonSettings ...
func (h *XBMCHost) AddonSettings(addonID string) (retVal string) {
	h.executeJSONRPCEx("AddonSettings", &retVal, Args{addonID})
	return
}

// AddonSettingsOpened ...
func (h *XBMCHost) AddonSettingsOpened() bool {
	retVal := 0
	h.executeJSONRPCEx("AddonSettingsOpened", &retVal, nil)
	return retVal != 0
}

// AddonFailure ...
func (h *XBMCHost) AddonFailure(addonID string) (failures int) {
	h.executeJSONRPCEx("AddonFailure", &failures, Args{addonID})
	return
}

// AddonCheck ...
func (h *XBMCHost) AddonCheck(addonID string) (failures int) {
	h.executeJSONRPCEx("AddonCheck", &failures, Args{addonID})
	return
}

// GetLocalizedString ...
func (h *XBMCHost) GetLocalizedString(id int) (retVal string) {
	h.executeJSONRPCEx("GetLocalizedString", &retVal, Args{id})
	return
}

// GetAllSettings ...
func (h *XBMCHost) GetAllSettings() (retVal []*Setting) {
	h.executeJSONRPCEx("GetAllSettings", &retVal, nil)
	return
}

// GetSettingString ...
func (h *XBMCHost) GetSettingString(id string) (retVal string) {
	h.executeJSONRPCEx("GetSetting", &retVal, Args{id})
	return
}

// GetSettingInt ...
func (h *XBMCHost) GetSettingInt(id string) int {
	val, _ := strconv.Atoi(h.GetSettingString(id))
	return val
}

// GetSettingBool ...
func (h *XBMCHost) GetSettingBool(id string) bool {
	return h.GetSettingString(id) == "true"
}

// SetSetting ...
func (h *XBMCHost) SetSetting(id string, value any) {
	retVal := 0
	h.executeJSONRPCEx("SetSetting", &retVal, Args{id, value})
}

// GetCurrentView ...
func (h *XBMCHost) GetCurrentView() (viewMode string) {
	h.executeJSONRPCEx("GetCurrentView", &viewMode, nil)
	return
}

// OpenDirectory ...
func (h *XBMCHost) OpenDirectory(path string) {
	retVal := 0
	h.executeJSONRPCEx("OpenDirectory", &retVal, Args{path})
}

// Ping ...
func (h *XBMCHost) Ping() bool {
	retVal := false
	h.executeJSONRPCEx("Ping", &retVal, nil)
	return retVal
}
