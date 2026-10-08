package livetv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// XtreamCredentials holds panel login details from the environment.
type XtreamCredentials struct {
	BaseURL  string
	Username string
	Password string
}

func (c XtreamCredentials) Configured() bool {
	return strings.TrimSpace(c.BaseURL) != "" &&
		strings.TrimSpace(c.Username) != "" &&
		strings.TrimSpace(c.Password) != ""
}

func (c XtreamCredentials) normalizedBase() string {
	return strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
}

// XtreamCategory is one live category from player_api.
type XtreamCategory struct {
	CategoryID   string `json:"category_id"`
	CategoryName string `json:"category_name"`
	ParentID     any    `json:"parent_id"`
}

// XtreamStream is one live stream from player_api.
type XtreamStream struct {
	Num              any    `json:"num"`
	Name             string `json:"name"`
	StreamType       string `json:"stream_type"`
	StreamID         any    `json:"stream_id"`
	StreamIcon       string `json:"stream_icon"`
	EPGChannelID     string `json:"epg_channel_id"`
	CategoryID       any    `json:"category_id"`
	TVArchive        any    `json:"tv_archive"`
	DirectSource     string `json:"direct_source"`
	TVArchiveDuration any   `json:"tv_archive_duration"`
}

type XtreamUserInfo struct {
	Auth          any    `json:"auth"`
	Status        string `json:"status"`
	ExpDate       string `json:"exp_date"`
	IsTrial       string `json:"is_trial"`
	ActiveCons    string `json:"active_cons"`
	CreatedAt     string `json:"created_at"`
	MaxConnections string `json:"max_connections"`
	Username      string `json:"username"`
}

type XtreamAuthResponse struct {
	UserInfo   XtreamUserInfo   `json:"user_info"`
	ServerInfo map[string]any   `json:"server_info"`
}

// XtreamClient talks to an Xtream Codes compatible panel.
type XtreamClient struct {
	cred   XtreamCredentials
	client *http.Client
}

