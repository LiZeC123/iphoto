package maintenance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"iphoto/internal/config"
	"iphoto/internal/importer"
	"iphoto/internal/store"
)

// VerifyIssue 描述一个文件层面的问题。
type VerifyIssue struct {
	Hash         string `json:"hash"`
	OriginalName string `json:"original_name"`
	RelPath      string `json:"rel_path"`
	Size         int64  `json:"size"`
	Problem      string `json:"problem,omitempty"`
}

// VerifyReport 是完整性校验结果；不自动删除任何文件。
type VerifyReport struct {
	Checked       int           `json:"checked"`
	Missing       []VerifyIssue `json:"missing"`
	SizeMismatch  []VerifyIssue `json:"size_mismatch"`
	HashMismatch  []VerifyIssue `json:"hash_mismatch"`
	Orphans       []string      `json:"orphans"`
	MarkedMissing int           `json:"marked_missing"`
	Hashed        int           `json:"hashed"`
	DurationMS    int64         `json:"duration_ms"`
}

// Verify 校验 ready 图片与 CAS 文件的一致性，并扫描孤儿文件。
// rehash=true 时额外重算 SHA-256（较慢）。缺失/大小不符的记录标记为 missing。
func Verify(ctx context.Context, st *store.Store, paths config.Paths, rehash bool) (VerifyReport, error) {
	start := time.Now()
	report := VerifyReport{
		Missing:      []VerifyIssue{},
		SizeMismatch: []VerifyIssue{},
		HashMismatch: []VerifyIssue{},
		Orphans:      []string{},
	}

	assets, err := st.ListAssetsByStatus(ctx, "ready")
	if err != nil {
		return report, err
	}
	for _, a := range assets {
		report.Checked++
		abs := paths.BlobPath(a.RelPath)
		info, statErr := os.Stat(abs)
		switch {
		case statErr != nil || !info.Mode().IsRegular():
			report.Missing = append(report.Missing, VerifyIssue{
				Hash: a.Hash, OriginalName: a.OriginalName, RelPath: a.RelPath, Size: a.Size, Problem: "文件不存在",
			})
			if err := st.SetAssetStatus(ctx, a.Hash, "missing", nil); err == nil {
				report.MarkedMissing++
			}
			continue
		case info.Size() != a.Size:
			report.SizeMismatch = append(report.SizeMismatch, VerifyIssue{
				Hash: a.Hash, OriginalName: a.OriginalName, RelPath: a.RelPath, Size: a.Size,
				Problem: "数据库记录大小与文件不一致",
			})
			if err := st.SetAssetStatus(ctx, a.Hash, "missing", nil); err == nil {
				report.MarkedMissing++
			}
			continue
		}

		if rehash {
			sum, _, hashErr := importer.HashFile(abs)
			if hashErr != nil {
				report.HashMismatch = append(report.HashMismatch, VerifyIssue{
					Hash: a.Hash, OriginalName: a.OriginalName, RelPath: a.RelPath, Size: a.Size,
					Problem: "重算哈希失败: " + hashErr.Error(),
				})
				continue
			}
			report.Hashed++
			if sum != a.Hash {
				report.HashMismatch = append(report.HashMismatch, VerifyIssue{
					Hash: a.Hash, OriginalName: a.OriginalName, RelPath: a.RelPath, Size: a.Size,
					Problem: "内容哈希与记录不一致",
				})
			}
		}
	}

	known, err := st.AllAssetRelPaths(ctx)
	if err != nil {
		return report, err
	}
	err = filepath.WalkDir(paths.Blobs, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		rel, relErr := filepath.Rel(paths.Blobs, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if _, ok := known[rel]; !ok && len(report.Orphans) < 200 {
			report.Orphans = append(report.Orphans, rel)
		}
		return nil
	})
	if err != nil {
		return report, err
	}

	report.DurationMS = time.Since(start).Milliseconds()
	return report, nil
}
