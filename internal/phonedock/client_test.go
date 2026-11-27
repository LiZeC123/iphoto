package phonedock

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeBaseURL(t *testing.T) {
	ok := map[string]string{
		"192.168.1.215:8765":             "http://192.168.1.215:8765",
		"  http://192.168.1.215:8765/  ": "http://192.168.1.215:8765",
		"https://phone.local:8765":       "https://phone.local:8765",
		"http://192.168.1.215:8765":      "http://192.168.1.215:8765",
	}
	for in, want := range ok {
		got, err := NormalizeBaseURL(in)
		if err != nil {
			t.Fatalf("NormalizeBaseURL(%q) 意外失败: %v", in, err)
		}
		if got != want {
			t.Fatalf("NormalizeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}

	bad := []string{
		"",
		"   ",
		"ftp://192.168.1.215:8765",
		"192.168.1.215:8765/v1",
		"http://192.168.1.215:8765/v1/album",
		"http://user:pass@192.168.1.215:8765",
		"http://192.168.1.215:8765?token=1",
		"http://",
	}
	for _, in := range bad {
		if got, err := NormalizeBaseURL(in); err == nil {
			t.Fatalf("NormalizeBaseURL(%q) 期望失败，得到 %q", in, got)
		}
	}
}

func TestNormalizeToken(t *testing.T) {
	if got, err := NormalizeToken(" 2179 "); err != nil || got != "2179" {
		t.Fatalf("NormalizeToken = %q, %v", got, err)
	}
	for _, bad := range []string{"", "  ", "21 79", strings.Repeat("9", 65)} {
		if _, err := NormalizeToken(bad); err == nil {
			t.Fatalf("NormalizeToken(%q) 期望失败", bad)
		}
	}
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "2179")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func writeEnvelope(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("写响应: %v", err)
	}
}

func TestDeviceInfoAndVersion(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer 2179" {
			t.Errorf("Authorization = %q", got)
		}
		switch r.URL.Path {
		case "/v1/device/info":
			writeEnvelope(t, w, http.StatusOK, map[string]any{
				"code": 0, "message": "ok",
				"data": map[string]any{
					"model": "22101317C", "manufacturer": "Xiaomi", "androidVersion": "13",
					"batteryLevel": 83, "ip": "192.168.1.215", "port": 8765,
				},
			})
		case "/v1/album/version":
			writeEnvelope(t, w, http.StatusOK, map[string]any{
				"code": 0, "message": "ok", "data": map[string]any{"version": 442415},
			})
		default:
			t.Errorf("未预期的路径: %s", r.URL.Path)
		}
	})

	info, err := c.DeviceInfo(context.Background())
	if err != nil {
		t.Fatalf("DeviceInfo: %v", err)
	}
	if info.Manufacturer != "Xiaomi" || info.Model != "22101317C" || info.BatteryLevel != 83 {
		t.Fatalf("设备信息不符: %+v", info)
	}

	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != 442415 {
		t.Fatalf("version = %d", v)
	}
}

func TestMediaPageQuery(t *testing.T) {
	var gotSince, gotOffset, gotLimit, gotKind string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		gotKind, gotSince = q.Get("kind"), q.Get("sinceGenerationModified")
		gotOffset, gotLimit = q.Get("offset"), q.Get("limit")
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"code": 0, "message": "ok",
			"data": map[string]any{
				"total": 2, "offset": 0, "limit": 500,
				"items": []map[string]any{
					{"id": 1000112680, "kind": "image", "displayName": "a.jpg", "size": 10,
						"dateAdded": 1795764800, "generationModified": 442415, "width": 4000, "height": 3000},
					{"id": 1000112657, "kind": "image", "displayName": "b.jpg", "size": 20,
						"dateAdded": 1795652045, "generationModified": 442312, "durationMs": nil},
				},
			},
		})
	})

	page, err := c.MediaPage(context.Background(), KindImage, 442300, 40, 2)
	if err != nil {
		t.Fatalf("MediaPage: %v", err)
	}
	if gotKind != "image" || gotSince != "442300" || gotOffset != "40" || gotLimit != "2" {
		t.Fatalf("查询参数不符: kind=%q since=%q offset=%q limit=%q", gotKind, gotSince, gotOffset, gotLimit)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("分页结果不符: %+v", page)
	}
	if page.Items[0].ID != 1000112680 || page.Items[0].Width != 4000 {
		t.Fatalf("item[0] 不符: %+v", page.Items[0])
	}
	if page.Items[1].DurationMs != nil {
		t.Fatalf("图片 durationMs 应为 nil: %+v", page.Items[1])
	}
}

func TestMediaPageOmitsSinceOnFirstSync(t *testing.T) {
	called := false
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		if _, ok := r.URL.Query()["sinceGenerationModified"]; ok {
			t.Errorf("首次同步不应带 sinceGenerationModified")
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"code": 0, "message": "ok", "data": map[string]any{"total": 0, "items": []any{}},
		})
	})
	if _, err := c.MediaPage(context.Background(), KindVideo, 0, 0, 0); err != nil {
		t.Fatalf("MediaPage: %v", err)
	}
	if !called {
		t.Fatal("服务端未被调用")
	}
}

