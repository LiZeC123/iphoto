package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"iphoto/internal/config"
	"iphoto/internal/phonedock"
	"iphoto/internal/store"
	"iphoto/internal/thumbs"
)

// fakePhone 是一个最小的 PhoneDock 假服务器，只实现同步用到的 4 个接口。
type fakePhone struct {
	token string

	mu            sync.Mutex
	version       int64
	items         map[string][]phonedock.MediaItem
	content       map[string][]byte
	broken        map[string]bool
	gone          map[string]bool
	downloads     int
	blockContent  bool
	contentStart  chan struct{}
	contentCloser sync.Once
	pageRequests  int
}

func newFakePhone(t *testing.T, token string) *fakePhone {
	t.Helper()
	return &fakePhone{
		token:        token,
		items:        map[string][]phonedock.MediaItem{},
		content:      map[string][]byte{},
		broken:       map[string]bool{},
		gone:         map[string]bool{},
		contentStart: make(chan struct{}),
	}
}

func mediaKey(kind string, id int64) string { return fmt.Sprintf("%s/%d", kind, id) }

// add 往手机相册里放一张媒体，返回元数据。
func (f *fakePhone) add(kind string, id int64, name string, data []byte, gen, dateAdded int64) phonedock.MediaItem {
	f.mu.Lock()
	defer f.mu.Unlock()
	item := phonedock.MediaItem{
		ID: id, Kind: kind, DisplayName: name, MimeType: "image/png",
		Size: int64(len(data)), DateAdded: dateAdded, DateModified: dateAdded,
		GenerationModified: gen, Width: 8, Height: 8,
	}
	if kind == phonedock.KindVideo {
		item.MimeType = "video/mp4"
		item.Width, item.Height = 1920, 1080
	}
	f.items[kind] = append(f.items[kind], item)
	f.content[mediaKey(kind, id)] = data
	if gen > f.version {
		f.version = gen
	}
	return item
}

func (f *fakePhone) downloadCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.downloads
}

func (f *fakePhone) setBlock(block bool) {
	f.mu.Lock()
	f.blockContent = block
	f.mu.Unlock()
}

func (f *fakePhone) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"code": 2002, "message": "token 错误", "data": nil})
			return
		}
		switch {
		case r.URL.Path == "/v1/device/info":
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": "ok", "data": map[string]any{
				"model": "22101317C", "manufacturer": "Xiaomi", "androidVersion": "13",
				"batteryLevel": 83, "batteryCharging": false,
				"ip": "192.168.1.215", "port": 8765, "uptimeSeconds": 100,
			}})
		case r.URL.Path == "/v1/album/version":
			f.mu.Lock()
			v := f.version
			f.mu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": "ok",
				"data": map[string]any{"version": v}})
		case r.URL.Path == "/v1/album/media":
			f.servePage(w, r)
		case strings.HasPrefix(r.URL.Path, "/v1/album/media/") && strings.HasSuffix(r.URL.Path, "/content"):
			f.serveContent(w, r)
		default:
			writeJSON(w, http.StatusOK, map[string]any{"code": 1004, "message": "unknown path", "data": nil})
		}
	}
}

func (f *fakePhone) servePage(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.pageRequests++
	kind := r.URL.Query().Get("kind")
	since := int64(0)
	if v := r.URL.Query().Get("sinceGenerationModified"); v != "" {
		fmt.Sscan(v, &since)
	}
	offset, limit := 0, 500
	if v := r.URL.Query().Get("offset"); v != "" {
		fmt.Sscan(v, &offset)
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscan(v, &limit)
	}
	all := make([]phonedock.MediaItem, 0, len(f.items[kind]))
	for _, item := range f.items[kind] {
		if since > 0 && item.GenerationModified <= since {
			continue
		}
		all = append(all, item)
	}
	f.mu.Unlock()

	total := len(all)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": "ok", "data": map[string]any{
		"total": total, "offset": offset, "limit": limit, "items": all[offset:end],
	}})
}