func NewXtreamClient(cred XtreamCredentials) *XtreamClient {
	return &XtreamClient{
		cred: cred,
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (x *XtreamClient) Configured() bool {
	return x != nil && x.cred.Configured()
}

func (x *XtreamClient) playerAPI(ctx context.Context, action string) (io.ReadCloser, error) {
	return x.playerAPIParams(ctx, action, nil)
}

func (x *XtreamClient) playerAPIParams(ctx context.Context, action string, extra url.Values) (io.ReadCloser, error) {
	if !x.Configured() {
		return nil, fmt.Errorf("xtream not configured")
	}
	u, err := url.Parse(x.cred.normalizedBase() + "/player_api.php")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("username", x.cred.Username)
	q.Set("password", x.cred.Password)
	if action != "" {
		q.Set("action", action)
	}
	for k, vals := range extra {
		for _, v := range vals {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Stevie/1.0")
	res, err := x.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		res.Body.Close()
		return nil, fmt.Errorf("xtream player_api %s: HTTP %d", action, res.StatusCode)
	}
	return res.Body, nil
}

func (x *XtreamClient) Authenticate(ctx context.Context) (XtreamAuthResponse, error) {
	rc, err := x.playerAPI(ctx, "")
	if err != nil {
		return XtreamAuthResponse{}, err
	}
	defer rc.Close()
	var out XtreamAuthResponse
	if err := json.NewDecoder(rc).Decode(&out); err != nil {
		return XtreamAuthResponse{}, fmt.Errorf("xtream auth decode: %w", err)
	}
	status := strings.ToLower(strings.TrimSpace(out.UserInfo.Status))
	if status != "" && status != "active" {
		return out, fmt.Errorf("xtream account status: %s", out.UserInfo.Status)
	}
	// Some panels omit status but set auth=1 / auth="1".
	switch v := out.UserInfo.Auth.(type) {
	case float64:
		if v == 0 {
			return out, fmt.Errorf("xtream authentication failed")
		}
	case string:
		if v == "0" || strings.EqualFold(v, "false") {
			return out, fmt.Errorf("xtream authentication failed")
		}
	}
	return out, nil
}

func (x *XtreamClient) LiveCategories(ctx context.Context) ([]XtreamCategory, error) {
	rc, err := x.playerAPI(ctx, "get_live_categories")
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var cats []XtreamCategory
	if err := json.NewDecoder(rc).Decode(&cats); err != nil {
		return nil, fmt.Errorf("xtream categories: %w", err)
	}
	return cats, nil
}

func (x *XtreamClient) LiveStreams(ctx context.Context) ([]XtreamStream, error) {
	rc, err := x.playerAPI(ctx, "get_live_streams")
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var streams []XtreamStream
	if err := json.NewDecoder(rc).Decode(&streams); err != nil {
		return nil, fmt.Errorf("xtream streams: %w", err)
	}
	return streams, nil
}

func (x *XtreamClient) OpenXMLTV(ctx context.Context) (io.ReadCloser, error) {
	if !x.Configured() {
		return nil, fmt.Errorf("xtream not configured")
	}
	u, err := url.Parse(x.cred.normalizedBase() + "/xmltv.php")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("username", x.cred.Username)
	q.Set("password", x.cred.Password)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Stevie/1.0")
	res, err := x.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		res.Body.Close()
		return nil, fmt.Errorf("xtream xmltv: HTTP %d", res.StatusCode)
	}
	return res.Body, nil
}

func (x *XtreamClient) StreamURL(streamID string) string {
	base := x.cred.normalizedBase()
	return fmt.Sprintf("%s/live/%s/%s/%s.m3u8",
		base,
		url.PathEscape(x.cred.Username),
		url.PathEscape(x.cred.Password),
		url.PathEscape(streamID),
	)
}

// XtreamVodStream is one VOD title from get_vod_streams.
type XtreamVodStream struct {
	Num                any    `json:"num"`
	Name               string `json:"name"`
	StreamType         string `json:"stream_type"`
	StreamID           any    `json:"stream_id"`
	StreamIcon         string `json:"stream_icon"`
	Rating             any    `json:"rating"`
	Rating5Based       any    `json:"rating_5based"`
	Added              any    `json:"added"`
	CategoryID         any    `json:"category_id"`
	ContainerExtension string `json:"container_extension"`
	DirectSource       string `json:"direct_source"`
	TMDB               any    `json:"tmdb"`
	Plot               string `json:"plot"`
	ReleaseDate        string `json:"releaseDate"`
	Year               any    `json:"year"`
	Genre              string `json:"genre"`
	Director           string `json:"director"`
	Cast               string `json:"cast"`
	Duration           string `json:"duration"`
	BackdropPath       any    `json:"backdrop_path"`
	YoutubeTrailer     string `json:"youtube_trailer"`
}

// XtreamSeriesItem is one series from get_series.
type XtreamSeriesItem struct {
	Num            any    `json:"num"`
	Name           string `json:"name"`
	SeriesID       any    `json:"series_id"`
	Cover          string `json:"cover"`
	Plot           string `json:"plot"`
	Cast           string `json:"cast"`
	Director       string `json:"director"`
	Genre          string `json:"genre"`
	ReleaseDate    string `json:"releaseDate"`
	LastModified   any    `json:"last_modified"`
	Rating         any    `json:"rating"`
	Rating5Based   any    `json:"rating_5based"`
	BackdropPath   any    `json:"backdrop_path"`
	YoutubeTrailer string `json:"youtube_trailer"`
	TMDB           any    `json:"tmdb"`
	EpisodeRunTime any    `json:"episode_run_time"`
	CategoryID     any    `json:"category_id"`
}

// XtreamVodInfo is the get_vod_info payload (info + movie_data).
type XtreamVodInfo struct {
	Info      map[string]any `json:"info"`
	MovieData map[string]any `json:"movie_data"`
}

// XtreamSeriesInfo is the get_series_info payload.
type XtreamSeriesInfo struct {
	Info     map[string]any              `json:"info"`
	Seasons  []map[string]any            `json:"seasons"`
	Episodes map[string][]map[string]any `json:"episodes"`
}

func (x *XtreamClient) VodCategories(ctx context.Context) ([]XtreamCategory, error) {
	rc, err := x.playerAPI(ctx, "get_vod_categories")
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var cats []XtreamCategory
	if err := json.NewDecoder(rc).Decode(&cats); err != nil {
		return nil, fmt.Errorf("xtream vod categories: %w", err)
	}
	return cats, nil
}

func (x *XtreamClient) VodStreams(ctx context.Context, categoryID string) ([]XtreamVodStream, error) {
	extra := url.Values{}
	if categoryID != "" {
		extra.Set("category_id", categoryID)
	}
	rc, err := x.playerAPIParams(ctx, "get_vod_streams", extra)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var streams []XtreamVodStream
	if err := json.NewDecoder(rc).Decode(&streams); err != nil {
		return nil, fmt.Errorf("xtream vod streams: %w", err)
	}
	return streams, nil
}

func (x *XtreamClient) VodInfo(ctx context.Context, vodID string) (XtreamVodInfo, error) {
	rc, err := x.playerAPIParams(ctx, "get_vod_info", url.Values{"vod_id": {vodID}})
	if err != nil {
		return XtreamVodInfo{}, err
	}
	defer rc.Close()
	var out XtreamVodInfo
	if err := json.NewDecoder(rc).Decode(&out); err != nil {
		return XtreamVodInfo{}, fmt.Errorf("xtream vod info: %w", err)
	}
	return out, nil
}

func (x *XtreamClient) SeriesCategories(ctx context.Context) ([]XtreamCategory, error) {
	rc, err := x.playerAPI(ctx, "get_series_categories")
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var cats []XtreamCategory
	if err := json.NewDecoder(rc).Decode(&cats); err != nil {
		return nil, fmt.Errorf("xtream series categories: %w", err)
	}
	return cats, nil
}

func (x *XtreamClient) SeriesList(ctx context.Context, categoryID string) ([]XtreamSeriesItem, error) {
	extra := url.Values{}
	if categoryID != "" {
		extra.Set("category_id", categoryID)
	}
	rc, err := x.playerAPIParams(ctx, "get_series", extra)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var items []XtreamSeriesItem
	if err := json.NewDecoder(rc).Decode(&items); err != nil {
		return nil, fmt.Errorf("xtream series: %w", err)
	}
	return items, nil
}

func (x *XtreamClient) SeriesInfo(ctx context.Context, seriesID string) (XtreamSeriesInfo, error) {
	rc, err := x.playerAPIParams(ctx, "get_series_info", url.Values{"series_id": {seriesID}})
	if err != nil {
		return XtreamSeriesInfo{}, err
	}
	defer rc.Close()
	var out XtreamSeriesInfo
	if err := json.NewDecoder(rc).Decode(&out); err != nil {
		return XtreamSeriesInfo{}, fmt.Errorf("xtream series info: %w", err)
	}
	return out, nil
}

func (x *XtreamClient) MovieStreamURL(streamID, ext string) string {
	ext = strings.TrimPrefix(strings.TrimSpace(ext), ".")
	if ext == "" {
		ext = "mkv"
	}
	base := x.cred.normalizedBase()
	return fmt.Sprintf("%s/movie/%s/%s/%s.%s",
		base,
		url.PathEscape(x.cred.Username),
		url.PathEscape(x.cred.Password),
		url.PathEscape(streamID),
		url.PathEscape(ext),
	)
}

func (x *XtreamClient) SeriesEpisodeURL(episodeID, ext string) string {
	ext = strings.TrimPrefix(strings.TrimSpace(ext), ".")
	if ext == "" {
		ext = "mkv"
	}
	base := x.cred.normalizedBase()
	return fmt.Sprintf("%s/series/%s/%s/%s.%s",
		base,
		url.PathEscape(x.cred.Username),
		url.PathEscape(x.cred.Password),
		url.PathEscape(episodeID),
		url.PathEscape(ext),
	)
}

func firstBackdrop(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return ""
		}
		// Panels sometimes store JSON array text: ["https://..."]
		if strings.HasPrefix(s, "[") {
			var arr []any
			if json.Unmarshal([]byte(s), &arr) == nil {
				return firstBackdrop(arr)
			}
		}
		if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
			return s
		}
		return ""
	case []any:
		for _, item := range t {
			if s := firstBackdrop(item); s != "" {
				return s
			}
		}
	case []string:
		for _, s := range t {
			if s = firstBackdrop(s); s != "" {
				return s
			}
		}
	}
	return ""
}

