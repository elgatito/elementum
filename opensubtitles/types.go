package opensubtitles

import "time"

type LoginPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type User struct {
	AllowedTranslations int    `json:"allowed_translations"`
	AllowedDownloads    int    `json:"allowed_downloads"`
	Level               string `json:"level"`
	UserID              int    `json:"user_id"`
	ExtInstalled        bool   `json:"ext_installed"`
	Vip                 bool   `json:"vip"`
}

type LoginResponse struct {
	User    User   `json:"user"`
	Token   string `json:"token"`
	Status  int    `json:"status"`
	BaseURL string `json:"base_url"`
}

type Uploader struct {
	UploaderID any    `json:"uploader_id"`
	Name       string `json:"name"`
	Rank       string `json:"rank"`
}

type FeatureDetails struct {
	FeatureID   int    `json:"feature_id"`
	FeatureType string `json:"feature_type"`
	Year        int    `json:"year"`
	Title       string `json:"title"`
	MovieName   string `json:"movie_name"`
	ImdbID      int    `json:"imdb_id"`
	TmdbID      int    `json:"tmdb_id"`
}

type RelatedLinks struct {
	Label  string `json:"label"`
	URL    string `json:"url"`
	ImgURL string `json:"img_url"`
}

type File struct {
	FileID   int    `json:"file_id"`
	CdNumber int    `json:"cd_number"`
	FileName string `json:"file_name"`
}

type Attributes struct {
	SubtitleID        string         `json:"subtitle_id"`
	Language          string         `json:"language"`
	DownloadCount     int            `json:"download_count"`
	NewDownloadCount  int            `json:"new_download_count"`
	HearingImpaired   bool           `json:"hearing_impaired"`
	Hd                bool           `json:"hd"`
	Fps               float64        `json:"fps"`
	Votes             int            `json:"votes"`
	Ratings           float64        `json:"ratings"`
	FromTrusted       bool           `json:"from_trusted"`
	ForeignPartsOnly  bool           `json:"foreign_parts_only"`
	UploadDate        time.Time      `json:"upload_date"`
	AiTranslated      bool           `json:"ai_translated"`
	NbCd              int            `json:"nb_cd"`
	MachineTranslated bool           `json:"machine_translated"`
	MovieHashMatch    bool           `json:"moviehash_match"`
	Release           string         `json:"release"`
	Comments          string         `json:"comments"`
	LegacySubtitleID  int            `json:"legacy_subtitle_id"`
	LegacyUploaderID  int            `json:"legacy_uploader_id"`
	Uploader          Uploader       `json:"uploader"`
	FeatureDetails    FeatureDetails `json:"feature_details"`
	URL               string         `json:"url"`
	RelatedLinks      []RelatedLinks `json:"related_links"`
	Files             []File         `json:"files"`
}

type SearchResponseData struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	Attributes Attributes `json:"attributes"`
}

type SearchResponse struct {
	TotalPages int                  `json:"total_pages"`
	TotalCount int                  `json:"total_count"`
	PerPage    int                  `json:"per_page"`
	Page       int                  `json:"page"`
	Data       []SearchResponseData `json:"data"`
}

type SearchPayload struct {
	Type      string `json:"type"`
	Year      int    `json:"year"`
	Season    int    `json:"season"`
	Episode   int    `json:"episode"`
	Query     string `json:"query"`
	Hash      string `json:"moviehash"`
	IMDBId    string `json:"imdb_id"`
	TMDBId    string `json:"tmdb_id"`
	Languages string `json:"languages"`
}

type DownloadResponse struct {
	Link         string    `json:"link"`
	FileName     string    `json:"file_name"`
	Requests     int       `json:"requests"`
	Remaining    int       `json:"remaining"`
	Message      string    `json:"message"`
	ResetTime    string    `json:"reset_time"`
	ResetTimeUtc time.Time `json:"reset_time_utc"`
	Uk           string    `json:"uk"`
	UID          int       `json:"uid"`
	TS           int       `json:"ts"`
}
