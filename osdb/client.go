package osdb

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/kolo/xmlrpc"
	"github.com/op/go-logging"
)

const (
	// DefaultOSDBServer ...
	DefaultOSDBServer = "https://api.opensubtitles.org/xml-rpc"
	// DefaultUserAgent ...
	DefaultUserAgent = "XBMC_Subtitles_v1" // XBMC OpenSubtitles Agent
	// SearchLimit ...
	SearchLimit = 100
	// StatusSuccess ...
	StatusSuccess = "200 OK"
)

var log = logging.MustGetLogger("osdb")

// Client ...
type Client struct {
	UserAgent string
	Token     string
	Login     string
	Password  string
	Language  string
	*xmlrpc.Client
}

// Movie ...
type Movie struct {
	ID             string            `xmlrpc:"id"`
	Title          string            `xmlrpc:"title"`
	Cover          string            `xmlrpc:"cover"`
	Year           string            `xmlrpc:"year"`
	Duration       string            `xmlrpc:"duration"`
	TagLine        string            `xmlrpc:"tagline"`
	Plot           string            `xmlrpc:"plot"`
	Goofs          string            `xmlrpc:"goofs"`
	Trivia         string            `xmlrpc:"trivia"`
	Cast           map[string]string `xmlrpc:"cast"`
	Directors      map[string]string `xmlrpc:"directors"`
	Writers        map[string]string `xmlrpc:"writers"`
	Awards         string            `xmlrpc:"awards"`
	Genres         []string          `xmlrpc:"genres"`
	Countries      []string          `xmlrpc:"country"`
	Languages      []string          `xmlrpc:"language"`
	Certifications []string          `xmlrpc:"certification"`
}

// SearchPayload ...
type SearchPayload struct {
	Query     string `xmlrpc:"query"`
	Hash      string `xmlrpc:"moviehash"`
	Size      int64  `xmlrpc:"moviebytesize"`
	IMDBId    string `xmlrpc:"imdbid"`
	Languages string `xmlrpc:"sublanguageid"`
}

// Movies A collection of movies.
type Movies []Movie

// Empty ...
func (m Movies) Empty() bool {
	return len(m) == 0
}

// NewClient ...
func NewClient() (*Client, error) {
	rpc, err := xmlrpc.NewClient(DefaultOSDBServer, nil)
	if err != nil {
		return nil, err
	}

	c := &Client{
		UserAgent: DefaultUserAgent,
		Client:    rpc, // xmlrpc.Client
	}

	return c, nil
}

// SearchSubtitles ...
func (c *Client) SearchSubtitles(payloads []SearchPayload) (Subtitles, error) {
	res := struct {
		Data Subtitles `xmlrpc:"data"`
	}{}

	args := []interface{}{
		c.Token,
		payloads,
	}
	if err := c.Call("SearchSubtitles", args, &res); err != nil {
		log.Errorf("Could not search subtitles: %s", err)
		if !strings.Contains(err.Error(), "type mismatch") {
			return nil, err
		}
	}
	return res.Data, nil
}

// SearchOnImdb Search movies on IMDB.
func (c *Client) SearchOnImdb(q string) (Movies, error) {
	params := []interface{}{c.Token, q}
	res := struct {
		Status string `xmlrpc:"status"`
		Data   Movies `xmlrpc:"data"`
	}{}
	if err := c.Call("SearchMoviesOnIMDB", params, &res); err != nil {
		return nil, err
	}
	if res.Status != StatusSuccess {
		return nil, fmt.Errorf("SearchMoviesOnIMDB error: %s", res.Status)
	}
	return res.Data, nil
}

// GetImdbMovieDetails Get movie details from IMDB.
func (c *Client) GetImdbMovieDetails(id string) (*Movie, error) {
	params := []interface{}{c.Token, id}
	res := struct {
		Status string `xmlrpc:"status"`
		Data   Movie  `xmlrpc:"data"`
	}{}
	if err := c.Call("GetIMDBMovieDetails", params, &res); err != nil {
		return nil, err
	}
	if res.Status != StatusSuccess {
		return nil, fmt.Errorf("GetIMDBMovieDetails error: %s", res.Status)
	}
	return &res.Data, nil
}