func floatRating(v any) float64 {
	s := anyString(v)
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func unixOrEmpty(v any) *time.Time {
	s := anyString(v)
	if s == "" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return nil
	}
	t := time.Unix(n, 0).UTC()
	return &t
}

func anyString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return cleanUTF8(strings.TrimSpace(t))
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	default:
		return cleanUTF8(strings.TrimSpace(fmt.Sprint(t)))
	}
}

func anyInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	default:
		s := anyString(v)
		if s == "" {
			return 0
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			f, err2 := strconv.ParseFloat(s, 64)
			if err2 != nil {
				return 0
			}
			return int(f)
		}
		return n
	}
}

// ChannelsFromStreams maps panel streams into livetv.Channel rows for selected categories.
func (x *XtreamClient) ChannelsFromStreams(streams []XtreamStream, selected map[string]string) []Channel {
	out := make([]Channel, 0, len(streams))
	order := 0
	for _, st := range streams {
		catID := anyString(st.CategoryID)
		groupName, ok := selected[catID]
		if !ok {
			continue
		}
		sid := anyString(st.StreamID)
		if sid == "" {
			continue
		}
		name := strings.TrimSpace(st.Name)
		if name == "" {
			name = "Channel " + sid
		}
		epg := strings.TrimSpace(st.EPGChannelID)
		tvg := epg
		if tvg == "" {
			tvg = sid
		}
		streamURL := strings.TrimSpace(st.DirectSource)
		if streamURL == "" {
			streamURL = x.StreamURL(sid)
		} else {
			streamURL = preferXtreamHLS(streamURL)
		}
		out = append(out, Channel{
			TVGID:              tvg,
			Name:               name,
			GroupTitle:         groupName,
			LogoURL:            SanitizeLogoURL(st.StreamIcon),
			StreamURL:          streamURL,
			SortOrder:          order,
			Source:             SourceXtream,
			ExternalID:         sid,
			CategoryExternalID: catID,
			EPGChannelID:       epg,
			Num:                anyInt(st.Num),
		})
		order++
	}
	return out
}
