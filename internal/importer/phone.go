package importer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"iphoto/internal/media"
	"iphoto/internal/phonedock"
	"iphoto/internal/store"
	"iphoto/internal/timeparse"
)

// ErrPhoneInput 表示手机地址 / Token / mode 不合法，属于用户输入错误。
var ErrPhoneInput = errors.New("手机地址或 Token 不合法")

// 手机同步模式。
const (
	// PhoneModeIncremental 从上次 checkpoint 增量拉取（新设备等同于全量）。
	PhoneModeIncremental = "incremental"
	// PhoneModeFull 从头列出全部媒体；账本命中的不重复下载，失败项自动重试。
	PhoneModeFull = "full"
)

const (
	// phonePageSize 是元数据分页大小（PhoneDock 上限 1000）。
	phonePageSize = 500
	// phoneMaxItems 是单轮同步的媒体数量上限，防止异常数据把内存撑爆。
	phoneMaxItems = 100000
	// phoneRetryLimit 是每轮自动重试的失败项上限。
	phoneRetryLimit = 500
)

// PhoneOptions 是一次手机同步的输入。
type PhoneOptions struct {
	Address string `json:"address"`
	// Token 为空时复用该地址上次保存的 Token（老设备一键同步）。
	Token string   `json:"token"`
	Mode  string   `json:"mode"`
	Tags  []string `json:"tags"`
}

// PhoneDeviceInfo 是 probe 返回给页面的设备信息。
type PhoneDeviceInfo struct {
	Name           string `json:"name"`
	Model          string `json:"model"`
	Manufacturer   string `json:"manufacturer"`
	AndroidVersion string `json:"android_version"`
	BatteryLevel   int    `json:"battery_level"`
	Charging       bool   `json:"charging"`
	IP             string `json:"ip"`
	Port           int    `json:"port"`
}

// PhoneKindStat 是某一类媒体（图片/视频）的数量与待同步估算。
type PhoneKindStat struct {
	Total   int `json:"total"`
	Pending int `json:"pending"`
}

// PhoneProbe 是「检查连接」的结果。
type PhoneProbe struct {
	Address        string          `json:"address"`
	Device         PhoneDeviceInfo `json:"device"`
	Known          bool            `json:"known"`
	DeviceID       int64           `json:"device_id,omitempty"`
	AddressChanged bool            `json:"address_changed"`
	LastSyncAt     *string         `json:"last_sync_at"`
	LastCheckpoint int64           `json:"last_checkpoint"`
	Version        int64           `json:"version"`
	Images         PhoneKindStat   `json:"images"`
	Videos         PhoneKindStat   `json:"videos"`
}

// phoneSession 是一次已建立连接的上下文。
type phoneSession struct {
	client  *phonedock.Client
	info    phonedock.DeviceInfo
	token   string
	address string
	name    string
	known   store.PhoneDevice // 已登记设备；零值表示新设备
}

func (s phoneSession) deviceKey() string {
	return store.PhoneDeviceKey(s.info.Manufacturer, s.info.Model)
}

// ProbePhone 检查手机连通性、鉴权、相册权限与待同步数量，不写入任何数据。
func ProbePhone(ctx context.Context, st *store.Store, address, token string) (PhoneProbe, error) {
	sess, err := openPhoneSession(ctx, st, address, token)
	if err != nil {
		return PhoneProbe{}, err
	}

	probeCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	version, err := sess.client.Version(probeCtx)
	if err != nil {
		return PhoneProbe{}, err
	}
	images, err := sess.pending(probeCtx, phonedock.KindImage)
	if err != nil {
		return PhoneProbe{}, err
	}
	videos, err := sess.pending(probeCtx, phonedock.KindVideo)
	if err != nil {
		return PhoneProbe{}, err
	}

	return PhoneProbe{
		Address:        sess.address,
		Device:         deviceInfoOf(sess.info, sess.name),
		Known:          sess.known.ID != 0,
		DeviceID:       sess.known.ID,
		AddressChanged: sess.known.ID != 0 && sess.known.Address != sess.address,
		LastSyncAt:     sess.known.LastSyncAt,
		LastCheckpoint: sess.known.LastCheckpoint,
		Version:        version,
		Images:         images,
		Videos:         videos,
	}, nil
}

