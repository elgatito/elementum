package burst

import (
	"bytes"
	"cmp"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cookie sync, as in Burst: a browser extension keeps the browser's cookies in a GitHub gist, per
// domain and optionally encrypted. Before a search they are put in the jar; after it, the cookies the
// sites changed go back to the gist, for the domains that came from it.

// GistAPI is GitHub's gists endpoint; tests point it at a local server.
var GistAPI = "https://api.github.com/gists"

const syncEvery = 5 * time.Minute // shortcut: the gist is not re-read on every search

type cookieSync struct {
	mu        sync.Mutex
	at        time.Time
	token     string
	data      map[string]any              // the gist file: domain -> its cookies as text
	raw       map[string][]map[string]any // the cookies of each domain, as the browser stored them
	userAgent string
}

var syncState cookieSync

// SyncedUserAgent is the browser the synced cookies belong to, "" without a sync.
func SyncedUserAgent() string {
	syncState.mu.Lock()
	defer syncState.mu.Unlock()
	return syncState.userAgent
}

// evpKey derives the AES key and IV as OpenSSL's EVP_BytesToKey does with MD5 and no salt.
func evpKey(password string) (key, iv []byte) {
	var d, last []byte
	for len(d) < 32 {
		sum := md5.Sum(append(last, password...))
		last = sum[:]
		d = append(d, last...)
	}
	return d[:16], d[16:32]
}

func aesDecode(text, password string) (string, error) {
	data, err := hex.DecodeString(text)
	if err != nil || len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return "", errors.New("not an encrypted value")
	}
	key, iv := evpKey(password)
	block, _ := aes.NewCipher(key)
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(data, data)
	pad := int(data[len(data)-1])
	if pad < 1 || pad > aes.BlockSize || pad > len(data) {
		return "", errors.New("wrong cookie sync password")
	}
	return string(data[:len(data)-pad]), nil
}

func aesEncode(text, password string) string {
	key, iv := evpKey(password)
	block, _ := aes.NewCipher(key)
	pad := aes.BlockSize - len(text)%aes.BlockSize
	data := append([]byte(text), bytes.Repeat([]byte{byte(pad)}, pad)...)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(data, data)
	return hex.EncodeToString(data)
}

func github(ctx context.Context, method, target, token string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := *Client()
	client.Jar = nil
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err == nil && resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("GitHub answered %d", resp.StatusCode)
	}
	return b, err
}

type gist struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Updated     string `json:"updated_at"`
	Files       map[string]struct {
		Filename  string `json:"filename"`
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
		RawURL    string `json:"raw_url"`
	} `json:"files"`
}

// gistContent returns the cookies file of a gist, "" when the gist has none.
func gistContent(ctx context.Context, id, token, filename string) (string, error) {
	b, err := github(ctx, http.MethodGet, GistAPI+"/"+id, token, nil)
	if err != nil {
		return "", err
	}
	var g gist
	if err := json.Unmarshal(b, &g); err != nil {
		return "", err
	}
	for _, f := range g.Files {
		if f.Filename != filename {
			continue
		}
		if f.Content != "" && !f.Truncated {
			return f.Content, nil
		}
		raw, err := github(ctx, http.MethodGet, f.RawURL, token, nil)
		return string(raw), err
	}
	return "", nil
}

// gistsWithFile returns the gists that have the cookies file, most recently updated first.
func gistsWithFile(ctx context.Context, token, filename string) []gist {
	var found []gist
	for page := 1; page <= 10; page++ { // the list is paged
		b, err := github(ctx, http.MethodGet, fmt.Sprintf("%s?per_page=100&page=%d", GistAPI, page), token, nil)
		var items []gist
		if err != nil || json.Unmarshal(b, &items) != nil || len(items) == 0 {
			break
		}
		for _, g := range items {
			for _, f := range g.Files {
				if f.Filename == filename {
					found = append(found, g)
				}
			}
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].Updated > found[j].Updated })
	return found
}

// Gist is a gist with the cookies file, for the user to choose the one to sync with.
type Gist struct {
	ID, Description string
	Updated         string // as GitHub gives it, "2026-11-15T17:20:00Z"
	Cookies, Sites  int    // both 0 when the file could not be read, as with a wrong password
}

// ListGists returns the gists of the signed-in GitHub account that have the cookies file, most recently
// updated first, with how many cookies of how many sites each holds.
func ListGists(ctx context.Context, dir string) ([]Gist, error) {
	opts := LoadGeneral(dir)
	token, password, filename := opts.raw("cookie_sync_token"), opts.raw("cookie_sync_password"), opts.raw("cookie_sync_filename")
	if token == "" {
		return nil, errors.New("GitHub is not connected")
	}
	var out []Gist
	for _, g := range gistsWithFile(ctx, token, filename) {
		item := Gist{ID: g.ID, Description: g.Description, Updated: g.Updated}
		if content, err := gistContent(ctx, g.ID, token, filename); err == nil {
			item.Cookies, item.Sites = countCookies(content, password)
		}
		out = append(out, item)
	}
	return out, nil
}

