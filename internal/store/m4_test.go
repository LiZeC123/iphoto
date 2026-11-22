package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestTimeRulesCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	rules, err := s.ListTimeRules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("内置规则数量 = %d, want 2", len(rules))
	}

	rule, err := s.CreateTimeRule(ctx, "微信截图", `mmexport(?P<year>\d{4})(?P<month>\d{2})(?P<day>\d{2})`, 30, true)
	if err != nil {
		t.Fatalf("CreateTimeRule: %v", err)
	}
	if rule.ID == 0 || !rule.Enabled || rule.Builtin {
		t.Fatalf("rule = %+v", rule)
	}

	name := "微信截图2"
	newPattern := `wx(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})`
	priority := 40
	updated, err := s.UpdateTimeRule(ctx, rule.ID, &name, &newPattern, &priority, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != name || updated.Pattern != newPattern || updated.Priority != 40 {
		t.Fatalf("updated = %+v", updated)
	}

	disabled := false
	updated, err = s.UpdateTimeRule(ctx, rule.ID, nil, nil, nil, &disabled)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Enabled {
		t.Fatal("应已禁用")
	}

	if err := s.DeleteTimeRule(ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTimeRule(ctx, rule.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删除后读取 err = %v", err)
	}
}

func TestTrashFlow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hash := hashOf('a')
	addReady(t, s, hash, "a.jpg", "", "2023-05-01T10:00:00", "ready")
	deletedAt := time.Now().Format(TimeLayout)
	if err := s.SetAssetStatus(ctx, hash, "trashed", &deletedAt); err != nil {
		t.Fatal(err)
	}

	page, err := s.ListTrash(ctx, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Hash != hash {
		t.Fatalf("trash page = %+v", page)
	}

	all, err := s.TrashedAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("trashed = %d", len(all))
	}

	if err := s.RestoreAsset(ctx, hash); err != nil {
		t.Fatal(err)
	}
	a, err := s.GetAsset(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "ready" || a.DeletedAt != nil {
		t.Fatalf("恢复后 = %+v", a)
	}

	if err := s.SetAssetStatus(ctx, hash, "trashed", &deletedAt); err != nil {
		t.Fatal(err)
	}
	rows, err := s.PurgeTrash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("purged = %d", rows)
	}
	if _, err := s.GetAsset(ctx, hash); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("清空后记录应删除")
	}
}

func TestListBatches(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	batch, err := s.CreateBatch(ctx, "/tmp/photos", time.Now().Format(TimeLayout))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddImportItem(ctx, batch.ID, "/tmp/photos/a.jpg", hashOf('a'), "new", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddImportItem(ctx, batch.ID, "/tmp/photos/b.jpg", hashOf('b'), "failed", "boom"); err != nil {
		t.Fatal(err)
	}
	batches, err := s.ListBatches(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 1 || batches[0].ItemCount != 2 {
		t.Fatalf("batches = %+v", batches)
	}
}
