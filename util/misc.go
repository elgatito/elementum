package util

// to make "choose art" work we can set fake DBID,
// but the issue is how to generate it w/o clashing with real library IDs
// we we try to use very high numbers
const KodiDBIDMax = 1000000000
const MovieFakeDBIDOffset = KodiDBIDMax
const ShowFakeDBIDOffset = KodiDBIDMax
const EpisodeFakeDBIDOffset = KodiDBIDMax

func GetMovieFakeDBID(id int) int {
	if id > 0 && id <= KodiDBIDMax {
		return id + MovieFakeDBIDOffset
	}
	return 0
}

func GetShowFakeDBID(id int) int {
	if id > 0 && id <= KodiDBIDMax {
		return id + ShowFakeDBIDOffset
	}
	return 0
}

func GetEpisodeFakeDBID(id int) int {
	if id > 0 && id <= KodiDBIDMax {
		return id + EpisodeFakeDBIDOffset
	}
	return 0
}