// countCookies counts the cookies and the sites of a cookies file.
func countCookies(content, password string) (cookies, sites int) {
	var data map[string]any
	if json.Unmarshal([]byte(content), &data) != nil {
		return 0, 0
	}
	for domain, v := range data {
		if strings.HasPrefix(domain, "__") || domain == "userAgent" {
			continue
		}
		text, _ := v.(string)
		if password != "" {
			var err error
			if text, err = aesDecode(text, password); err != nil {
				continue
			}
		}
		var list []any
		if json.Unmarshal([]byte(text), &list) == nil {
			cookies += len(list)
			sites++
		}
	}
	return cookies, sites
}

// GistInfo is how a gist is shown: its description and id, as Burst shows the active gist.
func GistInfo(description, id string) string {
	if description == "" {
		return id
	}
	return description + " (" + id + ")"
}

// SelectGist makes the cookie sync use a gist from now on.
func SelectGist(dir, id, description string) error {
	if err := SaveOption(dir, "cookie_sync_gist_id", id); err != nil {
		return err
	}
	if err := SaveOption(dir, "cookie_sync_info", GistInfo(description, id)); err != nil {
		return err
	}
	syncState.mu.Lock()
	syncState.at = time.Time{} // read it on the next search
	syncState.mu.Unlock()
	return nil
}

// pull reads the gist into the jar, when the sync is on and was not read a moment ago. The gist's
// cookies replace the local ones of the same name.
func (s *cookieSync) pull(ctx context.Context, dir string, opts General) error {
	token, password, filename := opts.raw("cookie_sync_token"), opts.raw("cookie_sync_password"), opts.raw("cookie_sync_filename")
	s.mu.Lock()
	defer s.mu.Unlock()
	if !opts.on("cookie_sync_enabled") || token == "" {
		s.data, s.raw, s.userAgent = nil, nil, ""
		return nil
	}
	if s.token == token && s.data != nil && time.Since(s.at) < syncEvery {
		return nil
	}
	id := opts.raw("cookie_sync_gist_id")
	content := ""
	var err error
	if id != "" {
		content, err = gistContent(ctx, id, token, filename)
	}
	if content == "" { // not set, or gone: the latest gist with the file
		if found := gistsWithFile(ctx, token, filename); len(found) > 0 {
			id = found[0].ID
			if content, err = gistContent(ctx, id, token, filename); content != "" && dir != "" {
				_ = SaveOption(dir, "cookie_sync_gist_id", id)
				_ = SaveOption(dir, "cookie_sync_info", GistInfo(found[0].Description, id))
			}
		}
	}
	if content == "" {
		if err == nil {
			err = errors.New("no gist with the cookies file")
		}
		return fmt.Errorf("cookie sync: %w", err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return fmt.Errorf("cookie sync: %w", err)
	}
	decode := func(v any) (string, error) {
		text, _ := v.(string)
		if password == "" {
			return text, nil
		}
		return aesDecode(text, password)
	}
	raw := map[string][]map[string]any{}
	userAgent := ""
	now := float64(time.Now().Unix())
	for domain, v := range data {
		text, err := decode(v)
		if domain == "__userAgent" || domain == "userAgent" {
			userAgent = text
			continue
		}
		var cookies []map[string]any
		if err != nil || len(domain) > 1 && domain[:2] == "__" || json.Unmarshal([]byte(text), &cookies) != nil {
			if err != nil {
				log.Warningf("Cookie sync: %s: %s", domain, err)
			}
			continue
		}
		raw[domain] = cookies
		for _, c := range cookies {
			str := func(k string) string { v, _ := c[k].(string); return v }
			flag := func(k string) bool { v, _ := c[k].(bool); return v }
			expires, _ := c["expirationDate"].(float64)
			if expires == 0 {
				expires = now + 1000*86400
			} else if expires <= now {
				log.Warningf("Cookie sync: %s for %s has expired, sync the browser again to use it", str("name"), domain)
				continue
			}
			jar.set(Cookie{Domain: domain, Name: str("name"), Path: str("path"), Value: str("value"), Secure: flag("secure"),
				HTTPOnly: flag("httpOnly"), HostOnly: flag("hostOnly"), Expires: float64(int64(expires))})
		}
	}
	s.at, s.token, s.data, s.raw, s.userAgent = time.Now(), token, data, raw, userAgent
	log.Infof("Cookie sync: cookies of %d domains", len(raw))
	return nil
}

// push sends the cookies that sites changed since the pull back to the gist.
func (s *cookieSync) push(ctx context.Context, opts General) {
	password, filename := opts.raw("cookie_sync_password"), opts.raw("cookie_sync_filename")
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.raw) == 0 || !opts.on("cookie_sync_enabled") {
		return
	}
	var changed []string
	for domain, raw := range s.raw {
		known := map[string]map[string]any{}
		for _, c := range raw {
			name, _ := c["name"].(string)
			path, _ := c["path"].(string)
			known[name+"|"+path] = c
		}
		dirty := false
		for _, c := range jar.all(domain) {
			entry := known[c.Name+"|"+c.Path]
			if entry != nil && entry["value"] == c.Value {
				continue
			}
			if entry == nil {
				entry = map[string]any{"domain": c.Domain, "name": c.Name, "path": c.Path, "secure": c.Secure, "httpOnly": c.HTTPOnly, "hostOnly": c.HostOnly, "session": false}
				known[c.Name+"|"+c.Path] = entry
				raw = append(raw, entry)
			}
			entry["value"] = c.Value
			if c.Expires != 0 {
				entry["expirationDate"] = c.Expires
			}
			dirty = true
		}
		if !dirty {
			continue
		}
		s.raw[domain] = raw
		b, _ := json.Marshal(raw)
		if s.data[domain] = string(b); password != "" {
			s.data[domain] = aesEncode(string(b), password)
		}
		changed = append(changed, domain)
	}
	if len(changed) == 0 {
		return
	}
	sort.Strings(changed)
	log.Infof("Cookie sync: sending changed cookies of %v", changed)
	content, _ := json.Marshal(s.data)
	body := map[string]any{"files": map[string]any{filename: map[string]any{"content": string(content)}}}
	if _, err := github(ctx, http.MethodPatch, GistAPI+"/"+opts.raw("cookie_sync_gist_id"), s.token, body); err != nil {
		log.Warningf("Cookie sync: update failed: %s", err)
	}
}

