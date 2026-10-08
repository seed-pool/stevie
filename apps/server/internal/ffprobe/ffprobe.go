package ffprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Probe is a normalized ffprobe result for a single media file.
type Probe struct {
	FormatName  string
	Container   string
	DurationMS  int64
	BitRate     int64
	FormatTags  map[string]string
	Streams     []Stream
	Raw         json.RawMessage
}

// Stream describes one elementary stream.
type Stream struct {
	Index                      int
	CodecType                  string
	CodecName                  string
	Profile                    string
	Width                      int
	Height                     int
	PixFmt                     string
	FPS                        string
	BitRate                    int64
	Channels                   int
	ChannelLayout              string
	Language                   string
	Title                      string
	DispositionDefault         bool
	DispositionForced          bool
	DispositionHearingImpaired bool
	ColorRange                 string
	ColorSpace                 string
	ColorTransfer              string
	BitDepth                   int
	Raw                        json.RawMessage
}

type rawProbe struct {
	Format  rawFormat    `json:"format"`
	Streams []rawStream  `json:"streams"`
}

type rawFormat struct {
	Filename   string            `json:"filename"`
	FormatName string            `json:"format_name"`
	Duration   string            `json:"duration"`
	BitRate    string            `json:"bit_rate"`
	Tags       map[string]string `json:"tags"`
}

type rawStream struct {
	Index          int               `json:"index"`
	CodecType      string            `json:"codec_type"`
	CodecName      string            `json:"codec_name"`
	Profile        string            `json:"profile"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	PixFmt         string            `json:"pix_fmt"`
	AvgFrameRate   string            `json:"avg_frame_rate"`
	RFrameRate     string            `json:"r_frame_rate"`
	BitRate        string            `json:"bit_rate"`
	Channels       int               `json:"channels"`
	ChannelLayout  string            `json:"channel_layout"`
	BitsPerRawSample string          `json:"bits_per_raw_sample"`
	ColorRange     string            `json:"color_range"`
	ColorSpace     string            `json:"color_space"`
	ColorTransfer  string            `json:"color_transfer"`
	ColorPrimaries string            `json:"color_primaries"`
	Tags           map[string]string `json:"tags"`
	Disposition    map[string]int    `json:"disposition"`
	SideDataList   []map[string]any  `json:"side_data_list"`
}

// Run executes ffprobe against path and returns a normalized probe (format + streams).
func Run(ctx context.Context, path string) (Probe, error) {
	return run(ctx, path, true)
}

// RunFormat is a lighter probe that only reads container format/tags (no streams).
// Used when listing recordings for EPG title/description tags.
func RunFormat(ctx context.Context, path string) (Probe, error) {
	return run(ctx, path, false)
}

func run(ctx context.Context, path string, withStreams bool) (Probe, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
	}
	if withStreams {
		args = append(args, "-show_streams")
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	out, err := cmd.Output()
	if err != nil {
		return Probe{}, fmt.Errorf("ffprobe %s: %w", path, err)
	}

	var raw rawProbe
	if err := json.Unmarshal(out, &raw); err != nil {
		return Probe{}, fmt.Errorf("decode ffprobe json: %w", err)
	}

	p := Probe{
		FormatName: raw.Format.FormatName,
		Container:  primaryContainer(raw.Format.FormatName),
		DurationMS: durationMS(raw.Format.Duration),
		BitRate:    parseInt64(raw.Format.BitRate),
		FormatTags: raw.Format.Tags,
		Raw:        json.RawMessage(out),
	}
	if p.FormatTags == nil {
		p.FormatTags = map[string]string{}
	}

	for _, s := range raw.Streams {
		streamRaw, _ := json.Marshal(s)
		fps := s.AvgFrameRate
		if fps == "" || fps == "0/0" {
			fps = s.RFrameRate
		}
		p.Streams = append(p.Streams, Stream{
			Index:                      s.Index,
			CodecType:                  s.CodecType,
			CodecName:                  s.CodecName,
			Profile:                    s.Profile,
			Width:                      s.Width,
			Height:                     s.Height,
			PixFmt:                     s.PixFmt,
			FPS:                        simplifyFPS(fps),
			BitRate:                    parseInt64(s.BitRate),
			Channels:                   s.Channels,
			ChannelLayout:              s.ChannelLayout,
			Language:                   s.Tags["language"],
			Title:                      s.Tags["title"],
			DispositionDefault:         s.Disposition["default"] == 1,
			DispositionForced:          s.Disposition["forced"] == 1,
			DispositionHearingImpaired: s.Disposition["hearing_impaired"] == 1,
			ColorRange:                 s.ColorRange,
			ColorSpace:                 s.ColorSpace,
			ColorTransfer:              s.ColorTransfer,
			BitDepth:                   int(parseInt64(s.BitsPerRawSample)),
			Raw:                        streamRaw,
		})
	}
	return p, nil
}

func primaryContainer(formatName string) string {
	parts := strings.Split(formatName, ",")
	if len(parts) == 0 {
		return formatName
	}
	return strings.TrimSpace(parts[0])
}

func durationMS(v string) int64 {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0
	}
	return int64(f * 1000)
}

func parseInt64(v string) int64 {
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func simplifyFPS(v string) string {
	if v == "" || v == "0/0" {
		return ""
	}
	parts := strings.Split(v, "/")
	if len(parts) != 2 {
		return v
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return v
	}
	return strconv.FormatFloat(num/den, 'f', 3, 64)
}
