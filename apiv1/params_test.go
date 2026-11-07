package apiv1

import "testing"

func TestCheckParams(t *testing.T) {
	page := func(n int) *int { return &n }
	for name, tc := range map[string]struct {
		page  *int
		limit *int
		enums []interface{ Valid() bool }
		bad   bool
	}{
		"defaults":     {},
		"valid":        {page: page(1), enums: []interface{ Valid() bool }{MovieListName("popular"), MediaKindShows}},
		"page zero":    {page: page(0), bad: true},
		"page below 0": {page: page(-3), bad: true},
		"limit ok":     {page: page(1), limit: page(100)},
		"limit zero":   {limit: page(0), bad: true},
		"limit 101":    {limit: page(101), bad: true},
		"bad enum":     {page: page(2), enums: []interface{ Valid() bool }{MediaKindMovies, MovieListName("nope")}, bad: true},
		"nil optional": {enums: []interface{ Valid() bool }{(*SearchType)(nil), MediaKindMovies}},
	} {
		t.Run(name, func(t *testing.T) {
			e := checkParams(tc.page, tc.limit, tc.enums...)
			if (e != nil) != tc.bad {
				t.Fatalf("got %+v, want bad=%v", e, tc.bad)
			}
			if e != nil && (e.Error.Code != ErrorCodeBadRequest || e.Error.Message == "") {
				t.Errorf("error = %+v", e.Error)
			}
		})
	}
}