// GitHub's OAuth device login, for the token of the cookie sync: the user opens a page on any device
// and enters a code. The client id is Burst's own GitHub app.
const githubClientID = "Ov23liyj1e79CVhOqohm"

// GitHubLogin is GitHub's login host; tests point it at a local server.
var GitHubLogin = "https://github.com"

// DeviceCode is what the user needs to sign in.
type DeviceCode struct {
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	deviceCode      string
}

func githubForm(ctx context.Context, path string, form url.Values, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GitHubLogin+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := *Client()
	client.Jar = nil
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// StartDeviceLogin asks GitHub for a code to show to the user.
func StartDeviceLogin(ctx context.Context) (*DeviceCode, error) {
	var answer struct {
		DeviceCode
		Device      string `json:"device_code"`
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := githubForm(ctx, "/login/device/code", url.Values{"client_id": {githubClientID}, "scope": {"gist"}}, &answer); err != nil {
		return nil, err
	}
	if answer.Device == "" {
		return nil, fmt.Errorf("GitHub: %s", cmp.Or(answer.Description, answer.Error, "no device code"))
	}
	code := answer.DeviceCode
	code.deviceCode = answer.Device
	if code.Interval <= 0 {
		code.Interval = 5
	}
	if code.ExpiresIn <= 0 {
		code.ExpiresIn = 900
	}
	return &code, nil
}

// ErrLoginExpired is returned by FinishDeviceLogin when the code ran out before the user used it.
var ErrLoginExpired = errors.New("the GitHub code has expired")

// FinishDeviceLogin waits until the user has entered the code (or ctx ends, or the code expires) and
// stores the token: the cookie sync is switched on, and a gist of another account is forgotten.
func FinishDeviceLogin(ctx context.Context, dir string, code *DeviceCode) error {
	interval := time.Duration(code.Interval) * time.Second
	deadline := time.Now().Add(time.Duration(code.ExpiresIn) * time.Second)
	form := url.Values{"client_id": {githubClientID}, "device_code": {code.deviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		var answer struct {
			Token       string `json:"access_token"`
			Error       string `json:"error"`
			Description string `json:"error_description"`
			Interval    int    `json:"interval"`
		}
		if err := githubForm(ctx, "/login/oauth/access_token", form, &answer); err != nil {
			continue // a network hiccup: try again until the code expires
		}
		switch {
		case answer.Token != "":
			return storeToken(ctx, dir, answer.Token)
		case answer.Error == "slow_down":
			interval = time.Duration(cmp.Or(answer.Interval, code.Interval+5)) * time.Second
		case answer.Error != "authorization_pending":
			return fmt.Errorf("GitHub: %s", cmp.Or(answer.Description, answer.Error))
		}
	}
	return ErrLoginExpired
}

func storeToken(ctx context.Context, dir, token string) error {
	// The gist of another account is not reachable with the new token.
	user := ""
	if b, err := github(ctx, http.MethodGet, strings.TrimSuffix(GistAPI, "/gists")+"/user", token, nil); err == nil {
		var u struct {
			Login string `json:"login"`
		}
		_ = json.Unmarshal(b, &u)
		user = u.Login
	}
	old := LoadGeneral(dir)
	if user != old.raw("cookie_sync_user") {
		if err := SaveOption(dir, "cookie_sync_gist_id", ""); err != nil {
			return err
		}
	}
	for id, value := range map[string]any{"cookie_sync_token": token, "cookie_sync_user": user, "cookie_sync_enabled": true} {
		if err := SaveOption(dir, id, value); err != nil {
			return err
		}
	}
	syncState.mu.Lock()
	syncState.at = time.Time{} // read the gist with the new token on the next search
	syncState.mu.Unlock()
	return nil
}
