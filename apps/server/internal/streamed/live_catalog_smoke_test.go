package streamed

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveCatalogSmoke(t *testing.T) {
	if os.Getenv("STEVIE_STREAMED_SMOKE") == "" {
		t.Skip("set STEVIE_STREAMED_SMOKE=1 for live ppv.st catalog smoke")
	}
	c := NewClient("https://api.ppv.st")
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	matches, err := c.AllMatches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) < 5 {
		t.Fatalf("expected catalog rows, got %d", len(matches))
	}
	live, err := c.LiveMatchIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("matches=%d live=%d sample=%q cat=%q sources=%d", len(matches), len(live), matches[0].Title, matches[0].Category, len(matches[0].Sources))
	ch := ChannelsForMatch(matches[0])
	if len(ch) == 0 || ch[0].Source == "" || ch[0].SourceID == "" {
		t.Fatalf("bad channels %#v", ch)
	}
}
