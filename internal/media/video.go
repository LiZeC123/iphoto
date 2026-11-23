package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ErrFFmpegNotFound 表示系统 PATH 中没有 ffmpeg，无法生成视频缩略图。
// 视频仍可导入与播放，只是网格没有缩略图。
var ErrFFmpegNotFound = errors.New("未找到 ffmpeg，无法生成视频缩略图")

// videoThumbTimeout 是单次抽帧的超时（长视频也只解码目标帧，正常在 1s 内完成）。
const videoThumbTimeout = 30 * time.Second

// IsVideo 判断是否为当前支持的视频格式（MVP 仅 MP4）。
// 目前按扩展名判断；视频不解码，只存原文件。
func IsVideo(p string) bool {
	return strings.EqualFold(filepath.Ext(p), ".mp4")
}

// FFmpegAvailable 报告 PATH 中是否有可执行的 ffmpeg。
func FFmpegAvailable() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// GenerateVideoThumbnail 用 ffmpeg 从视频中抽一帧，缩放到 384px 宽并编码为 JPEG。
// 优先取 0.5s 处（避开黑场/片头），极短视频回退到第一帧。
// 原视频不做任何修改；输出先写临时文件再 rename。
func GenerateVideoThumbnail(src, dst string) error {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ErrFFmpegNotFound
	}
	if _, err := os.Stat(src); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("创建缩略图目录: %w", err)
	}

	tmp := dst + ".tmp.jpg"
	defer os.Remove(tmp)

	var lastErr error
	for _, seek := range []string{"0.5", "0"} {
		args := []string{
			"-nostdin", "-v", "error", "-y",
			"-ss", seek,
			"-i", src,
			"-map", "0:v:0",
			"-frames:v", "1",
			"-an",
			"-threads", "1",
			"-vf", "scale=384:-2",
			"-q:v", "3",
			tmp,
		}
		ctx, cancel := context.WithTimeout(context.Background(), videoThumbTimeout)
		out, runErr := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput()
		cancel()
		if runErr == nil {
			if _, statErr := os.Stat(tmp); statErr == nil {
				if err := os.Rename(tmp, dst); err != nil {
					return fmt.Errorf("落盘视频缩略图: %w", err)
				}
				return nil
			}
			runErr = errors.New("ffmpeg 未生成输出文件")
		}
		lastErr = fmt.Errorf("ffmpeg 抽帧失败（-ss %s）: %w: %s", seek, runErr, tailOutput(out))
	}
	return lastErr
}

// tailOutput 截取 ffmpeg 输出的末尾，避免把整段日志塞进错误信息。
func tailOutput(out []byte) string {
	s := strings.TrimSpace(string(out))
	const max = 400
	if len(s) > max {
		return "..." + s[len(s)-max:]
	}
	return s
}
