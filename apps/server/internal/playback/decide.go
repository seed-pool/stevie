package playback

import (
	"strings"

	"github.com/stevie-media/stevie/apps/server/internal/store"
)

type Mode string

const (
	ModeDirect      Mode = "direct"
	ModeRemux       Mode = "remux"
	ModeUnsupported Mode = "unsupported"
)

// ClientCaps describes what the browser reports it can decode.
type ClientCaps struct {
	HEVC bool `json:"hevc"`
	AV1  bool `json:"av1"`
	VP9  bool `json:"vp9"`
	AAC  bool `json:"aac"`
	MP3  bool `json:"mp3"`
	AC3  bool `json:"ac3"`
	EAC3 bool `json:"eac3"`
	Opus bool `json:"opus"`
	FLAC bool `json:"flac"`
}

type Track struct {
	Index       int    `json:"index"`
	Codec       string `json:"codec,omitempty"`
	Language    string `json:"language,omitempty"`
	Title       string `json:"title,omitempty"`
	Channels    int    `json:"channels,omitempty"`
	Default     bool   `json:"default"`
	Forced      bool   `json:"forced"`
	Commentary  bool   `json:"commentary,omitempty"`
	TextBased   bool   `json:"text_based,omitempty"`
	BrowserSafe bool   `json:"browser_safe"`
}

type Decision struct {
	Mode            Mode    `json:"mode"`
	Reason          string  `json:"reason,omitempty"`
	VideoCodec      string  `json:"video_codec,omitempty"`
	Container       string  `json:"container,omitempty"`
	AudioTracks     []Track `json:"audio_tracks"`
	SubtitleTracks  []Track `json:"subtitle_tracks"`
	DefaultAudio    int     `json:"default_audio_index"`
	TranscodeAudio  bool    `json:"transcode_audio"`
	SelectedAudio   int     `json:"selected_audio_index"`
}

func Decide(file store.MediaFile, caps ClientCaps, preferAudio int) Decision {
	container := strings.ToLower(str(file.Container))
	if container == "" {
		container = strings.ToLower(str(file.FormatName))
	}

	var video *store.MediaStream
	audio := make([]Track, 0)
	subs := make([]Track, 0)
	for i := range file.Streams {
		st := &file.Streams[i]
		codec := strings.ToLower(str(st.CodecName))
		switch st.CodecType {
		case "video":
			if video == nil && codec != "mjpeg" && codec != "png" {
				video = st
			}
		case "audio":
			title := str(st.Title)
			t := Track{
				Index:       st.StreamIndex,
				Codec:       codec,
				Language:    str(st.Language),
				Title:       title,
				Default:     st.DispositionDefault,
				Forced:      st.DispositionForced,
				Commentary:  looksLikeCommentary(title),
				BrowserSafe: audioBrowserSafe(codec, caps),
			}
			if st.Channels != nil {
				t.Channels = *st.Channels
			}
			audio = append(audio, t)
		case "subtitle":
			subs = append(subs, Track{
				Index:       st.StreamIndex,
				Codec:       codec,
				Language:    str(st.Language),
				Title:       str(st.Title),
				Default:     st.DispositionDefault,
				Forced:      st.DispositionForced,
				TextBased:   textSubtitle(codec),
				BrowserSafe: textSubtitle(codec),
			})
		}
	}

	d := Decision{
		AudioTracks:    audio,
		SubtitleTracks: subs,
		Container:      container,
		DefaultAudio:   -1,
		SelectedAudio:  -1,
	}
	if video != nil {
		d.VideoCodec = strings.ToLower(str(video.CodecName))
	}

	d.DefaultAudio = pickDefaultAudio(audio, preferAudio)
	d.SelectedAudio = d.DefaultAudio
	if d.SelectedAudio >= 0 {
		for _, t := range audio {
			if t.Index == d.SelectedAudio {
				// Remux target is fMP4 — codecs like FLAC cannot be stream-copied.
				d.TranscodeAudio = !t.BrowserSafe || !audioMP4CopySafe(t.Codec)
				break
			}
		}
	}

	if video == nil {
		d.Mode = ModeUnsupported
		d.Reason = "no video stream"
		return d
	}
	if !videoBrowserSafe(d.VideoCodec, caps) {
		d.Mode = ModeUnsupported
		d.Reason = "browser cannot decode " + d.VideoCodec + "; use an external player"
		return d
	}

	if canDirectPlay(container, d.VideoCodec, audio, caps) {
		d.Mode = ModeDirect
		d.Reason = "container and codecs supported for direct play"
		return d
	}

	d.Mode = ModeRemux
	d.Reason = "remuxing without re-encoding video"
	return d
}

