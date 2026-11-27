package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// 同步账本状态。
const (
	// PhoneMediaSynced 表示该媒体已入库或库内已有相同内容。
	PhoneMediaSynced = "synced"
	// PhoneMediaFailed 表示上一次尝试失败，下一轮同步会重试。
	PhoneMediaFailed = "failed"
	// PhoneMediaUnsupported 表示类型不受支持，不会下载。
	PhoneMediaUnsupported = "unsupported"
	// PhoneMediaMissing 表示手机侧该媒体已不可用（3001），不再自动重试。
	PhoneMediaMissing = "missing"
)

// PhoneDevice 对应 phone_devices。Token 不放进结构体，避免被 JSON 序列化出去。
type PhoneDevice struct {
	ID             int64  `json:"id"`
	DeviceKey      string `json:"-"`
	Name           string `json:"name"`
	Model          string `json:"model"`
	Manufacturer   string `json:"manufacturer"`
	AndroidVersion string `json:"android_version"`
	Address        string `json:"address"`
	// Token 只用于写入，读接口不返回（HasToken 表示库里是否已保存）。
	Token          string  `json:"-"`
	HasToken       bool    `json:"has_token"`
	CreatedAt      string  `json:"created_at"`
	LastSeenAt     string  `json:"last_seen_at"`
	LastSyncAt     *string `json:"last_sync_at"`
	LastCheckpoint int64   `json:"last_checkpoint"`
	LastStatus     *string `json:"last_status"`
	LastBatchID    *int64  `json:"last_batch_id"`
}

// PhoneMedia 对应 phone_media（同步账本）。
type PhoneMedia struct {
	DeviceID           int64
	Kind               string
	MediaID            int64
	DisplayName        string
	GenerationModified int64
	Size               int64
	DateAdded          int64
	Width              *int
	Height             *int
	Hash               *string
	Status             string
	Message            *string
	UpdatedAt          string
}

// PhoneMediaKey 是账本的内存索引键。
func PhoneMediaKey(kind string, mediaID int64) string {
	return kind + ":" + fmt.Sprint(mediaID)
}

// PhoneDeviceKey 由厂商与型号生成设备身份（小写，忽略首尾空白）。
// 手机 IP 会变，所以不用地址做身份；同型号手机会命中同一台设备。
func PhoneDeviceKey(manufacturer, model string) string {
	return strings.ToLower(strings.TrimSpace(manufacturer)) + "|" + strings.ToLower(strings.TrimSpace(model))
}

const phoneDeviceColumns = `id, device_key, name, model, manufacturer, android_version,
	last_addr, token <> '', created_at, last_seen_at, last_sync_at,
	last_checkpoint, last_status, last_batch_id`

func scanPhoneDevice(row interface{ Scan(...any) error }) (PhoneDevice, error) {
	var (
		d         PhoneDevice
		lastSync  sql.NullString
		lastStat  sql.NullString
		lastBatch sql.NullInt64
	)
	if err := row.Scan(&d.ID, &d.DeviceKey, &d.Name, &d.Model, &d.Manufacturer, &d.AndroidVersion,
		&d.Address, &d.HasToken, &d.CreatedAt, &d.LastSeenAt, &lastSync,
		&d.LastCheckpoint, &lastStat, &lastBatch); err != nil {
		return PhoneDevice{}, err
	}
	d.LastSyncAt = nullStringPtr(lastSync)
	d.LastStatus = nullStringPtr(lastStat)
	if lastBatch.Valid {
		v := lastBatch.Int64
		d.LastBatchID = &v
	}
	return d, nil
}

