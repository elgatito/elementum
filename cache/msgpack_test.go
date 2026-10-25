package cache

import (
	"reflect"
	"testing"
)

func TestFingerprint(t *testing.T) {
	type before struct {
		Title string `json:"title"`
	}
	type after struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
	}
	type retagged struct {
		Title string `json:"name"`
	}
	type node struct {
		Children []*node `json:"children"`
	}

	fp := func(v any) uint64 { return fingerprint(reflect.TypeOf(v)) }
	if fp(before{}) == fp(after{}) || fp(before{}) == fp(retagged{}) {
		t.Fatal("changed layout must change fingerprint")
	}
	if fp(before{}) != fp(&before{}) || fp([]before{}) != fp(&[]*before{}) {
		t.Fatal("pointers must not change fingerprint")
	}
	if fp(node{}) == 0 {
		t.Fatal("recursive type must have fingerprint")
	}
}
