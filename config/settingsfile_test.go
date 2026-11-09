package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elgatito/elementum/xbmc"
)

const validFile = `{"Version":1,"Info":{"ID":"plugin.video.elementum","Profile":"/p"},"Platform":{"OS":"linux"},"Settings":{"language":"en","trakt_token_expiry":0,"trakt_token":"abc"}}`

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestImportValidate(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		ok   bool
	}{
		"valid":          {validFile, true},
		"legacy v0":      {strings.Replace(validFile, `"Version":1,`, "", 1), true},
		"future version": {strings.Replace(validFile, `"Version":1`, `"Version":99`, 1), false},
		"no id":          {strings.Replace(validFile, "plugin.video.elementum", "", 1), false},
		"no platform":    {strings.Replace(validFile, `"Platform":{"OS":"linux"},`, "", 1), false},
		"bad json":       {`{`, false},
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "settings.json")
			write(t, p, tc.body)
			if _, err := importConfig(p); (err == nil) != tc.ok {
				t.Fatalf("ok=%v err=%v", tc.ok, err)
			}
		})
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := writeFileAtomic(p, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	// Simulated failure: the target is a non-empty directory, so the rename fails.
	bad := filepath.Join(dir, "d")
	os.MkdirAll(filepath.Join(bad, "x"), 0700)
	if err := writeFileAtomic(bad, []byte("two")); err == nil {
		t.Fatal("expected error")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*.tmp*")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
	if b, _ := os.ReadFile(p); string(b) != "one" {
		t.Fatalf("original changed: %s", b)
	}
}

func TestPollingReload(t *testing.T) {
	// Other tests leave globals behind: with a leaked Args.ConfigPath/appliedNorm, WriteBackOnlyChange could
	// swallow the reload. Start from a clean state and restore it afterwards.
	fileMu.Lock()
	oldPath, oldNorm, oldStamp := Args.ConfigPath, appliedNorm, lastStamp
	Args.ConfigPath, appliedNorm = "", nil
	fileMu.Unlock()
	t.Cleanup(func() {
		fileMu.Lock()
		Args.ConfigPath, appliedNorm, lastStamp = oldPath, oldNorm, oldStamp
		fileMu.Unlock()
	})
	p := filepath.Join(t.TempDir(), "settings.json")
	write(t, p, validFile)
	if _, err := loadSettingsFile(p); err != nil {
		t.Fatal(err)
	}
	var reloads atomic.Int32
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() { WatchSettingsFile(p, 10*time.Millisecond, done, func() { reloads.Add(1) }); close(stopped) }()
	t.Cleanup(func() { close(done); <-stopped }) // runs before the state restore above

	wait := func(n int32) bool {
		for i := 0; i < 100; i++ {
			if reloads.Load() >= n {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	time.Sleep(50 * time.Millisecond)
	if reloads.Load() != 0 {
		t.Fatal("reload without a change")
	}
	write(t, p, strings.Replace(validFile, `"en"`, `"de-DE"`, 1))
	if !wait(1) {
		t.Fatal("change not picked up")
	}
	// settle: a non-atomic write can be seen twice, so wait until the count stops moving
	settle := func() int32 {
		last, same := reloads.Load(), 0
		for same < 8 {
			time.Sleep(10 * time.Millisecond)
			if n := reloads.Load(); n != last {
				last, same = n, 0
			} else {
				same++
			}
		}
		return last
	}
	before := settle()
	write(t, p, `{garbage`)
	if after := settle(); after != before {
		t.Fatalf("bad file triggered a reload (%d -> %d)", before, after)
	}
	write(t, p, validFile+" ") // fixed (and a different size)
	if !wait(before + 1) {
		t.Fatal("fixed file not picked up")
	}
}

func TestSetSettingWriteBack(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	write(t, p, validFile)
	old := Args.ConfigPath
	Args.ConfigPath = p
	defer func() { Args.ConfigPath = old }()
	if _, err := loadSettingsFile(p); err != nil {
		t.Fatal(err)
	}

	SetSetting("trakt_token", "new")
	SetSetting("trakt_token_expiry", "12345") // numeric setting keeps its type
	b, err := importConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if b.Settings.ToString("trakt_token") != "new" || b.Settings.ToInt("trakt_token_expiry") != 12345 || b.Settings.ToString("language") != "en" {
		t.Fatalf("%v", b.Settings)
	}
	if v, ok := b.Settings["trakt_token_expiry"].(float64); !ok || v != 12345 {
		t.Fatalf("type lost: %#v", b.Settings["trakt_token_expiry"])
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if settingsFileChanged(p) {
		t.Fatal("own write would trigger a reload")
	}
}

func TestMaskSecrets(t *testing.T) {
	in := `ProxyPassword: "hunter2", TraktToken: "tok", OSDBUser: "bob", ProxyLogin: "al", Language: "en"`
	out := maskSecrets(in)
	for _, s := range []string{"hunter2", "tok\"", "bob", "al\""} {
		if strings.Contains(out, s) {
			t.Fatalf("%q leaked: %s", s, out)
		}
	}
	if !strings.Contains(out, `Language: "en"`) || maskSecret("x") == "x" {
		t.Fatal(out)
	}
}

func TestDefaultFile(t *testing.T) {
	dir := t.TempDir()
	addon := filepath.Join(dir, "addon")
	os.MkdirAll(filepath.Join(addon, "resources"), 0700)
	write(t, filepath.Join(addon, "resources", "settings.xml"), `<settings><category>
<setting id="buffer_size" type="number" default="20"/>
<setting id="greeting_enabled" type="bool" default="true"/>
<setting id="torrents_path" type="folder" default="special://temp/elementum_torrents/"/>
<setting id="download_path" type="folder"/>
</category></settings>`)
	oldAddon, oldTemp := Args.AddonPath, Args.TempPath
	defer func() { Args.AddonPath, Args.TempPath = oldAddon, oldTemp }()
	Args.AddonPath = addon
	Args.TempPath = filepath.Join(dir, "tmp", "elementum")

	p := filepath.Join(dir, "profile", "settings.json")
	b, err := loadSettingsFile(p) // missing: created
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("file: %v %v", fi, err)
	}
	if b.Version != SettingsFileVersion || b.Settings.ToInt("buffer_size") != 20 || !b.Settings.ToBool("greeting_enabled") {
		t.Fatalf("%v", b.Settings)
	}
	if got := b.Settings.ToString("torrents_path"); strings.Contains(got, "special://") || !strings.Contains(got, "elementum_torrents") {
		t.Fatal(got)
	}
	if b.Settings.ToString("download_path") == "" {
		t.Fatal("download_path empty")
	}
}

func TestReloadFromFileNoKodi(t *testing.T) {
	dir := t.TempDir()
	oldArgs := struct{ cfg, temp string }{Args.ConfigPath, Args.TempPath}
	oldPort := xbmc.XBMCExJSONRPCPort
	defer func() {
		Args.ConfigPath, Args.TempPath = oldArgs.cfg, oldArgs.temp
		xbmc.XBMCExJSONRPCPort = oldPort
	}()
	Args.ConfigPath = filepath.Join(dir, "settings.json")
	Args.TempPath = filepath.Join(dir, "tmp", "elementum")

	// The file is missing: Reload creates the default (embedded settings) and loads it, with a nil Kodi host.
	c, err := Reload()
	if err != nil || c == nil {
		t.Fatalf("reload: %v", err)
	}
	if c.BufferSize == 0 || c.Language != "en" {
		t.Fatalf("defaults not applied: %+v", c.Language)
	}

	// Only tokens changed: applied without a restart.
	SetSetting("trakt_token", "T1")
	if !WriteBackOnlyChange() || Get().TraktToken != "T1" {
		t.Fatalf("token change not applied in place: %q", Get().TraktToken)
	}
	// A real setting changed: a reload is needed.
	b, _ := os.ReadFile(Args.ConfigPath)
	write(t, Args.ConfigPath, strings.Replace(string(b), `"buffer_size": 20`, `"buffer_size": 25`, 1))
	if WriteBackOnlyChange() {
		t.Fatal("a non-token change was treated as write-back only")
	}
}

func TestConcurrentSetSetting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	write(t, p, validFile)
	old := Args.ConfigPath
	Args.ConfigPath = p
	defer func() { Args.ConfigPath = old }()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			SetSetting("k"+strconv.Itoa(i), strconv.Itoa(i))
		}(i)
	}
	wg.Wait()
	b, err := importConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if b.Settings.ToInt("k"+strconv.Itoa(i)) != i {
			t.Fatalf("lost write k%d", i)
		}
	}
}

func TestSettingsYamlWriteBackRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.yml")
	write(t, p, "x: 1")
	if err := updateSettingsFile(p, "a", "b"); err == nil {
		t.Fatal("expected an error for yaml")
	}
}

func TestEnvConfigPath(t *testing.T) {
	old := Args.ConfigPath
	defer func() { Args.ConfigPath = old }()
	t.Setenv("ELEMENTUM_CONFIG_PATH", "/a b/settings.json")
	Args.ConfigPath = ""
	ApplyEnvConfigPath()
	if Args.ConfigPath != "/a b/settings.json" || SettingsSource() != "file" {
		t.Fatalf("env not applied: %q", Args.ConfigPath)
	}
	Args.ConfigPath = "/flag.json"
	ApplyEnvConfigPath()
	if Args.ConfigPath != "/flag.json" {
		t.Fatal("env overrode the flag")
	}
}

func TestLibraryReadOnlyNoKodi(t *testing.T) {
	old := Args.ConfigPath
	defer func() { Args.ConfigPath = old }()
	Args.ConfigPath = "x.json"
	if libraryIsReadOnly(nil, "/lib") {
		t.Fatal("file mode without Kodi must keep the library writable")
	}
	Args.ConfigPath = ""
	if !libraryIsReadOnly(nil, "/lib") {
		t.Fatal("Kodi mode keeps the old nil-host behaviour (read-only)")
	}
}

func TestEnvSeesLatePutenv(t *testing.T) {
	cSetenv("ELEMENTUM_TEST_LATE", "/a b/c.json")
	if got := lookupEnv("ELEMENTUM_TEST_LATE"); got != "/a b/c.json" {
		t.Fatalf("got %q", got)
	}
}
