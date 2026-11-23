package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"iphoto/internal/config"
	"iphoto/internal/media"
	"iphoto/internal/store"
	"iphoto/internal/thumbs"
	"iphoto/internal/timeparse"
)

// ErrBusy 表示已有导入批次在执行（导入串行，dev.md 5.4）。
var ErrBusy = errors.New("已有导入任务在执行")

const workerCount = 3

// Manager 管理唯一活动导入批次。
type Manager struct {
	store  *store.Store
	paths  config.Paths
	thumbs *thumbs.Queue
	log    *slog.Logger

	// Workers 是文件处理并发数，<=0 时用默认值 3。测试中可设为 1。
	Workers int

	// dedupeMu 串行化「查重 + 插入 importing」，避免并发写入同一哈希。
	dedupeMu sync.Mutex
	mu       sync.Mutex
	active   *run
}

// Snapshot 是进度接口返回的数据。
type Snapshot struct {
	BatchID          int64       `json:"batch_id"`
	Status           string      `json:"status"`
	Total            int64       `json:"total"`
	Processed        int64       `json:"processed"`
	Imported         int64       `json:"imported"`
	DuplicateSkipped int64       `json:"duplicate_skipped"`
	DuplicatePending int64       `json:"duplicate_pending"`
	Failed           int64       `json:"failed"`
	Unsupported      Unsupported `json:"unsupported"`
	Current          string      `json:"current,omitempty"`
	StartedAt        string      `json:"started_at"`
	FinishedAt       *string     `json:"finished_at,omitempty"`
}

// batchStats 是写入 import_batches.stats_json 的统计结构。
type batchStats struct {
	Total            int64       `json:"total"`
	Imported         int64       `json:"imported"`
	DuplicateSkipped int64       `json:"duplicate_skipped"`
	DuplicatePending int64       `json:"duplicate_pending"`
	Failed           int64       `json:"failed"`
	Unsupported      Unsupported `json:"unsupported"`
}

// Resolution 是重复项处理动作。
type Resolution struct {
	ID     int64  `json:"id"`
	Action string `json:"action"` // skip | replace
}

// ResolveResult 汇总批量处理结果。
type ResolveResult struct {
	Updated  int `json:"updated"`
	Replaced int `json:"replaced"`
	Skipped  int `json:"skipped"`
}

// NewManager 创建导入管理器。
func NewManager(st *store.Store, paths config.Paths, q *thumbs.Queue, log *slog.Logger) *Manager {
	return &Manager{store: st, paths: paths, thumbs: q, log: log}
}

// Start 校验目录、创建批次并异步执行扫描与导入。
// tags 会在导入完成后统一加到本批次新增的图片上。
func (m *Manager) Start(sourcePath string, tags []string) (int64, error) {
	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return 0, ErrNotDirectory
	}

	m.mu.Lock()
	if m.active != nil {
		select {
		case <-m.active.done:
			m.active = nil
		default:
			m.mu.Unlock()
			return 0, ErrBusy
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cleanTags := make([]string, 0, len(tags))
	for _, tag := range tags {
		if trimmed := strings.TrimSpace(tag); trimmed != "" {
			cleanTags = append(cleanTags, trimmed)
		}
	}
	r := &run{startedAt: time.Now(), ctx: ctx, cancel: cancel, done: make(chan struct{}), status: "running", tags: cleanTags}
	batch, err := m.store.CreateBatch(ctx, abs, r.startedAt.Format(store.TimeLayout))
	if err != nil {
		cancel()
		m.mu.Unlock()
		return 0, err
	}
	r.id = batch.ID
	m.active = r
	m.mu.Unlock()

	go m.execute(r, abs)
	return r.id, nil
}

// ActiveBatchID 返回当前活动批次，没有则 0。
func (m *Manager) ActiveBatchID() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return 0
	}
	select {
	case <-m.active.done:
		return 0
	default:
		return m.active.id
	}
}

// Cancel 取消指定批次。
func (m *Manager) Cancel(batchID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil || m.active.id != batchID {
		return sql.ErrNoRows
	}
	m.active.cancel()
	return nil
}

// CancelActive 取消当前正在执行的批次并返回其 ID；没有活动批次时返回 0。
func (m *Manager) CancelActive() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return 0
	}
	select {
	case <-m.active.done:
		return 0
	default:
		m.active.cancel()
		return m.active.id
	}
}

// WaitIdle 等待当前批次结束；超时返回 false。没有活动批次时立即返回 true。
func (m *Manager) WaitIdle(timeout time.Duration) bool {
	m.mu.Lock()
	r := m.active
	m.mu.Unlock()
	if r == nil {
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-r.done:
		return true
	case <-timer.C:
		return false
	}
}

