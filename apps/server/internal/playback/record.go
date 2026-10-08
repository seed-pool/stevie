package playback

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

const liveUA = "VLC/3.0.20 LibVLC/3.0.20"

var unsafeFileChars = regexp.MustCompile(`[^a-zA-Z0-9._+-]+`)

// serialize EnsureSeekableMKV so Stop + wait + boot repair never remux the same file together.
var finalizeMu sync.Mutex

// RecordingMeta is the public status of an active (or just-finished) recording.
type RecordingMeta struct {
	ChannelID    uuid.UUID `json:"channel_id"`
	ChannelName  string    `json:"channel_name"`
	FileName     string    `json:"file_name"`
	Path         string    `json:"-"` // host filesystem path — never send to browser
	StartedAt    time.Time `json:"started_at"`
	Status       string    `json:"status"` // recording | stopping | error
	ProgramTitle string    `json:"program_title,omitempty"`
	Error        string    `json:"error,omitempty"`
}

type activeRecording struct {
	meta  RecordingMeta
	cmd   *exec.Cmd
	stdin io.WriteCloser
}

// Recorder manages live TV remux jobs (ffmpeg -c copy → mkv).
type Recorder struct {
	dir string
	mu  sync.Mutex
	by  map[uuid.UUID]*activeRecording
}

func NewRecorder(dir string) (*Recorder, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("recordings directory not configured")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create recordings dir: %w", err)
	}
	return &Recorder{dir: dir, by: make(map[uuid.UUID]*activeRecording)}, nil
}

func (r *Recorder) Dir() string { return r.dir }

type StartOpts struct {
	ChannelID    uuid.UUID
	ChannelName  string
	StreamURL    string
	ProgramTitle string
	Description  string
	Category     string
	Network      string
	Date         time.Time
	// Optional planned air window (scheduled / current programme). Used for the
	// provisional filename; the final name is rewritten on stop with actual times + resolution.
	WindowStart time.Time
	WindowEnd   time.Time
}

func (r *Recorder) Start(opt StartOpts) (RecordingMeta, error) {
	if strings.TrimSpace(opt.StreamURL) == "" {
		return RecordingMeta{}, fmt.Errorf("missing stream URL")
	}
	name := strings.TrimSpace(opt.ChannelName)
	if name == "" {
		name = "channel"
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.by[opt.ChannelID]; ok && cur.cmd != nil && cur.cmd.Process != nil {
		return cur.meta, fmt.Errorf("already recording this channel")
	}

	started := time.Now()
	winStart := opt.WindowStart
	if winStart.IsZero() {
		winStart = started
	}
	fileName := buildRecordingFileName(name, opt.ProgramTitle, winStart, opt.WindowEnd, 0, 0)
	outPath := uniquePath(filepath.Join(r.dir, fileName))
	fileName = filepath.Base(outPath)

	title := strings.TrimSpace(opt.ProgramTitle)
	if title == "" {
		title = name
	}
	date := opt.Date
	if date.IsZero() {
		date = started
	}

	// Keep stdin open so Stop can send 'q' — more reliable than SIGINT alone for HLS inputs.
	args := []string{
		"-hide_banner", "-loglevel", "warning",
		"-user_agent", liveUA,
		// Local Stevie proxy is stable; avoid aggressive reconnect storms on live HLS.
		"-i", opt.StreamURL,
		"-map", "0",
		"-c", "copy",
		"-metadata", "title=" + title,
		"-metadata", "show=" + name,
		"-metadata", "artist=" + name,
		"-metadata", "network=" + strings.TrimSpace(opt.Network),
		"-metadata", "genre=" + strings.TrimSpace(opt.Category),
		"-metadata", "comment=" + truncate(strings.TrimSpace(opt.Description), 500),
		"-metadata", "date=" + date.UTC().Format("2006-01-02"),
		"-metadata", "creation_time=" + started.UTC().Format(time.RFC3339),
		"-f", "matroska",
		outPath,
	}

	cmd := exec.Command("ffmpeg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return RecordingMeta{}, fmt.Errorf("stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return RecordingMeta{}, fmt.Errorf("start ffmpeg: %w", err)
	}
	DefaultProcs.Add(cmd)

	meta := RecordingMeta{
		ChannelID:    opt.ChannelID,
		ChannelName:  name,
		FileName:     fileName,
		Path:         outPath,
		StartedAt:    started,
		Status:       "recording",
		ProgramTitle: strings.TrimSpace(opt.ProgramTitle),
	}
	rec := &activeRecording{meta: meta, cmd: cmd, stdin: stdin}
	r.by[opt.ChannelID] = rec

	go r.wait(opt.ChannelID, rec)

	slog.Info("live recording started", "channel", name, "path", outPath, "pid", cmd.Process.Pid)
	return meta, nil
}

func (r *Recorder) wait(channelID uuid.UUID, rec *activeRecording) {
	err := rec.cmd.Wait()
	DefaultProcs.Remove(rec.cmd)
	if rec.stdin != nil {
		_ = rec.stdin.Close()
	}

	r.mu.Lock()
	cur, ok := r.by[channelID]
	owned := ok && cur == rec
	path := rec.meta.Path
	if owned {
		wasStopping := rec.meta.Status == "stopping"
		if err != nil && !wasStopping {
			slog.Warn("live recording ended with error", "channel", rec.meta.ChannelName, "err", err)
		} else {
			slog.Info("live recording finished", "channel", rec.meta.ChannelName, "path", rec.meta.Path)
		}
		delete(r.by, channelID)
	}
	r.mu.Unlock()

	// Natural exit / crash / graceful stop all land here when we still own the job.
	// Force-kill path deletes from the map first and finalizes in Stop instead.
	if owned {
		if final, ferr := CompleteRecordingFile(path, rec.meta.ChannelName, rec.meta.ProgramTitle, rec.meta.StartedAt); ferr != nil {
			slog.Warn("recording finalize on exit failed", "path", path, "err", ferr)
		} else if final != path {
			slog.Info("recording renamed", "from", filepath.Base(path), "to", filepath.Base(final))
		}
	}
}

func (r *Recorder) Status(channelID uuid.UUID) (RecordingMeta, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.by[channelID]
	if !ok {
		return RecordingMeta{}, false
	}
	return rec.meta, true
}

// List returns a snapshot of all active recordings.
func (r *Recorder) List() []RecordingMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]RecordingMeta, 0, len(r.by))
	for _, rec := range r.by {
		out = append(out, rec.meta)
	}
	return out
}

