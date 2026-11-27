package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func newPhoneTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sampleDevice() PhoneDevice {
	return PhoneDevice{
		DeviceKey:      PhoneDeviceKey("Xiaomi", "22101317C"),
		Name:           "Xiaomi 22101317C",
		Model:          "22101317C",
		Manufacturer:   "Xiaomi",
		AndroidVersion: "13",
		Address:        "http://192.168.1.215:8765",
		Token:          "2179",
		CreatedAt:      "2026-11-27 22:00:00",
		LastSeenAt:     "2026-11-27 22:00:00",
	}
}

func TestPhoneDeviceKey(t *testing.T) {
	if got := PhoneDeviceKey(" Xiaomi ", "22101317C"); got != "xiaomi|22101317c" {
		t.Fatalf("PhoneDeviceKey = %q", got)
	}
}

func TestUpsertPhoneDevice(t *testing.T) {
	s := newPhoneTestStore(t)
	ctx := context.Background()

	device, created, err := s.UpsertPhoneDevice(ctx, sampleDevice())
	if err != nil {
		t.Fatalf("UpsertPhoneDevice: %v", err)
	}
	if !created || device.ID == 0 {
		t.Fatalf("首次登记应返回 created=true 且有 id: %+v created=%v", device, created)
	}
	if !device.HasToken || device.LastCheckpoint != 0 || device.LastSyncAt != nil {
		t.Fatalf("首次登记字段不符: %+v", device)
	}

	// 第二轮：地址变化（DHCP 换 IP）仍是同一台设备，进度保留。
	cp := int64(442415)
	if err := s.SetPhoneSyncResult(ctx, device.ID, &cp, "done", 7, "2026-11-27 22:30:00"); err != nil {
		t.Fatalf("SetPhoneSyncResult: %v", err)
	}

	again := sampleDevice()
	again.Address = "http://192.168.1.99:8765"
	updated, createdSecond, err := s.UpsertPhoneDevice(ctx, again)
	if err != nil {
		t.Fatalf("二次 UpsertPhoneDevice: %v", err)
	}
	if createdSecond {
		t.Fatal("同一 device_key 不应被当作新设备")
	}
	if updated.ID != device.ID {
		t.Fatalf("id 变化: %d → %d", device.ID, updated.ID)
	}
	if updated.Address != "http://192.168.1.99:8765" {
		t.Fatalf("地址未更新: %q", updated.Address)
	}
	if updated.LastCheckpoint != cp {
		t.Fatalf("第二轮登记不应覆盖 checkpoint，得到 %d", updated.LastCheckpoint)
	}

	byKey, err := s.PhoneDeviceByKey(ctx, PhoneDeviceKey("Xiaomi", "22101317C"))
	if err != nil || byKey.ID != device.ID {
		t.Fatalf("PhoneDeviceByKey = %+v, %v", byKey, err)
	}
	byAddr, err := s.PhoneDeviceByAddress(ctx, "http://192.168.1.99:8765")
	if err != nil || byAddr.ID != device.ID {
		t.Fatalf("PhoneDeviceByAddress = %+v, %v", byAddr, err)
	}

	token, err := s.PhoneDeviceToken(ctx, device.ID)
	if err != nil || token != "2179" {
		t.Fatalf("PhoneDeviceToken = %q, %v", token, err)
	}

	devices, err := s.ListPhoneDevices(ctx)
	if err != nil {
		t.Fatalf("ListPhoneDevices: %v", err)
	}
	if len(devices) != 1 || devices[0].ID != device.ID {
		t.Fatalf("ListPhoneDevices = %+v", devices)
	}
}

func TestUpsertPhoneDeviceRequiresToken(t *testing.T) {
	s := newPhoneTestStore(t)
	d := sampleDevice()
	d.Token = ""
	if _, _, err := s.UpsertPhoneDevice(context.Background(), d); err == nil {
		t.Fatal("缺少 token 应失败")
	}
}

func TestPhoneDeviceNotFound(t *testing.T) {
	s := newPhoneTestStore(t)
	ctx := context.Background()
	if _, err := s.PhoneDeviceByKey(ctx, "nokia|3310"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("PhoneDeviceByKey 期望 ErrNoRows，得到 %v", err)
	}
	if _, err := s.PhoneDeviceByAddress(ctx, "http://10.0.0.1:8765"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("PhoneDeviceByAddress 期望 ErrNoRows，得到 %v", err)
	}
}

