package proxy

import (
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elgatito/elementum/xbmc"
)

// PacStorage ...
type PacStorage struct {
	// *gopac.Parser
	cacheTime time.Time

	mu sync.RWMutex

	serversUsual   map[string]string
	serversSpecial map[string]string
	domains        []string
	specials       map[string]string
	ips            []string
	dn             [][]string
}

var (
	// PacParser ...
	PacParser = &PacStorage{}

	pacURL = "http://antizapret.prostovpn.org/proxy.pac"
)

// Update ...
func (p *PacStorage) Update() {
	PacParser.mu.Lock()

	defer func() {
		p.cacheTime = time.Now()
		PacParser.mu.Unlock()
	}()

	log.Debugf("Updating pac file")
	data := p.GetFileData()

	PacParser.serversUsual = map[string]string{}
	PacParser.serversSpecial = map[string]string{}
	PacParser.specials = map[string]string{}
	PacParser.domains = []string{}
	PacParser.ips = []string{}
	PacParser.dn = [][]string{}

	// Find servers for specific cases
	reUsual := regexp.MustCompile(`(?s)if \(yip === 1 \|\| curarr\.indexOf\(shost\) !== -1\).*?\{.*?return \"(.*?)\".*?\}`)
	reSpecial := regexp.MustCompile(`(?s)if \(isInNet\(oip, special\[i\]\[0\], special\[i\]\[1\]\)\) \{return "(.*?)";\}`)

	// Find arrays containing domains
	reDomains := regexp.MustCompile(`(?s)(d_\w+) = "(.*?)"`)

	reListSpecial := regexp.MustCompile(`(?s)special = \[(.*?)\];`)
	reDn := regexp.MustCompile(`(?s)dn = \{(.*?)\};`)

	if match := reUsual.FindStringSubmatch(data); len(match) != 0 {
		PacParser.serversUsual = getServers(match[1])
	}
	if match := reSpecial.FindStringSubmatch(data); len(match) != 0 {
		PacParser.serversSpecial = getServers(match[1])
	}

	if match := reDn.FindString(string(data)); match != "" {
		for _, i := range strings.Split(strings.Replace(match, "'", "", -1), ", ") {
			PacParser.dn = append(PacParser.dn, strings.Split(i, ":"))
		}
	}

	if match := reListSpecial.FindString(string(data)); match != "" {
		for _, i := range strings.Split(strings.Replace(strings.Replace(match, "],[", "]  ,  [", -1), `"`, "", -1), "  ,  ") {
			s := strings.TrimRight(strings.Replace(strings.Replace(i, "[", "", -1), "]", "", -1), ",")
			ary := strings.Split(strings.TrimRight(strings.TrimSpace(s), ","), ", ")
			PacParser.specials[ary[0]] = strconv.Itoa(ip2int(ary[1]))
		}
	}

	if matches := reDomains.FindAllStringSubmatch(string(data), -1); len(matches) != 0 {
		ips := ""
		domains := ""

		for _, r := range matches {
			l := strings.Replace(r[2], "\\", "", -1)

			if r[1] == "d_ipaddr" {
				ips += l
			} else {
				domains += l
			}
		}

		PacParser.domains = strings.Split(domains, " ")
		PacParser.ips = regexp.MustCompile(`.{8}`).FindAllString(ips, -1)
	}
}

// GetFileData ...
func (p *PacStorage) GetFileData() string {
	filePath := filepath.Join(xbmc.TranslatePath("special://temp"), "antizapret.pac")
	if stat, err := os.Stat(filePath); err == nil && stat.ModTime().After(time.Now().Add(-6*time.Hour)) {
		if data, err := ioutil.ReadFile(filePath); err == nil {
			log.Debugf("Using pac from cache: %s", filePath)
			return string(data)
		}
	}

	log.Debugf("Downloading pac file: %s", pacURL)
	req, _ := http.NewRequest("GET", pacURL, nil)
	req.Header.Add("User-Agent", `Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/39.0.2171.27 Safari/537.36`)

	resp, err := directClient.Do(req)
	if err != nil {
		return ""
	}

	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	if err := ioutil.WriteFile(filePath, data, 0666); err != nil {
		log.Debugf("Could not write cache file %s: %s", filePath, err)
	}

	return string(data)
}

