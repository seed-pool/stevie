package livetv

import (
	"fmt"
	"regexp"
	"strings"
)

// VodTech is technical metadata derived from Xtream stream names / get_vod_info.
// Many panels omit ffmpeg video/audio objects; release tags in the name are the reliable source.
type VodTech struct {
	Resolution string `json:"resolution,omitempty"` // 720p, 1080p, 2160p, …
	VideoCodec string `json:"video_codec,omitempty"` // H.264, HEVC, AV1, …
	AudioCodec string `json:"audio_codec,omitempty"` // AAC, DTS, Atmos, …
	Source     string `json:"source,omitempty"`      // BluRay, WEB-DL, …
	HDR        string `json:"hdr,omitempty"`         // HDR10, DV, …
	BitrateKbps int  `json:"bitrate_kbps,omitempty"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
}

var (
	reResExplicit = regexp.MustCompile(`(?i)\b(2160p|1440p|1080p|720p|576p|480p|360p)\b`)
	reRes4K       = regexp.MustCompile(`(?i)\b(4k|uhd)\b`)
	reVideoCodec  = regexp.MustCompile(`(?i)\b(x265|h\.?265|hevc|x264|h\.?264|avc|av1|xvid|mpeg-?2|mpeg-?4|vc-?1)\b`)
	reAudioCodec  = regexp.MustCompile(`(?i)\b(truehd|atmos|dts-?hd(?:\.?ma)?|dts-?x|dts|e-?ac-?3|dd\+?|ac-?3|ddp?5\.?1|aac(?:5\.?1)?|flac|opus|mp3|pcm)\b`)
	reSource      = regexp.MustCompile(`(?i)\b(blu-?ray|bdremux|remux|web-?dl|webrip|web|hdtv|hdrip|dvdrip|bdrip|brrip|hdtv|cam|telesync|ts|tc|pay-?per-?view|ppv)\b`)
	reHDR         = regexp.MustCompile(`(?i)\b(dolby[\s.-]?vision|\bdv\b|hdr10\+?|hdr|hlg)\b`)
)

// ParseVodTech extracts resolution/codecs/source/HDR tags from a VOD stream name.
func ParseVodTech(name string) VodTech {
	s := strings.TrimSpace(name)
	if s == "" {
		return VodTech{}
	}
	var t VodTech

	if m := reResExplicit.FindString(s); m != "" {
		t.Resolution = strings.ToLower(m)
	} else if reRes4K.MatchString(s) {
		t.Resolution = "2160p"
	}

	if m := reVideoCodec.FindString(s); m != "" {
		t.VideoCodec = normalizeVideoCodec(m)
	}
	if m := reAudioCodec.FindString(s); m != "" {
		t.AudioCodec = normalizeAudioCodec(m)
	}
	if m := reSource.FindString(s); m != "" {
		t.Source = normalizeSource(m)
	}
	if m := reHDR.FindString(s); m != "" {
		t.HDR = normalizeHDR(m)
	}

	if t.Height == 0 {
		switch t.Resolution {
		case "2160p":
			t.Height = 2160
		case "1440p":
			t.Height = 1440
		case "1080p":
			t.Height = 1080
		case "720p":
			t.Height = 720
		case "576p":
			t.Height = 576
		case "480p":
			t.Height = 480
		case "360p":
			t.Height = 360
		}
	}
	return t
}

// MergeVodTechInfo overlays get_vod_info fields (bitrate, optional ffmpeg video/audio maps).
func MergeVodTechInfo(base VodTech, info map[string]any) VodTech {
	if info == nil {
		return base
	}
	if v := anyInt(info["bitrate"]); v > 0 {
		base.BitrateKbps = v
	}
	if vid, ok := info["video"].(map[string]any); ok && vid != nil {
		if w := anyInt(vid["width"]); w > 0 {
			base.Width = w
		}
		if h := anyInt(vid["height"]); h > 0 {
			base.Height = h
			if base.Resolution == "" {
				base.Resolution = resolutionFromHeight(h)
			}
		}
		if c := strings.TrimSpace(anyString(vid["codec_name"])); c != "" && base.VideoCodec == "" {
			base.VideoCodec = normalizeVideoCodec(c)
		}
		if br := anyInt(vid["bit_rate"]); br > 1000 && base.BitrateKbps == 0 {
			base.BitrateKbps = br / 1000
		}
	}
	if aud, ok := info["audio"].(map[string]any); ok && aud != nil {
		if c := strings.TrimSpace(anyString(aud["codec_name"])); c != "" && base.AudioCodec == "" {
			base.AudioCodec = normalizeAudioCodec(c)
		}
	}
	return base
}

// ApplyVodTech fills empty tech fields on a movie from its name (and optional info map).
func ApplyVodTech(name string, info map[string]any) VodTech {
	return MergeVodTechInfo(ParseVodTech(name), info)
}

func resolutionFromHeight(h int) string {
	switch {
	case h >= 2000:
		return "2160p"
	case h >= 1400:
		return "1440p"
	case h >= 1000:
		return "1080p"
	case h >= 700:
		return "720p"
	case h >= 500:
		return "576p"
	case h >= 400:
		return "480p"
	case h > 0:
		return fmt.Sprintf("%dp", h)
	default:
		return ""
	}
}

func normalizeVideoCodec(s string) string {
	c := strings.ToLower(strings.TrimSpace(s))
	c = strings.ReplaceAll(c, ".", "")
	switch c {
	case "x265", "h265", "hevc":
		return "HEVC"
	case "x264", "h264", "avc":
		return "H.264"
	case "av1":
		return "AV1"
	case "xvid":
		return "Xvid"
	case "mpeg2":
		return "MPEG-2"
	case "mpeg4":
		return "MPEG-4"
	case "vc1":
		return "VC-1"
	default:
		return strings.ToUpper(c)
	}
}

func normalizeAudioCodec(s string) string {
	c := strings.ToLower(strings.TrimSpace(s))
	c = strings.ReplaceAll(c, " ", "")
	switch {
	case strings.Contains(c, "truehd"):
		return "TrueHD"
	case strings.Contains(c, "atmos"):
		return "Atmos"
	case strings.Contains(c, "dts-hd") || strings.Contains(c, "dtshd"):
		return "DTS-HD"
	case strings.Contains(c, "dts"):
		return "DTS"
	case strings.Contains(c, "eac3") || strings.Contains(c, "e-ac-3") || strings.Contains(c, "ddp"):
		return "EAC3"
	case strings.Contains(c, "ac3") || c == "dd" || strings.HasPrefix(c, "dd5"):
		return "AC3"
	case strings.HasPrefix(c, "aac"):
		return "AAC"
	case c == "flac":
		return "FLAC"
	case c == "opus":
		return "Opus"
	case c == "mp3":
		return "MP3"
	default:
		return strings.ToUpper(c)
	}
}

func normalizeSource(s string) string {
	c := strings.ToLower(strings.TrimSpace(s))
	c = strings.ReplaceAll(c, "-", "")
	switch c {
	case "bluray", "blu-ray":
		return "BluRay"
	case "bdremux", "remux":
		return "REMUX"
	case "webdl":
		return "WEB-DL"
	case "webrip":
		return "WEBRip"
	case "web":
		return "WEB"
	case "hdtv":
		return "HDTV"
	case "hdrip":
		return "HDRip"
	case "dvdrip":
		return "DVDRip"
	case "bdrip", "brrip":
		return "BDRip"
	case "cam":
		return "CAM"
	case "telesync", "ts", "tc":
		return "TS"
	default:
		return strings.ToUpper(c)
	}
}

func normalizeHDR(s string) string {
	c := strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(c, "vision") || c == "dv":
		return "DV"
	case strings.Contains(c, "hdr10"):
		return "HDR10"
	case strings.Contains(c, "hlg"):
		return "HLG"
	case strings.Contains(c, "hdr"):
		return "HDR"
	default:
		return strings.ToUpper(c)
	}
}

// NormalizeVideoCodecPublic exposes codec normalization for probes outside this package.
func NormalizeVideoCodecPublic(s string) string { return normalizeVideoCodec(s) }

// NormalizeAudioCodecPublic exposes audio codec normalization for probes outside this package.
func NormalizeAudioCodecPublic(s string) string { return normalizeAudioCodec(s) }

// ResolutionFromHeightPublic maps pixel height to a label like 1080p.
func ResolutionFromHeightPublic(h int) string { return resolutionFromHeight(h) }

// FormatBitrate returns a short display string like "3.2 Mbps".
func FormatBitrate(kbps int) string {
	if kbps <= 0 {
		return ""
	}
	if kbps >= 1000 {
		mbps := float64(kbps) / 1000
		if mbps >= 10 {
			return fmt.Sprintf("%.0f Mbps", mbps)
		}
		return fmt.Sprintf("%.1f Mbps", mbps)
	}
	return fmt.Sprintf("%d kbps", kbps)
}
