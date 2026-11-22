// Package logging 提供同时写 stdout 与 runtime/logs/app.log 的 slog logger，
// 并按大小滚动，仅保留最近一份归档（dev.md 3.4）。
package logging

import (
	"io"
	"log/slog"
	"os"
	"sync"
)

// Setup 打开日志文件并返回 logger 与关闭函数。
// maxBytes <= 0 时使用默认 10MB。
func Setup(path string, maxBytes int64) (*slog.Logger, func() error, error) {
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	w, err := newRotatingWriter(path, maxBytes)
	if err != nil {
		return nil, nil, err
	}
	// 控制台保留日志，便于双击启动时观察。
	multi := io.MultiWriter(os.Stdout, w)
	handler := slog.NewTextHandler(multi, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(handler)
	return logger, w.Close, nil
}

type rotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	file     *os.File
	size     int64
}

func newRotatingWriter(path string, maxBytes int64) (*rotatingWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &rotatingWriter{path: path, maxBytes: maxBytes, file: f, size: info.Size()}, nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.size+int64(len(p)) > w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate 关闭当前文件，覆盖式重命名为 .1，再重新打开新文件。
// 只保留一份归档，避免无限增长。
func (w *rotatingWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	if err := os.Remove(w.path + ".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	w.file = f
	w.size = 0
	return nil
}

func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}
