package tracker

// DiffItems returns items of other that are missing in base.
func DiffItems(base, other []*Item) []*Item {
	known := make(map[string]struct{}, len(base))
	for _, i := range base {
		if i != nil {
			known[i.Key()] = struct{}{}
		}
	}

	ret := make([]*Item, 0)
	for _, i := range other {
		if i == nil {
			continue
		}
		if _, ok := known[i.Key()]; !ok {
			ret = append(ret, i)
		}
	}
	return ret
}

// DiffWatchedMovies returns movies of other that are missing in base.
// With checkDate a movie also counts as missing when other has a newer watch than base.
func DiffWatchedMovies(base, other []*WatchedItem, checkDate bool) []*WatchedItem {
	known := make(map[string]*WatchedItem, len(base))
	for _, m := range base {
		if m != nil {
			known[m.Key()] = m
		}
	}

	ret := make([]*WatchedItem, 0)
	for _, m := range other {
		if m == nil {
			continue
		}
		if b, ok := known[m.Key()]; !ok || (checkDate && m.LastWatchedAt.After(b.LastWatchedAt)) {
			ret = append(ret, m)
		}
	}
	return ret
}

// DiffWatchedShows returns shows of other that are missing in base, and for shows present in both
// a copy holding only the episodes missing in base. Without a base there is nothing to compare, so it returns nothing.
func DiffWatchedShows(base, other []*WatchedItem) []*WatchedItem {
	if len(base) == 0 || len(other) == 0 {
		return nil
	}

	known := make(map[string]map[[2]int]struct{}, len(base))
	for _, s := range base {
		if s == nil {
			continue
		}
		eps := make(map[[2]int]struct{}, len(s.Episodes))
		for _, e := range s.Episodes {
			eps[[2]int{e.Season, e.Number}] = struct{}{}
		}
		known[s.Key()] = eps
	}

	var ret []*WatchedItem
	for _, s := range other {
		if s == nil {
			continue
		}
		eps, ok := known[s.Key()]
		if !ok {
			ret = append(ret, s)
			continue
		}

		var missing []WatchedEpisode
		for _, e := range s.Episodes {
			if _, ok := eps[[2]int{e.Season, e.Number}]; !ok {
				missing = append(missing, e)
			}
		}
		if len(missing) > 0 {
			ret = append(ret, &WatchedItem{Item: s.Item, LastWatchedAt: s.LastWatchedAt, Episodes: missing})
		}
	}
	return ret
}
