package util

import (
	"strconv"
)

// StrInterfaceToInt ...
func StrInterfaceToInt(t interface{}) (i int) {
	switch t := t.(type) {
	case string:
		if v, err := strconv.Atoi(t); err == nil {
			i = v
		}
	case float32:
		i = int(t)
	case float64:
		i = int(t)
	case int:
		i = t
	}
	return i
}