// DownloadSubtitles Download subtitles by file ID.
func (c *Client) DownloadSubtitles(ids []int) ([]SubtitleFile, error) {
	params := []interface{}{c.Token, ids}
	res := struct {
		Status string         `xmlrpc:"status"`
		Data   []SubtitleFile `xmlrpc:"data"`
	}{}
	if err := c.Call("DownloadSubtitles", params, &res); err != nil {
		return nil, err
	}
	if res.Status != StatusSuccess {
		return nil, fmt.Errorf("DownloadSubtitles error: %s", res.Status)
	}
	return res.Data, nil
}

// Download Save subtitle file to disk, using the OSDB specified name.
func (c *Client) Download(s *Subtitle) error {
	return c.DownloadTo(s, s.SubFileName)
}

// DownloadTo Save subtitle file to disk, using the specified path.
func (c *Client) DownloadTo(s *Subtitle, path string) (err error) {
	id, err := strconv.Atoi(s.IDSubtitleFile)
	if err != nil {
		return
	}

	// Download
	files, err := c.DownloadSubtitles([]int{id})
	if err != nil {
		return
	}
	if len(files) == 0 {
		return fmt.Errorf("No file match this subtitle ID")
	}

	// Save to disk.
	r, err := files[0].Reader()
	if err != nil {
		return
	}
	defer r.Close()

	w, err := os.Create(path)
	if err != nil {
		return
	}
	defer w.Close()

	_, err = io.Copy(w, r)
	return
}

// HasSubtitlesForFiles Checks whether OSDB already has subtitles for a movie and subtitle
// files.
func (c *Client) HasSubtitlesForFiles(movieFile string, subFile string) (bool, error) {
	subtitle, err := NewSubtitleWithFile(movieFile, subFile)
	if err != nil {
		return true, err
	}
	return c.HasSubtitles(Subtitles{subtitle})
}

// HasSubtitles Checks whether subtitles already exists in OSDB. The mandatory fields in the
// received Subtitle slice are: SubHash, SubFileName, MovieHash, MovieByteSize,
// and MovieFileName.
func (c *Client) HasSubtitles(subs Subtitles) (bool, error) {
	subArgs, err := subs.toUploadParams()
	if err != nil {
		return true, err
	}
	args := []interface{}{c.Token, subArgs}
	res := struct {
		Status string `xmlrpc:"status"`
		Exists int    `xmlrpc:"alreadyindb"`
	}{}
	if err := c.Call("TryUploadSubtitles", args, &res); err != nil {
		return true, err
	}
	if res.Status != StatusSuccess {
		return true, fmt.Errorf("HasSubtitles: %s", res.Status)
	}

	return res.Exists == 1, nil
}

// Noop Keep session alive
func (c *Client) Noop() (err error) {
	res := struct {
		Status string `xmlrpc:"status"`
	}{}
	err = c.Call("NoOperation", []interface{}{c.Token}, &res)
	if err == nil && res.Status != StatusSuccess {
		err = fmt.Errorf("NoOp: %s", res.Status)
	}
	return
}

// LogIn to the API, and return a session token.
func (c *Client) LogIn(user string, pass string, lang string) (err error) {
	c.Login = user
	c.Password = pass
	c.Language = lang
	args := []interface{}{user, pass, "en", c.UserAgent}
	res := struct {
		Status string `xmlrpc:"status"`
		Token  string `xmlrpc:"token"`
	}{}
	if err = c.Call("LogIn", args, &res); err != nil {
		return
	}

	if res.Status != StatusSuccess {
		return fmt.Errorf("Login: %s", res.Status)
	}
	c.Token = res.Token
	return
}

// LogOut ...
func (c *Client) LogOut() (err error) {
	args := []interface{}{c.Token}
	res := struct {
		Status string `xmlrpc:"status"`
	}{}
	return c.Call("LogOut", args, &res)
}

// Build query parameters for hash-based movie search.
func (c *Client) fileToParams(path string, langs []string) (*[]interface{}, error) {
	// File size
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	fi, err := file.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()

	// File hash
	h, err := HashFile(file)
	if err != nil {
		return nil, err
	}

	params := []interface{}{
		c.Token,
		[]struct {
			Hash  string `xmlrpc:"moviehash"`
			Size  int64  `xmlrpc:"moviebytesize"`
			Langs string `xmlrpc:"sublanguageid"`
		}{{
			h,
			size,
			strings.Join(langs, ","),
		}},
	}
	return &params, nil
}
