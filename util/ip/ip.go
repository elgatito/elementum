package ip

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/proxy"
	"github.com/elgatito/elementum/xbmc"

	"github.com/c-robinson/iplib/v2"
	"github.com/gin-gonic/gin"
	"github.com/jackpal/gateway"
	"github.com/op/go-logging"
	"github.com/wader/filtertransport"
)

var log = logging.MustGetLogger("ip")

var VPNFilteredNetworks = []net.IPNet{
	filtertransport.MustParseCIDR("10.0.0.0/8"),     // RFC1918
	filtertransport.MustParseCIDR("172.16.0.0/12"),  // private
	filtertransport.MustParseCIDR("192.168.0.0/16"), // private
}

func IsAddrLocal(ip net.IP) bool {
	return filtertransport.FindIPNet(filtertransport.DefaultFilteredNetworks, ip)
}

func IsAddrVPN(ip net.IP) bool {
	return filtertransport.FindIPNet(VPNFilteredNetworks, ip)
}

// GetInterfaceAddrs returns IPv4 and IPv6 for an interface string.
func GetInterfaceAddrs(input string) (v4 net.IP, v6 net.IP, err error) {
	addrs := []net.Addr{}

	// Try to parse input as IP
	if ip := net.ParseIP(input); ip != nil {
		addrs = append(addrs, &net.IPAddr{IP: ip, Zone: ""})
	} else {
		iface, err := net.InterfaceByName(input)
		if err != nil {
			log.Warningf("Could not resolve interface '%s': %s", input, err)
			return nil, nil, err
		}

		addrs, err = iface.Addrs()
		if err != nil {
			log.Warningf("Cannot get address for interface '%s': %s", iface.Name, err)
			return nil, nil, err
		}
	}

	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		default:
			continue
		}

		if resp := ip.To4(); resp != nil {
			v4 = resp
		} else if resp := ip.To16(); resp != nil {
			v6 = resp
		}
	}

	if v4 == nil && v6 == nil {
		err = fmt.Errorf("Could not detect IP addresses for %s", input)
	}
	return
}

func VPNIPs() ([]net.IP, error) {
	addrs, err := LocalIPs()
	if err != nil {
		return nil, err
	}

	// Make sure 10.x.x.x addresses are prioritized
	sort.Slice(addrs, func(i, j int) bool {
		return strings.HasPrefix(addrs[i].String(), "10.")
	})

	// Filter out only VPN IPs
	ret := []net.IP{}
	for _, addr := range addrs {
		if IsAddrVPN(addr) {
			ret = append(ret, addr)
		}
	}
	if len(ret) == 0 {
		return nil, errors.New("no VPN IP found")
	}
	return ret, nil
}

func LocalIPs() ([]net.IP, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Warningf("Cannot get list of interfaces: %s", err)
		return nil, err
	}

	ret := []net.IP{}

IFACES:
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			log.Warningf("Cannot get address for interface %s: %s", i.Name, err)
			return nil, err
		}

		for _, addr := range addrs {
			if strings.HasPrefix(addr.String(), "127.") {
				continue IFACES
			}
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			default:
				continue
			}
			v4 := ip.To4()
			if v4 != nil && IsAddrLocal(v4) {
				ret = append(ret, v4)
			}
		}
	}
	if len(ret) == 0 {
		return nil, errors.New("no local IP found")
	}
	return ret, nil
}

func LocalIP(xbmcHost *xbmc.XBMCHost) (net.IP, error) {
	// Use IP that was requested by client in the request, if possible
	if xbmcHost != nil && xbmcHost.Host != "" {
		if ip := net.ParseIP(xbmcHost.Host); ip != nil {
			return ip, nil
		}
	}

	// Get list of local IPs and return the first one
	if localIPs, err := LocalIPs(); err != nil {
		return nil, err
	} else if len(localIPs) > 0 {
		return localIPs[0], nil
	}

	// Return 127.0.0.1 as the fallback
	return net.IPv4(127, 0, 0, 1), errors.New("cannot find local IP address")
}

