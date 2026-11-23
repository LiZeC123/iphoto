package importer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"iphoto/internal/media"
	"iphoto/internal/store"
)

// ErrInvalidName 表示重命名参数不被接受（调用方应返回 400）。
var ErrInvalidName = errors.New("文件名不合法")

// Rename 重命名库内图片：同时更新数据库中的 original_name / storage_name / rel_path
// 与磁盘上的 CAS 文件。CAS 目录与文件名冲突的处理方式与导入一致（追加 __hash8）。
func (m *Manager) Rename(ctx context.Context, hash, newName string) (store.Asset, error) {
	if !ValidateHash(hash) {
		return store.Asset{}, fmt.Errorf("%w: 哈希格式非法", ErrInvalidName)
	}
	name := strings.TrimSpace(newName)
	if name == "" {
		return store.Asset{}, fmt.Errorf("%w: 文件名不能为空", ErrInvalidName)
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return store.Asset{}, fmt.Errorf("%w: 文件名不能包含路径分隔符", ErrInvalidName)
	}
	// 统一拒绝 Windows 不允许的字符，保证同一库在三大平台都能重命名成功。
	if strings.ContainsAny(name, `<>:"|?*`) {
		return store.Asset{}, fmt.Errorf(`%w: 文件名不能包含 < > : " | ? *`, ErrInvalidName)
	}

	m.dedupeMu.Lock()
	defer m.dedupeMu.Unlock()

	asset, err := m.store.GetAsset(ctx, hash)
	if err != nil {
		return store.Asset{}, err
	}
	if asset.Status != "ready" {
		return store.Asset{}, fmt.Errorf("%w: 当前状态为 %s，只有 ready 的图片可以重命名", ErrInvalidName, asset.Status)
	}

	currentExt := filepath.Ext(asset.StorageName)
	if next := filepath.Ext(name); next == "" {
		name += currentExt
	} else if !strings.EqualFold(next, currentExt) {
		return store.Asset{}, fmt.Errorf("%w: 不能修改扩展名（当前为 %s）", ErrInvalidName, currentExt)
	}
	if !media.IsSupported(name) {
		return store.Asset{}, fmt.Errorf("%w: 不支持的文件类型", ErrInvalidName)
	}
	name = sanitizeName(name, hash)
	if !strings.EqualFold(filepath.Ext(name), currentExt) {
		return store.Asset{}, fmt.Errorf("%w: 文件名不合法", ErrInvalidName)
	}

	dirRel := path.Dir(asset.RelPath)
	if dirRel == "." || dirRel == "/" {
		dirRel = filepath.ToSlash(filepath.Join(hash[:2], hash[2:4]))
	}
	dirAbs := filepath.Join(m.paths.Blobs, filepath.FromSlash(dirRel))
	currentAbs := m.paths.BlobPath(asset.RelPath)
	target := filepath.Join(dirAbs, name)

	if _, statErr := os.Stat(currentAbs); statErr != nil {
		return store.Asset{}, fmt.Errorf("原图文件缺失，无法重命名: %w", statErr)
	}
	if target != currentAbs {
		if _, statErr := os.Stat(target); statErr == nil {
			stem, ext := splitName(name)
			name = stem + "__" + hash[:8] + ext
			target = filepath.Join(dirAbs, name)
			if _, err := os.Stat(target); err == nil {
				return store.Asset{}, fmt.Errorf("%w: 目标文件名已存在: %s", ErrInvalidName, name)
			}
		} else if !os.IsNotExist(statErr) {
			return store.Asset{}, fmt.Errorf("检查目标文件名: %w", statErr)
		}
		if err := os.Rename(currentAbs, target); err != nil {
			return store.Asset{}, fmt.Errorf("重命名库内文件: %w", err)
		}
	}

	newRel := filepath.ToSlash(filepath.Join(dirRel, name))
	if err := m.store.SetAssetNames(ctx, hash, name, name, newRel); err != nil {
		if target != currentAbs {
			if rbErr := os.Rename(target, currentAbs); rbErr != nil {
				m.log.Error("回滚重命名失败", "hash", hash, "err", rbErr)
			}
		}
		return store.Asset{}, err
	}
	return m.store.GetAsset(ctx, hash)
}
