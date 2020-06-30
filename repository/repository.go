package repository

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/util"
	"github.com/elgatito/elementum/xbmc"
)

func copyFile(from string, to string) error {
	input, err := os.Open(from)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.Create(to)
	if err != nil {
		return err
	}
	defer output.Close()
	io.Copy(output, input)
	return nil
}

// MakeElementumRepositoryAddon ...
func MakeElementumRepositoryAddon() error {
	addonID := "repository.elementum"
	addonName := "Elementum Repository"

	elementumHost := fmt.Sprintf("http://%s:%d", config.Args.LocalHost, config.Args.LocalPort)
	addon := &xbmc.Addon{
		ID:           addonID,
		Name:         addonName,
		Version:      util.GetVersion(),
		ProviderName: config.Get().Info.Author,
		Extensions: []*xbmc.AddonExtension{
			&xbmc.AddonExtension{
				Point: "xbmc.addon.repository",
				Name:  addonName,
				Info: &xbmc.AddonRepositoryInfo{
					Text:       elementumHost + "/repository/elgatito/plugin.video.elementum/addons.xml",
					Compressed: false,
				},
				Checksum: elementumHost + "/repository/elgatito/plugin.video.elementum/addons.xml.md5",
				Datadir: &xbmc.AddonRepositoryDataDir{
					Text: elementumHost + "/repository/elgatito/",
					Zip:  true,
				},
			},
			&xbmc.AddonExtension{
				Point: "xbmc.addon.metadata",
				Summaries: []*xbmc.AddonText{
					&xbmc.AddonText{
						Text: "GitHub repository for Elementum updates",
						Lang: "en",
					},
				},
				Platform: "all",
			},
		},
	}

	addonPath := filepath.Clean(filepath.Join(config.Get().Info.Path, "..", addonID))
	if err := os.MkdirAll(addonPath, 0777); err != nil {
		return err
	}

	if err := copyFile(filepath.Join(config.Get().Info.Path, "icon.png"), filepath.Join(addonPath, "icon.png")); err != nil {
		return err
	}

	if err := copyFile(filepath.Join(config.Get().Info.Path, "fanart.png"), filepath.Join(addonPath, "fanart.png")); err != nil {
		return err
	}

	addonXMLFile, err := os.Create(filepath.Join(addonPath, "addon.xml"))
	if err != nil {
		return err
	}
	defer addonXMLFile.Close()
	return xml.NewEncoder(addonXMLFile).Encode(addon)
}
