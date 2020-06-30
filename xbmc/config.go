package xbmc

// DialogExpirationType describes dialog autoclose types
type DialogExpirationType struct {
	Default int

	Existing   int
	InTorrents int
}

var (
	// DialogExpiration sets time (in second) for autoclose setting
	DialogExpiration = DialogExpirationType{
		Existing:   7,
		InTorrents: 7,
	}
)