func (r *Recorder) Stop(channelID uuid.UUID) (RecordingMeta, error) {
	r.mu.Lock()
	rec, ok := r.by[channelID]
	if !ok || rec.cmd == nil || rec.cmd.Process == nil {
		r.mu.Unlock()
		return RecordingMeta{}, fmt.Errorf("not recording")
	}
	rec.meta.Status = "stopping"
	meta := rec.meta
	cmd := rec.cmd
	stdin := rec.stdin
	pid := cmd.Process.Pid
	r.mu.Unlock()

	requestGracefulQuit(stdin, pid)

	if waitUntilGone(r, channelID, 12*time.Second) {
		meta.Status = "stopped"
		meta = finishStoppedMeta(meta)
		slog.Info("live recording stopped", "channel", meta.ChannelName, "path", meta.Path)
		return meta, nil
	}

	// Escalate: SIGTERM, then SIGKILL.
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	_ = syscall.Kill(pid, syscall.SIGTERM)
	if waitUntilGone(r, channelID, 5*time.Second) {
		meta.Status = "stopped"
		meta = finishStoppedMeta(meta)
		slog.Info("live recording stopped after SIGTERM", "channel", meta.ChannelName, "path", meta.Path)
		return meta, nil
	}

	slog.Warn("recording stop timed out; killing", "pid", pid)
	DefaultProcs.Kill(cmd)
	r.mu.Lock()
	if cur, ok := r.by[channelID]; ok && cur == rec {
		delete(r.by, channelID)
	}
	r.mu.Unlock()
	meta.Status = "stopped"
	meta = finishStoppedMeta(meta)
	slog.Info("recording stopped after force kill", "channel", meta.ChannelName, "path", meta.Path)
	return meta, nil
}

func finishStoppedMeta(meta RecordingMeta) RecordingMeta {
	final, err := CompleteRecordingFile(meta.Path, meta.ChannelName, meta.ProgramTitle, meta.StartedAt)
	if err != nil {
		slog.Warn("recording finalize failed", "path", meta.Path, "err", err)
		return meta
	}
	meta.Path = final
	meta.FileName = filepath.Base(final)
	return meta
}

// StopAll gracefully stops every active recording (used on process shutdown).
func (r *Recorder) StopAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	ids := make([]uuid.UUID, 0, len(r.by))
	for id := range r.by {
		ids = append(ids, id)
	}
	r.mu.Unlock()
	for _, id := range ids {
		if _, err := r.Stop(id); err != nil {
			slog.Warn("stop-all recording failed", "channel_id", id, "err", err)
		}
	}
}

