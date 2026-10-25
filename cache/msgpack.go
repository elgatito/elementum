package cache

import (
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"reflect"
	"sync"

	"github.com/tinylib/msgp/msgp"
)

// msgpMarker prefixes values encoded with generated msgp bindings, it is a reserved msgpack byte, so vmihailenco entries never start with it
const msgpMarker = 0xc1

var msgpUnmarshalerType = reflect.TypeFor[msgp.Unmarshaler]()

// HasMsgp reports whether value's type has generated msgpack bindings, directly or through pointers and slices (e.g. *[]*Movie).
func HasMsgp(value any) bool {
	return value != nil && hasMsgp(reflect.TypeOf(value))
}

func hasMsgp(t reflect.Type) bool {
	if reflect.PointerTo(t).Implements(msgpUnmarshalerType) {
		return true
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice:
		return hasMsgp(t.Elem())
	}
	return false
}

// MarshalMsg appends value, which must satisfy HasMsgp, to b the same way msgp generated code does, prefixed with its type fingerprint.
func MarshalMsg(b []byte, value any) ([]byte, error) {
	if !HasMsgp(value) {
		return b, fmt.Errorf("no msgpack bindings for %T", value)
	}
	// copy into addressable value, as bindings mostly have pointer receivers
	v := reflect.New(reflect.TypeOf(value)).Elem()
	v.Set(reflect.ValueOf(value))
	return appendMsg(msgp.AppendUint64(b, fingerprint(v.Type())), v)
}

// UnmarshalMsg decodes b into value, which must be a non-nil pointer satisfying HasMsgp, allocating nil pointers.
// Data written for another layout of value's type is rejected, as msgp would silently skip or zero changed fields.
func UnmarshalMsg(b []byte, value any) ([]byte, error) {
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Pointer || v.IsNil() || !hasMsgp(v.Type()) {
		return b, fmt.Errorf("no msgpack bindings for %T", value)
	}
	fp, b, err := msgp.ReadUint64Bytes(b)
	if err != nil || fp != fingerprint(v.Type()) {
		return b, fmt.Errorf("cached data layout differs from %T", value)
	}
	return readMsg(b, v.Elem())
}

var fingerprints sync.Map // reflect.Type -> uint64

// fingerprint hashes t's structure (field names, tags and types, recursively), ignoring pointers as encoding does.
func fingerprint(t reflect.Type) uint64 {
	if fp, ok := fingerprints.Load(t); ok {
		return fp.(uint64)
	}
	h := fnv.New64a()
	describe(h, t, map[reflect.Type]bool{})
	fp := h.Sum64()
	fingerprints.Store(t, fp)
	return fp
}

func describe(w io.Writer, t reflect.Type, seen map[reflect.Type]bool) {
	if t.Kind() == reflect.Pointer {
		describe(w, t.Elem(), seen)
		return
	}
	io.WriteString(w, t.Kind().String())
	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		describe(w, t.Elem(), seen)
	case reflect.Map:
		describe(w, t.Key(), seen)
		describe(w, t.Elem(), seen)
	case reflect.Struct:
		// guards recursive types
		if seen[t] {
			return
		}
		seen[t] = true
		for i := range t.NumField() {
			if f := t.Field(i); f.IsExported() {
				fmt.Fprintf(w, "{%s %q ", f.Name, f.Tag)
				describe(w, f.Type, seen)
				io.WriteString(w, "}")
			}
		}
	}
}

func appendMsg(b []byte, v reflect.Value) ([]byte, error) {
	if m, ok := v.Addr().Interface().(msgp.Marshaler); ok {
		return m.MarshalMsg(b)
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return msgp.AppendNil(b), nil
		}
		return appendMsg(b, v.Elem())
	case reflect.Slice:
		b = msgp.AppendArrayHeader(b, uint32(v.Len()))
		var err error
		for i := range v.Len() {
			if b, err = appendMsg(b, v.Index(i)); err != nil {
				return b, err
			}
		}
		return b, nil
	}
	return b, errors.New("no msgpack bindings for " + v.Type().String())
}

func readMsg(b []byte, v reflect.Value) ([]byte, error) {
	if u, ok := v.Addr().Interface().(msgp.Unmarshaler); ok {
		return u.UnmarshalMsg(b)
	}
	switch v.Kind() {
	case reflect.Pointer:
		if msgp.IsNil(b) {
			v.SetZero()
			return msgp.ReadNilBytes(b)
		}
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return readMsg(b, v.Elem())
	case reflect.Slice:
		n, b, err := msgp.ReadArrayHeaderBytes(b)
		if err != nil {
			return b, err
		}
		s := reflect.MakeSlice(v.Type(), int(n), int(n))
		for i := range int(n) {
			if b, err = readMsg(b, s.Index(i)); err != nil {
				return b, err
			}
		}
		v.Set(s)
		return b, nil
	}
	return b, errors.New("no msgpack bindings for " + v.Type().String())
}
