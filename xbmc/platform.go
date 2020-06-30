package xbmc

// Platform ...
type Platform struct {
	OS      string
	Arch    string
	Version string
	Kodi    int
	Build   string
}

// GetPlatform ...
func GetPlatform() *Platform {
	retVal := Platform{}
	executeJSONRPCEx("GetPlatform", &retVal, nil)
	return &retVal
}