func getServers(data string) map[string]string {
	res := map[string]string{}

	for _, e := range strings.Split(data, "; ") {
		r := strings.Split(strings.Replace(e, ";", "", -1), " ")
		if len(r) == 0 {
			continue
		}

		r[0] = strings.ToLower(r[0])
		if r[0] == "proxy" {
			r[0] = "http"
		} else if r[0] == "direct" {
			continue
		}

		res[r[0]] = r[1]
	}

	return res
}

// FindProxy ...
func (p *PacStorage) FindProxy(query string) (res string, err error) {
	if p.cacheTime.Before(time.Now().Add(-12 * time.Hour)) {
		p.Update()
	}

	u, err := url.Parse(query)
	if err != nil {
		return
	}

	host := u.Hostname()
	shost := ""
	rtype := u.Scheme
	scheme := u.Scheme

	if regexp.MustCompile(`\.(ru|co|cu|com|info|net|org|gov|edu|int|mil|biz|pp|ne|msk|spb|nnov|od|in|ho|cc|dn|i|tut|v|dp|sl|ddns|dyndns|livejournal|herokuapp|azurewebsites|cloudfront|ucoz|3dn|nov|linode|amazonaws|sl-reverse|kiev)\.[^.]+$`).MatchString(host) {
		shost = regexp.MustCompile(`(.+)\.([^.]+\.[^.]+\.[^.]+$)`).ReplaceAllString(host, `\2`)
	} else {
		shost = regexp.MustCompile(`/(.+)\.([^.]+\.[^.]+$)`).ReplaceAllString(host, `\2`)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, k := range PacParser.dn {
		rx := regexp.MustCompile("\\." + k[0] + "$")
		if rx.MatchString(shost) {
			shost = rx.ReplaceAllString(shost, k[1])
			break
		}
	}

	oips := resolveAddr(host)
	iphex := ""
	if len(oips) != 0 && oips[0] != "" {
		iphex = strconv.Itoa(ip2int(oips[0]))
	}

	if iphex != "" {
		for _, i := range PacParser.ips {
			if iphex == i {
				h, t := getServerForRtype(rtype, PacParser.serversUsual)
				return fmt.Sprintf("%s://%s", t, h), nil
			}
		}
	}

	for i := 0; i < len(PacParser.domains); i++ {
		if shost == PacParser.domains[i] {
			return fmt.Sprintf("%s://%s", "http", PacParser.serversUsual["http"]), nil
		}
	}

	for i := range PacParser.specials {
		if len(oips) != 0 && oips[0] != "" && isInNet(oips[0], i, PacParser.specials[i]) {
			h, t := getServerForRtype(scheme, PacParser.serversSpecial)
			return fmt.Sprintf("%s://%s", t, h), nil
		}

		return "", nil
	}

	return "", nil
}

func getServerForRtype(rtype string, servers map[string]string) (string, string) {
	if v, ok := servers[rtype]; ok {
		return v, rtype
	}

	for k, v := range servers {
		return v, k
	}

	return "", ""
}

func isInNet(host, pattern, mask string) bool {
	hostIP := host
	if ip := net.ParseIP(host); ip == nil {
		ips := resolveAddr(host)
		if len(ips) > 0 {
			hostIP = ips[0]
		}
	}

	if hostIP == "" {
		return false
	}
	if ip := net.ParseIP(pattern); ip == nil {
		return false
	}
	if ip := net.ParseIP(mask); ip == nil {
		return false
	}

	if _, cidr, err := net.ParseCIDR(pattern + "/" + mask); err == nil {
		ip := net.ParseIP(hostIP)
		return cidr.Contains(ip)
	}

	return false
}

func ip2int(inp string) int {
	ip := net.ParseIP(inp)
	if ip == nil {
		return 0
	}
	if len(ip) == 16 {
		return int(binary.BigEndian.Uint32(ip[12:16]))
	}
	return int(binary.BigEndian.Uint32(ip))
}

func int2ip(inp string) string {
	nn, _ := strconv.Atoi(inp)
	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, uint32(nn))
	return ip.String()
}
