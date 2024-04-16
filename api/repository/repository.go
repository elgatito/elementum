package repository

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/anacrolix/missinggo/perf"
	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/proxy"
	"github.com/elgatito/elementum/xbmc"
	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"

	"github.com/goccy/go-json"
)

// Timestamp represents a time that can be unmarshalled from a JSON string
// formatted as either an RFC3339 or Unix timestamp. This is necessary for some
// fields since the GitHub API is inconsistent in how it represents times. All
// exported methods of time.Time can be called on Timestamp.
type Timestamp struct {
	time.Time
}

// ReleaseAsset represents a GitHub release asset in a repository.
type ReleaseAsset struct {
	ID                 int64     `json:"id,omitempty"`
	URL                string    `json:"url,omitempty"`
	Name               string    `json:"name,omitempty"`
	Label              string    `json:"label,omitempty"`
	State              string    `json:"state,omitempty"`
	ContentType        string    `json:"content_type,omitempty"`
	Size               int       `json:"size,omitempty"`
	DownloadCount      int       `json:"download_count,omitempty"`
	CreatedAt          Timestamp `json:"created_at,omitempty"`
	UpdatedAt          Timestamp `json:"updated_at,omitempty"`
	BrowserDownloadURL string    `json:"browser_download_url,omitempty"`
	Uploader           User      `json:"uploader,omitempty"`
	NodeID             string    `json:"node_id,omitempty"`
}

// User represents a GitHub user.
type User struct {
	Login                   string    `json:"login,omitempty"`
	ID                      int64     `json:"id,omitempty"`
	NodeID                  string    `json:"node_id,omitempty"`
	AvatarURL               string    `json:"avatar_url,omitempty"`
	HTMLURL                 string    `json:"html_url,omitempty"`
	GravatarID              string    `json:"gravatar_id,omitempty"`
	Name                    string    `json:"name,omitempty"`
	Company                 string    `json:"company,omitempty"`
	Blog                    string    `json:"blog,omitempty"`
	Location                string    `json:"location,omitempty"`
	Email                   string    `json:"email,omitempty"`
	Hireable                bool      `json:"hireable,omitempty"`
	Bio                     string    `json:"bio,omitempty"`
	PublicRepos             int       `json:"public_repos,omitempty"`
	PublicGists             int       `json:"public_gists,omitempty"`
	Followers               int       `json:"followers,omitempty"`
	Following               int       `json:"following,omitempty"`
	CreatedAt               Timestamp `json:"created_at,omitempty"`
	UpdatedAt               Timestamp `json:"updated_at,omitempty"`
	SuspendedAt             Timestamp `json:"suspended_at,omitempty"`
	Type                    string    `json:"type,omitempty"`
	SiteAdmin               bool      `json:"site_admin,omitempty"`
	TotalPrivateRepos       int       `json:"total_private_repos,omitempty"`
	OwnedPrivateRepos       int       `json:"owned_private_repos,omitempty"`
	PrivateGists            int       `json:"private_gists,omitempty"`
	DiskUsage               int       `json:"disk_usage,omitempty"`
	Collaborators           int       `json:"collaborators,omitempty"`
	TwoFactorAuthentication bool      `json:"two_factor_authentication,omitempty"`
	LdapDn                  string    `json:"ldap_dn,omitempty"`

	// API URLs
	URL               string `json:"url,omitempty"`
	EventsURL         string `json:"events_url,omitempty"`
	FollowingURL      string `json:"following_url,omitempty"`
	FollowersURL      string `json:"followers_url,omitempty"`
	GistsURL          string `json:"gists_url,omitempty"`
	OrganizationsURL  string `json:"organizations_url,omitempty"`
	ReceivedEventsURL string `json:"received_events_url,omitempty"`
	ReposURL          string `json:"repos_url,omitempty"`
	StarredURL        string `json:"starred_url,omitempty"`
	SubscriptionsURL  string `json:"subscriptions_url,omitempty"`
}

