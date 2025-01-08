package ip

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/proxy"

	"github.com/gin-gonic/gin"
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger("ip")

// LocalIP ...
func LocalIP() (net.IP, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			return nil, err
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
			if v4 != nil && (v4[0] == 192 || v4[0] == 172 || v4[0] == 10) {
				return v4, nil
			}
		}
	}
	return nil, errors.New("cannot find local IP address")
}

func GetLocalHost() string {
	if config.Args.LocalHost != "" {
		return config.Args.LocalHost
	} else {
		return "127.0.0.1"
	}
}

// GetHTTPHost ...
func GetHTTPHost() string {
	// We should always use local IP, instead of external one, if possible
	// to avoid situations when ip has changed and Kodi expects it anyway.
	host := "127.0.0.1"
	if config.Args.RemoteHost == "" || config.Args.RemoteHost == "127.0.0.1" {
		if localIP, err := LocalIP(); err == nil {
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
		if localIP, err := LocalIP(); err == nil {
			host = localIP.String()
		} else {
			log.Debugf("Error getting local IP: %s", err)
		}
	}

	return fmt.Sprintf("http://%s:%d", host, config.Args.LocalPort)
}

// GetListenAddr parsing configuration setted for interfaces and port range
// and returning IP, IPv6, and port
func GetListenAddr(confAutoIP bool, confAutoPort bool, confInterfaces string, confPortMin int, confPortMax int) (listenIP, listenIPv6 string, listenPort int, disableIPv6 bool, err error) {
	if confAutoIP {
		confInterfaces = ""
	}
	if confAutoPort {
		confPortMin = 0
		confPortMax = 0
	}

	listenIPs := []string{}
	listenIPv6s := []string{}

	if strings.TrimSpace(confInterfaces) != "" {
		for _, iName := range strings.Split(strings.Replace(strings.TrimSpace(confInterfaces), " ", "", -1), ",") {
			// Check whether value in interfaces string is already an IP value
			if addr := net.ParseIP(iName); addr != nil {
				a := addr.To4()
				if a == nil {
					continue
				}
				listenIPs = append(listenIPs, a.String())
				continue
			}

		ifaces:
			for iter := 0; iter < 5; iter++ {
				if iter > 0 {
					log.Infof("Could not get IP for interface %#v, sleeping %#v seconds till the next attempt (%#v out of %#v).", iName, iter*2, iter, 5)
					time.Sleep(time.Duration(iter*2) * time.Second)
				}

				done := false
				i, err := net.InterfaceByName(iName)
				// Maybe we need to raise an error that interface not available?
				if err != nil {
					continue
				}

				if addrs, aErr := i.Addrs(); aErr == nil && len(addrs) > 0 {
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

						v6 := ip.To16()
						v4 := ip.To4()

						if v6 != nil && v4 == nil {
							listenIPv6s = append(listenIPv6s, v6.String()+"%"+iName)
						}
						if v4 != nil {
							done = true
							listenIPs = append(listenIPs, v4.String())
						}
					}
				}

				if done {
					break ifaces
				}
			}
		}

		if len(listenIPs) == 0 {
			err = fmt.Errorf("Could not find IP for specified interfaces(IPs) %#v", confInterfaces)
			return
		}
	}

	if len(listenIPs) == 0 {
		listenIPs = append(listenIPs, "")
	}
	if len(listenIPv6s) == 0 {
		listenIPv6s = append(listenIPv6s, "")
	}

loopPorts:
	for p := confPortMax; p >= confPortMin; p-- {
		for _, ip := range listenIPs {
			addr := ip + ":" + strconv.Itoa(p)
			if !isPortUsed("tcp", addr) && !isPortUsed("udp", addr) {
				listenIP = ip
				listenPort = p
				break loopPorts
			}
		}
	}

	if len(listenIPv6s) != 0 {
		for _, ip := range listenIPv6s {
			addr := ip + ":" + strconv.Itoa(listenPort)
			if !isPortUsed("tcp6", addr) {
				listenIPv6 = ip
				break
			}
		}
	}

	if isPortUsed("tcp6", listenIPv6+":"+strconv.Itoa(listenPort)) {
		disableIPv6 = true
	}

	return
}

func isPortUsed(network string, addr string) bool {
	if strings.Contains(network, "tcp") {
		return isTCPPortUsed(network, addr)
	}
	return isUDPPortUsed(network, addr)
}

func isTCPPortUsed(network string, addr string) bool {
	conn, err := net.DialTimeout(network, addr, 100*time.Millisecond)
	if conn != nil && err == nil {
		conn.Close()
		return true
	} else if err != nil {
		cause := err.Error()
		if !strings.Contains(cause, "refused") {
			return true
		}
	}

	return false
}

// isUDPPortUsed checks whether UDP port is used by anyone
func isUDPPortUsed(network string, addr string) bool {
	udpaddr, _ := net.ResolveUDPAddr(network, addr)
	conn, err := net.ListenUDP(network, udpaddr)
	if conn != nil && err == nil {
		conn.Close()
		return false
	}

	return true
}

// ElementumURL returns elementum url for external calls
func ElementumURL() string {
	return GetHTTPHost()
}

// InternalProxyURL returns internal proxy url
func InternalProxyURL() string {
	ip := "127.0.0.1"
	if localIP, err := LocalIP(); err == nil {
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

	return nil
}
