//go:build ignore

// Build step (make bundle, scripts/bundle.sh): copies the add-on's translations (resources/language/*/strings.po) into ui/strings,
// gzipped, so the binary has them without the add-on. The add-on dir comes from ELEMENTUM_ADDON_PATH
// (default ../plugin.video.elementum next to the repo).
package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	addon := os.Getenv("ELEMENTUM_ADDON_PATH")
	if addon == "" {
		addon = "../../plugin.video.elementum" // go generate runs in ui/
	}
	files, _ := filepath.Glob(filepath.Join(addon, "resources", "language", "resource.language.*", "strings.po"))
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "no strings.po under %s (set ELEMENTUM_ADDON_PATH to the addon checkout)\n", addon)
		os.Exit(1)
	}
	old, _ := filepath.Glob("strings/*.po.gz")
	for _, f := range old {
		os.Remove(f)
	}
	if err := os.MkdirAll("strings", 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		var out bytes.Buffer
		// no name or time in the header, so an unchanged file gives the same bytes
		zw, _ := gzip.NewWriterLevel(&out, gzip.BestCompression)
		zw.Write(data)
		zw.Close()
		lang := strings.TrimPrefix(filepath.Base(filepath.Dir(f)), "resource.language.")
		if err := os.WriteFile(filepath.Join("strings", lang+".po.gz"), out.Bytes(), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Printf("%d languages copied from %s\n", len(files), addon)
}
