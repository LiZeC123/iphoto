package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestTagsCRUDAndBatch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, b := hashOf('a'), hashOf('b')
	addReady(t, s, a, "a.jpg", "", "2023-05-01T10:00:00", "ready")
	addReady(t, s, b, "b.jpg", "", "2024-01-01T10:00:00", "ready")

	if err := s.AddTagsToAssets(ctx, []string{a, b}, []string{"旅行", "Summer"}); err != nil {
		t.Fatalf("AddTagsToAssets: %v", err)
	}
	tags, err := s.ListTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 {
		t.Fatalf("tags = %+v", tags)
	}
	for _, tag := range tags {
		if tag.Count != 2 {
			t.Errorf("tag %q count = %d, want 2", tag.Name, tag.Count)
		}
	}

	assetTags, err := s.TagsForAsset(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(assetTags) != 2 {
		t.Fatalf("asset tags = %+v", assetTags)
	}

	// 标签筛选大小写不敏感。
	page, err := s.ListAssets(ctx, AssetFilter{Tag: "summer"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Fatalf("tag 筛选 total = %d, want 2", page.Total)
	}

	// 重复创建（忽略大小写）返回 ErrTagExists。
	if _, err := s.CreateTag(ctx, "summer"); !errors.Is(err, ErrTagExists) {
		t.Fatalf("重复创建 err = %v", err)
	}

	if err := s.RemoveTagsFromAssets(ctx, []string{a}, []string{"旅行"}); err != nil {
		t.Fatal(err)
	}
	assetTags, err = s.TagsForAsset(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(assetTags) != 1 {
		t.Fatalf("移除后 tags = %+v", assetTags)
	}

	tagID := tags[0].ID
	if _, err := s.RenameTag(ctx, tagID, "夏日"); err != nil {
		t.Fatalf("RenameTag: %v", err)
	}
	if err := s.DeleteTag(ctx, tagID); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}
	tags, err = s.ListTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 {
		t.Fatalf("删除后 tags = %+v", tags)
	}
}

func TestManualAlbumCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, b := hashOf('c'), hashOf('d')
	addReady(t, s, a, "c.jpg", "", "2023-01-01T10:00:00", "ready")
	addReady(t, s, b, "d.jpg", "", "2023-02-01T10:00:00", "ready")

	album, err := s.CreateAlbum(ctx, "旅行相册", "manual", ptr("2023 的旅行"), nil)
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	if album.Type != "manual" || album.Name != "旅行相册" {
		t.Fatalf("album = %+v", album)
	}

	added, err := s.AddAlbumItems(ctx, album.ID, []string{a, b, a})
	if err != nil {
		t.Fatal(err)
	}
	if added != 2 {
		t.Fatalf("added = %d, want 2", added)
	}
	album, err = s.GetAlbum(ctx, album.ID)
	if err != nil {
		t.Fatal(err)
	}
	if album.Count != 2 {
		t.Fatalf("count = %d, want 2", album.Count)
	}

	page, err := s.AlbumPage(ctx, album.ID, "", "", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 {
		t.Fatalf("album page total = %d", page.Total)
	}

	removed, err := s.RemoveAlbumItems(ctx, album.ID, []string{a})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d", removed)
	}

	updated, err := s.UpdateAlbum(ctx, album.ID, ptr("新名字"), ptr("新描述"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "新名字" || updated.Description == nil || *updated.Description != "新描述" {
		t.Fatalf("updated = %+v", updated)
	}

	if err := s.DeleteAlbum(ctx, album.ID); err != nil {
		t.Fatalf("DeleteAlbum: %v", err)
	}
	if _, err := s.GetAlbum(ctx, album.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删除后读取 err = %v", err)
	}
}

func TestSmartAlbumRule(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, b, c := hashOf('e'), hashOf('f'), hashOf('0')
	addReady(t, s, a, "trip-2023.jpg", "", "2023-05-01T10:00:00", "ready")
	addReady(t, s, b, "trip-2024.jpg", "", "2024-01-01T10:00:00", "ready")
	addReady(t, s, c, "work-2023.jpg", "", "2023-06-01T10:00:00", "ready")
	if err := s.AddTagsToAssets(ctx, []string{a, b}, []string{"旅行"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTagsToAssets(ctx, []string{c}, []string{"工作"}); err != nil {
		t.Fatal(err)
	}

	rule := `{"op":"and","children":[{"type":"tag","value":"旅行"},{"type":"time_range","from":"2023-01","to":"2023-12"}]}`
	album, err := s.CreateAlbum(ctx, "2023 旅行", "smart", nil, &rule)
	if err != nil {
		t.Fatalf("CreateAlbum smart: %v", err)
	}
	if album.Count != 1 {
		t.Fatalf("smart count = %d, want 1", album.Count)
	}
	page, err := s.SmartAlbumPage(ctx, rule, "", "", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Hash != a {
		t.Fatalf("smart page = %+v", page)
	}

	newRule := `{"op":"and","children":[{"type":"tag","value":"工作"}]}`
	updated, err := s.UpdateAlbum(ctx, album.ID, nil, nil, &newRule)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Count != 1 {
		t.Fatalf("更新规则后 count = %d, want 1", updated.Count)
	}

	if _, err := s.CreateAlbum(ctx, "坏规则", "smart", nil, ptr(`{"type":"rating","value":"5"}`)); err == nil {
		t.Fatal("未知节点应报错")
	}
	if _, err := s.CreateAlbum(ctx, "空 and", "smart", nil, ptr(`{"op":"and","children":[]}`)); err == nil {
		t.Fatal("空 and 应报错")
	}
	if _, err := s.AddAlbumItems(ctx, album.ID, []string{a}); err == nil {
		t.Fatal("智能相册不能手动加成员")
	}
}

func TestAlbumMonthsAndTimeFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, b := hashOf('1'), hashOf('2')
	addReady(t, s, a, "m1.jpg", "", "2023-05-01T10:00:00", "ready")
	addReady(t, s, b, "m2.jpg", "", "2024-02-01T10:00:00", "ready")

	album, err := s.CreateAlbum(ctx, "月份", "manual", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddAlbumItems(ctx, album.ID, []string{a, b}); err != nil {
		t.Fatal(err)
	}
	months, err := s.AlbumMonths(ctx, album.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(months) != 2 || months[0].Month != "2024-02" {
		t.Fatalf("months = %+v", months)
	}

	page, err := s.AlbumPage(ctx, album.ID, "2023-05", "2023-05", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Hash != a {
		t.Fatalf("时间筛选结果 = %+v", page)
	}

	rule := `{"op":"and","children":[{"type":"time_range","from":"2024-01","to":"2024-12"}]}`
	smart, err := s.CreateAlbum(ctx, "智能月份", "smart", nil, &rule)
	if err != nil {
		t.Fatal(err)
	}
	months, err = s.AlbumMonths(ctx, smart.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(months) != 1 || months[0].Month != "2024-02" {
		t.Fatalf("smart months = %+v", months)
	}
	page, err = s.AlbumPage(ctx, smart.ID, "", "", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Hash != b {
		t.Fatalf("smart page = %+v", page)
	}
}

func TestTagsSummaryForSelection(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, b, c := hashOf('7'), hashOf('8'), hashOf('9')
	addReady(t, s, a, "a.jpg", "", "2024-01-01T00:00:00", "ready")
	addReady(t, s, b, "b.jpg", "", "2024-01-02T00:00:00", "ready")
	addReady(t, s, c, "c.jpg", "", "2024-01-03T00:00:00", "ready")
	if err := s.AddTagsToAssets(ctx, []string{a, b}, []string{"旅行"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTagsToAssets(ctx, []string{b}, []string{"家人"}); err != nil {
		t.Fatal(err)
	}

	summary, err := s.TagsSummary(ctx, []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if len(summary) != 2 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary[0].Name != "旅行" || summary[0].Count != 2 {
		t.Fatalf("第一个标签 = %+v", summary[0])
	}
	if summary[1].Name != "家人" || summary[1].Count != 1 {
		t.Fatalf("第二个标签 = %+v", summary[1])
	}

	single, err := s.TagsSummary(ctx, []string{a})
	if err != nil {
		t.Fatal(err)
	}
	if len(single) != 1 || single[0].Count != 1 {
		t.Fatalf("单张统计 = %+v", single)
	}

	empty, err := s.TagsSummary(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("空选择 = %+v, %v", empty, err)
	}
}
