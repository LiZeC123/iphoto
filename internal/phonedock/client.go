package phonedock

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultPageSize 是元数据分页的默认大小（接口上限 1000）。
	DefaultPageSize = 500
	// MaxPageSize 是接口允许的最大分页大小。
	MaxPageSize = 1000

	// metadataTimeout 是元数据类请求的整体超时；原文件下载不受它限制。
	metadataTimeout = 20 * time.Second
	// dialTimeout 是建立连接的超时。
	dialTimeout = 5 * time.Second
	// idleTimeout 是下载过程中「连续多久没有新字节」的超时，用于发现手机掉线。
	idleTimeout = 60 * time.Second

	// metadataLimit 是元数据响应体的读取上限。
	metadataLimit = 4 << 20
	// errorBodyLimit 是二进制接口返回的错误信封上限。
	errorBodyLimit = 64 << 10
)

// envelope 是 PhoneDock 的统一响应信封；二进制成功响应不含信封。
type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Client 绑定一台手机地址与一个 Token，可并发使用（下载会与服务端传输锁串行）。
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New 规范化地址与 Token 后创建客户端。
func New(rawURL, token string) (*Client, error) {
	base, err := NormalizeBaseURL(rawURL)
	if err != nil {
		return nil, err
	}
	tok, err := NormalizeToken(token)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	return &Client{
		baseURL: base,
		token:   tok,
		http: &http.Client{
			// 不设整体超时：大文件下载需要时间，由 idleTimeout 与 context 控制。
			Transport: &http.Transport{
				DialContext:           dialer.DialContext,
				ResponseHeaderTimeout: metadataTimeout,
				MaxIdleConnsPerHost:   2,
				IdleConnTimeout:       90 * time.Second,
			},
			// 不跟随重定向：避免把 Token 带给意料之外的主机（含 http→https 跳转）。
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("不接受重定向")
			},
		},
	}, nil
}

// BaseURL 返回规范化后的地址，例如 http://192.168.1.215:8765。
func (c *Client) BaseURL() string { return c.baseURL }

// Token 返回当前 Token，仅供本机调试与日志脱敏前使用。
func (c *Client) Token() string { return c.token }

// DeviceInfo 读取设备信息（不需要相册权限）。
func (c *Client) DeviceInfo(ctx context.Context) (DeviceInfo, error) {
	var info DeviceInfo
	if err := c.getJSON(ctx, "/v1/device/info", nil, &info); err != nil {
		return DeviceInfo{}, err
	}
	return info, nil
}

// Version 读取媒体库水位（图片与视频 collection 的最大 GENERATION_MODIFIED）。
func (c *Client) Version(ctx context.Context) (int64, error) {
	var data struct {
		Version int64 `json:"version"`
	}
	if err := c.getJSON(ctx, "/v1/album/version", nil, &data); err != nil {
		return 0, err
	}
	return data.Version, nil
}

// MediaPage 查询一页媒体元数据。
// since > 0 时只返回 generationModified 严格更大的新增/变更记录（增量模式）。
func (c *Client) MediaPage(ctx context.Context, kind string, since int64, offset, limit int) (MediaPage, error) {
	if kind != KindImage && kind != KindVideo {
		return MediaPage{}, fmt.Errorf("kind 必须是 image 或 video，收到 %q", kind)
	}
	if limit <= 0 || limit > MaxPageSize {
		limit = DefaultPageSize
	}
	if offset < 0 {
		offset = 0
	}
	q := url.Values{
		"kind":   {kind},
		"offset": {strconv.Itoa(offset)},
		"limit":  {strconv.Itoa(limit)},
	}
	if since > 0 {
		q.Set("sinceGenerationModified", strconv.FormatInt(since, 10))
	}
	var page MediaPage
	if err := c.getJSON(ctx, "/v1/album/media", q, &page); err != nil {
		return MediaPage{}, err
	}
	if page.Items == nil {
		page.Items = []MediaItem{}
	}
	return page, nil
}

