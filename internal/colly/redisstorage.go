// Originally licensed under the Apache License Version 2.0.
// Copied and modified to use more recent dependencies from:
// https://github.com/gocolly/redisstorage

package colly

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStorage implements the redis storage backend for Colly
type RedisStorage struct {
	// Client is the redis connection
	Client *redis.Client

	// Prefix is an optional string in the keys. It can be used
	// to use one redis database for independent scraping tasks.
	Prefix string

	// Expiration time for Visited keys. After expiration pages
	// are to be visited again.
	Expires time.Duration

	ctx context.Context
	mu  sync.RWMutex // Only used for cookie methods.
}

func NewRedisStorage(ctx context.Context, redis *redis.Client, prefix string) *RedisStorage {
	return &RedisStorage{
		ctx: ctx,

		Prefix: prefix,
		Client: redis,
	}
}

func (s *RedisStorage) Init() error {
	return nil
}

// Clear removes all entries from the storage
func (s *RedisStorage) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.Client.Keys(s.ctx, s.getCookieID("*"))
	keys, err := r.Result()
	if err != nil {
		return err
	}
	r2 := s.Client.Keys(s.ctx, s.Prefix+":request:*")
	keys2, err := r2.Result()
	if err != nil {
		return err
	}
	keys = append(keys, keys2...)
	keys = append(keys, s.getQueueID())
	return s.Client.Del(s.ctx, keys...).Err()
}

// Visited implements colly/storage.Visited()
func (s *RedisStorage) Visited(requestID uint64) error {
	return s.Client.Set(s.ctx, s.getIDStr(requestID), "1", s.Expires).Err()
}

// IsVisited implements colly/storage.IsVisited()
func (s *RedisStorage) IsVisited(requestID uint64) (bool, error) {
	_, err := s.Client.Get(s.ctx, s.getIDStr(requestID)).Result()
	if err == redis.Nil {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// SetCookies implements colly/storage..SetCookies()
func (s *RedisStorage) SetCookies(u *url.URL, cookies string) {
	// TODO(js) Cookie methods currently have no way to return an error.

	// We need to use a write lock to prevent a race in the db:
	// if two callers set cookies in a very small window of time,
	// it is possible to drop the new cookies from one caller
	// ('last update wins' == best avoided).
	s.mu.Lock()
	defer s.mu.Unlock()
	// return s.Client.Set(s.getCookieID(u.Host), stringify(cnew), 0).Err()
	err := s.Client.Set(context.TODO(), s.getCookieID(u.Host), cookies, 0).Err()
	if err != nil {
		// return nil
		log.Printf("SetCookies() .Set error %s", err)
		return
	}
}

// Cookies implements colly/storage.Cookies()
func (s *RedisStorage) Cookies(u *url.URL) string {
	// TODO(js) Cookie methods currently have no way to return an error.

	s.mu.RLock()
	cookiesStr, err := s.Client.Get(s.ctx, s.getCookieID(u.Host)).Result()
	s.mu.RUnlock()
	if err == redis.Nil {
		cookiesStr = ""
	} else if err != nil {
		// return nil, err
		log.Printf("Cookies() .Get error %s", err)
		return ""
	}
	return cookiesStr
}

// AddRequest implements queue.RedisStorage.AddRequest() function
func (s *RedisStorage) AddRequest(r []byte) error {
	return s.Client.RPush(s.ctx, s.getQueueID(), r).Err()
}

// GetRequest implements queue.RedisStorage.GetRequest() function
func (s *RedisStorage) GetRequest() ([]byte, error) {
	r, err := s.Client.LPop(s.ctx, s.getQueueID()).Bytes()
	if err != nil {
		return nil, err
	}
	return r, err
}

// QueueSize implements queue.RedisStorage.QueueSize() function
func (s *RedisStorage) QueueSize() (int, error) {
	i, err := s.Client.LLen(s.ctx, s.getQueueID()).Result()
	return int(i), err
}

func (s *RedisStorage) getIDStr(ID uint64) string {
	return fmt.Sprintf("%s:request:%d", s.Prefix, ID)
}

func (s *RedisStorage) getCookieID(c string) string {
	return fmt.Sprintf("%s:cookie:%s", s.Prefix, c)
}

func (s *RedisStorage) getQueueID() string {
	return fmt.Sprintf("%s:queue", s.Prefix)
}
