package playback

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/stevie-media/stevie/apps/server/internal/ffprobe"
	"github.com/stevie-media/stevie/apps/server/internal/livetv"
	"github.com/stevie-media/stevie/apps/server/internal/mediainfo"
)

// VodAnalyzeResult is returned after sampling a VOD stream.
type VodAnalyzeResult struct {
	Tech        livetv.VodTech `json:"tech"`
	Container   string         `json:"container,omitempty"`
	SampleBytes int64          `json:"sample_bytes"`
	SampleSec   float64        `json:"sample_sec"`
	ReportText  string         `json:"report_text"`
	ReportJSON  []byte         `json:"-"`
	DurationMS  int64          `json:"duration_ms,omitempty"`
	BitRate     int64          `json:"bitrate,omitempty"`
	Streams     []ffprobe.Stream `json:"streams,omitempty"`
}

var analyzeMu sync.Mutex

// AnalyzeVodStream downloads a short sample of streamURL and runs MediaInfo (+ ffprobe).
func AnalyzeVodStream(ctx context.Context, streamURL, containerHint string) (VodAnalyzeResult, error) {
	if !analyzeMu.TryLock() {
		return VodAnalyzeResult{}, fmt.Errorf("another analyze is already running — try again shortly")
	}
	defer analyzeMu.Unlock()

	streamURL = strings.TrimSpace(streamURL)
	if streamURL == "" {
		return VodAnalyzeResult{}, fmt.Errorf("missing stream URL")
	}
	if !mediainfo.Available() {
		return VodAnalyzeResult{}, fmt.Errorf("mediainfo is not installed on the server")
	}

	ext := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(containerHint)), ".")
	if ext == "" {
		ext = "mkv"
	}
	dir := filepath.Join(os.TempDir(), "stevie-analyze")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return VodAnalyzeResult{}, err
	}
	sample := filepath.Join(dir, fmt.Sprintf("sample-%d.%s", time.Now().UnixNano(), ext))
	defer os.Remove(sample)

	const sampleSec = 20.0
	if err := grabVodSample(ctx, streamURL, sample, sampleSec); err != nil {
		return VodAnalyzeResult{}, err
	}
	st, err := os.Stat(sample)
	if err != nil {
		return VodAnalyzeResult{}, err
	}
	if st.Size() < 32*1024 {
		return VodAnalyzeResult{}, fmt.Errorf("sample too small (%d bytes) — stream may be blocked", st.Size())
	}

	mi, err := mediainfo.Run(ctx, sample)
	if err != nil {
		return VodAnalyzeResult{}, err
	}

	out := VodAnalyzeResult{
		SampleBytes: st.Size(),
		SampleSec:   sampleSec,
		ReportText:  mi.Text,
		ReportJSON:  mi.JSON,
		Container:   ext,
	}

	if probe, err := ffprobe.Run(ctx, sample); err == nil {
		out.Streams = probe.Streams
		out.DurationMS = probe.DurationMS
		out.BitRate = probe.BitRate
		if probe.Container != "" {
			out.Container = probe.Container
		}
		out.Tech = techFromProbe(probe)
	}
	return out, nil
}

func grabVodSample(ctx context.Context, streamURL, outPath string, sampleSec float64) error {
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()

	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-user_agent", "Stevie/1.0",
		"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5",
		"-i", streamURL,
		"-t", formatFloat(sampleSec),
		"-map", "0",
		"-c", "copy",
		"-y", outPath,
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = io.Discard
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	errBuf := &strings.Builder{}
	go func() { _, _ = io.Copy(errBuf, stderr) }()
	waitErr := cmd.Wait()
	if waitErr != nil {
		msg := strings.TrimSpace(errBuf.String())
		if ctx.Err() != nil {
			return fmt.Errorf("sample timed out")
		}
		if msg != "" {
			return fmt.Errorf("ffmpeg sample: %s", msg)
		}
		return waitErr
	}
	return nil
}

func techFromProbe(p ffprobe.Probe) livetv.VodTech {
	var t livetv.VodTech
	if p.BitRate > 0 {
		t.BitrateKbps = int(p.BitRate / 1000)
	}
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			if t.Width == 0 && s.Width > 0 {
				t.Width = s.Width
			}
			if t.Height == 0 && s.Height > 0 {
				t.Height = s.Height
			}
			if t.VideoCodec == "" && s.CodecName != "" {
				t.VideoCodec = livetv.NormalizeVideoCodecPublic(s.CodecName)
			}
			if t.HDR == "" {
				t.HDR = hdrFromProbeStream(s)
			}
		case "audio":
			if t.AudioCodec == "" && s.CodecName != "" {
				t.AudioCodec = livetv.NormalizeAudioCodecPublic(s.CodecName)
			}
		}
	}
	if t.Height > 0 {
		t.Resolution = livetv.ResolutionFromHeightPublic(t.Height)
	}
	return t
}

func hdrFromProbeStream(s ffprobe.Stream) string {
	ct := strings.ToLower(s.ColorTransfer)
	switch {
	case strings.Contains(ct, "smpte2084"), strings.Contains(ct, "pq"):
		return "HDR10"
	case strings.Contains(ct, "arib-std-b67"), strings.Contains(ct, "hlg"):
		return "HLG"
	default:
		return ""
	}
}