// Progress 优先返回内存中活动批次快照，否则从数据库重建。
func (m *Manager) Progress(ctx context.Context, batchID int64) (Snapshot, error) {
	m.mu.Lock()
	r := m.active
	m.mu.Unlock()
	if r != nil && r.id == batchID {
		return r.snapshot(), nil
	}

	b, err := m.store.GetBatch(ctx, batchID)
	if err != nil {
		return Snapshot{}, err
	}
	counts, err := m.store.BatchItemCounts(ctx, batchID)
	if err != nil {
		return Snapshot{}, err
	}
	var total int64
	unsupported := newUnsupported()
	if b.StatsJSON != nil {
		var stats batchStats
		if err := json.Unmarshal([]byte(*b.StatsJSON), &stats); err == nil {
			total = stats.Total
			unsupported = normalizeUnsupported(stats.Unsupported)
		}
	}
	processed := counts["new"] + counts["duplicate_skip"] + counts["duplicate_pending"] +
		counts["duplicate_replace"] + counts["failed"]
	return Snapshot{
		BatchID:          b.ID,
		Status:           b.Status,
		Total:            total,
		Processed:        processed,
		Imported:         counts["new"],
		DuplicateSkipped: counts["duplicate_skip"],
		DuplicatePending: counts["duplicate_pending"],
		Failed:           counts["failed"],
		Unsupported:      unsupported,
		StartedAt:        b.StartedAt,
		FinishedAt:       b.FinishedAt,
	}, nil
}

func normalizeUnsupported(u Unsupported) Unsupported {
	if u.ByExt == nil {
		u.ByExt = make([]ExtCount, 0, 4)
	}
	if u.Files == nil {
		u.Files = make([]string, 0, 16)
	}
	return u
}

// UnsupportedFromStats 从 import_batches.stats_json 中取出「不支持文件」统计。
// 旧批次没有该字段时返回空值。
func UnsupportedFromStats(statsJSON *string) Unsupported {
	if statsJSON == nil {
		return newUnsupported()
	}
	var stats batchStats
	if err := json.Unmarshal([]byte(*statsJSON), &stats); err != nil {
		return newUnsupported()
	}
	return normalizeUnsupported(stats.Unsupported)
}

func (m *Manager) execute(r *run, root string) {
	status := "done"
	defer func() {
		r.mu.Lock()
		r.status = status
		snapshot := r.snapshotLocked()
		r.mu.Unlock()

		stats, _ := json.Marshal(batchStats{
			Total:            snapshot.Total,
			Imported:         snapshot.Imported,
			DuplicateSkipped: snapshot.DuplicateSkipped,
			DuplicatePending: snapshot.DuplicatePending,
			Failed:           snapshot.Failed,
			Unsupported:      snapshot.Unsupported,
		})
		if err := m.store.FinishBatch(context.Background(), r.id, status,
			time.Now().Format(store.TimeLayout), string(stats)); err != nil {
			m.log.Error("结束导入批次失败", "batch", r.id, "err", err)
		}
		close(r.done)
	}()

	scan, err := Scan(root)
	if err != nil {
		m.log.Error("导入扫描失败", "path", root, "err", err)
		status = "failed"
		return
	}
	r.setTotal(int64(scan.Total))
	r.setUnsupported(scan.Unsupported)
	m.log.Info("导入扫描完成", "batch", r.id, "path", root, "total", scan.Total,
		"unreadable", scan.Unreadable, "unsupported", scan.Unsupported.Total)
	if scan.Total == 0 {
		return
	}

	rules, err := m.store.EnabledTimeRules(r.ctx)
	if err != nil {
		m.log.Error("读取时间解析规则失败", "err", err)
	}
	parseRules := make([]timeparse.Rule, 0, len(rules))
	for _, rule := range rules {
		parseRules = append(parseRules, timeparse.Rule{ID: rule.ID, Pattern: rule.Pattern})
	}

	workers := m.Workers
	if workers <= 0 {
		workers = workerCount
	}
	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				m.process(r, p, parseRules)
			}
		}()
	}

loop:
	for _, p := range scan.Files {
		select {
		case <-r.ctx.Done():
			break loop
		case jobs <- p:
		}
	}
	close(jobs)
	wg.Wait()

	// 统一给本批次新增图片打标签（源文件与重复项不受影响）。
	if tags := r.tagList(); len(tags) > 0 {
		if hashes := r.importedHashes(); len(hashes) > 0 {
			if err := m.store.AddTagsToAssets(context.Background(), hashes, tags); err != nil {
				m.log.Error("导入后添加标签失败", "batch", r.id, "err", err)
			}
		}
	}

	if r.ctx.Err() != nil {
		status = "cancelled"
	}
}

