package api

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stevie-media/stevie/apps/server/internal/ffprobe"
	"github.com/stevie-media/stevie/apps/server/internal/playback"
	"github.com/stevie-media/stevie/apps/server/internal/store"
)

type recordingFileInfo struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	SizeBytes    int64     `json:"size_bytes"`
	ModTime      time.Time `json:"mod_time"`
	Title        string    `json:"title"`
	ChannelName  string    `json:"channel_name,omitempty"`
	LogoURL      string    `json:"logo_url,omitempty"`
	ProgramTitle string    `json:"program_title,omitempty"`
	Description  string    `json:"description,omitempty"`
	Category     string    `json:"category,omitempty"`
	DurationMS   int64     `json:"duration_ms,omitempty"`
	Recording    bool      `json:"recording"` // true while ffmpeg is still writing this file
}

func (s *Server) recordingsDir() string {
	if s.recorder != nil {
		return s.recorder.Dir()
	}
	return strings.TrimSpace(s.cfg.RecordingsMount)
}

func (s *Server) resolveRecordingFile(raw string) (string, error) {
	name := filepath.Base(strings.TrimSpace(raw))
	if name == "" || name == "." || name == ".." {
		return "", os.ErrNotExist
	}
	lower := strings.ToLower(name)
	if !strings.HasSuffix(lower, ".mkv") && !strings.HasSuffix(lower, ".mp4") {
		return "", os.ErrNotExist
	}
	dir := filepath.Clean(s.recordingsDir())
	if dir == "" {
		return "", os.ErrNotExist
	}
	full := filepath.Clean(filepath.Join(dir, name))
	sep := string(os.PathSeparator)
	if full != dir && !strings.HasPrefix(full, dir+sep) {
		return "", os.ErrNotExist
	}
	return full, nil
}

func displayTitleFromRecordingName(name string) string {
	channel, stamp, res := parseRecordingNameParts(name)
	parts := []string{}
	if channel != "" {
		parts = append(parts, channel)
	}
	if stamp != "" {
		parts = append(parts, stamp)
	}
	if res != "" {
		parts = append(parts, res)
	}
	if len(parts) > 0 {
		return strings.Join(parts, " · ")
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	return strings.ReplaceAll(base, "_", " ")
}

var (
	recordingResSuffix = regexp.MustCompile(`_(\d{3,5}x\d{3,5})$`)
	recordingWindowRE  = regexp.MustCompile(`_(\d{4}-\d{2}-\d{2}_\d{6}(?:-(?:\d{4}-\d{2}-\d{2}_)?\d{6})?)$`)
)

func parseRecordingNameParts(name string) (channel string, stamp string, res string) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if m := recordingResSuffix.FindStringSubmatch(base); len(m) == 2 {
		res = m[1]
		base = strings.TrimSuffix(base, m[0])
	}
	if m := recordingWindowRE.FindStringSubmatch(base); len(m) == 2 {
		window := m[1]
		channel = strings.ReplaceAll(strings.TrimSuffix(base, m[0]), "_", " ")
		stamp = formatRecordingWindow(window)
		return strings.TrimSpace(channel), stamp, res
	}
	// Legacy: Channel_2006-01-02_150405
	if i := strings.LastIndex(base, "_"); i > 0 {
		if j := strings.LastIndex(base[:i], "_"); j > 0 {
			datePart := base[j+1 : i]
			timePart := base[i+1:]
			if len(datePart) == 10 && datePart[4] == '-' && datePart[7] == '-' && len(timePart) >= 6 {
				channel = strings.ReplaceAll(base[:j], "_", " ")
				stamp = datePart + " " + timePart[:2] + ":" + timePart[2:4] + ":" + timePart[4:6]
				return strings.TrimSpace(channel), stamp, res
			}
		}
	}
	// VOD downloads: Movie_Name_Year (optional res already stripped above).
	if base != "" {
		return strings.ReplaceAll(base, "_", " "), "", res
	}
	return "", "", res
}

func formatRecordingWindow(window string) string {
	// 2006-01-02_150405
	// 2006-01-02_150405-153005
	// 2006-01-02_150405-2006-01-03_003005
	if len(window) >= 17 && window[10] == '_' {
		start := window[:10] + " " + window[11:13] + ":" + window[13:15] + ":" + window[15:17]
		rest := window[17:]
		if rest == "" {
			return start
		}
		if strings.HasPrefix(rest, "-") {
			rest = rest[1:]
		}
		if len(rest) == 6 && rest[0] >= '0' && rest[0] <= '9' {
			return start + "–" + rest[:2] + ":" + rest[2:4] + ":" + rest[4:6]
		}
		if len(rest) >= 17 && rest[10] == '_' {
			end := rest[:10] + " " + rest[11:13] + ":" + rest[13:15] + ":" + rest[15:17]
			return start + " – " + end
		}
		return start
	}
	return strings.ReplaceAll(window, "_", " ")
}

