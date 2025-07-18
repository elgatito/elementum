// -*- coding: utf-8 -*-
// Module parses antizapret Proxy Auto Configuration (PAC) file in order to find if domain/ip is blocked and needs proxy.

package proxy

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/util"
)

const USER_AGENT = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36"

const CONFIG_FILE_NAME = "antizapret_config.gob" // Using gob for caching

const CACHE_TTL = 24 * time.Hour // 24 hour caching
// Use these values for debugging
//const CACHE_TTL = 1 * time.Second

var _config *AntizapretConfig
var configMutex sync.RWMutex // Mutex for accessing _config in memory

var CACHE_DIR = ""

// replace repeating sequences in domain to make it shorter
func patternReplace(s string, patterns []PatternEntry) string {
	for _, p := range patterns {
		s = strings.ReplaceAll(s, p.Value, p.Key)
	}
	return s
}

// restore repeating sequences in domain to get initial name
func patternRestore(s string, patterns []PatternEntry) string {
	for _, p := range patterns {
		s = strings.ReplaceAll(s, p.Key, p.Value)
	}
	return s
}

// variables for unlzp function
const TABLE_LEN_BITS = 18
const HASH_MASK = (1 << TABLE_LEN_BITS) - 1

// Point-to-Point Protocol (PPP) compression algorithm or LZP (LZ Prediction) by Charles Bloom.
// https://bitbucket.org/anticensority/antizapret-pac-generator-light/src/master/scripts/lzp.py
func unlzp(data []byte, mask []byte, limit int, table []byte, hashValue int) (string, int, int, int) {
	maskValue := byte(0)
	maskPosition := 0
	dataPosition := 0
	output := make([]byte, 8)
	outputPosition := 0
	finalOutput := ""

	maskLength := len(mask)
	dataLength := len(data)

	for {
		if maskPosition >= maskLength {
			break
		}
		maskValue = mask[maskPosition]
		maskPosition++
		outputPosition = 0
		for i := 0; i < 8; i++ {
			var character byte
			if maskValue&(1<<i) != 0 {
				character = table[hashValue]
			} else {
				if dataPosition >= dataLength {
					break
				}
				character = data[dataPosition]
				table[hashValue] = character
				dataPosition++
			}
			output[outputPosition] = character
			outputPosition++
			hashValue = ((hashValue << 7) ^ int(character)) & HASH_MASK
		}
		if outputPosition == 8 {
			finalOutput += string(output)
		}
		if len(finalOutput) >= limit {
			break
		}
	}
	if outputPosition < 8 {
		finalOutput += string(output[:outputPosition])
	}
	return finalOutput, dataPosition, maskPosition, hashValue
}

// base64 decode, called a2b in original JS code
func base64Decode(a string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(a)
}

// Simulate PAC standard function isInNet
func isInNet(ip net.IP, netaddr string, netmask int) bool {
	// Handle nil from dnsResolve
	if ip == nil {
		return false
	}
	// Ensure it's an IPv4 address as the PAC only uses IPv4 networks
	if ip.To4() == nil {
		return false
	}

	_, network, err := net.ParseCIDR(fmt.Sprintf("%s/%d", netaddr, netmask))
	if err != nil {
		log.Errorf("Error parsing network %s/%d: %s\n", netaddr, netmask, err)
		return false
	}

	return network.Contains(ip)
}