// UpsertPhoneDevice 按 device_key 创建设备或更新其信息，返回设备与「是否新建」。
// 更新不会覆盖 last_checkpoint / last_sync_at 等同步进度。
func (s *Store) UpsertPhoneDevice(ctx context.Context, d PhoneDevice) (PhoneDevice, bool, error) {
	if d.DeviceKey == "" {
		return PhoneDevice{}, false, errors.New("device_key 不能为空")
	}
	if d.Name == "" || d.Address == "" {
		return PhoneDevice{}, false, errors.New("设备名与地址不能为空")
	}
	if d.Token == "" {
		return PhoneDevice{}, false, errors.New("token 不能为空")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PhoneDevice{}, false, fmt.Errorf("登记手机设备: %w", err)
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM phone_devices WHERE device_key = ?`, d.DeviceKey).Scan(&id)
	created := errors.Is(err, sql.ErrNoRows)
	if err != nil && !created {
		return PhoneDevice{}, false, fmt.Errorf("查询手机设备: %w", err)
	}

	if created {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO phone_devices
				(device_key, name, model, manufacturer, android_version, last_addr, token,
				 created_at, last_seen_at, last_checkpoint)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
			d.DeviceKey, d.Name, d.Model, d.Manufacturer, d.AndroidVersion,
			d.Address, d.Token, d.CreatedAt, d.LastSeenAt)
		if err != nil {
			return PhoneDevice{}, false, fmt.Errorf("登记手机设备: %w", err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return PhoneDevice{}, false, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE phone_devices
			SET name = ?, model = ?, manufacturer = ?, android_version = ?,
				last_addr = ?, token = ?, last_seen_at = ?
			WHERE id = ?`,
			d.Name, d.Model, d.Manufacturer, d.AndroidVersion,
			d.Address, d.Token, d.LastSeenAt, id); err != nil {
			return PhoneDevice{}, false, fmt.Errorf("更新手机设备: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return PhoneDevice{}, false, fmt.Errorf("登记手机设备: %w", err)
	}
	device, err := s.GetPhoneDevice(ctx, id)
	if err != nil {
		return PhoneDevice{}, false, err
	}
	return device, created, nil
}

// GetPhoneDevice 按 id 读取设备。
func (s *Store) GetPhoneDevice(ctx context.Context, id int64) (PhoneDevice, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+phoneDeviceColumns+` FROM phone_devices WHERE id = ?`, id)
	return scanPhoneDevice(row)
}

// PhoneDeviceByKey 按设备身份读取；不存在时返回 sql.ErrNoRows。
func (s *Store) PhoneDeviceByKey(ctx context.Context, key string) (PhoneDevice, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+phoneDeviceColumns+` FROM phone_devices WHERE device_key = ?`, key)
	return scanPhoneDevice(row)
}

// PhoneDeviceByAddress 按地址读取最近使用过的设备；不存在时返回 sql.ErrNoRows。
func (s *Store) PhoneDeviceByAddress(ctx context.Context, address string) (PhoneDevice, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+phoneDeviceColumns+` FROM phone_devices WHERE last_addr = ?
		 ORDER BY last_seen_at DESC, id DESC LIMIT 1`, address)
	return scanPhoneDevice(row)
}

// ListPhoneDevices 返回全部已知设备，最近使用的在前。
func (s *Store) ListPhoneDevices(ctx context.Context) ([]PhoneDevice, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+phoneDeviceColumns+` FROM phone_devices
		 ORDER BY last_seen_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("查询手机设备: %w", err)
	}
	defer rows.Close()

	out := make([]PhoneDevice, 0, 4)
	for rows.Next() {
		d, err := scanPhoneDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PhoneDeviceToken 读取设备保存的 Token。
func (s *Store) PhoneDeviceToken(ctx context.Context, id int64) (string, error) {
	var token string
	if err := s.db.QueryRowContext(ctx,
		`SELECT token FROM phone_devices WHERE id = ?`, id).Scan(&token); err != nil {
		return "", err
	}
	return token, nil
}

// TouchPhoneDevice 只更新最后访问时间（检查连接时调用）。
func (s *Store) TouchPhoneDevice(ctx context.Context, id int64, seenAt string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE phone_devices SET last_seen_at = ? WHERE id = ?`, seenAt, id); err != nil {
		return fmt.Errorf("更新手机设备时间: %w", err)
	}
	return nil
}

