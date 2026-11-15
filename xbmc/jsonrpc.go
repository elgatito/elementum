package xbmc

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/elgatito/elementum/jsonrpc"
	"github.com/gin-gonic/gin"

	"github.com/anacrolix/sync"
)

// Args ...
type Args []any

// Object ...
type Object map[string]any

// Results ...
var Results map[string]chan any

var (
	XBMCLocalHost *XBMCHost = nil
	XBMCHosts               = []*XBMCHost{}

	// XBMCJSONRPCPort is a port for XBMCJSONRPC (RPC of Kodi)
	XBMCJSONRPCPort = "9090"

	// XBMCExJSONRPCPort is a port for XBMCExJSONRPC (RPC of Python part of the addon)
	XBMCExJSONRPCPort = "65221"

	BehindNAT = false

	// KodiCookie is a cookie (or query parameter), set by Web UI, with the connected Kodi host to use for its requests
	KodiCookie = "kodi"

	mu sync.RWMutex
)

func Init(externalIP string) {
	mu.Lock()
	BehindNAT = externalIP != ""
	XBMCHosts = []*XBMCHost{}
	XBMCLocalHost = nil
	mu.Unlock()

	ProbeLocalHost()
}

// ProbeLocalHost registers the local Kodi as XBMCLocalHost if its JSON-RPC port answers.
// Idempotent and safe to call repeatedly: does nothing when a local host is already known, so it also
// picks up a Kodi that started after Elementum.
func ProbeLocalHost() {
	if h, _ := GetLocalXBMCHost(); h != nil {
		return
	}

	for _, host := range []string{
		"127.0.0.1",
		"::1",
	} {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, XBMCJSONRPCPort), time.Second*3)
		if err != nil {
			log.Debugf("Could not connect to the host %s: %s", host, err)
			continue
		}
		conn.Close()

		mu.Lock()
		defer mu.Unlock()
		if XBMCLocalHost != nil { // lost the race to another prober
			return
		}
		log.Debugf("Adding local host %s", host)
		for _, h := range XBMCHosts { // the addon may have been registered already by its client IP
			if h != nil && sameHost(h.Host, host) {
				XBMCLocalHost = h
				return
			}
		}
		XBMCLocalHost = &XBMCHost{host}
		XBMCHosts = append(XBMCHosts, XBMCLocalHost)
		// Both loopbacks are the same Kodi, keep only the first one that answers
		return
	}
}

// sameHost compares hosts regardless of IPv4/IPv6 form: all loopbacks are equal, IPv4-mapped IPv6 equals IPv4
func sameHost(a, b string) bool {
	return normalizeHost(a) == normalizeHost(b)
}

func normalizeHost(host string) string {
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if ip.IsLoopback() {
		return "127.0.0.1"
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.String()
	}
	return ip.String()
}

func ContainsXBMCHost(host string) bool {
	mu.RLock()
	defer mu.RUnlock()

	for _, h := range XBMCHosts {
		if sameHost(h.Host, host) {
			return true
		}
	}
	return false
}

// GetXBMCHosts returns a copy of connected Kodi hosts list
func GetXBMCHosts() []*XBMCHost {
	mu.RLock()
	defer mu.RUnlock()

	return append([]*XBMCHost{}, XBMCHosts...)
}

func AddLocalXBMCHost(host string) (*XBMCHost, error) {
	h, err := AddXBMCHost(host)
	mu.Lock()
	XBMCLocalHost = h
	mu.Unlock()

	return h, err
}

func AddXBMCHost(host string) (*XBMCHost, error) {
	mu.Lock()
	defer mu.Unlock()

	for _, h := range XBMCHosts {
		if h != nil && sameHost(h.Host, host) {
			return h, nil
		}
	}

	h := &XBMCHost{host}
	XBMCHosts = append(XBMCHosts, h)

	return h, nil
}