// Simulate PAC standard function dnsResolve
func dnsResolve(hostname string) net.IP {
	// Standard PAC dnsResolve returns only the first A record.
	// And our PAC file has only IPv4 networks.
	resolver := net.Resolver{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ips, err := resolver.LookupIP(ctx, "ip4", hostname)
	if err != nil {
		log.Errorf("Error resolving %s: %s\n", hostname, err)
		return nil
	}
	if len(ips) > 0 {
		return ips[0]
	}
	return nil
}

// Helper to convert base36 string to int
func base36ToInt(s string) (int64, error) {
	return strconv.ParseInt(s, 36, 64)
}

// Helper to convert IPv4 net.IP to big-endian uint32
func ipToBigEndianUint32(ip net.IP) (uint32, error) {
	ipv4 := ip.To4()
	if ipv4 == nil {
		return 0, errors.New("Not an IPv4 address")
	}
	return binary.BigEndian.Uint32(ipv4), nil
}

// Structs to hold the configuration data
type PatternEntry struct {
	Key   string
	Value string
}

type LengthEntry struct {
	Length     int
	Data       []string
	DataLength int
}

type DomainEntry struct {
	TLD     string
	Lengths []LengthEntry // Use slice to preserve order
}

type SpecialEntry struct {
	Netaddr string
	Netmask int
}

type AntizapretConfig struct {
	CreatedAt                time.Time
	Domains                  []DomainEntry  // Use slice to preserve order of TLD
	Special                  []SpecialEntry // Use slice to preserve order
	DIpaddr                  []uint32       // IP addresses stored as big-endian uint32
	PatternsDomainsLzp       []PatternEntry // Use slice to preserve order
	PatternsMaskLzp          []PatternEntry // Use slice to preserve order
	ThreePartSuffixesPattern string
	ProxyURL                 string
}

// Cache file path
func getCacheFilePath() string {
	CACHE_DIR = config.Get().Info.Profile

	// Ensure cache directory exists
	if _, err := os.Stat(CACHE_DIR); os.IsNotExist(err) {
		err := os.MkdirAll(CACHE_DIR, 0755)
		if err != nil {
			error_message := fmt.Sprintf("Error creating cache directory %q: %v", CACHE_DIR, err)
			log.Error(error_message)
			// If directory creation fails, disable caching
			CACHE_DIR = ""
			return ""
		}
	}

	// Check if cache directory is writable
	if err := util.IsWritablePath(CACHE_DIR); err != nil {
		error_message := fmt.Sprintf("Cache directory %q is not writable, antizapret will not work: %v", CACHE_DIR, err)
		log.Errorf(error_message)
		// If directory is not writable, disable caching
		CACHE_DIR = ""
		return ""
	}

	return filepath.Join(CACHE_DIR, CONFIG_FILE_NAME)
}

// Load config from cache file
func loadConfigFromFile(filePath string) (*AntizapretConfig, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open cache file: %w", err)
	}
	defer f.Close()

	var cfg AntizapretConfig
	decoder := gob.NewDecoder(f)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode cache file: %w", err)
	}

	// Check TTL
	if time.Since(cfg.CreatedAt) > CACHE_TTL {
		return nil, errors.New("cache expired")
	}

	return &cfg, nil
}

// Save config to cache file
func saveConfigToFile(filePath string, cfg *AntizapretConfig) error {
	// Ensure directory exists before saving
	dir := filepath.Dir(filePath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create cache directory '%s': %w", dir, err)
		}
	}

	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create cache file: %w", err)
	}
	defer f.Close()

	encoder := gob.NewEncoder(f)
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to encode cache file: %w", err)
	}

	return nil
}

