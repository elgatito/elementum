package util

import (
	"runtime"
	"runtime/debug"
)

// FreeMemory runs FreeOSMemory() only
func FreeMemory() {
	debug.FreeOSMemory()
}

// FreeMemoryGC runs FreeOSMemory() and GC()
func FreeMemoryGC() {
	runtime.GC()
	debug.FreeOSMemory()
}