// RepairIncompleteAsync remuxes incomplete MKVs and removes leftover sidecar/temp files.
func (r *Recorder) RepairIncompleteAsync() {
	if r == nil {
		return
	}
	dir := r.dir
	go func() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			lower := strings.ToLower(name)
			path := filepath.Join(dir, name)
			switch {
			case strings.HasSuffix(lower, ".mkv") && strings.Contains(name, ".finalizing."):
				_ = os.Remove(path)
			case strings.HasSuffix(lower, ".mkv"):
				if err := EnsureSeekableMKV(path); err != nil {
					slog.Warn("boot recording repair failed", "path", path, "err", err)
					continue
				}
				// Upgrade legacy names to include full window + resolution.
				if !regexp.MustCompile(`_\d{3,5}x\d{3,5}\.mkv$`).MatchString(lower) {
					ch, started, ok := parseNameForRename(name)
					if !ok {
						continue
					}
					if final, err := CompleteRecordingFile(path, ch, "", started); err != nil {
						slog.Warn("boot recording rename failed", "path", path, "err", err)
					} else if final != path {
						slog.Info("recording renamed", "from", name, "to", filepath.Base(final))
					}
				}
			case strings.HasSuffix(lower, ".json"):
				// Legacy sidecars — EPG lives in MKV tags / Postgres schedules.
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					slog.Warn("recording sidecar cleanup failed", "path", path, "err", err)
				} else {
					slog.Info("removed recording sidecar", "path", path)
				}
			}
		}
	}()
}

func requestGracefulQuit(stdin io.WriteCloser, pid int) {
	if stdin != nil {
		_, _ = io.WriteString(stdin, "q\n")
		_ = stdin.Close()
	}
	// SIGINT also asks ffmpeg to finalize the container.
	if pid > 1 {
		_ = syscall.Kill(-pid, syscall.SIGINT)
		_ = syscall.Kill(pid, syscall.SIGINT)
	}
}

func waitUntilGone(r *Recorder, channelID uuid.UUID, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		_, still := r.by[channelID]
		r.mu.Unlock()
		if !still {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// EnsureSeekableMKV remuxes an incomplete Matroska file (no duration/cues) in place.
func EnsureSeekableMKV(path string) error {
	finalizeMu.Lock()
	defer finalizeMu.Unlock()
	return ensureSeekableMKVLocked(path)
}

// CompleteRecordingFile makes the MKV seekable, then renames it to include the
// full time window and video resolution: Channel_Matchup_YYYY-MM-DD_HHMMSS-HHMMSS_1920x1080.mkv
func CompleteRecordingFile(path, channel, programTitle string, startedAt time.Time) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	finalizeMu.Lock()
	defer finalizeMu.Unlock()

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			// wait() may have already renamed it.
			if found := findCompletedSibling(path, channel, startedAt); found != "" {
				return found, nil
			}
		}
		return path, err
	}
	if err := ensureSeekableMKVLocked(path); err != nil {
		return path, err
	}

	width, height, durSec := probeRecordingMedia(path)
	end := startedAt
	if durSec > 0 {
		end = startedAt.Add(time.Duration(durSec * float64(time.Second)))
	} else {
		end = time.Now()
	}
	if startedAt.IsZero() {
		startedAt = end
	}

	desired := buildRecordingFileName(channel, programTitle, startedAt, end, width, height)
	dir := filepath.Dir(path)
	target := filepath.Join(dir, desired)
	if filepath.Clean(target) == filepath.Clean(path) {
		return path, nil
	}
	target = uniquePath(target)
	if err := os.Rename(path, target); err != nil {
		return path, err
	}
	return target, nil
}

func ensureSeekableMKVLocked(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("empty path")
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if hasMKVDuration(path) {
		return nil
	}

	tmp := path + ".finalizing.mkv"
	_ = os.Remove(tmp)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-err_detect", "ignore_err",
		"-i", path,
		"-map", "0",
		"-c", "copy",
		"-f", "matroska",
		tmp,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("remux finalize: %w", err)
	}
	st, err := os.Stat(tmp)
	if err != nil || st.Size() < 1024 {
		_ = os.Remove(tmp)
		return fmt.Errorf("finalize produced empty output")
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if !hasMKVDuration(path) {
		return fmt.Errorf("finalize still missing duration")
	}
	slog.Info("recording made seekable", "path", path)
	return nil
}

