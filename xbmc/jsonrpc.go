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
type Args []interface{}

// Object ...
type Object map[string]interface{}

// Results ...
var Results map[string]chan interface{}

var (
	XBMCLocalHost *XBMCHost = nil
	XBMCHosts               = []*XBMCHost{}

	// XBMCJSONRPCPort is a port for XBMCJSONRPC (RCP of Kodi)
	XBMCJSONRPCPort = "9090"

	// XBMCExJSONRPCPort is a port for XBMCExJSONRPC (RCP of python part of the plugin)
	XBMCExJSONRPCPort = "65221"

	mu sync.RWMutex
)

func Init() {
	mu.Lock()
	defer mu.Unlock()

	XBMCHosts = []*XBMCHost{}
	for _, host := range []string{
		"::1",
		"127.0.0.1",
	} {
		if conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, XBMCJSONRPCPort), time.Second*3); err == nil {
			XBMCLocalHost = &XBMCHost{host}
			XBMCHosts = append(XBMCHosts, XBMCLocalHost)
			conn.Close()
			log.Debugf("Adding local host %s", host)
		} else {
			log.Debugf("Could not connect to the host %s: %s", host, err)
		}
	}
}

func ContainsXBMCHost(host string) bool {
	mu.RLock()
	defer mu.RUnlock()

	for _, h := range XBMCHosts {
		if h.Host == host {
			return true
		}
	}
	return false
}

func AddLocalXBMCHost(host string) (*XBMCHost, error) {
	h, err := AddXBMCHost(host)
	XBMCLocalHost = h

	return h, err
}

func AddXBMCHost(host string) (*XBMCHost, error) {
	mu.Lock()
	defer mu.Unlock()

	for _, h := range XBMCHosts {
		if h != nil && h.Host == host {
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
		if h.Host == host {
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
	if ctx.GetHeader("User-Agent") != "plugin.video.elementum" {
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
		if h != nil && h.Host == host {
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

func (h XBMCHost) executeJSONRPC(method string, retVal interface{}, args Args) error {
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

func (h XBMCHost) executeJSONRPCO(method string, retVal interface{}, args Object) error {
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

func (h XBMCHost) executeJSONRPCEx(method string, retVal interface{}, args Args) error {
	if args == nil {
		args = Args{}
	}
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
