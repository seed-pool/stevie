package ffprobe

import "testing"

func TestDetectDynamicRangeFromProbe(t *testing.T) {
	probe := []byte(`{
	  "streams":[{
	    "codec_type":"video",
	    "color_transfer":"smpte2084",
	    "color_primaries":"bt2020",
	    "side_data_list":[{"side_data_type":"DOVI configuration record"}]
	  }]
	}`)
	dr := DetectDynamicRange(probe, "Movie.2024.2160p.DV.HDR.mkv")
	if !dr.DolbyVision || !dr.HDR10 {
		t.Fatalf("got %+v", dr)
	}
}

func TestDetectDynamicRangeFilenameDV(t *testing.T) {
	dr := DetectDynamicRange(nil, "/media/library/Show.S01E01.2160p.DV.H.265.mkv")
	if !dr.DolbyVision {
		t.Fatalf("expected DV from filename, got %+v", dr)
	}
}
