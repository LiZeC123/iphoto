package similar

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"iphoto/internal/config"
	"iphoto/internal/store"
)

// 检测任务状态。
const (
	StatusIdle      = "idle"
	StatusRunning   = "running"
	StatusDone      = "done"
	StatusCancelled = "cancelled"
	StatusFailed    = "failed"
)

// 阈值与组数上限。
const (
	MinThreshold = 0
	MaxThreshold = 64
	// DefaultThreshold 是 dHash 的默认阈值（汉明距离）。
	DefaultThreshold = 8
	// DefaultMaxGroups 是单次检测最多返回的相似组数量。
	DefaultMaxGroups = 100
)

// 检测相关的可读错误。
var (
	ErrBusy       = errors.New("已有相似图片检测任务在执行")
	ErrNoResult   = errors.New("暂无检测结果")
	ErrNotRunning = errors.New("没有正在执行的检测任务")
	ErrAlgorithm  = errors.New("暂只支持 dhash 算法")
	ErrThreshold  = errors.New("阈值需为 0–64 的整数")
)

// Options 是一次检测的输入参数。
type Options struct {
	// From 为起始时间（有效拍摄时间 >= From），空表示不限；由调用方规范化为库内时间格式。
	From      string
	Algorithm string
	Threshold int
}

func (o Options) normalized() (Options, error) {
	o.Algorithm = strings.TrimSpace(o.Algorithm)
	if o.Algorithm == "" {
		o.Algorithm = AlgorithmDHash
	}
	if o.Algorithm != AlgorithmDHash {
		return o, ErrAlgorithm
	}
	if o.Threshold < MinThreshold || o.Threshold > MaxThreshold {
		return o, ErrThreshold
	}
	o.From = strings.TrimSpace(o.From)
	return o, nil
}

// GroupImage 是相似组里的一张图片，附带与组内上一张的汉明距离。
type GroupImage struct {
	store.Asset
	DistanceFromPrev *int `json:"distance_from_prev,omitempty"`
}

// Group 是一组按时间连续、相邻距离都不超过阈值的图片。
type Group struct {
	ID     int          `json:"id"`
	Images []GroupImage `json:"images"`
}

// Result 是检测完成后的完整结果。
type Result struct {
	Groups     []Group `json:"groups"`
	GroupCount int     `json:"group_count"`
	Total      int     `json:"total"`
	Scanned    int     `json:"scanned"`
	Failures   int     `json:"failures"`
	Cached     int     `json:"cached"`
	Computed   int     `json:"computed"`
	Truncated  bool    `json:"truncated"`
	Cancelled  bool    `json:"cancelled"`
	Threshold  int     `json:"threshold"`
	Algorithm  string  `json:"algorithm"`
	From       string  `json:"from,omitempty"`
	DurationMS int64   `json:"duration_ms"`
	FinishedAt string  `json:"finished_at"`
}

