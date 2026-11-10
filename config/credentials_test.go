package config

import "testing"

func TestApplyTrackerCredentials(t *testing.T) {
	settings := XbmcSettings{}
	for _, id := range deviceTrackers {
		settings[id+"_token"] = id + "-token"
		settings[id+"_refresh_token"] = id + "-refresh"
		settings[id+"_token_expiry"] = int64(12345)
		settings[id+"_username"] = id + "-user"
	}
	var cfg Configuration
	applyWriteBack(settings, &cfg)
	for _, id := range deviceTrackers {
		want := TrackerCredentials{Token: id + "-token", RefreshToken: id + "-refresh", TokenExpiry: 12345, Username: id + "-user"}
		if got := cfg.TrackerCredentials[id]; got == nil || *got != want {
			t.Fatalf("%s credentials=%+v", id, got)
		}
	}
	c := cfg.TrackerCredentials["trakt"]
	if cfg.TraktToken != c.Token || cfg.TraktRefreshToken != c.RefreshToken || cfg.TraktUsername != c.Username || cfg.TraktTokenExpiry != c.TokenExpiry {
		t.Fatal("legacy Trakt credentials differ from shared credentials")
	}
}

func TestCastDefaultKodiTypes(t *testing.T) {
	for _, tc := range []struct {
		typ, opt, value string
		want            any
	}{
		{"enum", "", "2", 2}, {"number", "", "bad", 0},
		{"slider", "percent", "75.5", 75}, {"slider", "int", "-2.5", -2},
		{"slider", "float", "1.5", float32(1.5)}, {"slider", "float", "0", 0}, {"slider", "float", "-1.5", 0},
		{"slider", "unknown", "4", 0}, {"bool", "", "true", true}, {"text", "", "foo", "foo"},
	} {
		if got := castDefault(tc.typ, tc.opt, tc.value); got != tc.want {
			t.Errorf("castDefault(%q,%q,%q)=%#v, want %#v", tc.typ, tc.opt, tc.value, got, tc.want)
		}
	}
}
