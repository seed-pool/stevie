package playback

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRemuxKilledOnCancel(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "clip.mkv")
	// 20s synthetic clip — long enough that cancel happens mid-remux.
	cmd := exec.Command(
		"ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=24:duration=20",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=20",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac",
		input,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create clip: %v (%s)", err, out)
	}

	before := countFFmpeg()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunRemuxFMP4(ctx, RemuxOptions{
			InputPath:  input,
			AudioIndex: 1,
			StartSec:   0,
		}, io.Discard)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if DefaultProcs.Count() > 0 || countFFmpeg() > before {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()

	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("remux did not return after cancel")
	}

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if DefaultProcs.Count() == 0 && countFFmpeg() <= before {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("ffmpeg still running after cancel: registry=%d host=%d", DefaultProcs.Count(), countFFmpeg())
}

func countFFmpeg() int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/comm")
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(b)) == "ffmpeg" {
			n++
		}
	}
	return n
}
