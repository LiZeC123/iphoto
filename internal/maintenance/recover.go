// Package maintenance 负责启动时的数据修复（dev.md 5.4、13.2）。
package maintenance

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"iphoto/internal/config"
	"iphoto/internal/media"
	"iphoto/internal/store"
	"iphoto/internal/timeparse"
)

// RecoverResult 汇总修复结果。
type RecoverResult struct {
	Ready   int
	Removed int
	Batches int64
	// Temp 是清理掉的临时文件数量（手机导入中断遗留）。
	Temp int
}

// RecoverInterrupted 处理进程崩溃遗留的中间状态：
//   - status='importing' 且文件存在、大小正确的记录补全字段并置 ready；
//   - 文件缺失或大小不符的记录删除半成品与数据库行；
//   - running 的导入批次标记为 interrupted。
func RecoverInterrupted(ctx context.Context, st *store.Store, paths config.Paths, log *slog.Logger) (RecoverResult, error) {
	var res RecoverResult

	assets, err := st.ListAssetsByStatus(ctx, "importing")
	if err != nil {
		return res, err
	}
	rules, err := st.EnabledTimeRules(ctx)
	if err != nil {
		log.Warn("读取时间解析规则失败", "err", err)
	}
	parseRules := make([]timeparse.Rule, 0, len(rules))
	for _, r := range rules {
		parseRules = append(parseRules, timeparse.Rule{ID: r.ID, Pattern: r.Pattern})
	}

	for _, a := range assets {
		abs := paths.BlobPath(a.RelPath)
		info, statErr := os.Stat(abs)
		if statErr != nil || info.Size() != a.Size {
			if rmErr := os.Remove(abs); rmErr != nil && !os.IsNotExist(rmErr) {
				log.Warn("删除半成品文件失败", "path", abs, "err", rmErr)
			}
			if dbErr := st.DeleteAssetRow(ctx, a.Hash); dbErr != nil && !errors.Is(dbErr, os.ErrNotExist) {
				log.Error("删除半成品记录失败", "hash", a.Hash, "err", dbErr)
			}
			res.Removed++
			continue
		}

		img, err := media.Inspect(abs)
		if err != nil {
			log.Warn("补全导入记录时解析图片失败", "hash", a.Hash, "err", err)
			img = media.Info{MIME: a.Mime, Width: intOrZero(a.Width), Height: intOrZero(a.Height)}
		}
		resolved := timeparse.Parse(a.OriginalName, img.TakenAt, img.TakenAtTag, info.ModTime(), time.Now(), parseRules)
		if err := st.MarkAssetReady(ctx, a.Hash, info.Size(), img.MIME,
			&img.Width, &img.Height, timeparse.Format(resolved.At), resolved.Source); err != nil {
			log.Error("补全导入记录失败", "hash", a.Hash, "err", err)
			continue
		}
		res.Ready++
	}

	batches, err := st.InterruptRunningBatches(ctx, time.Now().Format(store.TimeLayout))
	if err != nil {
		return res, err
	}
	res.Batches = batches
	res.Temp = cleanTempFiles(paths, log)
	return res, nil
}

// cleanTempFiles 删除进程崩溃后遗留的 *.part 临时文件（手机导入使用 runtime/tmp）。
func cleanTempFiles(paths config.Paths, log *slog.Logger) int {
	entries, err := os.ReadDir(paths.Temp)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Warn("读取临时目录失败", "dir", paths.Temp, "err", err)
		}
		return 0
	}
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".part") {
			continue
		}
		if rmErr := os.Remove(filepath.Join(paths.Temp, entry.Name())); rmErr != nil {
			log.Warn("清理临时文件失败", "name", entry.Name(), "err", rmErr)
			continue
		}
		removed++
	}
	return removed
}

func intOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
