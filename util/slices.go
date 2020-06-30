package util

import "reflect"

// Distinct ...
func Distinct(arr interface{}) (reflect.Value, bool) {
	// create a slice from our input interface
	slice, ok := takeArg(arr, reflect.Slice)
	if !ok {
		return reflect.Value{}, ok
	}

	// put the values of our slice into a map
	// the key's of the map will be the slice's unique values
	c := slice.Len()
	m := make(map[interface{}]bool)
	for i := 0; i < c; i++ {
		m[slice.Index(i).Interface()] = true
	}
	mapLen := len(m)

	// create the output slice and populate it with the map's keys
	out := reflect.MakeSlice(reflect.TypeOf(arr), mapLen, mapLen)
	i := 0
	for k := range m {
		v := reflect.ValueOf(k)
		o := out.Index(i)
		o.Set(v)
		i++
	}

	return out, ok
}

// Difference ...
func Difference(arrs ...interface{}) (reflect.Value, bool) {
	// create a temporary map to hold the contents of the arrays
	tempMap := make(map[interface{}]int)
	var kind reflect.Kind
	var kindHasBeenSet bool

	for _, arg := range arrs {
		tempArr, ok := Distinct(arg)
		if !ok {
			return reflect.Value{}, ok
		}

		// check to be sure the type hasn't changed
		if kindHasBeenSet && tempArr.Len() > 0 && tempArr.Index(0).Kind() != kind {
			return reflect.Value{}, false
		}
		if tempArr.Len() > 0 {
			kindHasBeenSet = true
			kind = tempArr.Index(0).Kind()
		}

		c := tempArr.Len()
		for idx := 0; idx < c; idx++ {
			// how many times have we encountered this elem?
			if _, ok := tempMap[tempArr.Index(idx).Interface()]; ok {
				tempMap[tempArr.Index(idx).Interface()]++
			} else {
				tempMap[tempArr.Index(idx).Interface()] = 1
			}
		}
	}

	// write the final val of the diffMap to an array and return
	numElems := 0
	for _, v := range tempMap {
		if v == 1 {
			numElems++
		}
	}
	out := reflect.MakeSlice(reflect.TypeOf(arrs[0]), numElems, numElems)
	i := 0
	for key, val := range tempMap {
		if val == 1 {
			v := reflect.ValueOf(key)
			o := out.Index(i)
			o.Set(v)
			i++
		}
	}

	return out, true
}

func takeArg(arg interface{}, kind reflect.Kind) (val reflect.Value, ok bool) {
	val = reflect.ValueOf(arg)
	if val.Kind() == kind {
		ok = true
	}
	return
}