// Build Antizapret config and store it in cache
func loadConfig() (*AntizapretConfig, error) {
	configMutex.RLock()
	if _config != nil && time.Since(_config.CreatedAt) <= CACHE_TTL {
		// Config is already loaded in memory and not expired
		configMutex.RUnlock()
		log.Infof("Using cached Antizapret config (in memory)")
		return _config, nil
	}
	configMutex.RUnlock()

	// Need to load or fetch
	configMutex.Lock() // Acquire write lock
	defer configMutex.Unlock()

	// Double-check if another goroutine loaded it while we waited for the lock
	if _config != nil && time.Since(_config.CreatedAt) <= CACHE_TTL {
		log.Infof("Using cached Antizapret config (in memory, double-checked)")
		return _config, nil
	}

	cacheFilePath := getCacheFilePath()

	// Attempt to load from file cache if enabled
	if cacheFilePath != "" {
		cfg, err := loadConfigFromFile(cacheFilePath)
		if err == nil {
			_config = cfg // Store in memory
			log.Infof("Using cached Antizapret config (from file)")
			return _config, nil
		}
		// Cache file load failed or expired, proceed to fetch
		log.Warningf("Cache file '%s' load failed or expired: %v. Fetching new config.\n", cacheFilePath, err)
	} else {
		return nil, errors.New("File caching is disabled.")
	}

	// Fetch Antizapret PAC file
	log.Infof("Fetching Antizapret PAC file")
	PAC_URL := config.Get().AntizapretPacUrl

	req, err := http.NewRequest("GET", PAC_URL, nil)
	if err != nil {
		error_message := fmt.Sprintf("Failed to create HTTP request: %v", err)
		log.Error(error_message)
		return nil, errors.New(error_message)
	}
	req.Header.Set("User-Agent", USER_AGENT)
	req.Header.Set("Origin", PAC_URL)
	req.Header.Set("Referer", PAC_URL)

	client := &http.Client{
		Timeout: 30 * time.Second, // Add a timeout
		Transport: &http.Transport{
			DisableCompression: false,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		error_message := fmt.Sprintf("Fetching Antizapret PAC file failed: %v", err)
		log.Error(error_message)
		// TODO: probably we can re-use stale cache if antizapret pac file is unavailable or blocked.
		return nil, errors.New(error_message)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		error_message := fmt.Sprintf("Fetching Antizapret PAC file failed with status code: %d", resp.StatusCode)
		log.Error(error_message)
		return nil, errors.New(error_message)
	}

	dataBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		error_message := fmt.Sprintf("Failed to read response body: %v", err)
		log.Error(error_message)
		return nil, errors.New(error_message)
	}
	data := string(dataBytes)

	// Parsing Antizapret PAC file
	log.Info("Parsing Antizapret PAC file")
	var domains []DomainEntry
	var dIPAddrStr []string
	var dIPAddr []uint32
	var special []SpecialEntry
	var domainsLZPStr string
	var maskLZPStr string
	var patternsDomainsLZP []PatternEntry
	var patternsMaskLZP []PatternEntry
	var threePartSuffixesPattern string
	var proxyUrl string

	// Regex patterns
	reDomains := regexp.MustCompile(`(?s)domains = \{(.*?)\};`)
	reDIPAddr := regexp.MustCompile(`(?s)var d_ipaddr = "(.*?)"`)
	reSpecial := regexp.MustCompile(`(?s)var special = \[(.*?)\];`)
	reDomainsLzp := regexp.MustCompile(`(?s)var domains_lzp = "(.*?)";`)
	reMaskLzp := regexp.MustCompile(`(?s)var mask_lzp = "(.*?)";`)
	rePatterns := regexp.MustCompile(`(?s)var patterns = \{(.*?)\};`)
	reThreePartSuffixesPattern := regexp.MustCompile(`if \(/(.*?)/\.test\(host\)`)
	reProxySSL := regexp.MustCompile(`return "HTTPS ([^;]*?); PROXY ([^;]*?);`)
	reProxyNoSSL := regexp.MustCompile(`return "PROXY ([^;]*?);`)

	// Extract domains
	domainsMatch := reDomains.FindStringSubmatch(data)
	if len(domainsMatch) < 2 {
		return nil, errors.New("failed to find domains section in PAC file")
	}
	// Parse domains string into ordered structure
	/* Example:
	"gdn":{2:2,3:3},
	"loans":{6:6},
	"dating":{6:6,8:8}
	*/
	domainsContent := strings.TrimSpace(domainsMatch[1]) // remove newline at the start and end
	// Split by TLD entries, handling nested structure
	tldEntries := regexp.MustCompile(`"[^"]+":\{.*?\},?`).FindAllString(domainsContent, -1)

	for _, tldEntry := range tldEntries {
		tldMatch := regexp.MustCompile(`"([^"]+)":\{(.*?)\},?`).FindStringSubmatch(tldEntry)
		if len(tldMatch) < 3 {
			log.Warningf("Warning: Failed to parse TLD entry: %s\n", tldEntry)
			continue
		}
		tld := tldMatch[1]
		lengthsContent := tldMatch[2]

		domainEntry := DomainEntry{TLD: tld, Lengths: []LengthEntry{}}

		// Split by length entries
		lengthEntries := strings.Split(lengthsContent, ",")
		for _, lengthEntryStr := range lengthEntries {
			lengthEntryStr = strings.TrimSpace(lengthEntryStr)
			if lengthEntryStr == "" {
				continue
			}
			parts := strings.Split(lengthEntryStr, ":")
			if len(parts) < 2 {
				log.Warningf("Warning: Failed to parse length entry: %s\n", lengthEntryStr)
				continue
			}
			length, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				log.Warningf("Warning: Failed to parse length integer '%s': %v\n", parts[0], err)
				continue
			}
			dataLength, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				log.Warningf("Warning: Failed to parse data length integer '%s': %v\n", parts[1], err)
				continue
			}
			domainEntry.Lengths = append(domainEntry.Lengths, LengthEntry{Length: length, DataLength: dataLength})
		}
		domains = append(domains, domainEntry)
	}

	// Extract d_ipaddr
	dIpaddrMatch := reDIPAddr.FindStringSubmatch(data)
	if len(dIpaddrMatch) < 2 {
		return nil, errors.New("failed to find d_ipaddr section in PAC file")
	}
	dIPAddrStr = strings.Fields(strings.ReplaceAll(dIpaddrMatch[1], `\`, ""))

	// Extract special
	specialMatch := reSpecial.FindStringSubmatch(data)
	if len(specialMatch) < 2 {
		return nil, errors.New("failed to find special section in PAC file")
	}
	// Parse the string like `["68.171.224.0", 19],["74.82.64.0", 19],["103.246.200.0", 22]`
	specialContent := strings.TrimSpace(specialMatch[1]) // remove newline at the start and end
	// Remove brackets from start and end
	specialContent = strings.TrimPrefix(specialContent, "[")
	specialContent = strings.TrimSuffix(specialContent, "]")
	// Split by "],["
	specialEntries := regexp.MustCompile(`\],\[`).Split(specialContent, -1)

	for _, entry := range specialEntries {
		parts := strings.Split(entry, `, `)
		if len(parts) == 2 {
			netaddr := strings.Trim(parts[0], `'"`) // Remove surrounding quotes
			netmask, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				log.Warningf("Warning: Failed to parse netmask integer '%s': %v\n", parts[1], err)
				continue
			}
			special = append(special, SpecialEntry{Netaddr: netaddr, Netmask: netmask})
		} else {
			log.Warningf("Warning: Failed to parse special entry: %s\n", entry)
		}
	}

	// Extract domains_lzp
	domainsLzpMatch := reDomainsLzp.FindStringSubmatch(data)
	if len(domainsLzpMatch) < 2 {
		return nil, errors.New("failed to find domains_lzp section in PAC file")
	}
	domainsLZPStr = strings.ReplaceAll(strings.ReplaceAll(domainsLzpMatch[1], `\`, ""), "\n", "")

	// Extract mask_lzp
	maskLzpMatch := reMaskLzp.FindStringSubmatch(data)
	if len(maskLzpMatch) < 2 {
		return nil, errors.New("failed to find mask_lzp section in PAC file")
	}
	maskLZPStr = strings.ReplaceAll(strings.ReplaceAll(maskLzpMatch[1], `\`, ""), "\n", "")

	// Extract patterns
	patternsMatches := rePatterns.FindAllStringSubmatch(data, -1)
	if len(patternsMatches) < 2 {
		return nil, errors.New("failed to find patterns sections in PAC file")
	}

	// Parse patterns_domains_lzp (first match)
	patternsDomainsLzpContent := patternsMatches[0][1]
	// Example: `'!:': 'info', ':': 'le', 'AA': '!'`
	patternEntries := strings.Split(patternsDomainsLzpContent, ", ")
	for _, entry := range patternEntries {
		parts := strings.SplitN(entry, ": ", 2)
		if len(parts) == 2 {
			key := strings.Trim(parts[0], `'"`)
			value := strings.Trim(parts[1], `'"`)
			patternsDomainsLZP = append(patternsDomainsLZP, PatternEntry{Key: key, Value: value})
		} else {
			log.Warningf("Warning: Failed to parse pattern entry: %s\n", entry)
		}
	}

	// Parse patterns_mask_lzp (second match)
	patternsMaskLzpContent := patternsMatches[1][1]
	patternEntries = strings.Split(patternsMaskLzpContent, ", ")
	for _, entry := range patternEntries {
		parts := strings.SplitN(entry, ": ", 2)
		if len(parts) == 2 {
			key := strings.Trim(parts[0], `'"`)
			value := strings.Trim(parts[1], `'"`)
			patternsMaskLZP = append(patternsMaskLZP, PatternEntry{Key: key, Value: value})
		} else {
			log.Warningf("Warning: Failed to parse pattern entry: %s\n", entry)
		}
	}

	// extract threePartSuffixesPattern
	threePartSuffixesPatternMatch := reThreePartSuffixesPattern.FindStringSubmatch(data)
	if len(threePartSuffixesPatternMatch) < 2 {
		return nil, errors.New("failed to find ThreePartSuffixesPattern section in PAC file")
	}
	threePartSuffixesPattern = threePartSuffixesPatternMatch[1]
	_, err = regexp.Compile(threePartSuffixesPattern)
	if err != nil {
		return nil, errors.New("failed to compile ThreePartSuffixesPattern section in PAC file")
	}

	// Extract proxy URL
	proxySSLMatch := reProxySSL.FindStringSubmatch(data)
	if len(proxySSLMatch) >= 3 {
		proxy_server := proxySSLMatch[1]
		proxyUrl = "https://" + proxy_server
	} else {
		proxyNoSSLMatch := reProxyNoSSL.FindStringSubmatch(data)
		if len(proxyNoSSLMatch) >= 2 {
			proxy_server := proxyNoSSLMatch[1]
			proxyUrl = "http://" + proxy_server
		}
	}

	if proxyUrl == "" {
		error_message := "Can't find proxy URL in PAC file"
		log.Error(error_message)
		return nil, errors.New(error_message)
	}

	// Initializing Antizapret config
	log.Info("Initializing Antizapret config")

	// Initialize d_ipaddr
	prevIPval := uint32(0)
	for _, ipStr := range dIPAddrStr {
		curIPvalInt64, err := base36ToInt(ipStr)
		if err != nil {
			log.Warningf("Warning: Failed to parse base36 IP '%s': %v\n", ipStr, err)
		}
		curIPval := uint32(curIPvalInt64) + prevIPval
		dIPAddr = append(dIPAddr, curIPval)
		prevIPval = curIPval
	}

	// Initialize domains (LZP decompression and splitting)
	maskLZPBytes, err := base64Decode(patternReplace(maskLZPStr, patternsMaskLZP))
	if err != nil {
		error_message := fmt.Sprintf("Failed to base64 decode mask_lzp: %v", err)
		log.Error(error_message)
		return nil, errors.New(error_message)
	}
	// Python 3 decodes to string, Python 2 keeps bytes. unlzp expects bytes.
	// The PAC file seems to contain ASCII/UTF-8 data in domains_lzp, so converting string to bytes is fine.
	domainsLZPBytes := []byte(domainsLZPStr)

	leftover := []byte{}
	lzpTable := make([]byte, 1<<TABLE_LEN_BITS) // Initialize LZP table
	lzpHashValue := 0

