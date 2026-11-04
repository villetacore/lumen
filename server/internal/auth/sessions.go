package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// SessionStore keeps sign-ins beyond the life of the process (ADR-0015).
// The provider's memory stays the first place it looks, so a store outage does
// not sign out anyone already working.
type SessionStore interface {
	Save(ctx context.Context, s *Session) error
	Load(ctx context.Context, id string) (*Session, error) // nil when absent
	Delete(ctx context.Context, id string) error
}

// RedisSessions stores sessions in Redis. Keys hold a hash of the cookie value,
// so reading the keys does not give a working cookie, and values hold who the
// user is but no Keycloak tokens: nothing in Redis opens Keycloak.
type RedisSessions struct{ client *redis.Client }

// NewRedisSessions connects with a redis:// or rediss:// URL.
func NewRedisSessions(ctx context.Context, url string) (*RedisSessions, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		// The parser's message may quote the URL, and with it the password.
		return nil, errors.New("неверный адрес Redis: ожидается redis://… или rediss://…")
	}
	client := redis.NewClient(opts)
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("подключение к Redis: %w", err)
	}
	return &RedisSessions{client: client}, nil
}

func (r *RedisSessions) Close() error { return r.client.Close() }

func sessionKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return "lumen:session:" + hex.EncodeToString(sum[:])
}

type storedSession struct {
	Claims *Claims   `json:"claims"`
	Expiry time.Time `json:"expiry"`
	Direct bool      `json:"direct,omitempty"`
}

func (r *RedisSessions) Save(ctx context.Context, s *Session) error {
	ttl := time.Until(s.Expiry)
	if ttl <= 0 {
		return nil
	}
	data, err := json.Marshal(storedSession{Claims: s.Claims, Expiry: s.Expiry, Direct: s.Direct})
	if err != nil {
		return err
	}
	return r.client.Set(ctx, sessionKey(s.ID), data, ttl).Err()
}

func (r *RedisSessions) Load(ctx context.Context, id string) (*Session, error) {
	data, err := r.client.Get(ctx, sessionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st storedSession
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &Session{ID: id, Claims: st.Claims, Expiry: st.Expiry, Direct: st.Direct}, nil
}

func (r *RedisSessions) Delete(ctx context.Context, id string) error {
	return r.client.Del(ctx, sessionKey(id)).Err()
}
