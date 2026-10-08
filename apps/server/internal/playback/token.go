package playback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const tokenTTL = 6 * time.Hour
const handoffTTL = 2 * time.Minute

type StreamToken struct {
	UserID  uuid.UUID `json:"user_id"`
	MediaID uuid.UUID `json:"media_id"`
	Kind    string    `json:"kind,omitempty"` // "media", "live", or "handoff"
}

type TokenStore struct {
	rdb *redis.Client
}

func NewTokenStore(rdb *redis.Client) *TokenStore {
	return &TokenStore{rdb: rdb}
}

func (t *TokenStore) Issue(ctx context.Context, userID, mediaID uuid.UUID) (string, time.Time, error) {
	return t.IssueKind(ctx, userID, mediaID, "media")
}

func (t *TokenStore) IssueKind(ctx context.Context, userID, mediaID uuid.UUID, kind string) (string, time.Time, error) {
	return t.issue(ctx, userID, mediaID, kind, tokenTTL)
}

// IssueHandoff creates a one-time code so the cleartext Live host can mint a session cookie.
func (t *TokenStore) IssueHandoff(ctx context.Context, userID uuid.UUID) (string, time.Time, error) {
	return t.issue(ctx, userID, uuid.Nil, "handoff", handoffTTL)
}

func (t *TokenStore) issue(ctx context.Context, userID, mediaID uuid.UUID, kind string, ttl time.Duration) (string, time.Time, error) {
	if kind == "" {
		kind = "media"
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(raw)
	exp := time.Now().Add(ttl)
	payload, _ := json.Marshal(StreamToken{UserID: userID, MediaID: mediaID, Kind: kind})
	if err := t.rdb.Set(ctx, key(token), payload, ttl).Err(); err != nil {
		return "", time.Time{}, err
	}
	return token, exp, nil
}

func (t *TokenStore) Get(ctx context.Context, token string) (StreamToken, error) {
	b, err := t.rdb.Get(ctx, key(token)).Bytes()
	if err != nil {
		return StreamToken{}, err
	}
	var st StreamToken
	if err := json.Unmarshal(b, &st); err != nil {
		return StreamToken{}, err
	}
	return st, nil
}

// Consume returns the token payload and deletes it (one-time use).
func (t *TokenStore) Consume(ctx context.Context, token string) (StreamToken, error) {
	b, err := t.rdb.GetDel(ctx, key(token)).Bytes()
	if err != nil {
		return StreamToken{}, err
	}
	var st StreamToken
	if err := json.Unmarshal(b, &st); err != nil {
		return StreamToken{}, err
	}
	return st, nil
}

func key(token string) string {
	return fmt.Sprintf("stevie:stream:%s", token)
}
