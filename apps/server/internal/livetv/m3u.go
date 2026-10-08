package livetv

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var attrRe = regexp.MustCompile(`([a-zA-Z0-9\-]+)="([^"]*)"`)

const (
	SourceM3U    = "m3u"
	SourceXtream = "xtream"
)

// CategoryInfo is a live category row for upserts.
type CategoryInfo struct {
	ExternalID   string
	Name         string
	ChannelCount int
	SortOrder    int
}

// Channel is one live channel entry (M3U or Xtream).
type Channel struct {
	TVGID              string
	Name               string
	GroupTitle         string
	LogoURL            string
	StreamURL          string
	SortOrder          int
	Source             string
	ExternalID         string
	CategoryExternalID string
	EPGChannelID       string
	Num                int
}

// ParseM3U reads an extended M3U playlist into channels.
func ParseM3U(r io.Reader) ([]Channel, error) {
	sc := bufio.NewScanner(r)
	// Some playlists have very long lines (logos / titles).
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	var out []Channel
	var pending *Channel
	order := 0

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#EXTM3U") {
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			ch := Channel{SortOrder: order}
			order++
			rest := strings.TrimPrefix(line, "#EXTINF:")
			comma := strings.LastIndex(rest, ",")
			attrPart := rest
			if comma >= 0 {
				attrPart = rest[:comma]
				ch.Name = strings.TrimSpace(rest[comma+1:])
			}
			for _, m := range attrRe.FindAllStringSubmatch(attrPart, -1) {
				key := strings.ToLower(m[1])
				val := m[2]
				switch key {
				case "tvg-id":
					ch.TVGID = val
				case "tvg-name":
					if ch.Name == "" {
						ch.Name = val
					}
				case "tvg-logo":
					ch.LogoURL = SanitizeLogoURL(val)
				case "group-title":
					ch.GroupTitle = val
				}
			}
			if ch.Name == "" {
				ch.Name = "Channel"
			}
			pending = &ch
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if pending != nil {
			pending.StreamURL = preferXtreamHLS(line)
			pending.Source = SourceM3U
			pending.ExternalID = m3uExternalID(*pending)
			if pending.EPGChannelID == "" {
				pending.EPGChannelID = pending.TVGID
			}
			out = append(out, *pending)
			pending = nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SanitizeLogoURL keeps only http(s) logo URLs; strips a common leading '-' typo.
func SanitizeLogoURL(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "-http://") || strings.HasPrefix(u, "-https://") {
		u = u[1:]
	}
	lower := strings.ToLower(u)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return u
	}
	return ""
}

// preferXtreamHLS rewrites classic Xtream /live/user/pass/id.ts URLs to .m3u8.
// IPTVNator and most web players prefer HLS when the panel advertises it.
func preferXtreamHLS(streamURL string) string {
	u := strings.TrimSpace(streamURL)
	if u == "" {
		return u
	}
	lower := strings.ToLower(u)
	if !strings.Contains(lower, "/live/") {
		return u
	}
	if strings.HasSuffix(lower, ".ts") {
		return u[:len(u)-3] + ".m3u8"
	}
	if strings.HasSuffix(lower, ".m3u8") || strings.Contains(lower, ".m3u8?") {
		return u
	}
	// Extension-less Xtream live URLs: append .m3u8
	if !strings.Contains(u[strings.LastIndex(u, "/")+1:], ".") {
		return u + ".m3u8"
	}
	return u
}

func m3uExternalID(ch Channel) string {
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%s|%s", ch.StreamURL, ch.TVGID, ch.Name)))
	return hex.EncodeToString(h[:])
}