func deviceInfoOf(info phonedock.DeviceInfo, name string) PhoneDeviceInfo {
	return PhoneDeviceInfo{
		Name:           name,
		Model:          info.Model,
		Manufacturer:   info.Manufacturer,
		AndroidVersion: info.AndroidVersion,
		BatteryLevel:   info.BatteryLevel,
		Charging:       info.BatteryCharging,
		IP:             info.IP,
		Port:           info.Port,
	}
}

func phoneDisplayName(info phonedock.DeviceInfo) string {
	parts := make([]string, 0, 2)
	if strings.TrimSpace(info.Manufacturer) != "" {
		parts = append(parts, strings.TrimSpace(info.Manufacturer))
	}
	if strings.TrimSpace(info.Model) != "" {
		parts = append(parts, strings.TrimSpace(info.Model))
	}
	name := strings.TrimSpace(strings.Join(parts, " "))
	if name == "" {
		name = "未知手机"
	}
	return name
}

// openPhoneSession 规范化地址、解析 Token（可为空=复用已保存）、探测设备信息。
func openPhoneSession(ctx context.Context, st *store.Store, address, token string) (phoneSession, error) {
	base, err := phonedock.NormalizeBaseURL(address)
	if err != nil {
		return phoneSession{}, fmt.Errorf("%w: %v", ErrPhoneInput, err)
	}

	tok := strings.TrimSpace(token)
	if tok == "" {
		known, err := st.PhoneDeviceByAddress(ctx, base)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return phoneSession{}, fmt.Errorf("%w: 第一次连接请填写手机 PhoneDock 上显示的 4 位 Token", ErrPhoneInput)
			}
			return phoneSession{}, err
		}
		if tok, err = st.PhoneDeviceToken(ctx, known.ID); err != nil {
			return phoneSession{}, err
		}
	}

	client, err := phonedock.New(base, tok)
	if err != nil {
		return phoneSession{}, fmt.Errorf("%w: %v", ErrPhoneInput, err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	info, err := client.DeviceInfo(probeCtx)
	if err != nil {
		return phoneSession{}, err
	}
	sess := phoneSession{
		client:  client,
		info:    info,
		token:   tok,
		address: base,
		name:    phoneDisplayName(info),
	}
	sess.known, err = st.PhoneDeviceByKey(ctx, sess.deviceKey())
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return phoneSession{}, err
	}
	return sess, nil
}

// pending 计算某类媒体「手机共有多少 / 本次待同步多少」。
func (s phoneSession) pending(ctx context.Context, kind string) (PhoneKindStat, error) {
	full, err := s.client.MediaPage(ctx, kind, 0, 0, 1)
	if err != nil {
		return PhoneKindStat{}, err
	}
	if s.known.ID == 0 || s.known.LastCheckpoint <= 0 {
		return PhoneKindStat{Total: full.Total, Pending: full.Total}, nil
	}
	since, err := s.client.MediaPage(ctx, kind, s.known.LastCheckpoint, 0, 1)
	if err != nil {
		return PhoneKindStat{}, err
	}
	return PhoneKindStat{Total: full.Total, Pending: since.Total}, nil
}