UNZLP_START:
	for i := range domains {
		for j := range domains[i].Lengths {
			dmnl := domains[i].Lengths[j].DataLength
			if len(leftover) < dmnl { // Need to unpack string
				reqd := max(8192, dmnl)
				if len(domainsLZPBytes) == 0 || len(maskLZPBytes) == 0 {
					log.Warningf("Warning: Ran out of LZP data during initialization.")
					break UNZLP_START
				}
				// Perform LZP decompression
				u, dpos, maskpos, newHashValue := unlzp(domainsLZPBytes, maskLZPBytes, reqd, lzpTable, lzpHashValue)
				domainsLZPBytes = domainsLZPBytes[dpos:]
				maskLZPBytes = maskLZPBytes[maskpos:]
				leftover = append(leftover, u...)
				lzpHashValue = newHashValue // Update hash value
			}

			if len(leftover) < dmnl {
				log.Warningf("Warning: Decompression did not yield enough data for domains['%s']['%d'] (needed %d, got %d). Storing partial data.\n", domains[i].TLD, domains[i].Lengths[j].Length, dmnl, len(leftover))
			}
			// We have enough data, take dmnl bytes and split them
			dataToSplit := leftover[:dmnl]
			leftover = leftover[dmnl:] // Consume the data

			// Split the dataToSplit into fragments of length lenEntry.Length
			fragments := []string{}
			fragmentSize := domains[i].Lengths[j].Length
			if fragmentSize <= 0 {
				log.Warningf("Warning: Invalid fragment size %d for domains['%s']['%d']\n", fragmentSize, domains[i].TLD, domains[i].Lengths[j].Length)
				continue
			}
			if len(dataToSplit)%fragmentSize != 0 {
				log.Warningf("Warning: Data length %d is not a multiple of fragment size %d for domains['%s']['%d']\n", len(dataToSplit), fragmentSize, domains[i].TLD, domains[i].Lengths[j].Length)
			}

			for i := 0; i < len(dataToSplit); i += fragmentSize {
				end := min(i+fragmentSize, len(dataToSplit))
				fragments = append(fragments, string(dataToSplit[i:end]))
			}
			domains[i].Lengths[j].Data = fragments
		}
	}

	// Debug info (optional)
	log.Infof("Number of blocked IP addresses = %d\n", len(dIPAddr))
	// for _, IPhex := range dIPAddr {
	// 	ipBytes := make([]byte, 4)
	// 	binary.BigEndian.PutUint32(ipBytes, IPhex)
	// 	log.Debug(net.IP(ipBytes).String())
	// }
	len_domains := 0
	for _, dmnEntry := range domains {
		// log.Debugf("domain=%s\n", dmnEntry.TLD)
		for _, lenEntry := range dmnEntry.Lengths {
			// log.Debugf("dcnt=%d\n", lenEntry.Length)
			// for _, fragment := range lenEntry.Data {
			// 	log.Debug(patternRestore(fragment, patternsDomainsLZP) + "." + dmnEntry.TLD)
			// }
			len_domains += len(lenEntry.Data)
		}
	}
	log.Infof("Number of blocked domains = %d\n", len_domains)

	_config = &AntizapretConfig{
		CreatedAt:                time.Now(),
		Domains:                  domains,
		Special:                  special,
		DIpaddr:                  dIPAddr,
		ProxyURL:                 proxyUrl,
		PatternsDomainsLzp:       patternsDomainsLZP,
		PatternsMaskLzp:          patternsMaskLZP,
		ThreePartSuffixesPattern: threePartSuffixesPattern,
	}
	// Save to cache file if enabled
	if cacheFilePath != "" {
		if err := saveConfigToFile(cacheFilePath, _config); err != nil {
			log.Warningf("Warning: Failed to save config to cache file '%s': %v\n", cacheFilePath, err)
			// Continue without saving, config is in memory
		} else {
			log.Info("Saved config to cache file")
		}
	}

	return _config, nil
}

