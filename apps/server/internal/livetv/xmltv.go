package livetv

import (
	"encoding/xml"
	"io"
	"strings"
	"time"
)

// Program is one XMLTV programme entry.
type Program struct {
	ChannelTVGID string
	Title        string
	Description  string
	Start        time.Time
	End          time.Time
	Category     string
}

type xmltv struct {
	XMLName    xml.Name       `xml:"tv"`
	Programmes []xmlProgramme `xml:"programme"`
}

type xmlProgramme struct {
	Start    string     `xml:"start,attr"`
	Stop     string     `xml:"stop,attr"`
	Channel  string     `xml:"channel,attr"`
	Title    []xmlText  `xml:"title"`
	Desc     []xmlText  `xml:"desc"`
	Category []xmlText  `xml:"category"`
}

type xmlText struct {
	Lang string `xml:"lang,attr"`
	Text string `xml:",chardata"`
}

// ParseXMLTV reads an XMLTV document into programmes.
func ParseXMLTV(r io.Reader) ([]Program, error) {
	return ParseXMLTVFiltered(r, nil)
}

// ParseXMLTVFiltered streams an XMLTV document and keeps programmes whose
// channel id is in allowed. If allowed is nil, every programme is kept.
// onBatch is invoked with each completed batch (and may be nil).
func ParseXMLTVFiltered(r io.Reader, allowed map[string]struct{}) ([]Program, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	out := make([]Program, 0, 4096)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "programme" {
			continue
		}
		var p xmlProgramme
		if err := dec.DecodeElement(&p, &se); err != nil {
			continue
		}
		chID := strings.TrimSpace(p.Channel)
		if allowed != nil {
			if _, ok := allowed[chID]; !ok {
				continue
			}
		}
		start, err := parseXMLTVTime(p.Start)
		if err != nil {
			continue
		}
		end, err := parseXMLTVTime(p.Stop)
		if err != nil {
			continue
		}
		title := firstText(p.Title)
		if title == "" {
			title = "Untitled"
		}
		out = append(out, Program{
			ChannelTVGID: chID,
			Title:        title,
			Description:  firstText(p.Desc),
			Start:        start,
			End:          end,
			Category:     firstText(p.Category),
		})
	}
	return out, nil
}

// StreamXMLTVFiltered parses programme-by-programme and calls emit for each
// kept programme. Preferred for very large guides when the caller batches writes.
func StreamXMLTVFiltered(r io.Reader, allowed map[string]struct{}, emit func(Program) error) error {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "programme" {
			continue
		}
		var p xmlProgramme
		if err := dec.DecodeElement(&p, &se); err != nil {
			continue
		}
		chID := strings.TrimSpace(p.Channel)
		if allowed != nil {
			if _, ok := allowed[chID]; !ok {
				continue
			}
		}
		start, err := parseXMLTVTime(p.Start)
		if err != nil {
			continue
		}
		end, err := parseXMLTVTime(p.Stop)
		if err != nil {
			continue
		}
		title := firstText(p.Title)
		if title == "" {
			title = "Untitled"
		}
		if err := emit(Program{
			ChannelTVGID: chID,
			Title:        title,
			Description:  firstText(p.Desc),
			Start:        start,
			End:          end,
			Category:     firstText(p.Category),
		}); err != nil {
			return err
		}
	}
}

func firstText(items []xmlText) string {
	for _, t := range items {
		if s := strings.TrimSpace(t.Text); s != "" {
			return s
		}
	}
	return ""
}

// XMLTV times look like "20260105120000 +0000" or "20260105120000".
func parseXMLTVTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	layouts := []string{
		"20060102150405 -0700",
		"20060102150405 +0000",
		"20060102150405",
	}
	var last error
	for _, layout := range layouts {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t.UTC(), nil
		}
		last = err
	}
	if len(s) >= 19 && (s[14] == '+' || s[14] == '-') {
		t, err := time.Parse("20060102150405-0700", s)
		if err == nil {
			return t.UTC(), nil
		}
		last = err
	}
	return time.Time{}, last
}