// StartPhone 探测手机、登记设备并异步执行同步；返回批次 ID。
// 地址、Token、相册权限有问题时在创建批次前就返回错误，避免留下空批次。
func (m *Manager) StartPhone(opts PhoneOptions) (int64, error) {
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "" {
		mode = PhoneModeIncremental
	}
	if mode != PhoneModeIncremental && mode != PhoneModeFull {
		return 0, fmt.Errorf("%w: mode 只能是 incremental 或 full", ErrPhoneInput)
	}

	sess, err := openPhoneSession(context.Background(), m.store, opts.Address, opts.Token)
	if err != nil {
		return 0, err
	}

	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 25*time.Second)
	version, err := sess.client.Version(probeCtx)
	cancelProbe()
	if err != nil {
		return 0, err
	}

	now := time.Now().Format(store.TimeLayout)
	device, created, err := m.store.UpsertPhoneDevice(context.Background(), store.PhoneDevice{
		DeviceKey:      sess.deviceKey(),
		Name:           sess.name,
		Model:          sess.info.Model,
		Manufacturer:   sess.info.Manufacturer,
		AndroidVersion: sess.info.AndroidVersion,
		Address:        sess.address,
		Token:          sess.token,
		CreatedAt:      now,
		LastSeenAt:     now,
	})
	if err != nil {
		return 0, err
	}
	if created {
		m.log.Info("登记新手机设备", "device", device.ID, "name", device.Name, "address", sess.address)
	} else if device.Address != sess.address {
		m.log.Info("手机地址已更新", "device", device.ID, "name", device.Name,
			"from", device.Address, "to", sess.address)
	}

	since := device.LastCheckpoint
	if mode == PhoneModeFull || device.LastCheckpoint == 0 {
		since = 0
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
	r := &run{
		startedAt: time.Now(), ctx: ctx, cancel: cancel, done: make(chan struct{}),
		status: "running", source: "phone", phase: "listing", tags: cleanTags(opts.Tags),
	}
	r.setPhoneMeta(device.ID, device.Name, version)
	batch, err := m.store.CreateBatchWithSource(ctx, "phone://"+device.Name, now, "phone", &device.ID)
	if err != nil {
		cancel()
		m.mu.Unlock()
		return 0, err
	}
	r.id = batch.ID
	m.active = r
	m.mu.Unlock()

	m.log.Info("开始手机同步", "batch", r.id, "device", device.Name, "mode", mode,
		"since", since, "version", version)
	go m.executePhone(r, sess, device, since, version)
	return r.id, nil
}

// executePhone 是手机同步的主流程。
func (m *Manager) executePhone(r *run, sess phoneSession, device store.PhoneDevice, since, version int64) {
	status := "done"
	listedAll := false
	defer func() {
		m.finishBatch(r, status)
		// 只有完整读完两种 kind 的元数据才推进 checkpoint：
		// 单项失败不阻塞（由账本自动重试），取消/中断绝不推进。
		var checkpoint *int64
		if status == "done" && listedAll {
			v := version
			checkpoint = &v
		}
		m.setPhoneSyncResult(r, device.ID, checkpoint)
	}()

	ledger, err := m.store.ListPhoneMedia(r.ctx, device.ID)
	if err != nil {
		m.log.Error("读取手机同步账本失败", "device", device.ID, "err", err)
		status = phoneFailureStatus(r)
		return
	}
	index := make(map[string]store.PhoneMedia, len(ledger))
	for _, entry := range ledger {
		index[store.PhoneMediaKey(entry.Kind, entry.MediaID)] = entry
	}

	r.setPhase("listing")
	images, err := m.listPhoneKind(r, sess.client, phonedock.KindImage, since)
	if err != nil {
		m.log.Error("读取手机图片列表失败", "device", device.ID, "err", err)
		status = phoneFailureStatus(r)
		return
	}
	videos, err := m.listPhoneKind(r, sess.client, phonedock.KindVideo, since)
	if err != nil {
		m.log.Error("读取手机视频列表失败", "device", device.ID, "err", err)
		status = phoneFailureStatus(r)
		return
	}
	listedAll = true
	r.setCurrent("")

	// 元数据清单 + 上一轮失败项（按 key 去重，元数据里的最新信息优先）。
	items := make([]phonedock.MediaItem, 0, len(images)+len(videos))
	items = append(items, images...)
	items = append(items, videos...)
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		seen[store.PhoneMediaKey(item.Kind, item.ID)] = struct{}{}
	}
	retries, err := m.store.ListFailedPhoneMedia(r.ctx, device.ID, phoneRetryLimit)
	if err != nil {
		m.log.Warn("读取待重试媒体失败", "device", device.ID, "err", err)
	}
	for _, retry := range retries {
		if _, ok := seen[store.PhoneMediaKey(retry.Kind, retry.MediaID)]; ok {
			continue
		}
		items = append(items, mediaItemFromLedger(retry))
	}

	// 开发/验收用的小规模开关：图片与视频各只取前 N 项，且不推进 checkpoint，
	// 避免把被截断的记录误判为已同步。
	if limit := phoneItemLimit(); limit > 0 && (len(images) > limit || len(videos) > limit) {
		if len(images) > limit {
			images = images[:limit]
		}
		if len(videos) > limit {
			videos = videos[:limit]
		}
		m.log.Warn("IPHOTO_PHONE_ITEM_LIMIT 生效：每类只同步前 N 项且不推进进度",
			"limit", limit, "images", len(images), "videos", len(videos))
		listedAll = false
		items = make([]phonedock.MediaItem, 0, len(images)+len(videos))
		items = append(items, images...)
		items = append(items, videos...)
	}

	// 规划：下载 / 跳过（账本命中且资产仍在库）/ 类型不支持。
	work := make([]phoneWorkItem, 0, len(items))
	unsupported := newUnsupportedTracker()
	var bytesTotal int64
	for _, item := range items {
		action := m.planPhoneItem(r, item, index)
		switch action {
		case phoneActionUnsupported:
			unsupported.add(phoneSourcePath(device.Name, item))
			m.upsertPhoneLedger(device.ID, item, nil, store.PhoneMediaUnsupported, "类型不支持")
		case phoneActionUnchanged:
			work = append(work, phoneWorkItem{item: item})
		case phoneActionDownload:
			work = append(work, phoneWorkItem{item: item, download: true})
			bytesTotal += item.Size
		}
	}
	r.setUnsupported(unsupported.result())
	r.setTotal(int64(len(work)))
	r.setBytesTotal(bytesTotal)
	r.setPhase("downloading")

	rules := m.loadTimeRules(r.ctx)
	for _, w := range work {
		if r.ctx.Err() != nil {
			status = "cancelled"
			return
		}
		if !w.download {
			r.addUnchanged()
			continue
		}
		r.setCurrent(w.item.DisplayName)
		m.syncPhoneItem(r, sess, device, w.item, rules)
	}
	r.setCurrent("")
	if r.ctx.Err() != nil {
		status = "cancelled"
	}

	if tags := r.tagList(); len(tags) > 0 {
		if hashes := r.importedHashes(); len(hashes) > 0 {
			if err := m.store.AddTagsToAssets(context.Background(), hashes, tags); err != nil {
				m.log.Error("手机同步后添加标签失败", "batch", r.id, "err", err)
			}
		}
	}
}

