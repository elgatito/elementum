package api

import (
	"fmt"
	"io/fs"

	"github.com/dustin/go-humanize"
	"github.com/gin-gonic/gin"

	"github.com/elgatito/elementum/bittorrent"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/diskusage"
	"github.com/elgatito/elementum/xbmc"
)

// Storage shows usage of the device holding the download path and how much of it Elementum takes
func Storage(s *bittorrent.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		items := xbmc.ListItems{}
		stat := func(label int, size uint64, icon string) *xbmc.ListItem {
			return &xbmc.ListItem{
				Label:     fmt.Sprintf("LOCALIZE[%d]: %s", label, humanize.Bytes(size)),
				Path:      URLForXBMC("/cmd/storage/refresh"),
				IsAction:  true,
				Thumbnail: config.AddonResource("img", icon),
			}
		}

		if disk, err := diskusage.DiskUsage(config.Get().DownloadPath); err == nil {
			items = append(items, stat(30779, uint64(disk.All), "storage.png"), stat(30780, uint64(disk.Used), "storage_used.png"), stat(30781, uint64(disk.Free), "storage_free.png"))
		} else {
			log.Warningf("Could not get disk usage: %s", err)
		}

		if root, err := openDownloadRoot(); err == nil {
			defer root.Close()
			total, leftovers := downloadPathUsage(root.FS(), ownedDownloadPaths(s))
			items = append(items, stat(30782, total, "cloud.png"), stat(30783, leftovers, "leftovers.png"))
		} else {
			log.Warningf("Could not open download path: %s", err)
		}

		items = append(items,
			&xbmc.ListItem{Label: "LOCALIZE[30562]", Path: URLForXBMC("/cmd/open_path/download"), IsAction: true, Thumbnail: config.AddonResource("img", "folder.png")},
			&xbmc.ListItem{Label: "LOCALIZE[30761]", Path: URLForXBMC("/cmd/leftovers"), Thumbnail: config.AddonResource("img", "trash.png")},
		)
		ctx.JSON(200, xbmc.NewView("", items))
	}
}

// downloadPathUsage returns bytes used by the whole download path and by entries not owned by any torrent
func downloadPathUsage(fsys fs.FS, owned map[string]bool) (total, leftovers uint64) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		log.Warningf("Could not read download path: %s", err)
	}
	for _, e := range entries {
		size := entrySize(fsys, e.Name())
		total += size
		if !owned[e.Name()] {
			leftovers += size
		}
	}
	return
}

// StorageRefresh recalculates the storage view, so clicking a stats item does not make Kodi try to play it
func StorageRefresh(ctx *gin.Context) {
	if xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx); xbmcHost != nil {
		xbmcHost.Refresh()
	}
	ctx.String(200, "")
}
