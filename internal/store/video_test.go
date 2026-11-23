package store

import (
	"context"
	"testing"
)

func TestMarkAssetReadyNullsZeroDimensions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	hash := hashOf('v')
	if err := s.InsertImportingAsset(ctx, Asset{
		Hash:         hash,
		OriginalName: "clip.mp4",
		StorageName:  "clip.mp4",
		RelPath:      hash[:2] + "/" + hash[2:4] + "/clip.mp4",
		Size:         100,
		Mime:         "video/mp4",
		Width:        intPtr(0),
		Height:       intPtr(0),
		AddedAt:      "2024-01-01T00:00:00",
		Status:       "importing",
	}); err != nil {
		t.Fatalf("InsertImportingAsset: %v", err)
	}
	if err := s.MarkAssetReady(ctx, hash, 100, "video/mp4", intPtr(0), intPtr(0), "2024-01-01T00:00:00", "test"); err != nil {
		t.Fatalf("MarkAssetReady: %v", err)
	}
	a, err := s.GetAsset(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	if a.Width != nil || a.Height != nil {
		t.Fatalf("0×0 应存 NULL，got %v x %v", a.Width, a.Height)
	}
}

func TestListDetectionAssetsExcludesVideos(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	imgHash := hashOf('i')
	videoHash := hashOf('v')
	addReady(t, s, imgHash, "img.jpg", "", "2024-01-01T10:00:00", "ready")
	addReady(t, s, videoHash, "clip.mp4", "", "2024-01-01T11:00:00", "ready")
	if _, err := s.DB().ExecContext(ctx, `UPDATE assets SET mime = 'video/mp4' WHERE hash = ?`, videoHash); err != nil {
		t.Fatal(err)
	}

	assets, err := s.ListDetectionAssets(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].Hash != imgHash {
		t.Fatalf("相似检测候选 = %+v, 应只含图片", assets)
	}
}