// Snapshot 是进度接口返回的数据。
type Snapshot struct {
	ID         int64   `json:"id"`
	Status     string  `json:"status"`
	Total      int     `json:"total"`
	Processed  int     `json:"processed"`
	Groups     int     `json:"groups"`
	Failures   int     `json:"failures"`
	Cached     int     `json:"cached"`
	Computed   int     `json:"computed"`
	Current    string  `json:"current,omitempty"`
	Threshold  int     `json:"threshold"`
	Algorithm  string  `json:"algorithm"`
	From       string  `json:"from,omitempty"`
	StartedAt  string  `json:"started_at,omitempty"`
	FinishedAt *string `json:"finished_at,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// Manager 管理唯一活动的相似检测任务，并保留最近一次结果供页面读取。
type Manager struct {
	store *store.Store
	paths config.Paths
	log   *slog.Logger

	// MaxGroups 是本次运行允许返回的最大组数，<=0 时使用 DefaultMaxGroups（测试可调小）。
	MaxGroups int

	mu     sync.Mutex
	seq    int64
	active *run
}

// NewManager 创建检测管理器。
func NewManager(st *store.Store, paths config.Paths, log *slog.Logger) *Manager {
	return &Manager{store: st, paths: paths, log: log}
}

// Start 启动一次异步检测，返回任务 ID。同一时间只允许一个任务。
func (m *Manager) Start(opts Options) (int64, error) {
	opts, err := opts.normalized()
	if err != nil {
		return 0, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		select {
		case <-m.active.done:
		default:
			return 0, ErrBusy
		}
	}

	m.seq++
	ctx, cancel := context.WithCancel(context.Background())
	maxGroups := m.MaxGroups
	if maxGroups <= 0 {
		maxGroups = DefaultMaxGroups
	}
	r := &run{
		id:        m.seq,
		opts:      opts,
		maxGroups: maxGroups,
		startedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
		status:    StatusRunning,
	}
	m.active = r
	go m.execute(r)
	return r.id, nil
}

// Progress 返回当前/最近任务的进度；没有任务时返回 idle。
func (m *Manager) Progress() Snapshot {
	m.mu.Lock()
	r := m.active
	m.mu.Unlock()
	if r == nil {
		return Snapshot{Status: StatusIdle}
	}
	return r.snapshot()
}

// Result 返回最近一次完成的检测结果。
func (m *Manager) Result() (Result, error) {
	m.mu.Lock()
	r := m.active
	m.mu.Unlock()
	if r == nil {
		return Result{}, ErrNoResult
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.result == nil {
		return Result{}, ErrNoResult
	}
	return *r.result, nil
}

// Cancel 取消正在执行的任务。
func (m *Manager) Cancel() error {
	m.mu.Lock()
	r := m.active
	m.mu.Unlock()
	if r == nil {
		return ErrNotRunning
	}
	select {
	case <-r.done:
		return ErrNotRunning
	default:
	}
	r.cancel()
	return nil
}

// CancelActive 在进程关闭时取消活动任务，返回是否确实取消了任务。
func (m *Manager) CancelActive() bool {
	m.mu.Lock()
	r := m.active
	m.mu.Unlock()
	if r == nil {
		return false
	}
	select {
	case <-r.done:
		return false
	default:
		r.cancel()
		return true
	}
}

// WaitIdle 等待当前任务结束；超时返回 false。
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

func (m *Manager) execute(r *run) {
	assets, err := m.store.ListDetectionAssets(context.Background(), r.opts.From)
	if err != nil {
		m.finish(r, StatusFailed, err.Error(), nil)
		return
	}
	r.setTotal(len(assets))

	cache, err := m.store.Signatures(context.Background(), r.opts.Algorithm)
	if err != nil {
		m.finish(r, StatusFailed, err.Error(), nil)
		return
	}

	groups := make([]Group, 0, 16)
	chain := make([]GroupImage, 0, 8)
	var prevBits uint64
	prevValid := false
	processed, failures, cachedCount, computedCount := 0, 0, 0, 0
	status := StatusDone
	cancelled := false
	maxGroups := r.maxGroups

	// flush 收尾当前相似链；返回是否已达到组数上限。
	flush := func() bool {
		if len(chain) >= 2 {
			groups = append(groups, Group{ID: len(groups) + 1, Images: chain})
			r.addGroup()
		}
		chain = nil
		return len(groups) >= maxGroups
	}

	stop := false
	for _, a := range assets {
		if r.ctx.Err() != nil {
			status = StatusCancelled
			cancelled = true
			break
		}
		r.setCurrent(a.OriginalName)

		value, fromCache, sigErr := m.signatureFor(a, cache)
		if sigErr != nil {
			failures++
			r.addFailure()
			m.log.Warn("计算相似指纹失败，跳过该图片", "hash", a.Hash, "name", a.OriginalName, "err", sigErr)
			// 无法比较的图片会打断相似链。
			stop = flush()
			prevValid = false
			processed++
			r.setProcessed(processed)
			if stop {
				break
			}
			continue
		}
		if fromCache {
			cachedCount++
			r.addCached()
		} else {
			computedCount++
			r.addComputed()
		}

		if prevValid {
			distance := Hamming(prevBits, value)
			if distance <= r.opts.Threshold {
				chain = append(chain, GroupImage{Asset: a, DistanceFromPrev: intPtr(distance)})
			} else {
				stop = flush()
				chain = []GroupImage{{Asset: a}}
			}
		} else {
			chain = []GroupImage{{Asset: a}}
		}
		prevBits = value
		prevValid = true
		processed++
		r.setProcessed(processed)
		if stop {
			break
		}
	}

	// 把最后一段（或取消时已收集的一段）补进结果；达到上限时不再追加。
	if len(chain) >= 2 && len(groups) < maxGroups {
		flush()
	}

	truncated := status == StatusDone && len(groups) >= maxGroups && processed < len(assets)
	res := &Result{
		Groups:     groups,
		GroupCount: len(groups),
		Total:      len(assets),
		Scanned:    processed,
		Failures:   failures,
		Cached:     cachedCount,
		Computed:   computedCount,
		Truncated:  truncated,
		Cancelled:  cancelled,
		Threshold:  r.opts.Threshold,
		Algorithm:  r.opts.Algorithm,
		From:       r.opts.From,
	}
	m.finish(r, status, "", res)
	if truncated {
		m.log.Info("相似检测达到组数上限，提前结束", "groups", len(groups), "processed", processed, "total", len(assets))
	}
}

// signatureFor 优先复用缓存；没有缓存时计算并写入。
func (m *Manager) signatureFor(a store.Asset, cache map[string]string) (uint64, bool, error) {
	if raw, ok := cache[a.Hash]; ok {
		if value, err := ParseSignature(raw); err == nil {
			return value, true, nil
		}
		m.log.Warn("指纹缓存格式非法，重新计算", "hash", a.Hash, "value", raw)
	}

	value, err := m.compute(a)
	if err != nil {
		return 0, false, err
	}
	formatted := FormatSignature(value)
	cache[a.Hash] = formatted
	if saveErr := m.store.SaveSignature(context.Background(), a.Hash, AlgorithmDHash, formatted); saveErr != nil {
		m.log.Warn("写入指纹缓存失败", "hash", a.Hash, "err", saveErr)
	}
	return value, false, nil
}

// compute 计算一张图片的指纹：优先 384px 缩略图（已按 EXIF 转正），缺失/损坏时回退原图。
func (m *Manager) compute(a store.Asset) (uint64, error) {
	thumb := m.paths.ThumbPath(a.Hash)
	if _, err := os.Stat(thumb); err == nil {
		if value, err := dhashFile(thumb, false); err == nil {
			return value, nil
		} else {
			m.log.Warn("用缩略图计算指纹失败，回退原图", "hash", a.Hash, "err", err)
		}
	}
	return dhashFile(m.paths.BlobPath(a.RelPath), true)
}

func (m *Manager) finish(r *run, status, errMsg string, res *Result) {
	now := time.Now()
	if res != nil {
		res.DurationMS = now.Sub(r.startedAt).Milliseconds()
		res.FinishedAt = now.Format(store.TimeLayout)
	}
	r.mu.Lock()
	r.status = status
	r.errMsg = errMsg
	r.finishedAt = &now
	r.result = res
	r.mu.Unlock()
	close(r.done)
}

// run 是一次检测任务的内存状态。
type run struct {
	id        int64
	opts      Options
	maxGroups int
	startedAt time.Time
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}

	mu         sync.Mutex
	status     string
	total      int
	processed  int
	groups     int
	failures   int
	cached     int
	computed   int
	current    string
	finishedAt *time.Time
	errMsg     string
	result     *Result
}

func (r *run) setTotal(n int) {
	r.mu.Lock()
	r.total = n
	r.mu.Unlock()
}

func (r *run) setCurrent(name string) {
	r.mu.Lock()
	r.current = name
	r.mu.Unlock()
}

func (r *run) setProcessed(n int) {
	r.mu.Lock()
	r.processed = n
	r.mu.Unlock()
}

func (r *run) addGroup() {
	r.mu.Lock()
	r.groups++
	r.mu.Unlock()
}

func (r *run) addFailure() {
	r.mu.Lock()
	r.failures++
	r.mu.Unlock()
}

func (r *run) addCached() {
	r.mu.Lock()
	r.cached++
	r.mu.Unlock()
}

func (r *run) addComputed() {
	r.mu.Lock()
	r.computed++
	r.mu.Unlock()
}

func (r *run) snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	var finished *string
	if r.finishedAt != nil {
		s := r.finishedAt.Format(store.TimeLayout)
		finished = &s
	}
	return Snapshot{
		ID:         r.id,
		Status:     r.status,
		Total:      r.total,
		Processed:  r.processed,
		Groups:     r.groups,
		Failures:   r.failures,
		Cached:     r.cached,
		Computed:   r.computed,
		Current:    r.current,
		Threshold:  r.opts.Threshold,
		Algorithm:  r.opts.Algorithm,
		From:       r.opts.From,
		StartedAt:  r.startedAt.Format(store.TimeLayout),
		FinishedAt: finished,
		Error:      r.errMsg,
	}
}

func intPtr(v int) *int { return &v }
