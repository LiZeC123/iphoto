package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"iphoto/internal/config"
	"iphoto/internal/httpapi"
	"iphoto/internal/importer"
	"iphoto/internal/logging"
	"iphoto/internal/maintenance"
	"iphoto/internal/media"
	"iphoto/internal/similar"
	"iphoto/internal/store"
	"iphoto/internal/thumbs"
	"iphoto/web"
)

// version 由构建时 -ldflags 覆盖。
var version = "0.1.0-dev"

func main() {
	noBrowser := flag.Bool("no-browser", false, "启动后不自动打开浏览器")
	flag.Parse()

	if err := run(*noBrowser); err != nil {
		fmt.Fprintf(os.Stderr, "iphoto 启动失败: %v\n", err)
		os.Exit(1)
	}
}

func run(noBrowser bool) error {
	paths, err := config.Load()
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return fmt.Errorf("初始化目录: %w", err)
	}

	logger, closeLog, err := logging.Setup(paths.LogFile, 10<<20)
	if err != nil {
		return fmt.Errorf("初始化日志: %w", err)
	}
	defer closeLog()
	slog.SetDefault(logger)

	logger.Info("iphoto 启动", "version", version, "base", paths.Base, "library", paths.Library, "runtime", paths.Runtime)
	if !media.FFmpegAvailable() {
		logger.Warn("未检测到 ffmpeg，视频缩略图将不可用（MP4 仍可导入与播放）")
	}

	st, err := store.Open(paths.DB)
	if err != nil {
		return fmt.Errorf("打开数据库: %w", err)
	}
	defer st.Close()

	queue := thumbs.New(paths, logger, 2)
	queueCtx, stopQueue := context.WithCancel(context.Background())
	defer stopQueue()
	queue.Start(queueCtx)

	if rec, recErr := maintenance.RecoverInterrupted(context.Background(), st, paths, logger); recErr != nil {
		logger.Warn("启动修复失败", "err", recErr)
	} else if rec.Ready > 0 || rec.Removed > 0 || rec.Batches > 0 {
		logger.Info("启动修复完成", "ready", rec.Ready, "removed", rec.Removed, "interrupted_batches", rec.Batches)
	}

	manager := importer.NewManager(st, paths, queue, logger)
	similarManager := similar.NewManager(st, paths, logger)

	// 设置页的「关闭服务」通过该通道触发与信号一致的优雅退出路径。
	shutdownCh := make(chan struct{})
	var shutdownOnce sync.Once
	requestShutdown := func() {
		shutdownOnce.Do(func() { close(shutdownCh) })
	}

	handler := httpapi.New(httpapi.Options{
		Store:    st,
		Paths:    paths,
		Web:      web.Dist(),
		Version:  version,
		Logger:   logger,
		Importer: manager,
		Similar:  similarManager,
		Thumbs:   queue,
		Shutdown: requestShutdown,
	})

	addr := net.JoinHostPort(config.BindAddr, strconv.Itoa(config.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败（端口固定为 %d，请先释放该端口）: %w", addr, config.Port, err)
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ln)
	}()

	url := "http://" + addr
	logger.Info("服务已就绪", "url", url, "db", paths.DB)
	if !noBrowser {
		if err := openBrowser(url); err != nil {
			logger.Warn("自动打开浏览器失败，请手动访问", "url", url, "err", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		logger.Info("收到退出信号，正在关闭")
	case <-shutdownCh:
		logger.Info("收到页面关闭请求，正在关闭")
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http 服务异常: %w", err)
		}
		return nil
	}

	// 优雅关闭：先取消进行中的后台任务并等待其收尾，再关闭 HTTP 服务。
	if batchID := manager.CancelActive(); batchID != 0 {
		logger.Info("正在取消进行中的导入批次", "batch", batchID)
		if !manager.WaitIdle(3 * time.Second) {
			logger.Warn("导入批次未在 3 秒内结束，继续关闭（启动时会做中断修复）", "batch", batchID)
		}
	}
	if similarManager.CancelActive() {
		logger.Info("正在取消进行中的相似图片检测")
		if !similarManager.WaitIdle(2 * time.Second) {
			logger.Warn("相似图片检测未在 2 秒内结束，继续关闭")
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("关闭 http 服务: %w", err)
	}
	logger.Info("已退出")
	return nil
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
