package bittorrent

import (
	"testing"

	"github.com/elgatito/elementum/config"
	"github.com/elgatito/elementum/library/uid"
)

func TestPlayerParamsResumePoint(t *testing.T) {
	stored, library := &uid.Resume{Position: 300}, &uid.Resume{Position: 120}
	empty, negative := &uid.Resume{}, &uid.Resume{Position: -1}
	for name, tc := range map[string]struct {
		stored, resume, want *uid.Resume
	}{
		"stored first":             {stored, library, stored},
		"stored only":              {stored, nil, stored},
		"library or tracker only":  {nil, library, library},
		"empty stored fallback":    {empty, library, library},
		"negative stored fallback": {negative, library, library},
		"missing":                  {},
		"empty":                    {empty, empty, nil},
		"negative":                 {negative, negative, nil},
	} {
		t.Run(name, func(t *testing.T) {
			p := PlayerParams{StoredResume: tc.stored, Resume: tc.resume}
			if got := p.ResumePoint(); got != tc.want {
				t.Errorf("got %p (%+v), want %p (%+v)", got, got, tc.want, tc.want)
			}
		})
	}
}

func TestPlayerParamsResumePosition(t *testing.T) {
	old := config.Get().PlayResumeBack
	t.Cleanup(func() { config.Get().PlayResumeBack = old })
	for name, tc := range map[string]struct {
		stored, resume *uid.Resume
		back           int
		want           float64
	}{
		"stored first":     {&uid.Resume{Position: 600}, &uid.Resume{Position: 120}, 10, 590},
		"library fallback": {&uid.Resume{}, &uid.Resume{Position: 120}, 10, 110},
		"no rewind":        {nil, &uid.Resume{Position: 120.5}, 0, 120.5},
		"negative rewind":  {nil, &uid.Resume{Position: 120}, -10, 120},
		"rewind to zero":   {nil, &uid.Resume{Position: 120}, 120, 0},
		"rewind past zero": {nil, &uid.Resume{Position: 120}, 130, 0},
		"missing":          {nil, nil, 0, -1},
		"missing rewind":   {nil, nil, 10, -1},
		"empty":            {&uid.Resume{}, &uid.Resume{}, 10, -1},
	} {
		t.Run(name, func(t *testing.T) {
			config.Get().PlayResumeBack = tc.back
			p := PlayerParams{StoredResume: tc.stored, Resume: tc.resume}
			raw := p.ResumePoint()
			var position float64
			if raw != nil {
				position = raw.Position
			}
			if got := p.ResumePosition(); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
			if p.ResumePoint() != raw || (raw != nil && raw.Position != position) {
				t.Fatal("raw resume point was modified")
			}
		})
	}
}