func (f *fakePhone) serveContent(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimSuffix(r.URL.Path, "/content"), "/")
	key := mediaKey(r.URL.Query().Get("kind"), mustParseInt(parts[len(parts)-1]))

	f.mu.Lock()
	block := f.blockContent
	broken := f.broken[key]
	gone := f.gone[key]
	data := f.content[key]
	if !gone {
		f.downloads++
	}
	f.mu.Unlock()

	if block {
		f.contentCloser.Do(func() { close(f.contentStart) })
		<-r.Context().Done()
		return
	}
	if gone {
		writeJSON(w, http.StatusOK, map[string]any{"code": 3001, "message": "媒体不存在或已被删除", "data": nil})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	if broken {
		// 声明比实际更多的字节，模拟传输中断。
		w.Header().Set("Content-Length", fmt.Sprint(len(data)+10))
	} else {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func mustParseInt(s string) int64 {
	var v int64
	fmt.Sscan(s, &v)
	return v
}

func pngBytes(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("编码 PNG: %v", err)
	}
	return buf.Bytes()
}

func newPhoneTestManager(t *testing.T) (*Manager, *store.Store, config.Paths) {
	t.Helper()
	paths := testPaths(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	st, err := store.Open(paths.DB)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := thumbs.New(paths, logger, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	q.Start(ctx)

	t.Cleanup(func() {
		cancel()
		q.Wait()
	})

	m := NewManager(st, paths, q, logger)
	m.Workers = 1
	return m, st, paths
}

func newPhoneServer(t *testing.T, fp *fakePhone) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(fp.handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestPhoneSyncFullThenIncremental(t *testing.T) {
	fp := newFakePhone(t, "2179")
	srv := newPhoneServer(t, fp)

	imgA := fp.add(phonedock.KindImage, 1, "a.png", pngBytes(t, color.RGBA{R: 10, A: 255}), 100, 1700000000)
	fp.add(phonedock.KindImage, 2, "b.png", pngBytes(t, color.RGBA{G: 20, A: 255}), 101, 1700000100)
	fp.add(phonedock.KindVideo, 3, "v.mp4", []byte("fake-mp4-bytes"), 102, 1700000200)

	m, st, _ := newPhoneTestManager(t)
	ctx := context.Background()

	batchID, err := m.StartPhone(PhoneOptions{Address: srv.URL, Token: "2179"})
	if err != nil {
		t.Fatalf("StartPhone: %v", err)
	}
	snap := waitDone(t, m, batchID)
	if snap.Status != "done" {
		t.Fatalf("status = %s", snap.Status)
	}
	if snap.Source != "phone" || snap.DeviceName != "Xiaomi 22101317C" {
		t.Fatalf("来源/设备名不符: %+v", snap)
	}
	if snap.Total != 3 || snap.Processed != 3 || snap.Imported != 3 || snap.Failed != 0 {
		t.Fatalf("统计不符: %+v", snap)
	}
	if snap.BytesTotal != int64(len(pngBytes(t, color.RGBA{R: 10, A: 255})))+
		int64(len(pngBytes(t, color.RGBA{G: 20, A: 255})))+int64(len("fake-mp4-bytes")) {
		t.Fatalf("bytes_total = %d", snap.BytesTotal)
	}
	if fp.downloadCount() != 3 {
		t.Fatalf("下载次数 = %d, want 3", fp.downloadCount())
	}

	assets, err := st.ListAssets(ctx, store.AssetFilter{})
	if err != nil {
		t.Fatalf("ListAssets: %v", err)
	}
	if assets.Total != 3 {
		t.Fatalf("库内资产 = %d, want 3", assets.Total)
	}
	// 无 EXIF 的图片应回退到手机 dateAdded。
	var imageAsset *store.Asset
	for i := range assets.Items {
		if assets.Items[i].OriginalName == "a.png" {
			imageAsset = &assets.Items[i]
		}
	}
	if imageAsset == nil {
		t.Fatal("未找到 a.png")
	}
	wantTime := time.Unix(imgA.DateAdded, 0).Format(store.TimeLayout)
	if imageAsset.TakenAt == nil || *imageAsset.TakenAt != wantTime {
		t.Fatalf("taken_at = %v, want %s", imageAsset.TakenAt, wantTime)
	}
	if imageAsset.TakenAtSource == nil || *imageAsset.TakenAtSource != "file_mtime" {
		t.Fatalf("taken_at_source = %v", imageAsset.TakenAtSource)
	}

	devices, err := st.ListPhoneDevices(ctx)
	if err != nil || len(devices) != 1 {
		t.Fatalf("设备列表 = %+v, %v", devices, err)
	}
	if devices[0].LastCheckpoint != 102 || devices[0].LastStatus == nil || *devices[0].LastStatus != "done" {
		t.Fatalf("设备进度不符: %+v", devices[0])
	}
	ledger, err := st.ListPhoneMedia(ctx, devices[0].ID)
	if err != nil || len(ledger) != 3 {
		t.Fatalf("账本 = %d 条, %v", len(ledger), err)
	}
	for _, entry := range ledger {
		if entry.Status != store.PhoneMediaSynced || entry.Hash == nil {
			t.Fatalf("账本条目未同步: %+v", entry)
		}
	}

	// 第二轮：无变化，用「不带 scheme 的地址 + 空 Token」验证复用已保存 Token。
	batch2, err := m.StartPhone(PhoneOptions{
		Address: strings.TrimPrefix(srv.URL, "http://"),
	})
	if err != nil {
		t.Fatalf("第二轮 StartPhone: %v", err)
	}
	snap2 := waitDone(t, m, batch2)
	if snap2.Status != "done" || snap2.Total != 0 || snap2.Imported != 0 {
		t.Fatalf("第二轮应无新增: %+v", snap2)
	}
	if fp.downloadCount() != 3 {
		t.Fatalf("第二轮不应下载，累计下载 = %d", fp.downloadCount())
	}

	// 第三轮：手机新增一张 → 只拉这一张。
	fp.add(phonedock.KindImage, 4, "c.png", pngBytes(t, color.RGBA{B: 30, A: 255}), 200, 1700000300)
	batch3, err := m.StartPhone(PhoneOptions{Address: srv.URL, Mode: PhoneModeIncremental})
	if err != nil {
		t.Fatalf("第三轮 StartPhone: %v", err)
	}
	snap3 := waitDone(t, m, batch3)
	if snap3.Status != "done" || snap3.Total != 1 || snap3.Imported != 1 {
		t.Fatalf("第三轮统计不符: %+v", snap3)
	}
	if fp.downloadCount() != 4 {
		t.Fatalf("第三轮下载次数 = %d, want 4", fp.downloadCount())
	}
	device, err := st.GetPhoneDevice(ctx, devices[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if device.LastCheckpoint != 200 {
		t.Fatalf("第三轮 checkpoint = %d, want 200", device.LastCheckpoint)
	}
}

func TestPhoneSyncAutoSkipsDuplicateContent(t *testing.T) {
	fp := newFakePhone(t, "2179")
	srv := newPhoneServer(t, fp)
	same := pngBytes(t, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	fp.add(phonedock.KindImage, 10, "one.png", same, 1, 1700000000)
	fp.add(phonedock.KindImage, 11, "two.png", same, 2, 1700000001)

	m, st, _ := newPhoneTestManager(t)
	batchID, err := m.StartPhone(PhoneOptions{Address: srv.URL, Token: "2179"})
	if err != nil {
		t.Fatalf("StartPhone: %v", err)
	}
	snap := waitDone(t, m, batchID)
	if snap.Imported != 1 || snap.DuplicateSkipped != 1 || snap.DuplicatePending != 0 || snap.Failed != 0 {
		t.Fatalf("统计不符: %+v", snap)
	}

	devices, err := st.ListPhoneDevices(context.Background())
	if err != nil || len(devices) != 1 {
		t.Fatalf("设备列表 = %+v, %v", devices, err)
	}
	ledger, err := st.ListPhoneMedia(context.Background(), devices[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range ledger {
		if entry.Status != store.PhoneMediaSynced || entry.Hash == nil {
			t.Fatalf("重复内容也应记账本为 synced: %+v", entry)
		}
	}

	// 全量重扫：全部账本命中，不重新下载。
	before := fp.downloadCount()
	batch2, err := m.StartPhone(PhoneOptions{Address: srv.URL, Mode: PhoneModeFull})
	if err != nil {
		t.Fatalf("全量重扫: %v", err)
	}
	snap2 := waitDone(t, m, batch2)
	if snap2.Total != 2 || snap2.Unchanged != 2 || snap2.Imported != 0 {
		t.Fatalf("全量重扫统计不符: %+v", snap2)
	}
	if fp.downloadCount() != before {
		t.Fatalf("全量重扫不应重新下载: %d → %d", before, fp.downloadCount())
	}
}

func TestPhoneUnsupportedNotDownloaded(t *testing.T) {
	fp := newFakePhone(t, "2179")
	srv := newPhoneServer(t, fp)
	fp.add(phonedock.KindImage, 20, "IMG_0001.DNG", []byte("dng-bytes"), 5, 1700000000)
	fp.add(phonedock.KindImage, 21, "ok.png", pngBytes(t, color.RGBA{R: 9, A: 255}), 6, 1700000001)

	m, st, _ := newPhoneTestManager(t)
	batchID, err := m.StartPhone(PhoneOptions{Address: srv.URL, Token: "2179"})
	if err != nil {
		t.Fatalf("StartPhone: %v", err)
	}
	snap := waitDone(t, m, batchID)
	if snap.Status != "done" {
		t.Fatalf("status = %s", snap.Status)
	}
	if snap.Unsupported.Total != 1 || snap.Total != 1 || snap.Imported != 1 {
		t.Fatalf("统计不符: %+v", snap)
	}
	if len(snap.Unsupported.ByExt) != 1 || snap.Unsupported.ByExt[0].Ext != ".dng" {
		t.Fatalf("后缀统计不符: %+v", snap.Unsupported)
	}
	if fp.downloadCount() != 1 {
		t.Fatalf("不支持的类型不应下载，下载次数 = %d", fp.downloadCount())
	}

	devices, err := st.ListPhoneDevices(context.Background())
	if err != nil || len(devices) != 1 {
		t.Fatalf("设备列表 = %+v, %v", devices, err)
	}
	ledger, err := st.ListPhoneMedia(context.Background(), devices[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[int64]string{}
	for _, entry := range ledger {
		statuses[entry.MediaID] = entry.Status
	}
	if statuses[20] != store.PhoneMediaUnsupported || statuses[21] != store.PhoneMediaSynced {
		t.Fatalf("账本状态不符: %+v", statuses)
	}
}

func TestPhoneMediaGoneAndFailedRetry(t *testing.T) {
	fp := newFakePhone(t, "2179")
	srv := newPhoneServer(t, fp)
	fp.add(phonedock.KindImage, 30, "gone.png", pngBytes(t, color.RGBA{R: 1, A: 255}), 1, 1700000000)
	fp.add(phonedock.KindImage, 31, "flaky.png", pngBytes(t, color.RGBA{R: 2, A: 255}), 2, 1700000001)
	fp.mu.Lock()
	fp.gone[mediaKey(phonedock.KindImage, 30)] = true
	fp.broken[mediaKey(phonedock.KindImage, 31)] = true
	fp.mu.Unlock()

	m, st, _ := newPhoneTestManager(t)
	ctx := context.Background()
	batchID, err := m.StartPhone(PhoneOptions{Address: srv.URL, Token: "2179"})
	if err != nil {
		t.Fatalf("StartPhone: %v", err)
	}
	snap := waitDone(t, m, batchID)
	if snap.Status != "done" {
		t.Fatalf("单项失败不应让整批失败: %+v", snap)
	}
	if snap.Failed != 2 || snap.Imported != 0 {
		t.Fatalf("统计不符: %+v", snap)
	}

	devices, err := st.ListPhoneDevices(ctx)
	if err != nil || len(devices) != 1 {
		t.Fatalf("设备列表 = %+v, %v", devices, err)
	}
	// 单项失败不阻塞 checkpoint（否则失败项会把进度永久卡住）。
	if devices[0].LastCheckpoint != 2 {
		t.Fatalf("checkpoint = %d, want 2", devices[0].LastCheckpoint)
	}
	ledger, err := st.ListPhoneMedia(ctx, devices[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[int64]string{}
	for _, entry := range ledger {
		statuses[entry.MediaID] = entry.Status
	}
	if statuses[30] != store.PhoneMediaMissing {
		t.Fatalf("3001 应记为 missing: %+v", statuses)
	}
	if statuses[31] != store.PhoneMediaFailed {
		t.Fatalf("传输中断应记为 failed: %+v", statuses)
	}

	// 修好传输问题后再同步：失败项从账本重试，缺失项不再请求。
	fp.mu.Lock()
	fp.broken = map[string]bool{}
	fp.mu.Unlock()
	batch2, err := m.StartPhone(PhoneOptions{Address: srv.URL})
	if err != nil {
		t.Fatalf("重试 StartPhone: %v", err)
	}
	snap2 := waitDone(t, m, batch2)
	if snap2.Status != "done" || snap2.Imported != 1 || snap2.Total != 1 {
		t.Fatalf("重试统计不符: %+v", snap2)
	}
	ledger, err = st.ListPhoneMedia(ctx, devices[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range ledger {
		if entry.MediaID == 31 && entry.Status != store.PhoneMediaSynced {
			t.Fatalf("重试后应同步成功: %+v", entry)
		}
		if entry.MediaID == 30 && entry.Status != store.PhoneMediaMissing {
			t.Fatalf("缺失项不应被重试: %+v", entry)
		}
	}
}

func TestPhoneCancelDoesNotAdvanceCheckpoint(t *testing.T) {
	fp := newFakePhone(t, "2179")
	srv := newPhoneServer(t, fp)
	fp.add(phonedock.KindImage, 40, "slow.png", pngBytes(t, color.RGBA{R: 4, A: 255}), 7, 1700000000)
	fp.setBlock(true)

	m, st, _ := newPhoneTestManager(t)
	batchID, err := m.StartPhone(PhoneOptions{Address: srv.URL, Token: "2179"})
	if err != nil {
		t.Fatalf("StartPhone: %v", err)
	}

	select {
	case <-fp.contentStart:
	case <-time.After(10 * time.Second):
		t.Fatal("等待下载开始超时")
	}
	if err := m.Cancel(batchID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	snap := waitDone(t, m, batchID)
	if snap.Status != "cancelled" {
		t.Fatalf("取消后 status = %s", snap.Status)
	}

	devices, err := st.ListPhoneDevices(context.Background())
	if err != nil || len(devices) != 1 {
		t.Fatalf("设备列表 = %+v, %v", devices, err)
	}
	if devices[0].LastCheckpoint != 0 {
		t.Fatalf("取消后不应推进 checkpoint，得到 %d", devices[0].LastCheckpoint)
	}
	if devices[0].LastStatus == nil || *devices[0].LastStatus != "cancelled" {
		t.Fatalf("last_status = %v", devices[0].LastStatus)
	}

	// 恢复后重新同步：checkpoint 仍是 0，会从头列全量并补齐。
	fp.setBlock(false)
	batch2, err := m.StartPhone(PhoneOptions{Address: srv.URL})
	if err != nil {
		t.Fatalf("恢复后 StartPhone: %v", err)
	}
	snap2 := waitDone(t, m, batch2)
	if snap2.Status != "done" || snap2.Imported != 1 {
		t.Fatalf("恢复后统计不符: %+v", snap2)
	}
}

func TestPhoneProbeReportsPending(t *testing.T) {
	fp := newFakePhone(t, "2179")
	srv := newPhoneServer(t, fp)
	fp.add(phonedock.KindImage, 50, "a.png", pngBytes(t, color.RGBA{R: 1, A: 255}), 10, 1700000000)
	fp.add(phonedock.KindImage, 51, "b.png", pngBytes(t, color.RGBA{R: 2, A: 255}), 11, 1700000001)
	fp.add(phonedock.KindVideo, 52, "v.mp4", []byte("v"), 12, 1700000002)

	m, st, _ := newPhoneTestManager(t)
	ctx := context.Background()

	probe, err := ProbePhone(ctx, st, srv.URL, "2179")
	if err != nil {
		t.Fatalf("ProbePhone: %v", err)
	}
	if probe.Known {
		t.Fatal("首次检查应为新设备")
	}
	if probe.Device.Name != "Xiaomi 22101317C" || probe.Device.AndroidVersion != "13" {
		t.Fatalf("设备信息不符: %+v", probe.Device)
	}
	if probe.Images.Total != 2 || probe.Images.Pending != 2 || probe.Videos.Total != 1 || probe.Videos.Pending != 1 {
		t.Fatalf("数量估算不符: %+v", probe)
	}
	// 检查连接不应写库。
	if devices, _ := st.ListPhoneDevices(ctx); len(devices) != 0 {
		t.Fatalf("probe 不应登记设备，得到 %d 条", len(devices))
	}

	batchID, err := m.StartPhone(PhoneOptions{Address: srv.URL, Token: "2179"})
	if err != nil {
		t.Fatalf("StartPhone: %v", err)
	}
	waitDone(t, m, batchID)

	probe2, err := ProbePhone(ctx, st, srv.URL, "")
	if err != nil {
		t.Fatalf("同步后 ProbePhone: %v", err)
	}
	if !probe2.Known || probe2.DeviceID == 0 {
		t.Fatalf("同步后应识别为已知设备: %+v", probe2)
	}
	if probe2.Images.Pending != 0 || probe2.Videos.Pending != 0 {
		t.Fatalf("同步后待同步数应为 0: %+v", probe2)
	}
	if probe2.LastCheckpoint != 12 {
		t.Fatalf("probe checkpoint = %d, want 12", probe2.LastCheckpoint)
	}
	if probe2.LastSyncAt == nil {
		t.Fatal("probe 应返回上次同步时间")
	}
}

func TestPhoneAddressChangeKeepsDevice(t *testing.T) {
	fp := newFakePhone(t, "2179")
	srvA := newPhoneServer(t, fp)
	fp.add(phonedock.KindImage, 60, "a.png", pngBytes(t, color.RGBA{R: 7, A: 255}), 5, 1700000000)

	m, st, _ := newPhoneTestManager(t)
	ctx := context.Background()
	batchID, err := m.StartPhone(PhoneOptions{Address: srvA.URL, Token: "2179"})
	if err != nil {
		t.Fatalf("StartPhone: %v", err)
	}
	waitDone(t, m, batchID)

	// 手机换了 IP（第二个监听地址）：仍应是同一台设备、增量续接。
	srvB := newPhoneServer(t, fp)
	batch2, err := m.StartPhone(PhoneOptions{Address: srvB.URL, Token: "2179"})
	if err != nil {
		t.Fatalf("换地址后 StartPhone: %v", err)
	}
	snap2 := waitDone(t, m, batch2)
	if snap2.Total != 0 || snap2.Imported != 0 {
		t.Fatalf("换地址不应全量重扫: %+v", snap2)
	}

	devices, err := st.ListPhoneDevices(ctx)
	if err != nil || len(devices) != 1 {
		t.Fatalf("换地址后设备数 = %d, %v", len(devices), err)
	}
	if devices[0].Address != srvB.URL {
		t.Fatalf("设备地址未更新: %q", devices[0].Address)
	}
}

func TestPhoneStartRejectsBadInput(t *testing.T) {
	fp := newFakePhone(t, "2179")
	srv := newPhoneServer(t, fp)
	fp.add(phonedock.KindImage, 70, "a.png", pngBytes(t, color.RGBA{R: 1, A: 255}), 1, 1700000000)

	m, st, _ := newPhoneTestManager(t)

	if _, err := m.StartPhone(PhoneOptions{Address: srv.URL, Token: "0000"}); err == nil {
		t.Fatal("错误 Token 应失败")
	} else if !strings.Contains(phonedock.UserMessage(err), "Token 不正确") {
		t.Fatalf("错误文案不符: %q", phonedock.UserMessage(err))
	}
	if devices, _ := st.ListPhoneDevices(context.Background()); len(devices) != 0 {
		t.Fatalf("失败不应登记设备，得到 %d 条", len(devices))
	}

	if _, err := m.StartPhone(PhoneOptions{Address: "http://192.168.1.215:8765/v1", Token: "2179"}); err == nil {
		t.Fatal("带路径的地址应失败")
	} else if !strings.Contains(err.Error(), "不要带路径") {
		t.Fatalf("错误文案不符: %v", err)
	}

	if _, err := m.StartPhone(PhoneOptions{Address: srv.URL, Token: "2179", Mode: "diff"}); err == nil {
		t.Fatal("非法 mode 应失败")
	}
}

// TestPhoneDeletedAssetNotReimported 固化「删除语义以 PC 为准」：
// 已同步的手机媒体即使库内资产被彻底删除，全量重扫也不应重新下载/导入；
// 只有「忘记设备」这个显式重置入口才会重新导入。
func TestPhoneDeletedAssetNotReimported(t *testing.T) {
	fp := newFakePhone(t, "2179")
	srv := newPhoneServer(t, fp)
	fp.add(phonedock.KindImage, 80, "keep.png", pngBytes(t, color.RGBA{R: 5, A: 255}), 1, 1700000000)

	m, st, _ := newPhoneTestManager(t)
	ctx := context.Background()

	batch1, err := m.StartPhone(PhoneOptions{Address: srv.URL, Token: "2179"})
	if err != nil {
		t.Fatalf("StartPhone: %v", err)
	}
	if snap := waitDone(t, m, batch1); snap.Imported != 1 {
		t.Fatalf("首轮统计不符: %+v", snap)
	}

	assets, err := st.ListAssets(ctx, store.AssetFilter{})
	if err != nil || assets.Total != 1 {
		t.Fatalf("首轮后资产 = %d, %v", assets.Total, err)
	}
	// 模拟「删除图片 + 清空回收站」：资产行被彻底删除。
	if err := st.DeleteAssetRow(ctx, assets.Items[0].Hash); err != nil {
		t.Fatalf("DeleteAssetRow: %v", err)
	}

	before := fp.downloadCount()
	batch2, err := m.StartPhone(PhoneOptions{Address: srv.URL, Mode: PhoneModeFull})
	if err != nil {
		t.Fatalf("全量重扫: %v", err)
	}
	snap2 := waitDone(t, m, batch2)
	if snap2.Status != "done" || snap2.Total != 1 || snap2.Unchanged != 1 || snap2.Imported != 0 {
		t.Fatalf("删除后全量重扫不应重新导入: %+v", snap2)
	}
	if fp.downloadCount() != before {
		t.Fatalf("删除后全量重扫不应重新下载: %d → %d", before, fp.downloadCount())
	}
	if assets, err = st.ListAssets(ctx, store.AssetFilter{}); err != nil || assets.Total != 0 {
		t.Fatalf("删除后资产应为 0，得到 %d, %v", assets.Total, err)
	}

	devices, err := st.ListPhoneDevices(ctx)
	if err != nil || len(devices) != 1 {
		t.Fatalf("设备列表 = %+v, %v", devices, err)
	}
	ledger, err := st.ListPhoneMedia(ctx, devices[0].ID)
	if err != nil || len(ledger) != 1 || ledger[0].Status != store.PhoneMediaSynced {
		t.Fatalf("账本应保留 synced 记录: %+v, %v", ledger, err)
	}

	// 「忘记设备」是显式重置：删除账本后重新全量，会重新导入。
	if err := st.DeletePhoneDevice(ctx, devices[0].ID); err != nil {
		t.Fatalf("DeletePhoneDevice: %v", err)
	}
	batch3, err := m.StartPhone(PhoneOptions{Address: srv.URL, Token: "2179"})
	if err != nil {
		t.Fatalf("重置后 StartPhone: %v", err)
	}
	snap3 := waitDone(t, m, batch3)
	if snap3.Status != "done" || snap3.Imported != 1 {
		t.Fatalf("忘记设备后应重新导入: %+v", snap3)
	}
}