func enrichRecordingFile(item *recordingFileInfo, logos map[string]string, active map[string]playback.RecordingMeta) {
	if ch, _, _ := parseRecordingNameParts(item.Name); ch != "" {
		item.ChannelName = ch
		if logos != nil {
			item.LogoURL = logos[store.NormalizeChannelKey(ch)]
		}
	}
	if job, ok := active[item.Name]; ok {
		item.Recording = true
		item.ChannelName = job.ChannelName
		item.ProgramTitle = job.ProgramTitle
		if logos != nil {
			if logo := logos[store.NormalizeChannelKey(job.ChannelName)]; logo != "" {
				item.LogoURL = logo
			}
		}
	}
}

func applyRecordingMeta(item *recordingFileInfo, meta recordingMetaCacheEntry, logos map[string]string) {
	if meta.ProgramTitle != "" {
		item.ProgramTitle = meta.ProgramTitle
		item.Title = meta.ProgramTitle
		if item.ChannelName != "" {
			item.Title = meta.ProgramTitle + " · " + item.ChannelName
		}
	}
	if meta.Description != "" {
		item.Description = meta.Description
	}
	if meta.Category != "" && item.Category == "" {
		item.Category = meta.Category
	}
	if meta.ChannelName != "" && item.ChannelName == "" {
		item.ChannelName = meta.ChannelName
	}
	if meta.DurationMS > 0 {
		item.DurationMS = meta.DurationMS
	}
	if logos != nil && item.ChannelName != "" && item.LogoURL == "" {
		item.LogoURL = logos[store.NormalizeChannelKey(item.ChannelName)]
	}
}

func metaFromProbeTags(tags map[string]string, durationMS int64) recordingMetaCacheEntry {
	if tags == nil {
		tags = map[string]string{}
	}
	e := recordingMetaCacheEntry{
		ProgramTitle: strings.TrimSpace(tags["title"]),
		Description:  strings.TrimSpace(tags["comment"]),
		Category:     strings.TrimSpace(tags["genre"]),
		DurationMS:   durationMS,
	}
	if show := strings.TrimSpace(tags["show"]); show != "" {
		e.ChannelName = show
	} else if artist := strings.TrimSpace(tags["artist"]); artist != "" {
		e.ChannelName = artist
	}
	return e
}

func (s *Server) liveChannelLogos(ctx context.Context) map[string]string {
	if s.logoIdx != nil {
		if logos, ok := s.logoIdx.get(); ok {
			return logos
		}
	}
	logos, err := s.store.LiveChannelLogoIndex(ctx)
	if err != nil || logos == nil {
		logos = map[string]string{}
	}
	if s.logoIdx != nil {
		s.logoIdx.put(logos)
	}
	return logos
}