func (m *Manager) process(r *run, source string, rules []timeparse.Rule) {
	if r.ctx.Err() != nil {
		return
	}
	r.setCurrent(source)
	name := filepath.Base(source)

	hash, size, err := HashFile(source)
	if err != nil {
		m.record(r, source, "", "failed", err.Error())
		return
	}

	m.dedupeMu.Lock()
	existing, err := m.store.GetAsset(r.ctx, hash)
	if err == nil {
		m.dedupeMu.Unlock()
		switch {
		case existing.Status == "trashed":
			m.record(r, source, hash, "duplicate_skip", "回收站已有相同内容，已跳过")
		case existing.OriginalName == name:
			m.record(r, source, hash, "duplicate_skip", "内容与文件名均相同，已跳过")
		default:
			m.record(r, source, hash, "duplicate_pending", "内容相同但文件名不同："+existing.OriginalName)
		}
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		m.dedupeMu.Unlock()
		m.record(r, source, hash, "failed", "查询库内记录失败："+err.Error())
		return
	}

	relPath, storageName, err := PlanTarget(m.paths.Blobs, name, hash)
	if err != nil {
		m.dedupeMu.Unlock()
		m.record(r, source, hash, "failed", err.Error())
		return
	}
	asset := store.Asset{
		Hash:         hash,
		OriginalName: name,
		StorageName:  storageName,
		RelPath:      relPath,
		Size:         size,
		Mime:         media.MIMEOf(name),
		AddedAt:      time.Now().Format(store.TimeLayout),
		Status:       "importing",
	}
	if err := m.store.InsertImportingAsset(r.ctx, asset); err != nil {
		m.dedupeMu.Unlock()
		m.record(r, source, hash, "failed", err.Error())
		return
	}
	m.dedupeMu.Unlock()

	if err := m.finishImport(r, source, name, hash, size, relPath, rules); err != nil {
		// 回滚半成品：删除物理文件与 importing 记录。
		if rmErr := os.Remove(m.paths.BlobPath(relPath)); rmErr != nil && !os.IsNotExist(rmErr) {
			m.log.Warn("回滚半成品文件失败", "path", relPath, "err", rmErr)
		}
		if dbErr := m.store.DeleteAssetRow(context.Background(), hash); dbErr != nil {
			m.log.Error("回滚 assets 记录失败", "hash", hash, "err", dbErr)
		}
		m.record(r, source, hash, "failed", err.Error())
		return
	}
	m.record(r, source, hash, "new", "")
}

func (m *Manager) finishImport(r *run, source, name, hash string, size int64, relPath string, rules []timeparse.Rule) error {
	written, err := CopyBlob(m.paths.Blobs, relPath, source)
	if err != nil {
		return err
	}
	if written != size {
		return fmt.Errorf("复制后大小校验失败：源 %d 字节，目标 %d 字节", size, written)
	}

	info, err := media.Inspect(m.paths.BlobPath(relPath))
	if err != nil {
		return err
	}
	srcStat, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("读取源文件时间: %w", err)
	}
	res := timeparse.Parse(name, info.TakenAt, info.TakenAtTag, srcStat.ModTime(), time.Now(), rules)
	if err := m.store.MarkAssetReady(r.ctx, hash, written, info.MIME,
		&info.Width, &info.Height, timeparse.Format(res.At), res.Source); err != nil {
		return err
	}
	m.thumbs.Enqueue(hash, relPath)
	return nil
}

func (m *Manager) record(r *run, source, hash, status, message string) {
	ctx := context.Background()
	if _, err := m.store.AddImportItem(ctx, r.id, source, hash, status, message); err != nil {
		m.log.Error("写入导入明细失败", "source", source, "err", err)
	}
	r.addResult(status, hash)
}