func TestContentSuccess(t *testing.T) {
	payload := bytes.Repeat([]byte("jpegdata"), 1000)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/album/media/1000112680/content" {
			t.Errorf("路径 = %s", r.URL.Path)
		}
		if r.URL.Query().Get("kind") != "image" {
			t.Errorf("kind = %q", r.URL.Query().Get("kind"))
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "8000")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(payload); err != nil {
			t.Errorf("写 body: %v", err)
		}
	})

	var buf bytes.Buffer
	n, err := c.Content(context.Background(), KindImage, 1000112680, &buf)
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	if n != int64(len(payload)) || buf.Len() != len(payload) {
		t.Fatalf("写入字节数 = %d/%d, payload = %d", n, buf.Len(), len(payload))
	}
}

func TestContentJSONError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"code": 3001, "message": "媒体不存在或已被删除", "data": nil,
		})
	})
	var buf bytes.Buffer
	n, err := c.Content(context.Background(), KindVideo, 42, &buf)
	if err == nil {
		t.Fatal("期望返回业务错误")
	}
	if n != 0 || buf.Len() != 0 {
		t.Fatalf("错误时不应写入数据: n=%d len=%d", n, buf.Len())
	}
	if !IsMediaGone(err) {
		t.Fatalf("期望 3001，得到 %v", err)
	}
	if msg := UserMessage(err); !strings.Contains(msg, "不可用") {
		t.Fatalf("UserMessage = %q", msg)
	}
}

func TestContentTruncated(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		// 只写 10 字节就关闭连接。
		_, _ = w.Write(bytes.Repeat([]byte("x"), 10))
	})
	var buf bytes.Buffer
	n, err := c.Content(context.Background(), KindImage, 1, &buf)
	if err == nil {
		t.Fatal("期望检测到传输不完整")
	}
	if n != 10 || buf.Len() != 10 {
		t.Fatalf("已写入字节 = %d/%d", n, buf.Len())
	}
	if !strings.Contains(UserMessage(err), "无法连接手机") && !strings.Contains(UserMessage(err), "传输") {
		t.Fatalf("UserMessage = %q", UserMessage(err))
	}
}

func TestAuthErrors(t *testing.T) {
	cases := []struct {
		status  int
		code    int
		message string
	}{
		{http.StatusUnauthorized, 2001, "缺少 Authorization: Bearer <token>"},
		{http.StatusUnauthorized, 2002, "token 错误"},
		{http.StatusTooManyRequests, 2003, "锁定"},
	}
	for _, tc := range cases {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeEnvelope(t, w, tc.status, map[string]any{
				"code": tc.code, "message": tc.message, "data": nil,
			})
		})
		_, err := c.DeviceInfo(context.Background())
		if err == nil {
			t.Fatalf("code %d 期望失败", tc.code)
		}
		if !IsAuthError(err) {
			t.Fatalf("code %d 应识别为鉴权错误: %v", tc.code, err)
		}
		if got, ok := CodeOf(err); !ok || got != tc.code {
			t.Fatalf("CodeOf = %d/%v", got, ok)
		}
		if strings.Contains(UserMessage(err), "phonedock:") {
			t.Fatalf("UserMessage 不应暴露内部格式: %q", UserMessage(err))
		}
	}
}

func TestMetadataRetriesThenSucceeds(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"code": 0, "message": "ok", "data": map[string]any{"version": 7},
		})
	})
	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != 7 {
		t.Fatalf("version = %d", v)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("请求次数 = %d, want 2", got)
	}
}

func TestBusinessErrorNotRetried(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		writeEnvelope(t, w, http.StatusOK, map[string]any{
			"code": 3002, "message": "缺少相册权限", "data": nil,
		})
	})
	_, err := c.Version(context.Background())
	if err == nil {
		t.Fatal("期望失败")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("业务错误不应重试，请求次数 = %d", got)
	}
	if !IsPermissionError(err) {
		t.Fatalf("3002 应识别为权限错误: %v", err)
	}
}

func TestRedirectRejected(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/other", http.StatusFound)
	})
	if _, err := c.DeviceInfo(context.Background()); err == nil {
		t.Fatal("期望拒绝重定向")
	}
}

func TestUserMessageForTransportError(t *testing.T) {
	c, err := New("127.0.0.1:1", "2179")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.DeviceInfo(context.Background())
	if err == nil {
		t.Fatal("期望连接失败")
	}
	msg := UserMessage(err)
	if !strings.Contains(msg, "无法连接手机") || !strings.Contains(msg, "127.0.0.1:1") {
		t.Fatalf("UserMessage = %q", msg)
	}
}

func TestContextCancelDuringContent(t *testing.T) {
	release := make(chan struct{})
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("x"), 100))
		w.(http.Flusher).Flush()
		<-release
	})
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithCancel(context.Background())
	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := c.Content(ctx, KindImage, 1, &buf)
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("取消后期望返回错误")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("取消未生效")
	}
}
