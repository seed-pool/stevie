package mediainfo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Report is a MediaInfo analysis of a local file.
type Report struct {
	Text string          `json:"text"`
	JSON json.RawMessage `json:"json,omitempty"`
}

// Run executes mediainfo against path and returns text + JSON output.
func Run(ctx context.Context, path string) (Report, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Report{}, fmt.Errorf("missing path")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	textCmd := exec.CommandContext(ctx, "mediainfo", "--Full", path)
	var textOut, textErr bytes.Buffer
	textCmd.Stdout = &textOut
	textCmd.Stderr = &textErr
	if err := textCmd.Run(); err != nil {
		msg := strings.TrimSpace(textErr.String())
		if msg == "" {
			msg = err.Error()
		}
		return Report{}, fmt.Errorf("mediainfo: %s", msg)
	}

	jsonCmd := exec.CommandContext(ctx, "mediainfo", "--Output=JSON", path)
	var jsonOut, jsonErr bytes.Buffer
	jsonCmd.Stdout = &jsonOut
	jsonCmd.Stderr = &jsonErr
	_ = jsonCmd.Run() // text is enough if JSON fails on older builds

	rep := Report{Text: strings.TrimSpace(textOut.String())}
	if rep.Text == "" {
		return Report{}, fmt.Errorf("mediainfo produced empty report")
	}
	if raw := bytes.TrimSpace(jsonOut.Bytes()); len(raw) > 0 && json.Valid(raw) {
		rep.JSON = json.RawMessage(raw)
	}
	return rep, nil
}

// Available reports whether the mediainfo binary is on PATH.
func Available() bool {
	_, err := exec.LookPath("mediainfo")
	return err == nil
}
