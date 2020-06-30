package util

// StringSliceContains ...
func StringSliceContains(ary []string, q string) bool {
	for _, i := range ary {
		if i == q {
			return true
		}
	}

	return false
}
