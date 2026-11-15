package burst

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Cookie is a stored cookie, with the field names browser cookie exports (and the cookie sync) use.
type Cookie struct {
	Domain   string  `json:"domain"` // without a leading dot
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	Value    string  `json:"value"`
	Secure   bool    `json:"secure,omitempty"`
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	HostOnly bool    `json:"hostOnly,omitempty"` // only for Domain itself, not its subdomains
	Expires  float64 `json:"expirationDate,omitempty"`
}

func (c Cookie) key() string { return c.Domain + "|" + c.Path + "|" + c.Name }

// domainMatches reports whether host is the cookie domain or one of its subdomains.
func domainMatches(host, domain string) bool {
	host, domain = strings.TrimPrefix(host, "."), strings.TrimPrefix(domain, ".")
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// Jar is the cookie jar of all providers. Unlike net/http/cookiejar it can be listed and is kept on
// disk, so what a site sets is there on the next run and can go back to the cookie sync.
// shortcut: no public-suffix check; a tracker could set a cookie for a whole TLD, which only it would read.
type Jar struct {
	mu      sync.Mutex
	cookies map[string]Cookie
	file    string
	dirty   bool
}

var jar = &Jar{cookies: map[string]Cookie{}}

const cookiesFile = "cookies.json"

// open loads the jar of dir once; an empty dir keeps it in memory.
func (j *Jar) open(dir string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if dir == "" || j.file == filepath.Join(dir, cookiesFile) {
		return
	}
	j.file = filepath.Join(dir, cookiesFile)
	var list []Cookie
	if b, err := os.ReadFile(j.file); err == nil && json.Unmarshal(b, &list) == nil {
		now := float64(time.Now().Unix())
		for _, c := range list {
			if c.Expires == 0 || c.Expires > now {
				j.cookies[c.key()] = c
			}
		}
	}
}

// save writes the jar when it changed. Session cookies are kept too: trackers hand out logins as such.
func (j *Jar) save() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file == "" || !j.dirty {
		return
	}
	list := make([]Cookie, 0, len(j.cookies))
	for _, c := range j.cookies {
		list = append(list, c)
	}
	b, err := json.Marshal(list)
	if err == nil {
		tmp := j.file + ".tmp"
		if err = os.WriteFile(tmp, b, 0o600); err == nil {
			err = os.Rename(tmp, j.file)
		}
	}
	if err != nil {
		log.Warningf("Could not save cookies: %s", err)
		return
	}
	j.dirty = false
}

// set stores a cookie; an expired one removes it.
func (j *Jar) set(c Cookie) {
	c.Domain = strings.TrimPrefix(c.Domain, ".")
	if c.Path == "" {
		c.Path = "/"
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	old, had := j.cookies[c.key()]
	if c.Expires != 0 && c.Expires <= float64(time.Now().Unix()) {
		if had {
			delete(j.cookies, c.key())
			j.dirty = true
		}
		return
	}
	if !had || old != c {
		j.cookies[c.key()] = c
		j.dirty = true
	}
}

// all returns the cookies of a domain and its subdomains.
func (j *Jar) all(domain string) []Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []Cookie
	for _, c := range j.cookies {
		if domainMatches(c.Domain, domain) {
			out = append(out, c)
		}
	}
	return out
}

// SetCookies implements http.CookieJar.
func (j *Jar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	host := u.Hostname()
	for _, hc := range cookies {
		c := Cookie{Domain: strings.TrimPrefix(hc.Domain, "."), Name: hc.Name, Path: hc.Path, Value: hc.Value, Secure: hc.Secure, HTTPOnly: hc.HttpOnly}
		if c.Domain == "" {
			c.Domain, c.HostOnly = host, true
		} else if !domainMatches(host, c.Domain) {
			continue // a site may not set cookies for another one
		}
		switch {
		case hc.MaxAge < 0:
			c.Expires = 1
		case hc.MaxAge > 0:
			c.Expires = float64(time.Now().Unix() + int64(hc.MaxAge))
		case !hc.Expires.IsZero():
			c.Expires = float64(hc.Expires.Unix())
		}
		j.set(c)
	}
}

// Cookies implements http.CookieJar.
func (j *Jar) Cookies(u *url.URL) []*http.Cookie {
	host, path := u.Hostname(), u.Path
	if path == "" {
		path = "/"
	}
	now := float64(time.Now().Unix())
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []*http.Cookie
	for _, c := range j.cookies {
		switch {
		case c.Expires != 0 && c.Expires <= now, c.Secure && u.Scheme != "https":
		case c.HostOnly && host != c.Domain, !c.HostOnly && !domainMatches(host, c.Domain):
		case !strings.HasPrefix(path, c.Path):
		default:
			out = append(out, &http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
	return out
}