func RemoveXBMCHost(host string) error {
	mu.Lock()
	defer mu.Unlock()

	index := -1
	for i, h := range XBMCHosts {
		if sameHost(h.Host, host) {
			index = i
			break
		}
	}

	if index > -1 {
		XBMCHosts = append(XBMCHosts[:index], XBMCHosts[index+1:]...)
		return nil
	} else {
		return fmt.Errorf("Could not find host '%s'", host)
	}
}

func GetLocalXBMCHost() (*XBMCHost, error) {
	mu.RLock()
	defer mu.RUnlock()

	if XBMCLocalHost != nil {
		return XBMCLocalHost, nil
	}

	return nil, errors.New("No local XBMCHost found")
}

func GetXBMCHostWithContext(ctx *gin.Context) (*XBMCHost, error) {
	// If request is not coming from addon's python part - then it can be a browser request or Kodi,
	// so we communicate with any existing Kodi connection.
	// Same if we are behind NAT - in this case ClientIP is NAT gateway IP, so we should not use it.
	if ctx.GetHeader("User-Agent") != "plugin.video.elementum" || BehindNAT {
		// Web UI can choose any of already connected Kodi hosts, for a single request or with a cookie
		host := ctx.Query(KodiCookie)
		if host == "" {
			host, _ = ctx.Cookie(KodiCookie)
		}
		if host != "" && ContainsXBMCHost(host) {
			return GetXBMCHost(host)
		}
		return GetLocalXBMCHost()
	}

	return GetXBMCHost(ctx.ClientIP())
}

func GetXBMCHost(host string) (*XBMCHost, error) {
	mu.RLock()

	if host == "" {
		mu.RUnlock()
		return GetLocalXBMCHost()
	}

	for _, h := range XBMCHosts {
		if h != nil && sameHost(h.Host, host) {
			mu.RUnlock()
			return h, nil
		}
	}

	mu.RUnlock()
	return AddXBMCHost(host)
}

func (h XBMCHost) getJSONConnection() (net.Conn, error) {
	return net.DialTimeout("tcp", net.JoinHostPort(h.Host, XBMCJSONRPCPort), time.Second*5)
}

func (h XBMCHost) getExJSONConnection() (net.Conn, error) {
	return net.DialTimeout("tcp", net.JoinHostPort(h.Host, XBMCExJSONRPCPort), time.Second*5)
}

func (h XBMCHost) executeJSONRPC(method string, retVal any, args Args) error {
	if args == nil {
		args = Args{}
	}
	conn, err := h.getJSONConnection()
	if err != nil {
		log.Error(err)
		log.Critical("No available JSON-RPC connection to Kodi")
		return err
	}
	if conn != nil {
		defer conn.Close()
		client := jsonrpc.NewClient(conn)
		return client.Call(method, args, retVal)
	}
	return errors.New("No available JSON-RPC connection to Kodi")
}

func (h XBMCHost) executeJSONRPCO(method string, retVal any, args Object) error {
	if args == nil {
		args = Object{}
	}
	conn, err := h.getJSONConnection()
	if err != nil {
		log.Error(err)
		log.Critical("No available JSON-RPC connection to Kodi")
		return err
	}
	if conn != nil {
		defer conn.Close()
		client := jsonrpc.NewClient(conn)
		return client.Call(method, args, retVal)
	}
	return errors.New("No available JSON-RPC connection to Kodi")
}

func (h XBMCHost) executeJSONRPCEx(method string, retVal any, args Args) error {
	if args == nil {
		args = Args{}
	}
	args = localizeArgs(args)
	conn, err := h.getExJSONConnection()
	if err != nil {
		log.Error(err)
		log.Critical("No available JSON-RPC connection to the add-on")
		return err
	}
	if conn != nil {
		defer conn.Close()
		client := jsonrpc.NewClient(conn)
		return client.Call(method, args, retVal)
	}
	return errors.New("No available JSON-RPC connection to the add-on")
}
