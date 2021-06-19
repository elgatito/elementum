package library

// DBItem ...
type DBItem struct {
	ID       int `json:"id"`
	State    int `json:"state"`
	Type     int `json:"type"`
	TVShowID int `json:"showid"`
}

type removedEpisode struct {
	ID       int
	ShowID   int
	ShowName string
	Season   int
	Episode  int
}