// setPhoneSyncResult 记录本轮结果；checkpoint 为 nil 表示不推进进度。
func (m *Manager) setPhoneSyncResult(r *run, deviceID int64, checkpoint *int64) {
	status := r.snapshot().Status
	ctx := context.Background()
	if err := m.store.SetPhoneSyncResult(ctx, deviceID, checkpoint, status, r.id,
		time.Now().Format(store.TimeLayout)); err != nil {
		m.log.Error("记录手机同步结果失败", "device", deviceID, "err", err)
	}
}

// listPhoneKind 翻完某一类媒体的全部分页（上限 phoneMaxItems）。
func (m *Manager) listPhoneKind(r *run, client *phonedock.Client, kind string, since int64) ([]phonedock.MediaItem, error) {
	out := make([]phonedock.MediaItem, 0, 128)
	offset := 0
	for {
		page, err := client.MediaPage(r.ctx, kind, since, offset, phonePageSize)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Items...)
		offset += len(page.Items)
		r.setCurrent(fmt.Sprintf("读取%s列表 %d/%d…", phoneKindLabel(kind), offset, page.Total))
		if len(page.Items) == 0 || offset >= page.Total {
			break
		}
		if len(out) > phoneMaxItems {
			return nil, fmt.Errorf("手机媒体数量超过 %d，暂不支持一次性同步", phoneMaxItems)
		}
	}
	return out, nil
}

// phoneFailureStatus 把失败归因为「用户取消」或「真失败」。
func phoneFailureStatus(r *run) string {
	if r.ctx.Err() != nil {
		return "cancelled"
	}
	return "failed"
}

// phoneItemLimit 读取开发/验收用的「每类媒体」单轮上限（环境变量 IPHOTO_PHONE_ITEM_LIMIT）。
func phoneItemLimit() int {
	raw := strings.TrimSpace(os.Getenv("IPHOTO_PHONE_ITEM_LIMIT"))
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0
	}
	return limit
}

