package playback

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// VodDownloadMeta is the public status of a VOD download job.
type VodDownloadMeta struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	Kind        string    `json:"kind"` // movie | episode
	FileName    string    `json:"file_name"`
	Path        string    `json:"-"` // host filesystem path — never send to browser
	StartedAt   time.Time `json:"started_at"`
	Status      string    `json:"status"` // downloading | finalizing | done | error | cancelled
	Error       string    `json:"error,omitempty"`
	Bytes       int64     `json:"bytes,omitempty"`
	TotalBytes  int64     `json:"total_bytes,omitempty"`
	BytesPerSec float64   `json:"bytes_per_sec,omitempty"`
	ETASec      float64   `json:"eta_sec,omitempty"`
}

type activeVodDownload struct {
	meta      VodDownloadMeta
	cmd       *exec.Cmd
	lastBytes int64
	lastAt    time.Time
	cancel    context.CancelFunc
}

// VodDownloader copies Xtream VOD streams into the recordings directory.
type VodDownloader struct {
	dir string
	mu  sync.Mutex
	by  map[uuid.UUID]*activeVodDownload
}

func NewVodDownloader(dir string) (*VodDownloader, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("recordings directory not configured")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &VodDownloader{dir: dir, by: make(map[uuid.UUID]*activeVodDownload)}, nil
}

func (d *VodDownloader) Dir() string { return d.dir }

type VodDownloadOpts struct {
	Title     string // display / filename stem (movie or show · SxxEyy)
	Kind      string
	StreamURL string
	Container string // mkv, mp4, …
}

func (d *VodDownloader) Start(opt VodDownloadOpts) (VodDownloadMeta, error) {
	if d == nil {
		return VodDownloadMeta{}, fmt.Errorf("downloads not configured")
	}
	if strings.TrimSpace(opt.StreamURL) == "" {
		return VodDownloadMeta{}, fmt.Errorf("missing stream URL")
	}
	title := strings.TrimSpace(opt.Title)
	if title == "" {
		title = "vod"
	}
	kind := strings.TrimSpace(opt.Kind)
	if kind == "" {
		kind = "movie"
	}
	ext := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(opt.Container)), ".")
	if ext == "" {
		ext = "mkv"
	}

	id := uuid.New()
	stem := sanitizeFilePart(title)
	provisional := stem + "_downloading." + ext
	full := uniquePath(filepath.Join(d.dir, provisional))
	fileName := filepath.Base(full)

	total := probeRemoteContentLength(opt.StreamURL)

	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-user_agent", "Stevie/1.0",
		"-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5",
		"-i", opt.StreamURL,
		"-map", "0",
		"-c", "copy",
		"-y", full,
	}
	cmd := exec.Command("ffmpeg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = io.Discard
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return VodDownloadMeta{}, err
	}
	if err := cmd.Start(); err != nil {
		return VodDownloadMeta{}, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	meta := VodDownloadMeta{
		ID:         id,
		Title:      title,
		Kind:       kind,
		FileName:   fileName,
		Path:       full,
		StartedAt:  time.Now().UTC(),
		Status:     "downloading",
		TotalBytes: total,
	}
	job := &activeVodDownload{
		meta:   meta,
		cmd:    cmd,
		lastAt: time.Now(),
		cancel: cancel,
	}

	d.mu.Lock()
	d.by[id] = job
	d.mu.Unlock()

	go d.watchProgress(ctx, id)

	go func() {
		_, _ = io.Copy(io.Discard, stderr)
		waitErr := cmd.Wait()
		cancel()

		d.mu.Lock()
		cur, ok := d.by[id]
		if !ok {
			d.mu.Unlock()
			return
		}
		cur.meta.Status = "finalizing"
		cur.meta.BytesPerSec = 0
		cur.meta.ETASec = 0
		d.mu.Unlock()

		finalName := fileName
		finalPath := full
		if waitErr == nil {
			if renamed, err := finalizeVodDownload(full, stem, ext); err == nil {
				finalPath = renamed
				finalName = filepath.Base(renamed)
			} else {
				slog.Warn("vod download rename failed", "path", full, "err", err)
			}
		}

		var size int64
		if st, err := os.Stat(finalPath); err == nil {
			size = st.Size()
		}

		d.mu.Lock()
		defer d.mu.Unlock()
		cur, ok = d.by[id]
		if !ok {
			return
		}
		cur.meta.FileName = finalName
		cur.meta.Path = finalPath
		cur.meta.Bytes = size
		if cur.meta.TotalBytes < size {
			cur.meta.TotalBytes = size
		}
		cur.meta.BytesPerSec = 0
		cur.meta.ETASec = 0
		if waitErr != nil {
			if cur.meta.Status == "cancelled" {
				_ = os.Remove(full)
				delete(d.by, id)
				return
			}
			cur.meta.Status = "error"
			cur.meta.Error = waitErr.Error()
			slog.Error("vod download failed", "title", title, "err", waitErr)
		} else if size < 1024 {
			cur.meta.Status = "error"
			cur.meta.Error = "download produced empty file"
			_ = os.Remove(finalPath)
		} else {
			cur.meta.Status = "done"
			slog.Info("vod download complete", "title", title, "file", finalName, "bytes", size)
		}
		cur.cmd = nil
		// Keep done/error briefly so UI can see them, then drop.
		go func() {
			time.Sleep(30 * time.Second)
			d.mu.Lock()
			defer d.mu.Unlock()
			if j, ok := d.by[id]; ok && j.cmd == nil {
				delete(d.by, id)
			}
		}()
	}()

	return meta, nil
}

