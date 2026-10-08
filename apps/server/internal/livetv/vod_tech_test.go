package livetv

import "testing"

func TestParseVodTech(t *testing.T) {
	cases := []struct {
		name string
		want VodTech
	}{
		{
			name: "AL - Cover-Up.1991.1080p.BluRay.x264.AAC-[YTS.MX]",
			want: VodTech{Resolution: "1080p", VideoCodec: "H.264", AudioCodec: "AAC", Source: "BluRay", Height: 1080},
		},
		{
			name: "4K-AR - The.Hangover.2.2011.2160p.OSN.WEB-DL",
			want: VodTech{Resolution: "2160p", Source: "WEB-DL", Height: 2160},
		},
		{
			name: "Movie.2024.720p.WEBRip.x265.HDR.DTS-HD.MA",
			want: VodTech{Resolution: "720p", VideoCodec: "HEVC", AudioCodec: "DTS-HD", Source: "WEBRip", HDR: "HDR", Height: 720},
		},
		{
			name: "007 - A View to a Kill (1985)",
			want: VodTech{},
		},
	}
	for _, tc := range cases {
		got := ParseVodTech(tc.name)
		if got.Resolution != tc.want.Resolution || got.VideoCodec != tc.want.VideoCodec ||
			got.AudioCodec != tc.want.AudioCodec || got.Source != tc.want.Source ||
			got.HDR != tc.want.HDR || got.Height != tc.want.Height {
			t.Fatalf("%q\n got %+v\nwant %+v", tc.name, got, tc.want)
		}
	}
}