// Release represents a GitHub release in a repository.
type Release struct {
	TagName         string `json:"tag_name,omitempty"`
	TargetCommitish string `json:"target_commitish,omitempty"`
	Name            string `json:"name,omitempty"`
	Body            string `json:"body,omitempty"`
	Draft           bool   `json:"draft,omitempty"`
	Prerelease      bool   `json:"prerelease,omitempty"`

	// The following fields are not used in CreateRelease or EditRelease:
	ID          int64          `json:"id,omitempty"`
	CreatedAt   Timestamp      `json:"created_at,omitempty"`
	PublishedAt Timestamp      `json:"published_at,omitempty"`
	URL         string         `json:"url,omitempty"`
	HTMLURL     string         `json:"html_url,omitempty"`
	AssetsURL   string         `json:"assets_url,omitempty"`
	Assets      []ReleaseAsset `json:"assets,omitempty"`
	UploadURL   string         `json:"upload_url,omitempty"`
	ZipballURL  string         `json:"zipball_url,omitempty"`
	TarballURL  string         `json:"tarball_url,omitempty"`
	Author      User           `json:"author,omitempty"`
	NodeID      string         `json:"node_id,omitempty"`
}

const (
	githubUserContentURL    = "https://elementumorg.github.io/packages/%s"
	githubAltUserContentURL = "https://elementum.surge.sh/packages/%s"
	githubLatestReleaseURL  = "https://api.github.com/repos/%s/%s/releases/latest"

	releaseChangelog = "[B]%s[/B] - %s\n%s\n\n"
)

var (
	addonZipRE       = regexp.MustCompile(`[\w]+\.[\w]+(\.[\w]+)?-\d+\.\d+\.\d+(-[\w]+\.?\d+)?\.zip`)
	addonChangelogRE = regexp.MustCompile(`changelog.*\.txt`)
	log              = logging.MustGetLogger("repository")
)

func getContentURL(repository, url string) (resp *http.Response, err error) {
	resp, err = proxy.GetClient().Get(fmt.Sprintf(githubUserContentURL, repository) + url)
	if err == nil && resp != nil {
		return resp, err
	}

	resp, err = proxy.GetClient().Get(fmt.Sprintf(githubAltUserContentURL, repository) + url)
	if err == nil && resp != nil {
		return resp, err
	}

	return
}

func getLastRelease(repository string) string {
	defer perf.ScopeTimer()()

	resp, err := getContentURL(repository, "/release")
	if err != nil || resp == nil {
		return ""
	} else if err == nil && resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	return string(bodyBytes)
}

func GetLatestRelease(user string, repository string) *Release {
	defer perf.ScopeTimer()()

	res, _ := http.Get(fmt.Sprintf(githubLatestReleaseURL, user, repository))
	if res == nil {
		return nil
	}
	defer res.Body.Close()
	var release Release
	json.NewDecoder(res.Body).Decode(&release)
	return &release
}

func getReleases(user string, repository string) []Release {
	defer perf.ScopeTimer()()

	res, _ := http.Get(fmt.Sprintf(githubLatestReleaseURL, user, repository))
	if res == nil {
		return nil
	}
	defer res.Body.Close()

	var releases []Release
	json.NewDecoder(res.Body).Decode(&releases)
	return releases
}

func getAddonXML(repository string) (string, error) {
	defer perf.ScopeTimer()()

	resp, err := getContentURL(repository, "/addon.xml")
	if resp == nil {
		return "", errors.New("Not found")
	} else if err == nil && resp.StatusCode != 200 {
		return "", errors.New(resp.Status)
	}

	defer resp.Body.Close()
	retBytes, _ := io.ReadAll(resp.Body)
	return string(retBytes), nil
}

func getAddons() (*xbmc.AddonList, error) {
	defer perf.ScopeTimer()()
	var addons []xbmc.Addon

	for _, repo := range []string{"elgatito/plugin.video.elementum", "elgatito/script.elementum.burst", "elgatito/context.elementum", "ElementumOrg/service.lt2http"} {
		addonXML, err := getAddonXML(repo)
		if err != nil {
			continue
		}

		addon := xbmc.Addon{}
		xml.Unmarshal([]byte(addonXML), &addon)
		addons = append(addons, addon)
	}

	return &xbmc.AddonList{
		Addons: addons,
	}, nil
}

// GetAddonsXML ...
func GetAddonsXML(ctx *gin.Context) {
	addons, err := getAddons()
	if err != nil {
		ctx.AbortWithError(404, errors.New("Unable to retrieve the remote's addons.xml file"))
	}
	ctx.XML(200, addons)
}