// ResolveDuplicates 处理“跳过 / 替换”选择。
func (m *Manager) ResolveDuplicates(ctx context.Context, batchID int64, list []Resolution) (ResolveResult, error) {
	var res ResolveResult
	for _, item := range list {
		it, err := m.store.GetImportItem(ctx, batchID, item.ID)
		if err != nil {
			return res, fmt.Errorf("导入项 %d 不存在: %w", item.ID, err)
		}
		if it.Status != "duplicate_pending" {
			return res, fmt.Errorf("导入项 %d 当前状态为 %s，无法处理", item.ID, it.Status)
		}
		switch item.Action {
		case "skip":
			if err := m.store.SetImportItemStatus(ctx, it.ID, "duplicate_skip", "用户选择跳过"); err != nil {
				return res, err
			}
			res.Skipped++
		case "replace":
			if err := m.replaceExisting(ctx, it); err != nil {
				return res, fmt.Errorf("导入项 %d 替换失败: %w", item.ID, err)
			}
			res.Replaced++
		default:
			return res, fmt.Errorf("未知动作: %q", item.Action)
		}
		res.Updated++
	}
	return res, nil
}

// replaceExisting 只重命名库内物理文件与记录，不复制内容、不动源文件。
func (m *Manager) replaceExisting(ctx context.Context, it store.ImportItem) error {
	if !ValidateHash(it.Hash) {
		return errors.New("缺少有效哈希")
	}
	newName := filepath.Base(it.SourcePath)
	if !media.IsSupported(newName) {
		return errors.New("源文件类型不受支持")
	}

	// 与导入线程互斥，避免同时改同一哈希。
	m.dedupeMu.Lock()
	defer m.dedupeMu.Unlock()

	asset, err := m.store.GetAsset(ctx, it.Hash)
	if err != nil {
		return err
	}
	if asset.Status != "ready" {
		return fmt.Errorf("库内记录状态为 %s，无法替换", asset.Status)
	}

	currentAbs := m.paths.BlobPath(asset.RelPath)
	dirRel := filepath.Dir(filepath.FromSlash(asset.RelPath))
	dirAbs := filepath.Join(m.paths.Blobs, dirRel)

	name := sanitizeName(newName, it.Hash)
	target := filepath.Join(dirAbs, name)
	if target != currentAbs {
		if _, statErr := os.Stat(target); statErr == nil {
			stem, ext := splitName(name)
			name = stem + "__" + it.Hash[:8] + ext
			target = filepath.Join(dirAbs, name)
			if _, err := os.Stat(target); err == nil {
				return fmt.Errorf("目标文件名已存在: %s", name)
			}
		}
		if err := os.Rename(currentAbs, target); err != nil {
			return fmt.Errorf("重命名库内文件: %w", err)
		}
	}

	newRel := filepath.ToSlash(filepath.Join(dirRel, name))
	if err := m.store.SetAssetNames(ctx, it.Hash, newName, name, newRel); err != nil {
		if target != currentAbs {
			if rbErr := os.Rename(target, currentAbs); rbErr != nil {
				m.log.Error("回滚重命名失败", "hash", it.Hash, "err", rbErr)
			}
		}
		return err
	}
	return m.store.SetImportItemStatus(ctx, it.ID, "duplicate_replace", "已替换为 "+newName)
}

// ---- run 状态 ----

type run struct {
	id        int64
	startedAt time.Time
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}

	mu          sync.Mutex
	total       int64
	imported    int64
	dupSkip     int64
	dupPend     int64
	failed      int64
	current     string
	status      string
	tags        []string
	newHashes   []string
	unsupported Unsupported
}

func (r *run) tagList() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.tags...)
}

func (r *run) importedHashes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.newHashes...)
}

func (r *run) setTotal(n int64) {
	r.mu.Lock()
	r.total = n
	r.mu.Unlock()
}

func (r *run) setUnsupported(u Unsupported) {
	r.mu.Lock()
	r.unsupported = u
	r.mu.Unlock()
}

func (r *run) setCurrent(p string) {
	r.mu.Lock()
	r.current = p
	r.mu.Unlock()
}

func (r *run) addResult(status, hash string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch status {
	case "new":
		r.imported++
		if hash != "" {
			r.newHashes = append(r.newHashes, hash)
		}
	case "duplicate_skip":
		r.dupSkip++
	case "duplicate_pending":
		r.dupPend++
	case "failed":
		r.failed++
	}
}

func (r *run) snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked()
}

func (r *run) snapshotLocked() Snapshot {
	return Snapshot{
		BatchID:          r.id,
		Status:           r.status,
		Total:            r.total,
		Processed:        r.imported + r.dupSkip + r.dupPend + r.failed,
		Imported:         r.imported,
		DuplicateSkipped: r.dupSkip,
		DuplicatePending: r.dupPend,
		Failed:           r.failed,
		Unsupported:      normalizeUnsupported(r.unsupported),
		Current:          r.current,
		StartedAt:        r.startedAt.Format(store.TimeLayout),
	}
}
