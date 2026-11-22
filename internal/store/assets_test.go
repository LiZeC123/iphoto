package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func addReady(t *testing.T, s *Store, hash, name, note, takenAt, status string) {
	t.Helper()
	ctx := context.Background()
	if err := s.InsertImportingAsset(ctx, Asset{
		Hash:         hash,
		OriginalName: name,
		StorageName:  name,
		RelPath:      hash[:2] + "/" + hash[2:4] + "/" + name,
		Size:         100,
		Mime:         "image/jpeg",
		AddedAt:      "2024-01-01T00:00:00",
		Status:       "importing",
	}); err != nil {
		t.Fatalf("InsertImportingAsset: %v", err)
	}
	if err := s.MarkAssetReady(ctx, hash, 100, "image/jpeg", intPtr(10), intPtr(20), takenAt, "test"); err != nil {
		t.Fatalf("MarkAssetReady: %v", err)
	}
	if note != "" {
		if _, err := s.PatchAsset(ctx, hash, AssetPatch{Note: &note}); err != nil {
			t.Fatalf("PatchAsset: %v", err)
		}
	}
	if status != "ready" {
		if err := s.SetAssetStatus(ctx, hash, status, nil); err != nil {
			t.Fatalf("SetAssetStatus: %v", err)
		}
	}
}

func intPtr(v int) *int { return &v }

func TestListAssetsFilters(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	addReady(t, s, hashOf('a'), "beach.jpg", "summer 旅行", "2023-05-01T10:00:00", "ready")
	addReady(t, s, hashOf('b'), "city.jpg", "", "2024-02-01T10:00:00", "ready")
	addReady(t, s, hashOf('c'), "old.jpg", "", "2023-12-31T10:00:00", "trashed")

	page, err := s.ListAssets(ctx, AssetFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Fatalf("默认应只含 ready，total = %d", page.Total)
	}
	// 默认按有效时间倒序：city(2024) 在前。
	if page.Items[0].OriginalName != "city.jpg" {
		t.Fatalf("排序错误: %s", page.Items[0].OriginalName)
	}

	page, err = s.ListAssets(ctx, AssetFilter{Q: "旅行"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].OriginalName != "beach.jpg" {
		t.Fatalf("note 搜索失败: %+v", page)
	}

	page, err = s.ListAssets(ctx, AssetFilter{Q: "%"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("LIKE 通配符应被转义，total = %d", page.Total)
	}

	page, err = s.ListAssets(ctx, AssetFilter{From: "2023-05", To: "2023-12"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].OriginalName != "beach.jpg" {
		t.Fatalf("时间范围失败: %+v", page)
	}

	page, err = s.ListAssets(ctx, AssetFilter{Status: "trashed"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].OriginalName != "old.jpg" {
		t.Fatalf("回收站筛选失败: %+v", page)
	}

	page, err = s.ListAssets(ctx, AssetFilter{Page: 2, PageSize: 1, Sort: "name_asc"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].OriginalName != "city.jpg" {
		t.Fatalf("分页失败: %+v", page)
	}
}

func TestPatchAssetOverrideAffectsEffectiveTime(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hash := hashOf('d')
	addReady(t, s, hash, "x.jpg", "", "2020-01-01T00:00:00", "ready")

	override := "2025-06-01T12:00:00"
	a, err := s.PatchAsset(ctx, hash, AssetPatch{TakenAtOverride: &override, Note: ptr("备注")})
	if err != nil {
		t.Fatal(err)
	}
	if a.TakenAtOverride == nil || *a.TakenAtOverride != override || a.Note == nil || *a.Note != "备注" {
		t.Fatalf("patch 失败: %+v", a)
	}
	if eff := a.EffectiveTime(); eff == nil || *eff != override {
		t.Fatalf("有效时间 = %v", eff)
	}

	page, err := s.ListAssets(ctx, AssetFilter{From: "2025-06", To: "2025-06"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 {
		t.Fatalf("覆盖时间应参与筛选，total = %d", page.Total)
	}

	empty := ""
	if _, err := s.PatchAsset(ctx, hash, AssetPatch{TakenAtOverride: &empty}); err != nil {
		t.Fatal(err)
	}
	a, err = s.GetAsset(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if a.TakenAtOverride != nil {
		t.Fatalf("空字符串应清空覆盖值: %v", *a.TakenAtOverride)
	}
}

func TestMissingAsset(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.GetAsset(context.Background(), hashOf('z')); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestImportItemStatusGuard(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	batch, err := s.CreateBatch(ctx, "/tmp/photos", time.Now().Format(TimeLayout))
	if err != nil {
		t.Fatal(err)
	}
	hash := hashOf('e')
	addReady(t, s, hash, "dup.jpg", "", "2024-01-01T00:00:00", "ready")
	itemID, err := s.AddImportItem(ctx, batch.ID, "/tmp/photos/dup2.jpg", hash, "duplicate_pending", "")
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.ListDuplicateItems(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ExistingName != "dup.jpg" || items[0].ID != itemID {
		t.Fatalf("重复项 = %+v", items)
	}
	if err := s.SetImportItemStatus(ctx, itemID, "duplicate_skip", "test"); err != nil {
		t.Fatal(err)
	}
	items, err = s.ListDuplicateItems(ctx, batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("处理后不应再出现在重复列表: %+v", items)
	}
}

func ptr[T any](v T) *T { return &v }

func hashOf(ch rune) string {
	h := make([]rune, 64)
	for i := range h {
		h[i] = ch
	}
	return string(h)
}

func TestTimelineMonths(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	addReady(t, s, hashOf('a'), "a.jpg", "", "2023-05-01T10:00:00", "ready")
	addReady(t, s, hashOf('b'), "b.jpg", "", "2023-05-20T10:00:00", "ready")
	addReady(t, s, hashOf('c'), "c.jpg", "", "2024-01-02T10:00:00", "ready")
	addReady(t, s, hashOf('d'), "d.jpg", "", "2024-01-03T10:00:00", "trashed")
	addReady(t, s, hashOf('e'), "e.jpg", "", "2024-01-04T10:00:00", "ready")

	months, err := s.TimelineMonths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(months) != 2 {
		t.Fatalf("months = %+v", months)
	}
	if months[0].Month != "2024-01" || months[0].Count != 2 {
		t.Fatalf("最新月份 = %+v", months[0])
	}
	if months[1].Month != "2023-05" || months[1].Count != 2 {
		t.Fatalf("次新月份 = %+v", months[1])
	}

	// 覆盖时间应改变聚合结果。
	override := "2022-07-01T00:00:00"
	if _, err := s.PatchAsset(ctx, hashOf('a'), AssetPatch{TakenAtOverride: &override}); err != nil {
		t.Fatal(err)
	}
	months, err = s.TimelineMonths(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range months {
		if m.Month == "2022-07" && m.Count == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("覆盖时间未参与聚合: %+v", months)
	}
}
