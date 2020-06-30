package util

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// TrailerURL returns trailer url, constructed for Kodi
func TrailerURL(u string) (ret string) {
	if len(u) == 0 {
		return
	}

	if strings.Contains(u, "?v=") {
		ret = fmt.Sprintf("plugin://plugin.video.youtube/play/?video_id=%s", strings.Split(u, "?v=")[1])
	} else {
		ret = fmt.Sprintf("plugin://plugin.video.youtube/play/?video_id=%s", u)
	}

	return
}

// DecodeFileURL decodes file path from url
func DecodeFileURL(u string) (ret string) {
	us := strings.Split(u, string("/"))
	for _, v := range us {
		v, _ = url.PathUnescape(v)
	}

	return strings.Join(us, string(os.PathSeparator))
}

// EncodeFileURL encode file path into proper url
func EncodeFileURL(u string) (ret string) {
	us := strings.Split(u, string(os.PathSeparator))
	for i, v := range us {
		us[i] = url.PathEscape(v)
	}

	return strings.Join(us, "/")
}
