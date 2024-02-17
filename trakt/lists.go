package trakt

import "github.com/elgatito/elementum/config"

func (l *List) IsPersonal() bool {
	return l.Type == "personal"
}

func (l *List) IsOfficial() bool {
	return l.Type == "official"
}

func (l *List) IsPrivate() bool {
	return l.Privacy == "private"
}

func (l *List) IsPublic() bool {
	return l.Privacy == "public"
}

func (l *List) IsOur() bool {
	return l.User != nil && l.User.Ids.Slug == config.Get().TraktUsername
}

func (l *List) Username() string {
	if l.User != nil && l.User.Ids.Slug != "" {
		return l.User.Ids.Slug
	}
	return l.User.Username
}

func (l *List) ID() int {
	return l.IDs.Trakt
}
