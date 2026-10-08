package ffprobe

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
)

// DynamicRange summarizes HDR / Dolby Vision signalling for a media file.
type DynamicRange struct {
	DolbyVision bool `json:"dolby_vision"`
	HDR10       bool `json:"hdr10"`
	HDR10Plus   bool `json:"hdr10_plus"`
	HLG         bool `json:"hlg"`
}

var (
	reDoVi     = regexp.MustCompile(`(?i)(?:^|[.\-_])(dv|dovi|dolby[.\-_]?vision)(?:[.\-_]|$)`)
	reHDR10Plus = regexp.MustCompile(`(?i)hdr10\+`)
	reHDR10    = regexp.MustCompile(`(?i)(?:^|[.\-_])hdr10(?:[.\-_]|$)`)
	reHDR      = regexp.MustCompile(`(?i)(?:^|[.\-_])hdr(?:[.\-_]|$)`)
	reHLG      = regexp.MustCompile(`(?i)(?:^|[.\-_])hlg(?:[.\-_]|$)`)
)

type probeStreams struct {
	Streams []struct {
		CodecType      string `json:"codec_type"`
		ColorTransfer  string `json:"color_transfer"`
		ColorPrimaries string `json:"color_primaries"`
		SideDataList   []struct {
			SideDataType string `json:"side_data_type"`
		} `json:"side_data_list"`
	} `json:"streams"`
}

// DetectDynamicRange inspects ffprobe JSON and falls back to filename tokens.
func DetectDynamicRange(probeJSON json.RawMessage, path string) DynamicRange {
	var dr DynamicRange
	var raw probeStreams
	if len(probeJSON) > 0 {
		_ = json.Unmarshal(probeJSON, &raw)
	}
	for _, s := range raw.Streams {
		if s.CodecType != "video" {
			continue
		}
		for _, sd := range s.SideDataList {
			t := strings.ToLower(sd.SideDataType)
			if strings.Contains(t, "dovi") || strings.Contains(t, "dolby vision") {
				dr.DolbyVision = true
			}
			if strings.Contains(t, "hdr10+") || strings.Contains(t, "hdr10plus") {
				dr.HDR10Plus = true
			}
		}
		switch strings.ToLower(s.ColorTransfer) {
		case "smpte2084", "smpte2084 (pq)":
			dr.HDR10 = true
		case "arib-std-b67":
			dr.HLG = true
		}
	}

	base := filepath.Base(path)
	if reDoVi.MatchString(base) {
		dr.DolbyVision = true
	}
	if reHDR10Plus.MatchString(base) {
		dr.HDR10Plus = true
	}
	if reHLG.MatchString(base) {
		dr.HLG = true
	}
	if reHDR10.MatchString(base) || (reHDR.MatchString(base) && !dr.HLG) {
		// Filename HDR often accompanies DV.HDR dual-layer encodes.
		dr.HDR10 = true
	}

	// DV profile 8 commonly carries an HDR10 base layer; keep both flags when present.
	if dr.HDR10Plus {
		dr.HDR10 = false
	}
	return dr
}

// Labels returns stable UI tokens, e.g. ["dolby_vision","hdr10"].
func (d DynamicRange) Labels() []string {
	var out []string
	if d.DolbyVision {
		out = append(out, "dolby_vision")
	}
	if d.HDR10Plus {
		out = append(out, "hdr10_plus")
	} else if d.HDR10 {
		out = append(out, "hdr10")
	}
	if d.HLG {
		out = append(out, "hlg")
	}
	return out
}
