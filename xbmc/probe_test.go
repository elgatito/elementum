package xbmc

import (
	"net"
	"strconv"
	"testing"
)

func TestProbeLocalHostPicksUpLateKodi(t *testing.T) {
	oldPort, oldLocal, oldHosts := XBMCJSONRPCPort, XBMCLocalHost, XBMCHosts
	defer func() { XBMCJSONRPCPort, XBMCLocalHost, XBMCHosts = oldPort, oldLocal, oldHosts }()
	XBMCLocalHost, XBMCHosts = nil, nil

	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close() // Kodi is not up yet
	XBMCJSONRPCPort = strconv.Itoa(port)

	ProbeLocalHost()
	if h, _ := GetLocalXBMCHost(); h != nil {
		t.Fatal("host registered without Kodi")
	}

	l, err := net.Listen("tcp", "127.0.0.1:"+XBMCJSONRPCPort) // Kodi starts
	if err != nil {
		t.Skip(err)
	}
	defer l.Close()
	ProbeLocalHost()
	ProbeLocalHost()
	if h, _ := GetLocalXBMCHost(); h == nil || len(XBMCHosts) != 1 {
		t.Fatalf("want one local host, got %v %v", h, XBMCHosts)
	}
}