func GetLocalHost() string {
	if config.Args.LocalHost != "" {
		return config.Args.LocalHost
	} else {
		return "127.0.0.1"
	}
}

// GetHTTPHost ...
func GetHTTPHost(xbmcHost *xbmc.XBMCHost) string {
	// We should always use local IP, instead of external one, if possible
	// to avoid situations when ip has changed and Kodi expects it anyway.
	host := "127.0.0.1"
	if xbmcHost != nil && xbmcHost.Host != "" {
		host = xbmcHost.Host
	} else if config.Args.RemoteHost == "" || config.Args.RemoteHost == "127.0.0.1" {
		// If behind NAT - use external server IP to create URL for client.
		if config.Args.ServerExternalIP != "" {
			host = config.Args.ServerExternalIP
		} else if localIP, err := LocalIP(xbmcHost); err == nil {
			host = localIP.String()
		} else {
			log.Debugf("Error getting local IP: %s", err)
		}
	}

	return fmt.Sprintf("http://%s:%d", host, config.Args.LocalPort)
}

// GetLocalHTTPHost ...
func GetLocalHTTPHost() string {
	return fmt.Sprintf("http://%s:%d", "127.0.0.1", config.Args.LocalPort)
}

// GetContextHTTPHost ...
func GetContextHTTPHost(ctx *gin.Context) string {
	// We should always use local IP, instead of external one, if possible
	// to avoid situations when ip has changed and Kodi expects it anyway.
	host := "127.0.0.1"
	if (config.Args.RemoteHost != "" && config.Args.RemoteHost != "127.0.0.1") || !strings.HasPrefix(ctx.Request.RemoteAddr, "127.0.0.1") {
		// If behind NAT - use external server IP to create URL for client.
		if config.Args.ServerExternalIP != "" {
			host = config.Args.ServerExternalIP
		} else if localIP, err := LocalIP(nil); err == nil {
			host = localIP.String()
		} else {
			log.Debugf("Error getting local IP: %s", err)
		}
	}

	return fmt.Sprintf("http://%s:%d", host, config.Args.LocalPort)
}

// ElementumURL returns elementum url for external calls
func ElementumURL(xbmcHost *xbmc.XBMCHost) string {
	return GetHTTPHost(xbmcHost)
}

// InternalProxyURL returns internal proxy url
func InternalProxyURL(xbmcHost *xbmc.XBMCHost) string {
	ip := "127.0.0.1"
	if xbmcHost != nil && xbmcHost.Host != "" {
		ip = xbmcHost.Host
	} else if localIP, err := LocalIP(xbmcHost); err == nil {
		ip = localIP.String()
	} else {
		log.Debugf("Error getting local IP: %s", err)
	}

	return fmt.Sprintf("http://%s:%d", ip, proxy.ProxyPort)
}

func RequestUserIP(r *http.Request) string {
	IPAddress := r.Header.Get("X-Real-Ip")
	if IPAddress == "" {
		IPAddress = r.Header.Get("X-Forwarded-For")
	}
	if IPAddress == "" {
		IPAddress = r.RemoteAddr
	}
	return IPAddress
}

func TestRepositoryURL() error {
	port := 65223
	resp, err := http.Get(fmt.Sprintf("http://%s:%d/addons.xml", GetLocalHost(), port))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	return nil
}

// GetPossibleGateways calculates possible gateways for interface IP
func GetPossibleGateways(addr net.IP) (ret []net.IP) {
	if gw, err := gateway.DiscoverGateway(); err == nil {
		ret = append(ret, gw)
	}

	// Ignore IPv6 addr and 0.0.0.0 addr
	if addr.To4() == nil || addr.String() == "0.0.0.0" {
		return
	}

	// Iterate through common subnets to get popular gateways
	for _, subnet := range []int{8, 16, 24} {
		n := iplib.NewNet4(addr, subnet)
		ip := n.FirstAddress()

		if !slices.ContainsFunc(ret, func(i net.IP) bool {
			return i.Equal(ip)
		}) {
			ret = append([]net.IP{ip}, ret...)
		}
	}

	return ret
}
