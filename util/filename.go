package util

import "strings"

var audioExtensions = []string{
	".nsv",
	".m4a",
	".flac",
	".aac",
	".strm",
	".pls",
	".rm",
	".rma",
	".mpa",
	".wav",
	".wma",
	".ogg",
	".mp3",
	".mp2",
	".m3u",
	".gdm",
	".imf",
	".m15",
	".sfx",
	".uni",
	".ac3",
	".dts",
	".cue",
	".aif",
	".aiff",
	".wpl",
	".ape",
	".mac",
	".mpc",
	".mp+",
	".mpp",
	".shn",
	".wv",
	".dsp",
	".xsp",
	".xwav",
	".waa",
	".wvs",
	".wam",
	".gcm",
	".idsp",
	".mpdsp",
	".mss",
	".spt",
	".rsd",
	".sap",
	".cmc",
	".cmr",
	".dmc",
	".mpt",
	".mpd",
	".rmt",
	".tmc",
	".tm8",
	".tm2",
	".oga",
	".tta",
	".wtv",
	".mka",
	".tak",
	".opus",
	".dff",
	".dsf",
	".m4b",
}

var srtExtensions = []string{
	".srt",         // SubRip text file
	".ssa", ".ass", // Advanced Substation
	".usf", // Universal Subtitle Format
	".cdg",
	".idx", // VobSub
	".sub", // MicroDVD or SubViewer
	".utf",
	".aqt", // AQTitle
	".jss", // JacoSub
	".psb", // PowerDivX
	".rt",  // RealText
	".smi", // SAMI
	// ".txt", // MPEG 4 Timed Text
	".smil",
	".stl", // Spruce Subtitle Format
	".dks",
	".pjs", // Phoenix Subtitle
	".mpl2",
	".mks",
}

// ToFileName ...
func ToFileName(filename string) string {
	reserved := []string{"<", ">", ":", "\"", "/", "\\", "", "", "?", "*", "%", "+"}
	for _, reservedchar := range reserved {
		filename = strings.Replace(filename, reservedchar, "", -1)
	}
	return filename
}

// IsSubtitlesExt checks if extension belong to Subtitles type
func IsSubtitlesExt(ext string) bool {
	for _, e := range srtExtensions {
		if ext == e {
			return true
		}
	}

	return false
}

// HasSubtitlesExt searches different subtitles extensions in file name
func HasSubtitlesExt(filename string) bool {
	for _, e := range srtExtensions {
		if strings.HasSuffix(filename, e) {
			return true
		}
	}

	return false
}

// IsAudioExt checks if extension belong to Audio type
func IsAudioExt(ext string) bool {
	for _, e := range audioExtensions {
		if ext == e {
			return true
		}
	}

	return false
}

// HasAudioExt searches different audio extensions in file name
func HasAudioExt(filename string) bool {
	for _, e := range audioExtensions {
		if strings.HasSuffix(filename, e) {
			return true
		}
	}

	return false
}
