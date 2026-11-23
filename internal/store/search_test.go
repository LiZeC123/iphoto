package store

import (
	"context"
	"errors"
	"testing"
)

func addTag(t *testing.T, s *Store, hash, name string) {
	t.Helper()
	if err := s.AddTagsToAssets(context.Background(), []string{hash}, []string{name}); err != nil {
		t.Fatalf("AddTagsToAssets: %v", err)
	}
}

func namesOf(page AssetPage) []string {
	out := make([]string, 0, len(page.Items))
	for _, a := range page.Items {
		out = append(out, a.OriginalName)
	}
	return out
}

func TestListAssetsSearchSyntax(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	addReady(t, s, hashOf('a'), "beach.jpg", "summer 旅行", "2023-05-01T10:00:00", "ready")
	addReady(t, s, hashOf('b'), "city.jpg", "夜景", "2024-02-01T10:00:00", "ready")
	addReady(t, s, hashOf('c'), "screenshot.png", "", "2024-03-01T10:00:00", "ready")
	addTag(t, s, hashOf('a'), "旅行")
	addTag(t, s, hashOf('a'), "海边")
	addTag(t, s, hashOf('b'), "夜景")
	addTag(t, s, hashOf('c'), "截图")

	cases := []struct {
		q    string
		want []string // 按 taken_at_desc
	}{
		{"旅行", []string{"beach.jpg"}},     // note 与 tag 都能命中
		{"beach", []string{"beach.jpg"}},  // 文件名
		{"tag:旅行", []string{"beach.jpg"}}, // 标签字段
		{"tag:夜景", []string{"city.jpg"}},
		{"name:city", []string{"city.jpg"}},
		{"note:夜景", []string{"city.jpg"}},
		{"海边 AND NOT name:city", []string{"beach.jpg"}},
		{"旅行 OR 夜景", []string{"city.jpg", "beach.jpg"}},
		{"旅行 AND 海边", []string{"beach.jpg"}},
		{"(夜景 OR 旅行) AND NOT tag:海边", []string{"city.jpg"}},
		{"NOT tag:旅行", []string{"screenshot.png", "city.jpg"}},
		{"-旅行", []string{"screenshot.png", "city.jpg"}},
		{"tag:旅行 OR tag:截图", []string{"screenshot.png", "beach.jpg"}},
		{`"summer 旅行"`, []string{"beach.jpg"}},
		{"旅行 NOT 海边", []string{}}, // 只剩 city？不：city 没有 旅行 标签，故为空
	}

	for _, tc := range cases {
		page, err := s.ListAssets(ctx, AssetFilter{Q: tc.q})
		if err != nil {
			t.Errorf("ListAssets(%q): %v", tc.q, err)
			continue
		}
		got := namesOf(page)
		if len(got) != len(tc.want) {
			t.Errorf("q=%q 结果 = %v, want %v", tc.q, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("q=%q 结果 = %v, want %v", tc.q, got, tc.want)
				break
			}
		}
	}
}

func TestListAssetsSearchEscapesWildcards(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	addReady(t, s, hashOf('a'), "100%.jpg", "", "2023-05-01T10:00:00", "ready")
	addReady(t, s, hashOf('b'), "100x.jpg", "", "2023-05-02T10:00:00", "ready")

	page, err := s.ListAssets(ctx, AssetFilter{Q: "100%"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].OriginalName != "100%.jpg" {
		t.Fatalf("LIKE 通配符应被转义: %v", namesOf(page))
	}
}

func TestListAssetsUntagged(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	addReady(t, s, hashOf('a'), "tagged.jpg", "", "2023-05-01T10:00:00", "ready")
	addReady(t, s, hashOf('b'), "untagged.jpg", "", "2023-05-02T10:00:00", "ready")
	addReady(t, s, hashOf('c'), "trashed.jpg", "", "2023-05-03T10:00:00", "trashed")
	addTag(t, s, hashOf('a'), "旅行")

	page, err := s.ListAssets(ctx, AssetFilter{Untagged: true})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].OriginalName != "untagged.jpg" {
		t.Fatalf("untagged = %v", namesOf(page))
	}

	n, err := s.CountUntagged(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("CountUntagged = %d, want 1", n)
	}
}

func TestListAssetsInvalidSearch(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.ListAssets(context.Background(), AssetFilter{Q: "cat AND"}); !errors.Is(err, ErrSearchSyntax) {
		t.Fatalf("err = %v, want ErrSearchSyntax", err)
	}
}