// Content 把媒体原文件流式写入 w，返回写入的字节数。
//
// PhoneDock 的二进制接口不套信封：成功时直接是文件字节；
// 失败时仍然返回 HTTP 200 + application/json 信封（例如 3001）。
// 因此这里先看 Content-Type，再决定是解析错误还是复制字节。
func (c *Client) Content(ctx context.Context, kind string, id int64, w io.Writer) (int64, error) {
	if kind != KindImage && kind != KindVideo {
		return 0, fmt.Errorf("kind 必须是 image 或 video，收到 %q", kind)
	}
	if id <= 0 {
		return 0, fmt.Errorf("媒体 id 非法: %d", id)
	}
	q := url.Values{"kind": {kind}}
	fullURL := c.baseURL + "/v1/album/media/" + strconv.FormatInt(id, 10) + "/content?" + q.Encode()

	// 单独一层 cancel，配合「无新字节」计时器发现手机掉线。
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fullURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "*/*")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, &TransportError{Op: "GET", URL: c.baseURL, Err: err}
	}
	defer resp.Body.Close()

	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if isJSONContentType(contentType) {
		return 0, decodeAPIError(resp, c.baseURL)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, &TransportError{
			Op:  "GET",
			URL: c.baseURL,
			Err: fmt.Errorf("HTTP %d：%s", resp.StatusCode, resp.Status),
		}
	}

	idle := time.AfterFunc(idleTimeout, cancel)
	defer idle.Stop()

	var written int64
	buf := make([]byte, 64<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			idle.Reset(idleTimeout)
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return written, fmt.Errorf("写入本地文件失败: %w", writeErr)
			}
			written += int64(n)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			if ctx.Err() != nil {
				return written, ctx.Err()
			}
			return written, &TransportError{Op: "GET", URL: c.baseURL, Err: readErr}
		}
	}
	idle.Stop()

	// Content-Length 与实际字节数不一致通常意味着连接被中断。
	if cl := resp.ContentLength; cl >= 0 && cl != written {
		return written, &TransportError{
			Op:  "GET",
			URL: c.baseURL,
			Err: fmt.Errorf("传输不完整：Content-Length=%d，实际收到 %d 字节", cl, written),
		}
	}
	return written, nil
}

// getJSON 带重试地读取一个 JSON 接口。业务错误不重试。
func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	fullURL := c.baseURL + path
	if len(q) > 0 {
		fullURL += "?" + q.Encode()
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt) * 300 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
		lastErr = c.getJSONOnce(ctx, fullURL, out)
		if lastErr == nil {
			return nil
		}
		var api *APIError
		if errors.As(lastErr, &api) {
			return lastErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return lastErr
}

func (c *Client) getJSONOnce(ctx context.Context, fullURL string, out any) error {
	reqCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return &TransportError{Op: "GET", URL: c.baseURL, Err: fmt.Errorf("元数据请求超时")}
		}
		return &TransportError{Op: "GET", URL: c.baseURL, Err: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, metadataLimit))
	if err != nil {
		return &TransportError{Op: "GET", URL: c.baseURL, Err: err}
	}

	var env envelope
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &env); err != nil {
			return &APIError{
				Code:       CodeInternal,
				Message:    fmt.Sprintf("响应不是可解析的信封（HTTP %d）", resp.StatusCode),
				HTTPStatus: resp.StatusCode,
			}
		}
	}
	if env.Code != CodeOK {
		return &APIError{Code: env.Code, Message: env.Message, HTTPStatus: resp.StatusCode}
	}
	if resp.StatusCode != http.StatusOK {
		// 没有业务信封的 HTTP 失败（例如网关 502、代理错误页）按传输错误处理，允许重试。
		return &TransportError{
			Op:  "GET",
			URL: c.baseURL,
			Err: fmt.Errorf("HTTP %d：%s", resp.StatusCode, resp.Status),
		}
	}
	if out == nil {
		return nil
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return errors.New("响应缺少 data 字段")
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("解析 data 失败: %w", err)
	}
	return nil
}

// decodeAPIError 读取二进制接口失败时返回的 JSON 信封。
func decodeAPIError(resp *http.Response, baseURL string) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
	if err != nil {
		return &TransportError{Op: "GET", URL: baseURL, Err: err}
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return &APIError{
			Code:       CodeInternal,
			Message:    fmt.Sprintf("二进制接口返回了无法解析的 JSON（HTTP %d）", resp.StatusCode),
			HTTPStatus: resp.StatusCode,
		}
	}
	return &APIError{Code: env.Code, Message: env.Message, HTTPStatus: resp.StatusCode}
}

func isJSONContentType(ct string) bool {
	return strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ct, "text/json")
}
