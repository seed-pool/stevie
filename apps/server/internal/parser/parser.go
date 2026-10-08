package parser

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Kind classifies a parsed media path.
type Kind string

const (
	KindMovie   Kind = "movie"
	KindTV      Kind = "tv"
	KindUnknown Kind = "unknown"
)

// Result is the identity inferred from a file path.
type Result struct {
	Kind    Kind
	Title   string
	Year    int
	Season  int
	Episode int
}

var (
	// SxxEyy anywhere in the basename (release-group style names).
	tvPatternSE = regexp.MustCompile(`(?i)^(.+?)[.\s_-]+S(\d{1,2})E(\d{1,3})(?:[.\s_-]|$)`)
	// 1x02 style.
	tvPatternX = regexp.MustCompile(`(?i)^(.+?)[.\s_-]+(\d{1,2})x(\d{1,3})(?:[.\s_-]|$)`)
	yearToken   = regexp.MustCompile(`(?:^|[\s._\-(])((?:19|20)\d{2})(?:$|[\s._\-)])`)
	yearPattern = regexp.MustCompile(`^(?P<title>.+?)[.\s_\-(]+(?P<year>(?:19|20)\d{2})(?:[.\s_\-)]|$)`)
	junkTokens  = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|4k|uhd|bluray|blu-ray|web[-.]?dl|webrip|hdtv|x264|x265|h\.?264|h\.?265|hevc|avc|dts|ddp?|aac|ac3|atmos|truehd|remux|proper|repack|extended|directors?\.?cut|multi|dual|dubbed|subs?|hdr|dv|dovi|nf|amzn|dsnp|hulu|cr|with\.audio\.description|audio\.description|flac|bluray)\b`)
	releaseGroup = regexp.MustCompile(`-[A-Za-z0-9]+$`)
	spaceClean   = regexp.MustCompile(`[\s._-]+`)
)

// Parse extracts title/year/season/episode hints from a media file path.
// libraryType may be "movie", "tv", or "mixed".
// For mixed libraries, SxxEyy / NxNN filenames become TV; everything else becomes a movie.
func Parse(path string, libraryType string) Result {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base = strings.TrimSpace(base)
	base = releaseGroup.ReplaceAllString(base, "")

	if season, episode, title, year, ok := matchTV(base, path); ok {
		return Result{
			Kind:    KindTV,
			Title:   title,
			Year:    year,
			Season:  season,
			Episode: episode,
		}
	}

	year := 0
	title := base
	if m := yearPattern.FindStringSubmatch(base); len(m) >= 3 {
		title = m[1]
		year, _ = strconv.Atoi(m[2])
	}
	title = cleanTitle(title)

	kind := KindUnknown
	switch libraryType {
	case "movie":
		kind = KindMovie
	case "tv":
		kind = KindTV
	case "mixed", "":
		kind = KindMovie
	default:
		if year > 0 {
			kind = KindMovie
		}
	}

	return Result{
		Kind:  kind,
		Title: title,
		Year:  year,
	}
}

func matchTV(base, path string) (season, episode int, title string, year int, ok bool) {
	var rawTitle string
	if m := tvPatternSE.FindStringSubmatch(base); len(m) >= 4 {
		season, _ = strconv.Atoi(m[2])
		episode, _ = strconv.Atoi(m[3])
		rawTitle = m[1]
	} else if m := tvPatternX.FindStringSubmatch(base); len(m) >= 4 {
		season, _ = strconv.Atoi(m[2])
		episode, _ = strconv.Atoi(m[3])
		rawTitle = m[1]
	} else {
		return 0, 0, "", 0, false
	}

	title, year = splitTitleYear(rawTitle)
	title = cleanTitle(title)
	if title == "" {
		parent := filepath.Base(filepath.Dir(path))
		title, year = splitTitleYear(releaseGroup.ReplaceAllString(parent, ""))
		title = cleanTitle(title)
		// Season-pack folders like Outside.2026.S01.1080p...
		title = stripSeasonToken(title)
		title = cleanTitle(title)
	}
	if title == "" {
		title = titleFromParents(path)
	}
	return season, episode, title, year, season > 0 && episode > 0 && title != ""
}

func splitTitleYear(s string) (title string, year int) {
	s = strings.TrimSpace(s)
	if m := yearPattern.FindStringSubmatch(s); len(m) >= 3 {
		y, _ := strconv.Atoi(m[2])
		return m[1], y
	}
	if m := yearToken.FindStringSubmatch(s); len(m) >= 2 {
		y, _ := strconv.Atoi(m[1])
		title = strings.TrimSpace(yearToken.ReplaceAllString(s, " "))
		return title, y
	}
	return s, 0
}

func stripSeasonToken(s string) string {
	re := regexp.MustCompile(`(?i)[\s._-]+S\d{1,2}\b`)
	return re.ReplaceAllString(s, " ")
}

func titleFromParents(path string) string {
	title := cleanTitle(filepath.Base(filepath.Dir(filepath.Dir(path))))
	if strings.HasPrefix(strings.ToLower(title), "season") {
		title = cleanTitle(filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path)))))
	}
	title, _ = splitTitleYear(title)
	title = stripSeasonToken(title)
	return cleanTitle(title)
}

func cleanTitle(s string) string {
	s = junkTokens.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "(", " ")
	s = strings.ReplaceAll(s, ")", " ")
	s = strings.ReplaceAll(s, "[", " ")
	s = strings.ReplaceAll(s, "]", " ")
	s = spaceClean.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
