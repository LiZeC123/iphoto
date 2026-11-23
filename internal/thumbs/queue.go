// Package thumbs 实现缩略图后台队列（dev.md 第 6 章）。
package thumbs

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"

	"iphoto/internal/config"
	"iphoto/internal/media"
)

// Queue 是固定 worker 数的缩略图生成队列，同一哈希不会重复排队。
type Queue struct {
	paths   config.Paths
	log     *slog.Logger
	workers int
	ch      chan job

	mu      sync.Mutex
	pending map[string]struct{}
	// failed 记录本进程内已确认生成失败的哈希（如动图 WebP、损坏文件），
	// 避免前端每次请求缩略图都重复解码。重启后仍会重试一次。
	failed map[string]struct{}
	// ffmpegOnce 保证「未安装 ffmpeg」只提示一次，而不是每张视频一条日志。
	ffmpegOnce sync.Once
	wg         sync.WaitGroup
}

type job struct {
	hash    string
	relPath string
}

// New 创建队列。workers <= 0 时默认 2。
func New(paths config.Paths, log *slog.Logger, workers int) *Queue {
	if workers <= 0 {
		workers = 2
	}
	return &Queue{
		paths:   paths,
		log:     log,
		workers: workers,
		ch:      make(chan job, 1024),
		pending: make(map[string]struct{}),
		failed:  make(map[string]struct{}),
	}
}

// Start 启动 worker，ctx 取消后退出。
func (q *Queue) Start(ctx context.Context) {
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case j := <-q.ch:
					q.generate(j)
				}
			}
		}()
	}
}

func (q *Queue) generate(j job) {
	defer func() {
		q.mu.Lock()
		delete(q.pending, j.hash)
		q.mu.Unlock()
	}()

	dst := q.paths.ThumbPath(j.hash)
	if _, err := os.Stat(dst); err == nil {
		return
	}
	src := q.paths.BlobPath(j.relPath)
	var err error
	if media.IsVideo(j.relPath) {
		err = media.GenerateVideoThumbnail(src, dst)
	} else {
		err = media.GenerateThumbnail(src, dst)
	}
	if err == nil {
		return
	}

	q.mu.Lock()
	q.failed[j.hash] = struct{}{}
	q.mu.Unlock()
	if errors.Is(err, media.ErrFFmpegNotFound) {
		q.ffmpegOnce.Do(func() {
			q.log.Warn("未检测到 ffmpeg，视频缩略图将不可用（视频仍可播放）")
		})
		return
	}
	q.log.Warn("缩略图生成失败，本次运行不再重试", "hash", j.hash, "err", err)
}

// Enqueue 非阻塞入队；已存在缩略图或已在队列中则忽略。
func (q *Queue) Enqueue(hash, relPath string) {
	if _, err := os.Stat(q.paths.ThumbPath(hash)); err == nil {
		return
	}
	q.mu.Lock()
	if _, ok := q.failed[hash]; ok {
		q.mu.Unlock()
		return
	}
	if _, ok := q.pending[hash]; ok {
		q.mu.Unlock()
		return
	}
	q.pending[hash] = struct{}{}
	q.mu.Unlock()

	select {
	case q.ch <- job{hash: hash, relPath: relPath}:
	default:
		// 队列满时丢弃，前端再次请求缩略图会重新触发。
		q.mu.Lock()
		delete(q.pending, hash)
		q.mu.Unlock()
		q.log.Warn("缩略图队列已满，丢弃任务", "hash", hash)
	}
}
