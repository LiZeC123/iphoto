// Package phonedock 是 PhoneDock（手机坞）v1 接口的客户端。
//
// 只依赖标准库，职责是：规范化地址与 Token、解析统一信封（{code,message,data}）、
// 把业务错误码翻译成可读中文、分页查询媒体元数据、流式下载原文件。
// 业务判断（增量、去重、落库）不在这里，属于 importer。
package phonedock

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// 媒体类型：PhoneDock 的相册接口必须明确指定 image 或 video，不支持 all。
const (
	KindImage = "image"
	KindVideo = "video"
)

// 业务错误码（见 GET /v1/capabilities 的 errors 字段）。
const (
	CodeOK                 = 0
	CodeBadRequest         = 1001
	CodeBadPagination      = 1002
	CodeUnknownPath        = 1004
	CodeMissingToken       = 2001
	CodeInvalidToken       = 2002
	CodeAuthLocked         = 2003
	CodeMediaNotFound      = 3001
	CodePermissionMissing  = 3002
	CodeCapabilityDisabled = 3003
	CodeInternal           = 5001
)

// DeviceInfo 对应 GET /v1/device/info 的 data。
type DeviceInfo struct {
	Model             string `json:"model"`
	Manufacturer      string `json:"manufacturer"`
	AndroidVersion    string `json:"androidVersion"`
	AppVersion        string `json:"appVersion"`
	BatteryLevel      int    `json:"batteryLevel"`
	BatteryCharging   bool   `json:"batteryCharging"`
	StorageFreeBytes  int64  `json:"storageFreeBytes"`
	StorageTotalBytes int64  `json:"storageTotalBytes"`
	IP                string `json:"ip"`
	Port              int    `json:"port"`
	UptimeSeconds     int64  `json:"uptimeSeconds"`
}

// MediaItem 对应 GET /v1/album/media 的 data.items[]。
type MediaItem struct {
	ID                 int64  `json:"id"`
	Kind               string `json:"kind"`
	DisplayName        string `json:"displayName"`
	MimeType           string `json:"mimeType"`
	Size               int64  `json:"size"`
	DateAdded          int64  `json:"dateAdded"`
	DateModified       int64  `json:"dateModified"`
	GenerationModified int64  `json:"generationModified"`
	Width              int    `json:"width"`
	Height             int    `json:"height"`
	DurationMs         *int64 `json:"durationMs"`
}

// MediaPage 是 GET /v1/album/media 的 data。
// 携带 sinceGenerationModified 时 Total 是过滤后的数量。
type MediaPage struct {
	Total  int         `json:"total"`
	Offset int         `json:"offset"`
	Limit  int         `json:"limit"`
	Items  []MediaItem `json:"items"`
}

// APIError 是 PhoneDock 返回的业务错误（HTTP 状态码可能是 200、401、429）。
type APIError struct {
	Code       int
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("phonedock: code=%d http=%d message=%s", e.Code, e.HTTPStatus, e.Message)
}

// TransportError 包装连接、超时、TLS、重定向等传输层错误。
type TransportError struct {
	Op  string
	URL string
	Err error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("phonedock: %s %s 失败: %v", e.Op, e.URL, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

// CodeOf 取出业务错误码；不是业务错误时返回 false。
func CodeOf(err error) (int, bool) {
	var api *APIError
	if errors.As(err, &api) {
		return api.Code, true
	}
	return 0, false
}

// IsCode 判断是否为指定业务错误码。
func IsCode(err error, code int) bool {
	c, ok := CodeOf(err)
	return ok && c == code
}

// IsAuthError 判断是否为鉴权类错误（缺少 Token / Token 错误 / 被锁定）。
func IsAuthError(err error) bool {
	c, ok := CodeOf(err)
	return ok && c >= CodeMissingToken && c <= CodeAuthLocked
}

// IsPermissionError 判断是否为手机端相册权限缺失或能力被关闭。
func IsPermissionError(err error) bool {
	c, ok := CodeOf(err)
	return ok && (c == CodePermissionMissing || c == CodeCapabilityDisabled)
}

// IsMediaGone 判断该媒体在手机侧已不可用（可能已被删除）。
func IsMediaGone(err error) bool { return IsCode(err, CodeMediaNotFound) }

// UserMessage 把客户端错误翻译成一句中文说明，直接用于页面提示。
func UserMessage(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "已取消"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "连接手机超时：请确认手机与电脑在同一 Wi-Fi、PhoneDock 服务已启动且地址端口正确"
	}

	var api *APIError
	if errors.As(err, &api) {
		switch api.Code {
		case CodeMissingToken:
			return "手机端说没有携带 Token：请在页面填写 PhoneDock 上显示的 4 位 Token"
		case CodeInvalidToken:
			return "Token 不正确：请核对手机 PhoneDock 上的 Token（重置过 Token 的话请重新填写）"
		case CodeAuthLocked:
			return "同一 IP 认证失败次数过多，已被手机锁定 60 秒，请稍后重试"
		case CodePermissionMissing:
			return "手机未授予相册权限：请在 PhoneDock 里点「授予相册权限」后再试"
		case CodeCapabilityDisabled:
			return "手机端已关闭相册能力：请在 PhoneDock 设置页开启后再试"
		case CodeMediaNotFound:
			return "该媒体在手机上已不可用（可能已被删除或移动）"
		case CodeBadRequest, CodeBadPagination:
			return "手机拒绝了请求参数：" + api.Message
		case CodeUnknownPath:
			return "手机上没有这个接口：请确认 PhoneDock 版本为 v1 且地址端口正确"
		case CodeInternal:
			return "手机端处理失败：" + api.Message
		default:
			if api.Message != "" {
				return fmt.Sprintf("手机端返回错误（code=%d）：%s", api.Code, api.Message)
			}
			return fmt.Sprintf("手机端返回错误（code=%d）", api.Code)
		}
	}

	var te *TransportError
	if errors.As(err, &te) {
		return fmt.Sprintf("无法连接手机 %s：请确认手机与电脑在同一 Wi-Fi、PhoneDock 服务已启动且地址端口正确", te.URL)
	}
	return "请求手机失败：" + err.Error()
}

// NormalizeBaseURL 规范化用户填写的手机地址：
// 允许 "192.168.1.215:8765" 或 "http://192.168.1.215:8765"，
// 只保留 scheme://host，拒绝路径、查询、片段与用户信息（避免误把 Token 发到奇怪的位置）。
func NormalizeBaseURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("手机地址不能为空")
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("手机地址不合法: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return "", fmt.Errorf("手机地址只支持 http/https，收到 %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("手机地址缺少主机与端口，例如 192.168.1.215:8765")
	}
	if u.User != nil {
		return "", errors.New("手机地址不能包含用户名或密码")
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		return "", errors.New("手机地址只需填主机与端口，不要带路径")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("手机地址不能带查询参数或锚点")
	}
	return u.Scheme + "://" + u.Host, nil
}

// NormalizeToken 去除首尾空白并做最小校验。
// 当前 PhoneDock 的 Token 是 4 位数字，但这里不强制长度，便于后续扩展。
func NormalizeToken(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("Token 不能为空")
	}
	if len(s) > 64 {
		return "", errors.New("Token 过长")
	}
	if strings.ContainsAny(s, " \t\r\n") {
		return "", errors.New("Token 中不能包含空白字符")
	}
	return s, nil
}