func phoneKindLabel(kind string) string {
	if kind == phonedock.KindVideo {
		return "视频"
	}
	return "图片"
}

// phoneAction 是单个媒体在本轮同步中的处理方式。
type phoneAction int

const (
	// phoneActionDownload 需要下载并入库（或重试）。
	phoneActionDownload phoneAction = iota
	// phoneActionUnchanged 账本命中且库内资产仍在，跳过且不下载。
	phoneActionUnchanged
	// phoneActionUnsupported 类型不受支持，只记账本与统计。
	phoneActionUnsupported
)

type phoneWorkItem struct {
	item     phonedock.MediaItem
	download bool
}

// planPhoneItem 依据账本决定单个媒体的处理方式。
func (m *Manager) planPhoneItem(r *run, item phonedock.MediaItem, index map[string]store.PhoneMedia) phoneAction {
	entry, ok := index[store.PhoneMediaKey(item.Kind, item.ID)]

	if !media.IsSupported(item.DisplayName) {
		if ok && entry.GenerationModified == item.GenerationModified &&
			(entry.Status == store.PhoneMediaUnsupported || entry.Status == store.PhoneMediaSynced) {
			return phoneActionUnchanged
		}
		return phoneActionUnsupported
	}

	if !ok || entry.GenerationModified != item.GenerationModified {
		// 新记录，或手机侧内容已变：都要重新下载。
		return phoneActionDownload
	}

	switch entry.Status {
	case store.PhoneMediaSynced:
		if entry.Hash == nil {
			return phoneActionDownload
		}
		if m.assetStillPresent(r.ctx, *entry.Hash) {
			return phoneActionUnchanged
		}
		// 库内资产已被删除（回收站清空等），重新拉一份。
		return phoneActionDownload
	case store.PhoneMediaMissing:
		// 手机侧此前已报 3001，generation 未变就不再反复请求。
		return phoneActionUnchanged
	default:
		return phoneActionDownload
	}
}

// assetStillPresent 判断哈希对应的资产是否还值得跳过（ready 或回收站中的同样算「库里有」）。
func (m *Manager) assetStillPresent(ctx context.Context, hash string) bool {
	asset, err := m.store.GetAsset(ctx, hash)
	if err != nil {
		return false
	}
	return asset.Status == "ready" || asset.Status == "trashed"
}

// syncPhoneItem 下载单个媒体并入库，然后更新账本。
func (m *Manager) syncPhoneItem(r *run, sess phoneSession, device store.PhoneDevice,
	item phonedock.MediaItem, rules []timeparse.Rule) {

	source := phoneSourcePath(device.Name, item)
	tmpPath := filepath.Join(m.paths.Temp, fmt.Sprintf("phone-%d-%s-%d.part",
		device.ID, item.Kind, item.ID))

	hash, size, err := downloadPhoneMedia(r, sess.client, item, tmpPath)
	if err != nil {
		if rmErr := os.Remove(tmpPath); rmErr != nil && !os.IsNotExist(rmErr) {
			m.log.Warn("清理手机临时文件失败", "path", tmpPath, "err", rmErr)
		}
		status, message := store.PhoneMediaFailed, phonedock.UserMessage(err)
		if r.ctx.Err() != nil {
			message = "已取消"
		} else if phonedock.IsMediaGone(err) {
			status, message = store.PhoneMediaMissing, "手机上的该媒体已不可用"
		}
		m.upsertPhoneLedger(device.ID, item, nil, status, message)
		m.record(r, source, "", "failed", message)
		return
	}
	r.addBytes(size)

	name := item.DisplayName
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("%s-%d", item.Kind, item.ID)
	}
	var modTime time.Time
	if item.DateAdded > 0 {
		modTime = time.Unix(item.DateAdded, 0)
	}
	meta := assetMeta{ModTime: modTime, Width: item.Width, Height: item.Height}
	commit := func(relPath string) (int64, error) {
		return AdoptFile(m.paths.Blobs, relPath, tmpPath, size)
	}
	result := m.ingest(r, source, name, hash, size, meta, dupAutoSkip, commit, rules)

	if rmErr := os.Remove(tmpPath); rmErr != nil && !os.IsNotExist(rmErr) {
		m.log.Warn("清理手机临时文件失败", "path", tmpPath, "err", rmErr)
	}

	switch result.Status {
	case "new", "duplicate_skip":
		m.upsertPhoneLedger(device.ID, item, &hash, store.PhoneMediaSynced, result.Message)
	case "failed":
		m.upsertPhoneLedger(device.ID, item, &hash, store.PhoneMediaFailed, result.Message)
	default:
		m.upsertPhoneLedger(device.ID, item, &hash, store.PhoneMediaFailed, result.Message)
	}
}

