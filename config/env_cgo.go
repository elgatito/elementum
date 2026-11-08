//go:build cgo

package config

/*
#include <stdlib.h>
*/
import "C"

import (
	"os"
	"unsafe"
)

// lookupEnv reads the live libc environment as a fallback: a c-shared library loaded into Python
// only sees variables set before load through os.Getenv, but the host's later putenv shows up in libc.
func lookupEnv(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	k := C.CString(key)
	defer C.free(unsafe.Pointer(k))
	return C.GoString(C.getenv(k))
}

// cSetenv simulates the host calling putenv after load (used by tests).
func cSetenv(key, value string) {
	k, v := C.CString(key), C.CString(value)
	defer C.free(unsafe.Pointer(k))
	defer C.free(unsafe.Pointer(v))
	C.setenv(k, v, 1)
}