var antizapretProxy = &AntizapretProxy{}

// AntizapretProxy struct
type AntizapretProxy struct {
	config *AntizapretConfig
}

// Constructor for AntizapretProxy
func NewAntizapretProxy() *AntizapretProxy {
	proxy := &AntizapretProxy{}
	cfg, err := loadConfig()
	if err != nil {
		error_message := fmt.Sprintf("Got an error during config generation: %v", err)
		log.Error(error_message)
		proxy.config = nil // Set config to nil on error
	} else {
		proxy.config = cfg
	}
	return proxy
}

func (ap *AntizapretProxy) Update() {
	cfg, err := loadConfig()
	if err != nil {
		error_message := fmt.Sprintf("Got an error during config generation: %v", err)
		log.Error(error_message)
		ap.config = nil // Set config to nil on error
	} else {
		ap.config = cfg
	}
}

// ProxyURL is used for Transport.Proxy
func (ap *AntizapretProxy) ProxyURL(r *http.Request) (*url.URL, error) {
	if proxy := ap.Detect(r.URL.Host); proxy != "" {
		log.Debugf("Using Antizapret proxy %s for %s", proxy, r.URL.Host)
		if u, err := url.Parse(proxy); err == nil && u != nil {
			return u, nil
		}
	}
	return nil, nil
}

