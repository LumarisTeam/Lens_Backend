// Package feedbackcache 封装反馈中心所需的 Redis 原子操作。
package feedbackcache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"lens-backend/internal/model"
)

const (
	keyPrefix = "fc:"
)

var (
	// ErrCacheMiss 表示反馈中心缓存未命中。
	ErrCacheMiss = errors.New("feedback center cache miss")
)

// ConsumeCodeResult 表示校验码消费结果。
type ConsumeCodeResult int

const (
	ConsumeCodeOK ConsumeCodeResult = iota
	ConsumeCodeUsed
	ConsumeCodeVersionMismatch
	ConsumeCodeMismatch
	ConsumeCodeNonceReused
)

var consumeCodeScript = redis.NewScript(`
local storedCode = redis.call('GET', KEYS[1])
if not storedCode then
  return 1
end
local storedVersion = redis.call('GET', KEYS[3])
if storedVersion ~= ARGV[2] then
  return 2
end
if storedCode ~= ARGV[1] then
  return 3
end
local nonceSet = redis.call('SET', KEYS[2], '1', 'EX', ARGV[3], 'NX')
if not nonceSet then
  return 4
end
redis.call('DEL', KEYS[1])
redis.call('DEL', KEYS[3])
return 0
`)

var rateLimitScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
end
if n > tonumber(ARGV[1]) then
  return 0
end
return 1
`)

var releaseLockScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

// Client 是反馈中心 Redis 客户端。
type Client struct {
	rdb *redis.Client
}

// New 构造 Redis 客户端。
func New(addr, password string, db int) *Client {
	return &Client{
		rdb: redis.NewClient(&redis.Options{
			Addr:         addr,
			Password:     password,
			DB:           db,
			DialTimeout:  2 * time.Second,
			ReadTimeout:  1 * time.Second,
			WriteTimeout: 1 * time.Second,
			PoolTimeout:  2 * time.Second,
		}),
	}
}

// Ping 检查 Redis 连通性。
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Close 关闭连接池。
func (c *Client) Close() error {
	return c.rdb.Close()
}

// StoreCenter 缓存反馈中心非敏感元数据。
func (c *Client) StoreCenter(ctx context.Context, center *model.FeedbackCenter, ttl time.Duration) error {
	expireAt := ""
	if center.ExpireAt != nil {
		expireAt = center.ExpireAt.UTC().Format(time.RFC3339Nano)
	}
	pipe := c.rdb.TxPipeline()
	pipe.HSet(ctx, centerKey(center.CenterID), map[string]any{
		"appId":         center.AppID,
		"status":        center.Status,
		"secretVersion": center.SecretVersion,
		"snMode":        center.SNMode,
		"expireAt":      expireAt,
	})
	pipe.Expire(ctx, centerKey(center.CenterID), ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// GetCenter 读取反馈中心缓存。
func (c *Client) GetCenter(ctx context.Context, centerID string) (*model.FeedbackCenter, error) {
	values, err := c.rdb.HGetAll(ctx, centerKey(centerID)).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, ErrCacheMiss
	}

	version, err := strconv.Atoi(values["secretVersion"])
	if err != nil {
		return nil, fmt.Errorf("invalid cached secretVersion: %w", err)
	}
	center := &model.FeedbackCenter{
		CenterID:      centerID,
		AppID:         values["appId"],
		Status:        values["status"],
		SecretVersion: version,
		SNMode:        values["snMode"],
	}
	if values["expireAt"] != "" {
		t, err := time.Parse(time.RFC3339Nano, values["expireAt"])
		if err != nil {
			return nil, fmt.Errorf("invalid cached expireAt: %w", err)
		}
		center.ExpireAt = &t
	}
	return center, nil
}

// DeleteCenter 删除反馈中心缓存。
func (c *Client) DeleteCenter(ctx context.Context, centerID string) error {
	return c.rdb.Del(ctx, centerKey(centerID)).Err()
}

// StoreCode 写入校验码，值中带 secretVersion 用于识别密钥重置。
func (c *Client) StoreCode(
	ctx context.Context,
	centerID, sn string,
	timestamp int64,
	nonce, code string,
	secretVersion int,
	ttl time.Duration,
) error {
	pipe := c.rdb.TxPipeline()
	pipe.Set(ctx, codeKey(centerID, sn, timestamp, nonce), code, ttl)
	pipe.Set(ctx, codeVersionKey(centerID, sn, timestamp, nonce), secretVersion, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// ConsumeCode 原子校验版本、比对 code、写入 nonce 并删除 code。
func (c *Client) ConsumeCode(
	ctx context.Context,
	centerID, sn string,
	timestamp int64,
	nonce, code string,
	secretVersion int,
	ttl time.Duration,
) (ConsumeCodeResult, error) {
	n, err := consumeCodeScript.Run(
		ctx,
		c.rdb,
		[]string{
			codeKey(centerID, sn, timestamp, nonce),
			nonceKey(centerID, sn, nonce),
			codeVersionKey(centerID, sn, timestamp, nonce),
		},
		code,
		strconv.Itoa(secretVersion),
		int64(ttl/time.Second),
	).Int()
	if err != nil {
		return ConsumeCodeOK, err
	}
	return ConsumeCodeResult(n), nil
}

// DeleteCodes 删除某个中心的全部校验码及版本标记。
func (c *Client) DeleteCodes(ctx context.Context, centerID string) error {
	for _, prefix := range []string{"code", "codever"} {
		if err := c.deleteByPattern(ctx, keyPrefix+prefix+":"+centerID+":*"); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) deleteByPattern(ctx context.Context, pattern string) error {
	var cursor uint64
	for {
		keys, next, err := c.rdb.Scan(ctx, cursor, pattern, 200).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

// Allow 按 centerId+sn+分钟 做固定窗口限流。
func (c *Client) Allow(ctx context.Context, centerID, sn string, limit int, minute time.Time) (bool, error) {
	key := keyPrefix + "rate:" + centerID + ":" + sn + ":" + minute.UTC().Format("200601021504")
	n, err := rateLimitScript.Run(ctx, c.rdb, []string{key}, limit, 60).Int()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// AcquireLock 获取带 TTL 的分布式锁。
func (c *Client) AcquireLock(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	return c.rdb.SetNX(ctx, keyPrefix+"lock:"+key, token, ttl).Result()
}

// ReleaseLock 仅释放当前 token 持有的锁。
func (c *Client) ReleaseLock(ctx context.Context, key, token string) error {
	return releaseLockScript.Run(ctx, c.rdb, []string{keyPrefix + "lock:" + key}, token).Err()
}

func centerKey(centerID string) string {
	return keyPrefix + "center:" + centerID
}

func codeKey(centerID, sn string, timestamp int64, nonce string) string {
	return keyPrefix + "code:" + centerID + ":" + sn + ":" +
		strconv.FormatInt(timestamp, 10) + ":" + nonce
}

func codeVersionKey(centerID, sn string, timestamp int64, nonce string) string {
	return keyPrefix + "codever:" + centerID + ":" + sn + ":" +
		strconv.FormatInt(timestamp, 10) + ":" + nonce
}

func nonceKey(centerID, sn, nonce string) string {
	return keyPrefix + "nonce:" + centerID + ":" + sn + ":" + nonce
}
