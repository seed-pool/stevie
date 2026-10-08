package livetv

import (
	"strings"
	"time"
	"unicode/utf8"
)

// cleanUTF8 ensures Postgres-safe text. Invalid UTF-8 from panels is reinterpreted
// as Latin-1 (byte→rune) so we keep readable characters instead of aborting the import.
func cleanUTF8(s string) string {
	if s == "" || utf8.ValidString(s) {
		return s
	}
	b := []byte(s)
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// yearPrefix returns a 4-digit ASCII year from a date string without slicing mid-rune.
func yearPrefix(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 4 && s[0] >= '0' && s[0] <= '9' && s[1] >= '0' && s[1] <= '9' &&
		s[2] >= '0' && s[2] <= '9' && s[3] >= '0' && s[3] <= '9' {
		return s[:4]
	}
	return ""
}

const (
	VodKindMovie  = "movie"
	VodKindSeries = "series"
)

// VodCategoryInfo is a panel VOD/series category for store upsert.
type VodCategoryInfo struct {
	Kind       string
	ExternalID string
	Name       string
	SortOrder  int
	TitleCount int
}

// VodMovie is a lightweight imported movie row.
type VodMovie struct {
	ExternalID         string
	CategoryExternalID string
	Name               string
	Plot               string
	PosterURL          string
	BackdropURL        string
	TMDBID             string
	Rating             float64
	Year               string
	ReleaseDate        string
	Genre              string
	Director           string
	Cast               string
	Duration           string
	Container          string
	Trailer            string
	Resolution         string
	VideoCodec         string
	AudioCodec         string
	SourceQuality      string
	HDR                string
	BitrateKbps        int
	Width              int
	Height             int
	AddedAt            *time.Time
	SortOrder          int
}

// VodSeries is a lightweight imported series row (no episodes).
type VodSeries struct {
	ExternalID         string
	CategoryExternalID string
	Name               string
	Plot               string
	PosterURL          string
	BackdropURL        string
	TMDBID             string
	Rating             float64
	Year               string
	ReleaseDate        string
	Genre              string
	Director           string
	Cast               string
	EpisodeRunTime     string
	Trailer            string
	LastModified       *time.Time
	SortOrder          int
}

// MoviesFromStreams maps panel VOD streams into store rows for one category.
func (x *XtreamClient) MoviesFromStreams(streams []XtreamVodStream, categoryID, categoryName string) []VodMovie {
	_ = categoryName
	out := make([]VodMovie, 0, len(streams))
	for i, st := range streams {
		sid := anyString(st.StreamID)
		if sid == "" {
			continue
		}
		catID := anyString(st.CategoryID)
		if catID == "" {
			catID = categoryID
		}
		name := cleanUTF8(strings.TrimSpace(st.Name))
		if name == "" {
			name = "Movie " + sid
		}
		year := anyString(st.Year)
		if year == "" {
			year = yearPrefix(st.ReleaseDate)
		}
		tech := ParseVodTech(name)
		out = append(out, VodMovie{
			ExternalID:         sid,
			CategoryExternalID: catID,
			Name:               name,
			Plot:               cleanUTF8(strings.TrimSpace(st.Plot)),
			PosterURL:          cleanUTF8(strings.TrimSpace(st.StreamIcon)),
			BackdropURL:        cleanUTF8(firstBackdrop(st.BackdropPath)),
			TMDBID:             anyString(st.TMDB),
			Rating:             floatRating(st.Rating),
			Year:               year,
			ReleaseDate:        strings.TrimSpace(st.ReleaseDate),
			Genre:              cleanUTF8(strings.TrimSpace(st.Genre)),
			Director:           cleanUTF8(strings.TrimSpace(st.Director)),
			Cast:               cleanUTF8(strings.TrimSpace(st.Cast)),
			Duration:           strings.TrimSpace(st.Duration),
			Container:          strings.TrimSpace(st.ContainerExtension),
			Trailer:            strings.TrimSpace(st.YoutubeTrailer),
			Resolution:         tech.Resolution,
			VideoCodec:         tech.VideoCodec,
			AudioCodec:         tech.AudioCodec,
			SourceQuality:      tech.Source,
			HDR:                tech.HDR,
			BitrateKbps:        tech.BitrateKbps,
			Width:              tech.Width,
			Height:             tech.Height,
			AddedAt:            unixOrEmpty(st.Added),
			SortOrder:          i,
		})
	}
	return out
}

// SeriesFromItems maps panel series list items into store rows for one category.
func (x *XtreamClient) SeriesFromItems(items []XtreamSeriesItem, categoryID, categoryName string) []VodSeries {
	_ = categoryName
	out := make([]VodSeries, 0, len(items))
	for i, st := range items {
		sid := anyString(st.SeriesID)
		if sid == "" {
			continue
		}
		catID := anyString(st.CategoryID)
		if catID == "" {
			catID = categoryID
		}
		name := cleanUTF8(strings.TrimSpace(st.Name))
		if name == "" {
			name = "Series " + sid
		}
		year := yearPrefix(st.ReleaseDate)
		out = append(out, VodSeries{
			ExternalID:         sid,
			CategoryExternalID: catID,
			Name:               name,
			Plot:               cleanUTF8(strings.TrimSpace(st.Plot)),
			PosterURL:          cleanUTF8(strings.TrimSpace(st.Cover)),
			BackdropURL:        cleanUTF8(firstBackdrop(st.BackdropPath)),
			TMDBID:             anyString(st.TMDB),
			Rating:             floatRating(st.Rating),
			Year:               year,
			ReleaseDate:        strings.TrimSpace(st.ReleaseDate),
			Genre:              cleanUTF8(strings.TrimSpace(st.Genre)),
			Director:           cleanUTF8(strings.TrimSpace(st.Director)),
			Cast:               cleanUTF8(strings.TrimSpace(st.Cast)),
			EpisodeRunTime:     anyString(st.EpisodeRunTime),
			Trailer:            strings.TrimSpace(st.YoutubeTrailer),
			LastModified:       unixOrEmpty(st.LastModified),
			SortOrder:          i,
		})
	}
	return out
}

// FirstBackdropPublic extracts the first backdrop URL from panel JSON shapes.
func FirstBackdropPublic(v any) string { return firstBackdrop(v) }

// IsAdultCategoryName reports names that Select All should skip by default.
func IsAdultCategoryName(name string) bool {
	s := strings.ToLower(strings.TrimSpace(name))
	for _, needle := range []string{"adult", "xxx", "porn", "erotic", "+18", "18+"} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