// Detect if a host needs proxying
// Returns the proxy URL string if proxy is needed, otherwise returns an empty string.
func (ap *AntizapretProxy) Detect(host string) string {
	if ap.config == nil {
		return "" // Return empty string for None
	}

	// remove port
	// net.SplitHostPort handles IPv6 addresses correctly
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		// If SplitHostPort fails, assume no port was present
		h = strings.TrimSpace(host)
	}
	host = h // Use the host part without port

	shost := host
	threePartSuffixesPattern, _ := regexp.Compile(ap.config.ThreePartSuffixesPattern)
	if threePartSuffixesPattern.MatchString(host) {
		shost = regexp.MustCompile(`(.+)\.([^.]+\.[^.]+\.[^.]+$)`).ReplaceAllString(host, `$2`)
	} else {
		shost = regexp.MustCompile(`(.+)\.([^.]+\.[^.]+$)`).ReplaceAllString(host, `$2`)
	}

	// remove leading www
	shost = strings.TrimPrefix(shost, "www.")

	// Split shost into curhost and curzone
	lastDotIndex := strings.LastIndex(shost, ".")
	if lastDotIndex == -1 {
		return "" // can happen if host is `www.com` and shost is `com` after removal of `www.`
	}
	curhost := shost[:lastDotIndex]
	curzone := shost[lastDotIndex+1:]
	if curhost == "" {
		return "" // can happen if host is `.com`
	}

	// Apply patternreplace to curhost
	curhost = patternReplace(curhost, ap.config.PatternsDomainsLzp)

	// Try to find list for curzone and curhost length
	curarr := []string{}
	for _, dmnEntry := range ap.config.Domains {
		if dmnEntry.TLD == curzone {
			for _, lenEntry := range dmnEntry.Lengths {
				if lenEntry.Length == len(curhost) {
					curarr = lenEntry.Data
					break // Found the correct length entry
				}
			}
			break // Found the correct TLD entry
		}
	}

	var oip net.IP
	// Do not resolve IPv4/v6 addresses to prevent slowdown if host is already an IP
	parsedIP := net.ParseIP(host)
	if parsedIP == nil {
		// Not an IP, resolve it
		oip = dnsResolve(host)
	} else {
		// It's an IP, use it directly
		if ipv4 := parsedIP.To4(); ipv4 != nil {
			oip = ipv4 // Use IPv4 string representation
		}
	}

	yip := false
	if oip != nil { // Only check if we got a resolved IP
		// Convert resolved IP to big-endian uint32 for comparison with d_ipaddr
		ipUint32, err := ipToBigEndianUint32(oip)
		if err == nil { // Only check if conversion was successful (i.e., it was a valid IPv4)
			// Check if the IP integer is in the d_ipaddr list
			yip = slices.Contains(ap.config.DIpaddr, ipUint32)
		}
		// Debug print IP hex
		// if ipUint32 != 0 {
		// 	log.Debugf("iphex=%d\n", ipUint32)
		// }
	}

	rip := false
	if oip != nil { // Only check if we got a resolved IP
		for _, specialEntry := range ap.config.Special {
			if isInNet(oip, specialEntry.Netaddr, specialEntry.Netmask) {
				rip = true
				break
			}
		}
	}

	// Check if curhost is in the list of fragments for this zone/length
	curhostInArr := slices.Contains(curarr, curhost)

	if yip || rip || curhostInArr {
		return ap.config.ProxyURL
	}

	return "" // Return empty string for None
}