func canDirectPlay(container, videoCodec string, audio []Track, caps ClientCaps) bool {
	if !directContainer(container) {
		return false
	}
	if !videoBrowserSafe(videoCodec, caps) {
		return false
	}
	if len(audio) == 0 {
		return true
	}
	// Direct play needs at least the default audio to be browser-safe in-container.
	for _, t := range audio {
		if t.Default && t.BrowserSafe {
			return true
		}
	}
	for _, t := range audio {
		if t.BrowserSafe {
			return true
		}
	}
	return false
}

func directContainer(c string) bool {
	c = strings.ToLower(c)
	// Matroska often reports "matroska,webm" — that is not browser-direct-playable.
	if strings.Contains(c, "matroska") || strings.Contains(c, "mkv") {
		return false
	}
	switch {
	case strings.Contains(c, "mp4"), strings.Contains(c, "m4v"), strings.Contains(c, "mov"),
		strings.Contains(c, "isom"), strings.Contains(c, "iso5"),
		c == "webm" || strings.HasPrefix(c, "webm,"):
		return true
	default:
		return false
	}
}

func videoBrowserSafe(codec string, caps ClientCaps) bool {
	switch strings.ToLower(codec) {
	case "h264", "avc", "avc1":
		return true
	case "hevc", "h265", "hev1", "hvc1":
		return caps.HEVC
	case "av1", "av01":
		return caps.AV1
	case "vp9", "vp09":
		return caps.VP9
	case "vp8":
		return true
	default:
		return false
	}
}

func audioBrowserSafe(codec string, caps ClientCaps) bool {
	switch strings.ToLower(codec) {
	case "aac", "mp4a":
		return true
	case "mp3", "mp3float":
		return true
	case "opus":
		return caps.Opus
	case "vorbis":
		return true
	case "flac":
		return caps.FLAC
	case "ac3", "ac-3":
		return caps.AC3
	case "eac3", "ec-3":
		return caps.EAC3
	default:
		return false
	}
}

func textSubtitle(codec string) bool {
	switch strings.ToLower(codec) {
	case "subrip", "srt", "ass", "ssa", "webvtt", "mov_text", "text", "subtitle",
		"ttml", "dfxp", "sami", "realtext", "stl", "microdvd", "mpl2", "jacosub",
		"subviewer", "subviewer1", "vplayer", "pjs", "eia_608", "cea_608", "dvb_teletext":
		return true
	default:
		return false
	}
}

func audioMP4CopySafe(codec string) bool {
	switch strings.ToLower(codec) {
	case "aac", "mp4a", "mp3", "ac3", "ac-3", "eac3", "ec-3", "alac", "opus":
		return true
	default:
		// flac/truehd/dts/pcm/vorbis etc. cannot be reliably copied into fMP4
		return false
	}
}

func looksLikeCommentary(title string) bool {
	t := strings.ToLower(title)
	return strings.Contains(t, "commentary") || strings.Contains(t, "comment by")
}

func pickDefaultAudio(tracks []Track, prefer int) int {
	if prefer >= 0 {
		for _, t := range tracks {
			if t.Index == prefer {
				return prefer
			}
		}
	}
	bestIdx := -1
	bestScore := -1 << 30
	for i, t := range tracks {
		score := 0
		// Never auto-select commentary when a normal track exists.
		if t.Commentary {
			score -= 1000
		}
		if t.Default {
			score += 200
		}
		// Prefer the first audio stream (typical "main" track) over later ones.
		score += 50 - i
		if t.BrowserSafe && audioMP4CopySafe(t.Codec) {
			score += 15
		} else if t.BrowserSafe {
			score += 8
		}
		if score > bestScore {
			bestScore = score
			bestIdx = t.Index
		}
	}
	return bestIdx
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// CapsFromQuery parses client capability flags from query values ("1"/"true").
func CapsFromQuery(q map[string]string) ClientCaps {
	return ClientCaps{
		HEVC: truthy(q["hevc"]),
		AV1:  truthy(q["av1"]),
		VP9:  truthy(q["vp9"]),
		AAC:  truthyDefault(q["aac"], true),
		MP3:  truthyDefault(q["mp3"], true),
		AC3:  truthy(q["ac3"]),
		EAC3: truthy(q["eac3"]),
		Opus: truthyDefault(q["opus"], true),
		FLAC: truthy(q["flac"]),
	}
}

func truthy(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "1" || v == "true" || v == "yes"
}

func truthyDefault(v string, fallback bool) bool {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return truthy(v)
}
