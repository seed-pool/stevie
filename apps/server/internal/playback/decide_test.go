package playback

import (
	"testing"

	"github.com/stevie-media/stevie/apps/server/internal/store"
)

func strp(s string) *string { return &s }

func TestDecideRemuxMKV(t *testing.T) {
	file := store.MediaFile{
		Container: strp("matroska,webm"),
		Streams: []store.MediaStream{
			{StreamIndex: 0, CodecType: "video", CodecName: strp("hevc")},
			{StreamIndex: 1, CodecType: "audio", CodecName: strp("aac"), DispositionDefault: true},
			{StreamIndex: 2, CodecType: "subtitle", CodecName: strp("subrip")},
		},
	}
	d := Decide(file, ClientCaps{HEVC: true}, -1)
	if d.Mode != ModeRemux {
		t.Fatalf("mode=%s want remux", d.Mode)
	}
	if d.DefaultAudio != 1 {
		t.Fatalf("audio=%d", d.DefaultAudio)
	}
	if len(d.SubtitleTracks) != 1 || !d.SubtitleTracks[0].TextBased {
		t.Fatalf("subs=%+v", d.SubtitleTracks)
	}
}

func TestDecideUnsupportedHEVC(t *testing.T) {
	file := store.MediaFile{
		Container: strp("matroska"),
		Streams: []store.MediaStream{
			{StreamIndex: 0, CodecType: "video", CodecName: strp("hevc")},
			{StreamIndex: 1, CodecType: "audio", CodecName: strp("aac")},
		},
	}
	d := Decide(file, ClientCaps{}, -1)
	if d.Mode != ModeUnsupported {
		t.Fatalf("mode=%s", d.Mode)
	}
}

func TestDecideDirectMP4(t *testing.T) {
	file := store.MediaFile{
		Container: strp("mp4"),
		Streams: []store.MediaStream{
			{StreamIndex: 0, CodecType: "video", CodecName: strp("h264")},
			{StreamIndex: 1, CodecType: "audio", CodecName: strp("aac"), DispositionDefault: true},
		},
	}
	d := Decide(file, ClientCaps{}, -1)
	if d.Mode != ModeDirect {
		t.Fatalf("mode=%s reason=%s", d.Mode, d.Reason)
	}
}

func TestDecideSkipsCommentaryOpus(t *testing.T) {
	file := store.MediaFile{
		Container: strp("matroska"),
		Streams: []store.MediaStream{
			{StreamIndex: 0, CodecType: "video", CodecName: strp("h264")},
			{StreamIndex: 1, CodecType: "audio", CodecName: strp("flac"), DispositionDefault: true, Title: strp("Japanese FLAC 2.0")},
			{StreamIndex: 2, CodecType: "audio", CodecName: strp("flac"), Title: strp("English FLAC 2.0")},
			{StreamIndex: 3, CodecType: "audio", CodecName: strp("opus"), Title: strp("Commentary by Doug Smith")},
		},
	}
	d := Decide(file, ClientCaps{Opus: true, FLAC: true}, -1)
	if d.Mode != ModeRemux {
		t.Fatalf("mode=%s", d.Mode)
	}
	if d.SelectedAudio != 1 {
		t.Fatalf("selected=%d want default Japanese FLAC index 1", d.SelectedAudio)
	}
	if !d.TranscodeAudio {
		t.Fatalf("flac must be transcoded for fMP4")
	}
}

func TestDecideTranscodesFLAC(t *testing.T) {
	file := store.MediaFile{
		Container: strp("matroska"),
		Streams: []store.MediaStream{
			{StreamIndex: 0, CodecType: "video", CodecName: strp("h264")},
			{StreamIndex: 1, CodecType: "audio", CodecName: strp("flac"), DispositionDefault: true},
		},
	}
	d := Decide(file, ClientCaps{FLAC: true}, -1)
	if !d.TranscodeAudio {
		t.Fatalf("flac must be transcoded for fMP4 remux")
	}
}
