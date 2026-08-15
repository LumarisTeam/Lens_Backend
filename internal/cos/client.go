// Package cos 封装腾讯云 COS 原生 SDK 的核心操作。
// 桶完全私有，绝不对外暴露固定 URL；所有访问均通过预签名 URL。
package cos

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	cossdk "github.com/tencentyun/cos-go-sdk-v5"
)

// operationTimeout 是单次 COS 操作（Head/Get/Delete）的超时时间。
const operationTimeout = 30 * time.Second

// Client 是 COS 操作的薄封装。
type Client struct {
	cli *cossdk.Client
	ak  string
	sk  string
}

// New 构造 COS Client。endpoint 形如 https://{bucket}.cos.{region}.myqcloud.com。
func New(endpoint, ak, sk string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("cos: invalid endpoint: %w", err)
	}
	b := &cossdk.BaseURL{BucketURL: u}
	cli := cossdk.NewClient(b, &http.Client{
		Timeout: operationTimeout,
		Transport: &cossdk.AuthorizationTransport{
			SecretID:  ak,
			SecretKey: sk,
		},
	})
	return &Client{cli: cli, ak: ak, sk: sk}, nil
}

func newCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), operationTimeout)
}

// PresignGetObject 生成有效期 expireSeconds 秒的 GET 预签名 URL。
func (c *Client) PresignGetObject(fileKey string, expireSeconds int) (string, error) {
	u, err := c.cli.Object.GetPresignedURL(
		context.Background(),
		http.MethodGet,
		fileKey,
		c.ak,
		c.sk,
		time.Duration(expireSeconds)*time.Second,
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("cos: presign get: %w", err)
	}
	return u.String(), nil
}

// PresignPutObject 生成 PUT 预签名 URL，并把 Content-Type 与 Content-Length 纳入签名。
//
// 体积约束（方案 B）：COS 预签名不支持 content-length-range 区间条件，
// 但可以把 Content-Length 钉死为客户端在 presign 阶段声明的 size（服务端已强制 ≤5MB）。
// 客户端 PUT 时必须携带精确匹配的 Content-Length，否则 COS 直接 403 拒绝、不落盘，
// 从而在签名层面杜绝超大文件上传。生产环境再叠加网关层 client_max_body_size 兜底（方案 D）。
func (c *Client) PresignPutObject(fileKey, contentType string, contentLength int64, expireSeconds int) (string, error) {
	opt := &cossdk.ObjectPutOptions{
		ObjectPutHeaderOptions: &cossdk.ObjectPutHeaderOptions{
			ContentType:   contentType,
			ContentLength: contentLength,
		},
	}
	u, err := c.cli.Object.GetPresignedURL(
		context.Background(),
		http.MethodPut,
		fileKey,
		c.ak,
		c.sk,
		time.Duration(expireSeconds)*time.Second,
		opt,
	)
	if err != nil {
		return "", fmt.Errorf("cos: presign put: %w", err)
	}
	return u.String(), nil
}

// HeadObject 返回对象大小（字节），对象不存在时返回 error。
func (c *Client) HeadObject(fileKey string) (int64, error) {
	ctx, cancel := newCtx()
	defer cancel()
	resp, err := c.cli.Object.Head(ctx, fileKey, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.ContentLength, nil
}

// GetObjectRange 读取 [start, end] 闭区间的字节，用于 Magic Number 校验。
func (c *Client) GetObjectRange(fileKey string, start, end int64) ([]byte, error) {
	ctx, cancel := newCtx()
	defer cancel()
	opt := &cossdk.ObjectGetOptions{
		Range: fmt.Sprintf("bytes=%d-%d", start, end),
	}
	resp, err := c.cli.Object.Get(ctx, fileKey, opt)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// DeleteObject 删除单个对象（幂等：删除不存在的 key 视为成功）。
func (c *Client) DeleteObject(fileKey string) error {
	ctx, cancel := newCtx()
	defer cancel()
	_, err := c.cli.Object.Delete(ctx, fileKey)
	if err != nil {
		return fmt.Errorf("cos: delete object: %w", err)
	}
	return nil
}