func TestSetPhoneSyncResultKeepsCheckpointWhenNil(t *testing.T) {
	s := newPhoneTestStore(t)
	ctx := context.Background()
	device, _, err := s.UpsertPhoneDevice(ctx, sampleDevice())
	if err != nil {
		t.Fatal(err)
	}

	cp := int64(100)
	if err := s.SetPhoneSyncResult(ctx, device.ID, &cp, "done", 1, "2026-11-27 22:10:00"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPhoneSyncResult(ctx, device.ID, nil, "cancelled", 2, "2026-11-27 22:20:00"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetPhoneDevice(ctx, device.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastCheckpoint != 100 {
		t.Fatalf("checkpoint 被覆盖: %d", got.LastCheckpoint)
	}
	if got.LastStatus == nil || *got.LastStatus != "cancelled" {
		t.Fatalf("last_status = %v", got.LastStatus)
	}
	if got.LastBatchID == nil || *got.LastBatchID != 2 {
		t.Fatalf("last_batch_id = %v", got.LastBatchID)
	}
	if got.LastSyncAt == nil || *got.LastSyncAt != "2026-11-27 22:20:00" {
		t.Fatalf("last_sync_at = %v", got.LastSyncAt)
	}
}

func TestPhoneMediaLedger(t *testing.T) {
	s := newPhoneTestStore(t)
	ctx := context.Background()
	device, _, err := s.UpsertPhoneDevice(ctx, sampleDevice())
	if err != nil {
		t.Fatal(err)
	}

	hash := strings.Repeat("a", 64)
	width, height := 4000, 3000
	base := PhoneMedia{
		DeviceID:           device.ID,
		Kind:               "image",
		MediaID:            1000112680,
		DisplayName:        "IMG_20261127_153320.jpg",
		GenerationModified: 442415,
		Size:               5412777,
		DateAdded:          1795764800,
		Width:              &width,
		Height:             &height,
		Hash:               &hash,
		Status:             PhoneMediaSynced,
		UpdatedAt:          "2026-11-27 22:00:00",
	}
	if err := s.UpsertPhoneMedia(ctx, base); err != nil {
		t.Fatalf("UpsertPhoneMedia: %v", err)
	}

	// 失败重试时没有哈希，不应抹掉已有哈希。
	failed := base
	failed.GenerationModified = 442500
	failed.Hash = nil
	failed.Status = PhoneMediaFailed
	msg := "下载中断"
	failed.Message = &msg
	failed.UpdatedAt = "2026-11-27 22:05:00"
	if err := s.UpsertPhoneMedia(ctx, failed); err != nil {
		t.Fatalf("二次 UpsertPhoneMedia: %v", err)
	}

	items, err := s.ListPhoneMedia(ctx, device.ID)
	if err != nil {
		t.Fatalf("ListPhoneMedia: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("账本条目数 = %d, want 1", len(items))
	}
	got := items[0]
	if got.GenerationModified != 442500 || got.Status != PhoneMediaFailed || got.Message == nil {
		t.Fatalf("账本未更新: %+v", got)
	}
	if got.Hash == nil || *got.Hash != hash {
		t.Fatalf("哈希丢失: %v", got.Hash)
	}
	if got.Width == nil || *got.Width != 4000 || got.Height == nil || *got.Height != 3000 {
		t.Fatalf("宽高丢失: %v x %v", got.Width, got.Height)
	}

	failedItems, err := s.ListFailedPhoneMedia(ctx, device.ID, 10)
	if err != nil || len(failedItems) != 1 || failedItems[0].MediaID != 1000112680 {
		t.Fatalf("ListFailedPhoneMedia = %+v, %v", failedItems, err)
	}

	counts, err := s.PhoneMediaCounts(ctx, device.ID)
	if err != nil || counts[PhoneMediaFailed] != 1 {
		t.Fatalf("PhoneMediaCounts = %+v, %v", counts, err)
	}

	// 删除设备应级联删除账本，但不动 assets。
	if err := s.DeletePhoneDevice(ctx, device.ID); err != nil {
		t.Fatalf("DeletePhoneDevice: %v", err)
	}
	items, err = s.ListPhoneMedia(ctx, device.ID)
	if err != nil {
		t.Fatalf("ListPhoneMedia: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("删除设备后账本仍有 %d 条", len(items))
	}
	if _, err := s.GetPhoneDevice(ctx, device.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删除后 GetPhoneDevice 期望 ErrNoRows，得到 %v", err)
	}
}
