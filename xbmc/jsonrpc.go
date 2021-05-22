package xbmc

import (
	"errors"
	"net"
	"time"

	"github.com/elgatito/elementum/jsonrpc"
)

// Args ...
type Args []interface{}

// Object ...
type Object map[string]interface{}

// Results ...
var Results map[string]chan interface{}

var (
	// XBMCJSONRPCHosts ...
	XBMCJSONRPCHosts = []string{
		net.JoinHostPort("127.0.0.1", "9090"),
	}
	// XBMCExJSONRPCHosts ...
	XBMCExJSONRPCHosts = []string{
		net.JoinHostPort("127.0.0.1", "65221"),
	}

	// LastCallerIP represents the IP of last request, made by client to backend.
	LastCallerIP = ""

	// XBMCExJSONRPCPort is a port for XBMCExJSONRPC (RCP of python part of the plugin)
	XBMCExJSONRPCPort = "65221"
)

func getXBMCExJSONRPCHosts() []string {
	if LastCallerIP != "" {
		return []string{net.JoinHostPort(LastCallerIP, XBMCExJSONRPCPort)}
	}

	return XBMCExJSONRPCHosts
}

func getConnection(hosts ...string) (net.Conn, error) {
	var err error

	for _, host := range hosts {
		if c, errCon := net.DialTimeout("tcp", host, time.Second*5); errCon == nil {
			return c, nil
		}
	}

	return nil, err
}

func executeJSONRPC(method string, retVal interface{}, args Args) error {
	if args == nil {
		args = Args{}
	}
	conn, err := getConnection(XBMCJSONRPCHosts...)
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

func executeJSONRPCO(method string, retVal interface{}, args Object) error {
	if args == nil {
		args = Object{}
	}
	conn, err := getConnection(XBMCJSONRPCHosts...)
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

func executeJSONRPCEx(method string, retVal interface{}, args Args) error {
	if args == nil {
		args = Args{}
	}
	conn, err := getConnection(getXBMCExJSONRPCHosts()...)
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