// SetPhoneSyncResult 记录一轮同步的结果；checkpoint 为 nil 时不改动上次进度。
func (s *Store) SetPhoneSyncResult(ctx context.Context, id int64, checkpoint *int64, status string, batchID int64, syncAt string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE phone_devices
		SET last_checkpoint = COALESCE(?, last_checkpoint),
			last_status = ?, last_batch_id = ?, last_sync_at = ?
		WHERE id = ?`,
		checkpoint, status, batchID, syncAt, id)
	if err != nil {
		return fmt.Errorf("记录手机同步结果: %w", err)
	}
	return nil
}

// DeletePhoneDevice 删除设备及其同步账本（库内图片不受影响）。
func (s *Store) DeletePhoneDevice(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM phone_devices WHERE id = ?`, id); err != nil {
		return fmt.Errorf("删除手机设备: %w", err)
	}
	return nil
}

// ListPhoneMedia 读取一台设备的全部账本条目，供同步开始时建内存索引。
func (s *Store) ListPhoneMedia(ctx context.Context, deviceID int64) ([]PhoneMedia, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT device_id, kind, media_id, display_name, generation_modified, size,
			date_added, width, height, hash, status, message, updated_at
		FROM phone_media WHERE device_id = ?`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("查询同步账本: %w", err)
	}
	defer rows.Close()

	out := make([]PhoneMedia, 0, 128)
	for rows.Next() {
		m, err := scanPhoneMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func scanPhoneMedia(row interface{ Scan(...any) error }) (PhoneMedia, error) {
	var (
		m       PhoneMedia
		width   sql.NullInt64
		height  sql.NullInt64
		hash    sql.NullString
		message sql.NullString
	)
	if err := row.Scan(&m.DeviceID, &m.Kind, &m.MediaID, &m.DisplayName, &m.GenerationModified,
		&m.Size, &m.DateAdded, &width, &height, &hash, &m.Status, &message, &m.UpdatedAt); err != nil {
		return PhoneMedia{}, err
	}
	if width.Valid {
		v := int(width.Int64)
		m.Width = &v
	}
	if height.Valid {
		v := int(height.Int64)
		m.Height = &v
	}
	m.Hash = nullStringPtr(hash)
	m.Message = nullStringPtr(message)
	return m, nil
}

// UpsertPhoneMedia 写入/更新一条账本记录。
// hash 为空时保留旧值（下载失败不应抹掉此前成功记录的哈希）。
func (s *Store) UpsertPhoneMedia(ctx context.Context, m PhoneMedia) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO phone_media
			(device_id, kind, media_id, display_name, generation_modified, size, date_added,
			 width, height, hash, status, message, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id, kind, media_id) DO UPDATE SET
			display_name = excluded.display_name,
			generation_modified = excluded.generation_modified,
			size = excluded.size,
			date_added = excluded.date_added,
			width = excluded.width,
			height = excluded.height,
			hash = COALESCE(excluded.hash, phone_media.hash),
			status = excluded.status,
			message = excluded.message,
			updated_at = excluded.updated_at`,
		m.DeviceID, m.Kind, m.MediaID, m.DisplayName, m.GenerationModified, m.Size, m.DateAdded,
		m.Width, m.Height, m.Hash, m.Status, m.Message, m.UpdatedAt)
	if err != nil {
		return fmt.Errorf("写入同步账本: %w", err)
	}
	return nil
}

// ListFailedPhoneMedia 返回上一轮失败、需要重试的账本条目（最多 limit 条）。
func (s *Store) ListFailedPhoneMedia(ctx context.Context, deviceID int64, limit int) ([]PhoneMedia, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT device_id, kind, media_id, display_name, generation_modified, size,
			date_added, width, height, hash, status, message, updated_at
		FROM phone_media WHERE device_id = ? AND status = ?
		ORDER BY updated_at DESC LIMIT ?`, deviceID, PhoneMediaFailed, limit)
	if err != nil {
		return nil, fmt.Errorf("查询待重试媒体: %w", err)
	}
	defer rows.Close()

	out := make([]PhoneMedia, 0, limit)
	for rows.Next() {
		m, err := scanPhoneMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// PhoneMediaCounts 按状态统计账本条目，用于设备列表展示。
func (s *Store) PhoneMediaCounts(ctx context.Context, deviceID int64) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT status, count(*) FROM phone_media WHERE device_id = ? GROUP BY status`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("统计同步账本: %w", err)
	}
	defer rows.Close()

	out := map[string]int64{}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[status] = n
	}
	return out, rows.Err()
}
