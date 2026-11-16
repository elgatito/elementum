//go:build ignore

// Build step (make bundle, scripts/bundle.sh): copies the addon's resources/settings.xml into default_settings.xml.
// The addon dir comes from ELEMENTUM_ADDON_PATH (default ../plugin.video.elementum next to the repo).
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	addon := os.Getenv("ELEMENTUM_ADDON_PATH")
	if addon == "" {
		addon = "../../plugin.video.elementum" // go generate runs in config/, so this is ../ from the repo root
	}
	src := filepath.Join(addon, "resources", "settings.xml")
	data, err := os.ReadFile(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read %s (set ELEMENTUM_ADDON_PATH to the addon checkout): %v\n", src, err)
		os.Exit(1)
	}
	if err := os.WriteFile("default_settings.xml", data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