func buildRecordingFileName(channel, programTitle string, start, end time.Time, width, height int) string {
	ch := sanitizeFilePart(channel)
	if start.IsZero() {
		start = time.Now()
	}
	startPart := start.Format("2006-01-02_150405")
	window := startPart
	if !end.IsZero() && end.After(start) {
		if start.Year() == end.Year() && start.YearDay() == end.YearDay() {
			window = startPart + "-" + end.Format("150405")
		} else {
			window = startPart + "-" + end.Format("2006-01-02_150405")
		}
	}
	name := ch
	if title := sanitizeProgramFilePart(programTitle, channel); title != "" {
		name = name + "_" + title
	}
	name = name + "_" + window
	if width > 0 && height > 0 {
		name += fmt.Sprintf("_%dx%d", width, height)
	}
	return name + ".mkv"
}

func uniquePath(path string) string {
	if _, err := os.Stat(path); err != nil {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
	return fmt.Sprintf("%s_%d%s", base, time.Now().Unix(), ext)
}

func probeRecordingMedia(path string) (width, height int, durationSec float64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height:format=duration",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, 0
	}
	var parsed struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return 0, 0, 0
	}
	if len(parsed.Streams) > 0 {
		width = parsed.Streams[0].Width
		height = parsed.Streams[0].Height
	}
	if d := strings.TrimSpace(parsed.Format.Duration); d != "" && !strings.EqualFold(d, "N/A") {
		fmt.Sscanf(d, "%f", &durationSec)
	}
	return width, height, durationSec
}

func findCompletedSibling(original, channel string, startedAt time.Time) string {
	dir := filepath.Dir(original)
	chPrefix := sanitizeFilePart(channel) + "_"
	origStem := strings.TrimSuffix(filepath.Base(original), filepath.Ext(original))
	if m := regexp.MustCompile(`_\d{3,5}x\d{3,5}$`).FindStringIndex(origStem); m != nil {
		origStem = origStem[:m[0]]
	}
	// Match the start stamp from the provisional name or wall-clock start.
	startKey := ""
	if idx := strings.Index(origStem, "_20"); idx >= 0 && len(origStem) >= idx+18 {
		startKey = origStem[idx+1 : idx+18] // YYYY-MM-DD_HHMMSS
	}
	if startKey == "" && !startedAt.IsZero() {
		startKey = startedAt.Format("2006-01-02_150405")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var fallback string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, chPrefix) || !strings.HasSuffix(strings.ToLower(name), ".mkv") {
			continue
		}
		if startKey != "" && !strings.Contains(name, startKey) {
			continue
		}
		full := filepath.Join(dir, name)
		// Prefer a resolution-suffixed final name.
		if regexp.MustCompile(`_\d{3,5}x\d{3,5}\.mkv$`).MatchString(strings.ToLower(name)) {
			return full
		}
		if fallback == "" {
			fallback = full
		}
	}
	return fallback
}

func hasMKVDuration(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	s := strings.TrimSpace(string(out))
	if s == "" || strings.EqualFold(s, "N/A") {
		return false
	}
	var f float64
	_, err = fmt.Sscanf(s, "%f", &f)
	return err == nil && f > 0
}

func parseNameForRename(name string) (channel string, started time.Time, ok bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = regexp.MustCompile(`_\d{3,5}x\d{3,5}$`).ReplaceAllString(base, "")
	re := regexp.MustCompile(`^(.*)_(\d{4}-\d{2}-\d{2})_(\d{6})`)
	m := re.FindStringSubmatch(base)
	if len(m) != 4 {
		return "", time.Time{}, false
	}
	channel = strings.ReplaceAll(m[1], "_", " ")
	started, err := time.ParseInLocation("2006-01-02_150405", m[2]+"_"+m[3], time.Local)
	if err != nil {
		return "", time.Time{}, false
	}
	return strings.TrimSpace(channel), started, true
}

func sanitizeFilePart(s string) string {
	s = strings.TrimSpace(s)
	s = unsafeFileChars.ReplaceAllString(s, "_")
	s = strings.Trim(s, "._-")
	if s == "" {
		return "channel"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// sanitizeProgramFilePart returns a filename-safe programme / matchup slug, or ""
// when empty or redundant with the channel name.
func sanitizeProgramFilePart(programTitle, channel string) string {
	title := strings.TrimSpace(programTitle)
	if title == "" {
		return ""
	}
	ch := strings.TrimSpace(channel)
	if ch != "" && strings.EqualFold(title, ch) {
		return ""
	}
	part := sanitizeFilePart(title)
	if part == "" || part == "channel" {
		return ""
	}
	chPart := sanitizeFilePart(ch)
	if chPart != "" && strings.EqualFold(part, chPart) {
		return ""
	}
	return part
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