// GetAddonsXMLChecksum ...
func GetAddonsXMLChecksum(ctx *gin.Context) {
	addons, err := getAddons()
	if addons != nil && len(addons.Addons) > 0 {
		for _, a := range addons.Addons {
			log.Infof("Last available release of %s: v%s", a.ID, a.Version)
		}
	}
	if err != nil {
		ctx.Error(errors.New("Unable to retrieve the remote's addon.xml file"))
	}
	hasher := md5.New()
	xml.NewEncoder(hasher).Encode(addons)
	ctx.String(200, hex.EncodeToString(hasher.Sum(nil)))
}

// GetAddonFiles ...
func GetAddonFiles(ctx *gin.Context) {
	user := ctx.Params.ByName("user")
	repository := ctx.Params.ByName("repository")
	filepath := ctx.Params.ByName("filepath")[1:] // strip the leading "/"
	if repository == "service.lt2http" {
		user = "ElementumOrg"
	}

	xbmcHost, _ := xbmc.GetXBMCHostWithContext(ctx)
	if xbmcHost == nil {
		return
	}

	lastReleaseTag := getLastRelease(user + "/" + repository)

	switch filepath {
	case "addons.xml":
		GetAddonsXML(ctx)
		return
	case "addons.xml.md5":
		// go writeChangelog(user, "plugin.video.elementum")
		// go writeChangelog(user, "script.elementum.burst")
		// go writeChangelog(user, "context.elementum")
		// go writeChangelog(user, "service.lt2http")
		GetAddonsXMLChecksum(ctx)
		return
	case "fanart.jpg":
		fallthrough
	case "fanart.png":
		fallthrough
	case "icon.png":
		ctx.Redirect(302, fmt.Sprintf(githubUserContentURL+"/"+filepath, user, repository, lastReleaseTag))
		return
	}

	switch {
	case addonZipRE.MatchString(filepath):
		addonZip(ctx, xbmcHost, user, repository, lastReleaseTag)
	case addonChangelogRE.MatchString(filepath):
		writeChangelog(user, repository)
		addonChangelog(ctx, user, repository)
	default:
		ctx.AbortWithError(404, errors.New(filepath))
	}
}

// GetAddonFilesHead ...
func GetAddonFilesHead(ctx *gin.Context) {
	ctx.String(200, "")
}

func addonZip(ctx *gin.Context, xbmcHost *xbmc.XBMCHost, user string, repository string, lastReleaseTag string) {
	defer perf.ScopeTimer()()
	release := GetLatestRelease(user, repository)
	// if there a release with an asset that matches a addon zip, use it
	if release == nil {
		return
	}

	platformStruct := xbmcHost.GetPlatform()
	platform := platformStruct.OS + "_" + platformStruct.Arch
	var assetAllPlatforms string
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, platform+".zip") {
			assetPlatform := asset.BrowserDownloadURL
			log.Infof("Using release asset for %s: %s", platform, assetPlatform)
			ctx.Redirect(302, assetPlatform)
			return
		}
		if addonZipRE.MatchString(asset.Name) {
			assetAllPlatforms = asset.BrowserDownloadURL
			log.Infof("Found all platforms release asset: %s", assetAllPlatforms)
			continue
		}
	}
	if assetAllPlatforms != "" {
		log.Infof("Using release asset for all platforms: %s", assetAllPlatforms)
		ctx.Redirect(302, assetAllPlatforms)
		return
	}
}

func fetchChangelog(user string, repository string) (changelog string) {
	log.Infof("Fetching add-on changelog for %s...", repository)
	releases := getReleases(user, repository)
	if len(releases) == 0 {
		return
	}

	changelog = repository + " changelog\n======\n\n"
	for _, release := range releases {
		changelog += fmt.Sprintf(releaseChangelog, release.TagName, release.PublishedAt.Format("Jan 2 2006"), release.Body)
	}

	return
}

func writeChangelog(user string, repository string) error {
	changelog := fetchChangelog(user, repository)
	lines := strings.Split(changelog, "\n")
	path := filepath.Clean(filepath.Join(config.Get().Info.Path, "..", repository, "changelog.txt"))

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	w := bufio.NewWriter(file)
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	return w.Flush()
}

func addonChangelog(ctx *gin.Context, user string, repository string) {
	changelog := fetchChangelog(user, repository)
	ctx.String(200, changelog)
}
