package api

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/util/ip"
	"github.com/elgatito/elementum/xbmc"
)

// Leftovers lists entries in the download path that do not belong to any torrent in the session
func Leftovers(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
		items := xbmc.ListItems{}
		dir := path.Clean(ctx.DefaultQuery("path", "."))

		root, err := openDownloadRoot()
		if err != nil {
			log.Warningf("Could not open download path: %s", err)
			ctx.JSON(200, xbmc.NewView("", items))
			return
		}
		defer root.Close()

		owned := ownedDownloadPaths(s)
		// fs.ReadDir returns entries sorted by filename
		entries, err := fs.ReadDir(root.FS(), dir)
		if err != nil {
			log.Warningf("Could not read %s: %s", dir, err)
		}

		for _, e := range entries {
			rel := path.Join(dir, e.Name())
			if owned[topComponent(rel)] {
				continue
			}

			item := &xbmc.ListItem{
				Label: fmt.Sprintf("%s [%s]", e.Name(), humanize.Bytes(entrySize(root.FS(), rel))),
				ContextMenu: [][]string{
					{"LOCALIZE[30311]", fmt.Sprintf("RunPlugin(%s)", URLQuery(URLForXBMC("/cmd/leftovers/delete"), "path", rel))},
				},
			}
			if e.IsDir() {
				item.Path = URLQuery(URLForXBMC("/cmd/leftovers"), "path", rel)
			} else {
				item.Path = leftoverFileURL(xbmcHost, rel)
				item.IsPlayable = true
			}
			items = append(items, item)
		}

		ctx.JSON(200, xbmc.NewView("", items))
	}
}

// LeftoversDelete removes a leftover file or folder from the download path
func LeftoversDelete(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
		if xbmcHost == nil {
			return
		}

		rel := path.Clean(ctx.Query("path"))
		if rel == "." || !fs.ValidPath(rel) || ownedDownloadPaths(s)[topComponent(rel)] {
			ctx.String(400, "Invalid path")
			return
		}

		if !xbmcHost.DialogConfirm("Elementum", fmt.Sprintf("LOCALIZE[30751];;%s", rel)) {
			ctx.String(200, "")
			return
		}

		root, err := openDownloadRoot()
		if err != nil {
			ctx.String(500, err.Error())
			return
		}
		defer root.Close()

		log.Infof("Removing leftover %s from download path", rel)
		if err := root.RemoveAll(rel); err != nil {
			log.Errorf("Could not remove %s: %s", rel, err)
			xbmcHost.Notify("Elementum", err.Error(), config.AddonIcon())
		}

		xbmcHost.Refresh()
		ctx.String(200, "")
	}
}

// LeftoversFile serves a file from the download path, so Kodi can play it even when it runs on another machine
func LeftoversFile(ctx *gin.Context) {
	root, err := openDownloadRoot()
	if err != nil {
		ctx.String(404, err.Error())
		return
	}
	defer root.Close()

	ctx.FileFromFS(ctx.Param("path"), http.FS(root.FS()))
}

func leftoverFileURL(xbmcHost *xbmc.XBMCHost, rel string) string {
	u, err := url.Parse(ip.GetHTTPHost(xbmcHost))
	if err != nil {
		return ""
	}
	u = u.JoinPath("cmd/leftovers/files", rel)
	// Player requests go through Auth middleware too, Kodi redacts credentials in its log
	if config.Args.LocalLogin != "" || config.Args.LocalPassword != "" {
		u.User = url.UserPassword(config.Args.LocalLogin, config.Args.LocalPassword)
	}
	return u.String()
}

// openDownloadRoot confines all access to the download path, rejecting "..", absolute paths and escaping symlinks
func openDownloadRoot() (*os.Root, error) {
	dir := config.Get().DownloadPath
	if dir == "" || dir == "." {
		return nil, fmt.Errorf("download path is not set")
	}
	return os.OpenRoot(dir)
}

// ownedDownloadPaths returns top-level names in the download path used by torrents in the session
func ownedDownloadPaths(s *bittorrent.Service) map[string]bool {
	owned := map[string]bool{}
	for _, t := range s.GetTorrents() {
		if t == nil {
			continue
		}
		owned[fmt.Sprintf(".%s.parts", t.InfoHash())] = true
		// Torrent without metadata has no files yet, but its name is still reserved
		if name := t.Name(); name != "" {
			owned[topComponent(name)] = true
		}
		for _, f := range t.GetFiles() {
			owned[topComponent(f.Path)] = true
		}
	}
	return owned
}

func topComponent(p string) string {
	top, _, _ := strings.Cut(filepath.ToSlash(p), "/")
	return top
}

func entrySize(fsys fs.FS, name string) (size uint64) {
	fs.WalkDir(fsys, name, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				size += uint64(info.Size())
			}
		}
		return nil
	})
	return
}