func (d *VodDownloader) watchProgress(ctx context.Context, id uuid.UUID) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.mu.Lock()
			j, ok := d.by[id]
			if !ok || (j.meta.Status != "downloading" && j.meta.Status != "finalizing") {
				d.mu.Unlock()
				return
			}
			d.refreshProgressLocked(j)
			d.mu.Unlock()
		}
	}
}

func (d *VodDownloader) refreshProgressLocked(j *activeVodDownload) {
	if j == nil || j.meta.Path == "" {
		return
	}
	st, err := os.Stat(j.meta.Path)
	if err != nil {
		return
	}
	now := time.Now()
	size := st.Size()
	if !j.lastAt.IsZero() {
		dt := now.Sub(j.lastAt).Seconds()
		if dt >= 0.4 {
			delta := size - j.lastBytes
			if delta >= 0 {
				inst := float64(delta) / dt
				if j.meta.BytesPerSec <= 0 {
					j.meta.BytesPerSec = inst
				} else {
					j.meta.BytesPerSec = j.meta.BytesPerSec*0.55 + inst*0.45
				}
			}
			j.lastBytes = size
			j.lastAt = now
		}
	} else {
		j.lastBytes = size
		j.lastAt = now
	}
	j.meta.Bytes = size
	// Drop junk probes (some panels answer Range with Content-Length: 1).
	if j.meta.TotalBytes > 0 && j.meta.TotalBytes < 1024 {
		j.meta.TotalBytes = 0
	}
	if j.meta.TotalBytes > 0 && size > j.meta.TotalBytes {
		j.meta.TotalBytes = size
	}
	j.meta.ETASec = 0
	if j.meta.TotalBytes > size && j.meta.BytesPerSec > 256 {
		j.meta.ETASec = float64(j.meta.TotalBytes-size) / j.meta.BytesPerSec
	}
}

func finalizeVodDownload(path, stem, ext string) (string, error) {
	w, h, _ := probeRecordingMedia(path)
	name := stem
	if w > 0 && h > 0 {
		name = fmt.Sprintf("%s_%dx%d", stem, w, h)
	}
	dest := uniquePath(filepath.Join(filepath.Dir(path), name+"."+ext))
	if dest == path {
		return path, nil
	}
	if err := os.Rename(path, dest); err != nil {
		return path, err
	}
	return dest, nil
}

func (d *VodDownloader) List() []VodDownloadMeta {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]VodDownloadMeta, 0, len(d.by))
	for _, j := range d.by {
		if j.meta.Status == "downloading" || j.meta.Status == "finalizing" {
			d.refreshProgressLocked(j)
		}
		out = append(out, j.meta)
	}
	return out
}

func (d *VodDownloader) Cancel(id uuid.UUID) error {
	if d == nil {
		return fmt.Errorf("downloads not configured")
	}
	d.mu.Lock()
	j, ok := d.by[id]
	if !ok {
		d.mu.Unlock()
		return fmt.Errorf("download not found")
	}
	j.meta.Status = "cancelled"
	path := j.meta.Path
	cmd := j.cmd
	cancel := j.cancel
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	// Best-effort immediate cleanup; the waiter also removes on cancelled exit.
	if path != "" {
		_ = os.Remove(path)
	}
	return nil
}

func (d *VodDownloader) StopAll() {
	if d == nil {
		return
	}
	d.mu.Lock()
	ids := make([]uuid.UUID, 0, len(d.by))
	for id := range d.by {
		ids = append(ids, id)
	}
	d.mu.Unlock()
	for _, id := range ids {
		_ = d.Cancel(id)
	}
}

// ActiveFileNames returns filenames currently being written (for recordings list).
func (d *VodDownloader) ActiveFileNames() map[string]VodDownloadMeta {
	out := map[string]VodDownloadMeta{}
	if d == nil {
		return out
	}
	for _, m := range d.List() {
		if m.Status == "downloading" || m.Status == "finalizing" {
			out[m.FileName] = m
		}
	}
	return out
}

func probeRemoteContentLength(streamURL string) int64 {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	parseSize := func(resp *http.Response) int64 {
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			// bytes 0-0/12345
			if i := strings.LastIndex(cr, "/"); i >= 0 {
				total := strings.TrimSpace(cr[i+1:])
				if total != "" && total != "*" {
					if n, err := strconv.ParseInt(total, 10, 64); err == nil && n > 1024 {
						return n
					}
				}
			}
		}
		if resp.ContentLength > 1024 {
			return resp.ContentLength
		}
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			if n, err := strconv.ParseInt(strings.TrimSpace(cl), 10, 64); err == nil && n > 1024 {
				return n
			}
		}
		return 0
	}

	try := func(method string, headers map[string]string) int64 {
		req, err := http.NewRequestWithContext(ctx, method, streamURL, nil)
		if err != nil {
			return 0
		}
		req.Header.Set("User-Agent", "Stevie/1.0")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return 0
		}
		return parseSize(resp)
	}

	if n := try(http.MethodHead, nil); n > 0 {
		return n
	}
	return try(http.MethodGet, map[string]string{"Range": "bytes=0-0"})
}
