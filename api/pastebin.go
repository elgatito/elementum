package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os/user"

	humanize "github.com/dustin/go-humanize"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/xbmc"
	"github.com/gin-gonic/gin"
)

// PasteProject describes each pastebin project
type PasteProject struct {
	Name    string
	URL     string
	BaseURL string
	IsJSON  bool
	IsRAW   bool
	Fields  PasteFields
	Values  PasteFields
}

// PasteFields describes [string]string values for projects
type PasteFields struct {
	Title      string
	Poster     string
	Syntax     string
	Expiration string
	Content    string
}

var pasteProjects = []PasteProject{
	{
		URL:     "https://paste.kodi.tv/documents",
		BaseURL: "https://paste.kodi.tv",
		Name:    "Kodi.tv Hastebin",
		IsRAW:   true,
	},
	{
		URL:     "https://paste.ubuntu.com/",
		BaseURL: "https://paste.ubuntu.com",
		Name:    "Ubuntu Pastebin",
		Fields: PasteFields{
			Poster:     "poster",
			Syntax:     "syntax",
			Expiration: "expiration",
			Content:    "content",
		},
		Values: PasteFields{
			Syntax: "text",
		},
	},
	{
		URL:     "https://paste.fedoraproject.org/api/paste/submit",
		BaseURL: "https://paste.fedoraproject.org",
		Name:    "Fedora Pastebin",
		IsJSON:  true,
		Fields: PasteFields{
			Title:      "title",
			Syntax:     "language",
			Expiration: "expiry_time",
			Content:    "contents",
		},
		Values: PasteFields{
			Syntax: "text",
		},
	},
}

// Pastebin uploads /debug/:type to pastebin
func Pastebin(ctx *gin.Context) {
	dialog := xbmc.NewDialogProgressBG("Elementum", "LOCALIZE[30457]", "LOCALIZE[30457]")
	if dialog != nil {
		dialog.Update(0, "Elementum", "LOCALIZE[30457]")
	}
	pasteURL := ""
	defer func() {
		if dialog != nil {
			dialog.Close()
		}

		if pasteURL != "" {
			xbmc.Dialog("Elementum", "LOCALIZE[30454];;"+pasteURL)
		}
	}()

	rurl := fmt.Sprintf("http://%s:%d%s%s", config.Args.LocalHost, config.Args.LocalPort, "/debug/", ctx.Params.ByName("type"))

	log.Infof("Requesting %s before uploading to pastebin", rurl)
	resp, err := http.Get(rurl)
	if err != nil {
		log.Infof("Could not get %s: %#v", rurl, err)
		return
	}
	defer resp.Body.Close()
	content, _ := ioutil.ReadAll(resp.Body)

	// u, err := user.Current()
	// if err != nil {
	// 	u = &user.User{
	// 		Name:     "Elementum Uploader",
	// 		Username: "Elementum Uploader",
	// 	}
	// }
	u := &user.User{
		Name:     "Elementum Uploader",
		Username: "Elementum Uploader",
	}

	for _, p := range pasteProjects {
		log.Infof("Uploading to %#v, %s bytes", p, humanize.Bytes(uint64(len(content))))
		values := url.Values{}

		if !p.IsRAW {
			if p.Fields.Poster != "" {
				values.Set(p.Fields.Poster, u.Name)
			}
			if p.Fields.Syntax != "" {
				values.Set(p.Fields.Syntax, p.Values.Syntax)
			}
			if p.Fields.Expiration != "" {
				values.Set(p.Fields.Expiration, p.Values.Expiration)
			}
			if p.Fields.Title != "" {
				values.Set(p.Fields.Title, rurl)
			}

			values.Set(p.Fields.Content, string(content))
		}

		var resp *http.Response
		var err error

		log.Infof("Doing upload to %s", p.URL)
		if p.IsRAW {
			resp, err = http.Post(p.URL, "application/x-www-form-urlencoded", bytes.NewReader(content))
			if err != nil {
				log.Errorf("Error creating http request: %s", err)
				continue
			}
		} else if !p.IsJSON {
			resp, err = http.PostForm(p.URL, values)
		} else {
			jsonValue, _ := json.Marshal(values)
			resp, err = http.Post(p.URL, "application/json", bytes.NewBuffer(jsonValue))
		}

		if err != nil {
			log.Noticef("Could not upload log file. Error: %#v", err)
			continue
		} else if resp != nil && resp.StatusCode != 200 {
			log.Noticef("Could not upload log file. Status: %#v", resp.StatusCode)
			continue
		}

		defer resp.Body.Close()
		if !p.IsJSON && !p.IsRAW {
			pasteURL = resp.Request.URL.String()
		} else {
			content, _ := ioutil.ReadAll(resp.Body)

			var respData map[string]*json.RawMessage
			if err := json.Unmarshal(content, &respData); err != nil {
				log.Warningf("Could not unmarshal response: %s", err)
				continue
			}

			log.Infof("Got response: %#v", respData)
			if _, ok := respData["url"]; ok {
				json.Unmarshal(*respData["url"], &pasteURL)
			} else if _, ok := respData["key"]; ok {
				json.Unmarshal(*respData["key"], &pasteURL)
				pasteURL = p.BaseURL + "/" + pasteURL
			}
		}

		if pasteURL == p.BaseURL || pasteURL == p.BaseURL+"/" {
			log.Warningf("Trying next Paste service, because there is no key in url: %s", pasteURL)
			continue
		}

		log.Noticef("Log uploaded to: %s", pasteURL)
		return
	}
}
