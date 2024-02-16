package trakt

func (l *List) IsPersonal() bool {
	return l.User == nil || l.User.Ids.Slug == ""
}

func (l *List) Slug() string {
	if l.User != nil && l.User.Ids.Slug != "" {
		return l.User.Ids.Slug
	}
	return l.IDs.Slug
}

func (l *List) ID() int {
	return l.IDs.Trakt
}