func (s *Server) listRecordingFiles(ctx context.Context, logos map[string]string) ([]recordingFileInfo, error) {
	dir := s.recordingsDir()
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	activeByName := map[string]playback.RecordingMeta{}
	if s.recorder != nil {
		for _, job := range s.recorder.List() {
			activeByName[job.FileName] = job
		}
	}
	vodActive := map[string]playback.VodDownloadMeta{}
	if s.vodDL != nil {
		vodActive = s.vodDL.ActiveFileNames()
	}

	seen := map[string]struct{}{}
	out := make([]recordingFileInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".mkv") && !strings.HasSuffix(lower, ".mp4") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		full := filepath.Join(dir, name)
		seen[name] = struct{}{}
		item := recordingFileInfo{
			Name:      name,
			Path:      name, // never expose host filesystem paths to the browser
			SizeBytes: info.Size(),
			ModTime:   info.ModTime().UTC(),
			Title:     displayTitleFromRecordingName(name),
		}
		if vd, ok := vodActive[name]; ok {
			item.Recording = true
			item.ProgramTitle = vd.Title
			item.Title = vd.Title
			item.Category = "VOD"
		}
		enrichRecordingFile(&item, logos, activeByName)
		// Pull EPG tags from the container (cached by name+size+mtime).
		if !item.Recording && item.ProgramTitle == "" {
			if cached, ok := s.recMeta.get(name, item.SizeBytes, item.ModTime); ok {
				applyRecordingMeta(&item, cached, logos)
			} else {
				probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				if probe, err := ffprobe.RunFormat(probeCtx, full); err == nil {
					meta := metaFromProbeTags(probe.FormatTags, probe.DurationMS)
					s.recMeta.put(name, item.SizeBytes, item.ModTime, meta)
					applyRecordingMeta(&item, meta, logos)
				}
				cancel()
			}
		}
		out = append(out, item)
	}
	if s.recMeta != nil {
		s.recMeta.retain(seen)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

func (s *Server) handleListLiveRecordings(w http.ResponseWriter, r *http.Request) {
	logos := s.liveChannelLogos(r.Context())
	active := []map[string]any{}
	if s.recorder != nil {
		for _, meta := range s.recorder.List() {
			resp := s.recordingResponse(meta)
			if logo := logos[store.NormalizeChannelKey(meta.ChannelName)]; logo != "" {
				resp["logo_url"] = logo
			}
			active = append(active, resp)
		}
	}
	downloads := []playback.VodDownloadMeta{}
	if s.vodDL != nil {
		downloads = s.vodDL.List()
		if downloads == nil {
			downloads = []playback.VodDownloadMeta{}
		}
	}
	files, err := s.listRecordingFiles(r.Context(), logos)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if files == nil {
		files = []recordingFileInfo{}
	}
	scheduled, err := s.store.ListScheduledRecordings(r.Context(), false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if scheduled == nil {
		scheduled = []store.ScheduledRecording{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active":    active,
		"files":     files,
		"scheduled": scheduled,
		"downloads": downloads,
		// Back-compat for older clients that expected `recordings` = active jobs.
		"recordings": active,
	})
}

func (s *Server) handleGetRecordingFile(w http.ResponseWriter, r *http.Request) {
	if _, err := s.userFromRequest(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	full, err := s.resolveRecordingFile(chi.URLParam(r, "name"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid recording")
		return
	}
	stat, err := os.Stat(full)
	if err != nil {
		writeErr(w, http.StatusNotFound, "recording not found")
		return
	}
	name := filepath.Base(full)
	logos := s.liveChannelLogos(r.Context())
	activeByName := map[string]playback.RecordingMeta{}
	if s.recorder != nil {
		for _, job := range s.recorder.List() {
			activeByName[job.FileName] = job
		}
	}
	info := recordingFileInfo{
		Name:      name,
		Path:      name, // never expose host filesystem paths to the browser
		SizeBytes: stat.Size(),
		ModTime:   stat.ModTime().UTC(),
		Title:     displayTitleFromRecordingName(name),
	}
	enrichRecordingFile(&info, logos, activeByName)
	if !info.Recording {
		// Incomplete stops (force-kill) leave MKVs without a duration index — repair once.
		if err := playback.EnsureSeekableMKV(full); err != nil {
			slog.Warn("recording seek repair skipped", "file", name, "err", err)
		}
		if st, err := os.Stat(full); err == nil {
			info.SizeBytes = st.Size()
			info.ModTime = st.ModTime().UTC()
		}
		if cached, ok := s.recMeta.get(name, info.SizeBytes, info.ModTime); ok && cached.DurationMS > 0 {
			applyRecordingMeta(&info, cached, logos)
		} else if probe, err := ffprobe.Run(r.Context(), full); err == nil {
			meta := metaFromProbeTags(probe.FormatTags, probe.DurationMS)
			s.recMeta.put(name, info.SizeBytes, info.ModTime, meta)
			applyRecordingMeta(&info, meta, logos)
		}
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleStreamRecordingFile(w http.ResponseWriter, r *http.Request) {
	if _, err := s.userFromRequest(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	full, err := s.resolveRecordingFile(chi.URLParam(r, "name"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid recording")
		return
	}
	f, err := os.Open(full)
	if err != nil {
		writeErr(w, http.StatusNotFound, "recording not found")
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "video/x-matroska")
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, filepath.Base(full), stat.ModTime(), f)
}

func (s *Server) handleRemuxRecordingFile(w http.ResponseWriter, r *http.Request) {
	if _, err := s.userFromRequest(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	full, err := s.resolveRecordingFile(chi.URLParam(r, "name"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid recording")
		return
	}
	if _, err := os.Stat(full); err != nil {
		writeErr(w, http.StatusNotFound, "recording not found")
		return
	}
	start, _ := strconv.ParseFloat(r.URL.Query().Get("start"), 64)
	if start < 0 {
		start = 0
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Accept-Ranges", "none")
	if f, ok := w.(http.Flusher); ok {
		w.WriteHeader(http.StatusOK)
		f.Flush()
	} else {
		w.WriteHeader(http.StatusOK)
	}
	_ = playback.RunCopyFMP4(r.Context(), full, start, w)
}

func (s *Server) handleDeleteRecordingFile(w http.ResponseWriter, r *http.Request) {
	if _, err := s.userFromRequest(r); err != nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	full, err := s.resolveRecordingFile(chi.URLParam(r, "name"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid recording")
		return
	}
	base := filepath.Base(full)
	// Don't delete a file still being written.
	if s.recorder != nil {
		for _, job := range s.recorder.List() {
			if job.FileName == base {
				writeErr(w, http.StatusConflict, "recording still in progress")
				return
			}
		}
	}
	if err := os.Remove(full); err != nil {
		if os.IsNotExist(err) {
			if s.recMeta != nil {
				s.recMeta.delete(base)
			}
			writeErr(w, http.StatusNotFound, "recording not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if s.recMeta != nil {
		s.recMeta.delete(base)
	}
	_ = os.Remove(strings.TrimSuffix(full, filepath.Ext(full)) + ".json")
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "name": base})
}
