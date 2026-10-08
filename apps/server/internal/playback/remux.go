package playback

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// RemuxOptions controls a server-side remux (video copy, optional audio transcode).
type RemuxOptions struct {
	InputPath      string
	AudioIndex     int // absolute stream index, or -1 for none
	StartSec       float64
	TranscodeAudio bool
}

// RunCopyFMP4 remuxes a file to fragmented MP4 with stream copy (no re-encode).
// Used for live TV recordings (.mkv) so browsers can play them.
func RunCopyFMP4(ctx context.Context, inputPath string, startSec float64, w io.Writer) error {
	if inputPath == "" {
		return fmt.Errorf("missing input")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if startSec > 0 {
		args = append(args, "-ss", formatFloat(startSec))
	}
	args = append(args,
		"-i", inputPath,
		"-map", "0:v:0", "-c:v", "copy",
		// AAC for browser MSE — AC3/EAC3/DTS cannot be stream-copied into fMP4 reliably.
		"-map", "0:a:0?", "-c:a", "aac", "-ac", "2", "-b:a", "192k",
		"-sn",
		"-avoid_negative_ts", "make_zero",
		"-f", "mp4",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"pipe:1",
	)

	return runFFmpegPipe(ctx, cancel, args, w)
}

// RunVodFMP4 remuxes a remote VOD URL to fragmented MP4.
// Video is stream-copied; first audio is transcoded to AAC (EAC3/DTS cannot copy into fMP4).
func RunVodFMP4(ctx context.Context, inputURL string, startSec float64, w io.Writer) error {
	if inputURL == "" {
		return fmt.Errorf("missing input")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-user_agent", "Stevie/1.0",
		"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5",
	}
	if startSec > 0 {
		args = append(args, "-ss", formatFloat(startSec))
	}
	args = append(args,
		"-i", inputURL,
		"-map", "0:v:0", "-c:v", "copy",
		"-map", "0:a:0?", "-c:a", "aac", "-ac", "2", "-b:a", "192k",
		"-sn",
		"-avoid_negative_ts", "make_zero",
		"-f", "mp4",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"pipe:1",
	)
	return runFFmpegPipe(ctx, cancel, args, w)
}

func runFFmpegPipe(ctx context.Context, cancel context.CancelFunc, args []string, w io.Writer) error {
	cmd := exec.Command("ffmpeg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	DefaultProcs.Add(cmd)
	defer DefaultProcs.Kill(cmd)

	go func() {
		<-ctx.Done()
		DefaultProcs.Kill(cmd)
	}()

	errCh := make(chan error, 1)
	go func() {
		_, copyErr := copyWithFlush(ctx, w, stdout)
		errCh <- copyErr
	}()
	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case copyErr := <-errCh:
		if copyErr != nil {
			cancel()
			return copyErr
		}
		return nil
	}
}

// RunRemuxFMP4 streams a fragmented MP4 to w. Video is always stream-copied.
// The ffmpeg child is registered and always SIGKILL'd when this returns.
func RunRemuxFMP4(ctx context.Context, opt RemuxOptions, w io.Writer) error {
	if opt.InputPath == "" {
		return fmt.Errorf("missing input")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if opt.StartSec > 0 {
		args = append(args, "-ss", formatFloat(opt.StartSec))
	}
	args = append(args, "-i", opt.InputPath, "-map", "0:v:0", "-c:v", "copy")
	if opt.AudioIndex >= 0 {
		args = append(args, "-map", fmt.Sprintf("0:%d", opt.AudioIndex))
		if opt.TranscodeAudio {
			args = append(args, "-c:a", "aac", "-ac", "2", "-b:a", "256k")
		} else {
			args = append(args, "-c:a", "copy")
		}
	} else {
		args = append(args, "-an")
	}
	args = append(args,
		"-sn",
		"-avoid_negative_ts", "make_zero",
		"-f", "mp4",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"pipe:1",
	)

	cmd := exec.Command("ffmpeg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	DefaultProcs.Add(cmd)
	defer DefaultProcs.Kill(cmd)

	// Cancel + kill as soon as the request context ends (client gone / shutdown).
	go func() {
		<-ctx.Done()
		DefaultProcs.Kill(cmd)
	}()

	errCh := make(chan error, 1)
	go func() {
		_, copyErr := copyWithFlush(ctx, w, stdout)
		errCh <- copyErr
	}()
	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case copyErr := <-errCh:
		if copyErr != nil {
			cancel()
			return copyErr
		}
		return nil
	}
}

func copyWithFlush(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	var written int64
	flusher, _ := dst.(http.Flusher)
	var controller *http.ResponseController
	if rw, ok := dst.(http.ResponseWriter); ok {
		controller = http.NewResponseController(rw)
	}

	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		nr, readErr := src.Read(buf)
		if nr > 0 {
			if controller != nil {
				// Detect dead clients quickly instead of buffering forever in proxies.
				_ = controller.SetWriteDeadline(time.Now().Add(15 * time.Second))
			}
			nw, writeErr := dst.Write(buf[:nr])
			written += int64(nw)
			if nw < nr && writeErr == nil {
				writeErr = io.ErrShortWrite
			}
			if writeErr != nil {
				return written, writeErr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return written, nil
			}
			return written, readErr
		}
	}
}

// ExtractWebVTT converts a text subtitle stream to WebVTT.
// When startSec > 0, cues are shifted so t=0 matches a remux that began at startSec.
func ExtractWebVTT(ctx context.Context, inputPath string, streamIndex int, startSec float64, w io.Writer) error {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if startSec > 0 {
		args = append(args, "-ss", formatFloat(startSec))
	}
	args = append(args,
		"-i", inputPath,
		"-map", fmt.Sprintf("0:%d", streamIndex),
		"-c:s", "webvtt",
		"-f", "webvtt",
		"pipe:1",
	)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	DefaultProcs.Add(cmd)
	defer DefaultProcs.Kill(cmd)

	err := cmd.Wait()
	// If Wait succeeded, process already exited — still Remove via Kill's Wait no-op path.
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if msg != "" {
			return fmt.Errorf("ffmpeg subtitles: %s", msg)
		}
		return err
	}
	// Successful exit: unregister without SIGKILL race — remove only.
	DefaultProcs.Remove(cmd)

	out := stdout.Bytes()
	if len(bytes.TrimSpace(out)) == 0 {
		return fmt.Errorf("empty subtitle output")
	}
	if !bytes.HasPrefix(bytes.TrimSpace(out), []byte("WEBVTT")) {
		out = append([]byte("WEBVTT\n\n"), out...)
	}
	_, err = w.Write(out)
	return err
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 3, 64)
}