// downloadPhoneMedia 流式下载到临时文件并同步计算 SHA-256，避免二次读盘。
func downloadPhoneMedia(r *run, client *phonedock.Client, item phonedock.MediaItem, tmpPath string) (string, int64, error) {
	if err := os.MkdirAll(filepath.Dir(tmpPath), 0o755); err != nil {
		return "", 0, fmt.Errorf("创建临时目录: %w", err)
	}
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", 0, fmt.Errorf("创建临时文件: %w", err)
	}

	hasher := sha256.New()
	written, downloadErr := client.Content(r.ctx, item.Kind, item.ID, io.MultiWriter(f, hasher))
	closeErr := f.Close()
	if downloadErr != nil {
		return "", 0, downloadErr
	}
	if closeErr != nil {
		return "", 0, fmt.Errorf("写入临时文件: %w", closeErr)
	}
	if written != item.Size {
		return "", 0, fmt.Errorf("下载大小与手机元数据不一致：元数据 %d 字节，实际 %d 字节", item.Size, written)
	}
	return hex.EncodeToString(hasher.Sum(nil)), written, nil
}

// upsertPhoneLedger 写一条账本记录；hash 为 nil 时保留旧值。
func (m *Manager) upsertPhoneLedger(deviceID int64, item phonedock.MediaItem,
	hash *string, status, message string) {

	var width, height *int
	if item.Width > 0 {
		w := item.Width
		width = &w
	}
	if item.Height > 0 {
		h := item.Height
		height = &h
	}
	var msg *string
	if strings.TrimSpace(message) != "" {
		msg = &message
	}

	err := m.store.UpsertPhoneMedia(context.Background(), store.PhoneMedia{
		DeviceID:           deviceID,
		Kind:               item.Kind,
		MediaID:            item.ID,
		DisplayName:        item.DisplayName,
		GenerationModified: item.GenerationModified,
		Size:               item.Size,
		DateAdded:          item.DateAdded,
		Width:              width,
		Height:             height,
		Hash:               hash,
		Status:             status,
		Message:            msg,
		UpdatedAt:          time.Now().Format(store.TimeLayout),
	})
	if err != nil {
		m.log.Error("写入手机同步账本失败", "media", item.ID, "kind", item.Kind, "err", err)
	}
}

// mediaItemFromLedger 把账本条目还原成元数据项（用于失败重试）。
func mediaItemFromLedger(entry store.PhoneMedia) phonedock.MediaItem {
	item := phonedock.MediaItem{
		ID:                 entry.MediaID,
		Kind:               entry.Kind,
		DisplayName:        entry.DisplayName,
		Size:               entry.Size,
		DateAdded:          entry.DateAdded,
		GenerationModified: entry.GenerationModified,
	}
	if entry.Width != nil {
		item.Width = *entry.Width
	}
	if entry.Height != nil {
		item.Height = *entry.Height
	}
	return item
}

// phoneSourcePath 生成可读的合成路径，用于 import_items 与 CSV 报告：
// phone://Xiaomi 22101317C/image/1000112680/IMG_20261127_153320.jpg
func phoneSourcePath(deviceName string, item phonedock.MediaItem) string {
	name := strings.NewReplacer("/", "_", "\\", "_").Replace(item.DisplayName)
	if name == "" {
		name = item.Kind + "-" + strconv.FormatInt(item.ID, 10)
	}
	return "phone://" + deviceName + "/" + item.Kind + "/" + strconv.FormatInt(item.ID, 10) + "/" + name
}
