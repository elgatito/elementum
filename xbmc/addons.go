package xbmc

import "encoding/xml"

// AddonsList ...
type AddonsList struct {
	Addons []*struct {
		ID      string `json:"addonid"`
		Type    string `json:"type"`
		Name    string `json:"name"`
		Version string `json:"version"`
		Enabled bool   `json:"enabled"`
	} `json:"addons"`
}

// AddonInfoDetails ...
type AddonInfoDetails struct {
	Addon struct {
		ID        string `json:"addonid"`
		Type      string `json:"type"`
		Name      string `json:"name"`
		Version   string `json:"version"`
		Enabled   bool   `json:"enabled"`
		Installed bool   `json:"installed"`
	} `json:"addon"`
}

// GetAddons ...
func GetAddons(args ...interface{}) *AddonsList {
	addons := AddonsList{}
	executeJSONRPC("Addons.GetAddons", &addons, args)
	return &addons
}

// UpdateLocalAddons ...
func UpdateLocalAddons() (ret string) {
	executeJSONRPCEx("UpdateLocalAddons", &ret, nil)
	return
}

// InstallAddon ...
func InstallAddon(addonID string) (ret string) {
	executeJSONRPCEx("InstallAddon", &ret, Args{addonID})
	return
}

// SetAddonEnabled ...
func SetAddonEnabled(addonID string, enabled bool) (retval string) {
	executeJSONRPC("Addons.SetAddonEnabled", &retval, Args{addonID, enabled})
	return
}

// ExecuteAddon ...
func ExecuteAddon(addonID string, args ...interface{}) {
	var retVal string
	executeJSONRPC("Addons.ExecuteAddon", &retVal, Args{addonID, args})
}

// GetAddonDetails ...
func GetAddonDetails(addonID string) AddonInfoDetails {
	params := map[string]interface{}{
		"addonid": addonID,
		"properties": []interface{}{
			"enabled",
			"installed",
		}}

	addon := AddonInfoDetails{}
	executeJSONRPCO("Addons.GetAddonDetails", &addon, params)
	return addon
}

// IsAddonInstalled ...
func IsAddonInstalled(addonID string) bool {
	addon := GetAddonDetails(addonID)
	return addon.Addon.ID != "" && addon.Addon.Installed
}

// IsAddonEnabled ...
func IsAddonEnabled(addonID string) bool {
	addon := GetAddonDetails(addonID)
	return addon.Addon.ID != "" && addon.Addon.Enabled
}

// Addon ...
type Addon struct {
	XMLName      xml.Name          `xml:"addon"`
	ID           string            `xml:"id,attr"`
	Name         string            `xml:"name,attr"`
	Version      string            `xml:"version,attr"`
	ProviderName string            `xml:"provider-name,attr"`
	Requires     []*AddonImport    `xml:"requires>import,omitempty"`
	Extensions   []*AddonExtension `xml:"extension"`
}

// AddonImport ...
type AddonImport struct {
	XMLName  xml.Name `xml:"import"`
	Addon    string   `xml:"addon,attr"`
	Version  string   `xml:"version,attr"`
	Optional string   `xml:"optional,attr,omitempty"`
}

// AddonText ...
type AddonText struct {
	Text string `xml:",chardata"`
	Lang string `xml:"lang,attr"`
}

// AddonRepositoryInfo ...
type AddonRepositoryInfo struct {
	XMLName    xml.Name `xml:"info"`
	Text       string   `xml:",chardata"`
	Compressed bool     `xml:"compressed,attr"`
}

// AddonRepositoryDataDir ...
type AddonRepositoryDataDir struct {
	XMLName xml.Name `xml:"datadir"`
	Text    string   `xml:",chardata"`
	Zip     bool     `xml:"zip,attr"`
}

// AddonSkinResolution ...
type AddonSkinResolution struct {
	XMLName xml.Name `xml:"res"`
	Width   int      `xml:"width,attr"`
	Height  int      `xml:"height,attr"`
	Aspect  string   `xml:"aspect,attr"`
	Default bool     `xml:"default,attr"`
	Folder  string   `xml:"folder,attr"`
}

// AddonExtension ...
type AddonExtension struct {
	Point string `xml:"point,attr"`

	// xbmc.python.pluginsource
	// xbmc.service
	Library string `xml:"library,attr,omitempty"`

	// xbmc.python.pluginsource
	Provides string `xml:"provides,omitempty"`

	// xbmc.service
	Start string `xml:"start,attr,omitempty"`

	// xbmc.addon.metadata
	Language     string       `xml:"language,omitempty"`
	Platform     string       `xml:"platform,omitempty"`
	License      string       `xml:"license,omitempty"`
	Forum        string       `xml:"forum,omitempty"`
	Website      string       `xml:"website,omitempty"`
	Email        string       `xml:"email,omitempty"`
	Source       string       `xml:"source,omitempty"`
	Broken       string       `xml:"broken,omitempty"`
	Summaries    []*AddonText `xml:"summary,omitempty"`
	Disclaimers  []*AddonText `xml:"disclaimer,omitempty"`
	Descriptions []*AddonText `xml:"description,omitempty"`

	// xbmc.addon.repository
	Name     string                  `xml:"name,attr,omitempty"`
	Info     *AddonRepositoryInfo    `xml:"info,omitempty"`
	Checksum string                  `xml:"checksum,omitempty"`
	Datadir  *AddonRepositoryDataDir `xml:"datadir,omitempty"`

	// xbmc.gui.skin
	DefaultResolution     string                 `xml:"defaultresolution,omitempty"`
	DefaultResolutionWide string                 `xml:"defaultresolutionwide,omitempty"`
	DefaultThemeName      string                 `xml:"defaultthemename,omitempty"`
	EffectsSlowdown       string                 `xml:"effectslowdown,omitempty"`
	Debugging             string                 `xml:"debugging,omitempty"`
	Resolutions           []*AddonSkinResolution `xml:"res,omitempty"`
}

// AddonList ...
type AddonList struct {
	XMLName xml.Name `xml:"addons"`
	Addons  []Addon
}
