package util

import (
	"sync"
)

func SyncMapLen(m *sync.Map) int {
	var i int
	m.Range(func(k, v any) bool {
		i++
		return true
	})
	return i
}
