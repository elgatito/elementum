package tmdb

import "testing"

func TestGetManyPreservesOrder(t *testing.T) {
	got := getMany([]int{3, 1, 2}, func(id int) *int {
		v := id * 10
		return &v
	})

	want := []int{30, 10, 20}
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d", len(got), len(want))
	}

	for i, v := range got {
		if v == nil || *v != want[i] {
			t.Fatalf("item %d = %v, want %d", i, v, want[i])
		}
	}
}
