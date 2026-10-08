package livetv

import (
	"strings"
	"testing"
)

func TestParseM3U(t *testing.T) {
	src := `#EXTM3U
#EXTINF:-1 tvg-id="1" tvg-name="News" group-title="CA| News" tvg-logo="http://logo/x.png",News HD
http://example.com/1.ts
#EXTINF:-1 tvg-id="2" group-title="Sports",Sports 1
http://example.com/2.ts
`
	channels, err := ParseM3U(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 2 {
		t.Fatalf("got %d channels", len(channels))
	}
	if channels[0].TVGID != "1" || channels[0].Name != "News HD" || channels[0].GroupTitle != "CA| News" {
		t.Fatalf("unexpected first channel: %+v", channels[0])
	}
	if channels[1].StreamURL != "http://example.com/2.ts" {
		t.Fatalf("unexpected url: %s", channels[1].StreamURL)
	}
}

func TestPreferXtreamHLS(t *testing.T) {
	in := "http://example.com:80/live/user/pass/123.ts"
	out := preferXtreamHLS(in)
	if out != "http://example.com:80/live/user/pass/123.m3u8" {
		t.Fatalf("got %s", out)
	}
}
