package xbmc

// SetResolvedURL ...
func SetResolvedURL(url string) {
	retVal := -1
	executeJSONRPCEx("SetResolvedUrl", &retVal, Args{url})
}
