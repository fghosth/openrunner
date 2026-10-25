package store

import (
	"errors"

	"gorm.io/gorm"
)

// ErrPlatformNotFound 平台连接不存在。
var ErrPlatformNotFound = errors.New("平台连接不存在")

// SavePlatformConnection 创建/更新平台连接（凭证已由调用方加密）。
func SavePlatformConnection(db *gorm.DB, c *PlatformConnectionModel) error {
	if c.AthleteID == 0 {
		return &ValidationError{"缺少档案"}
	}
	if c.Platform == "" {
		return &ValidationError{"缺少平台标识"}
	}
	if len(c.CredentialEnc) == 0 {
		return &ValidationError{"缺少凭证"}
	}
	if c.ID == 0 {
		// 同档案同平台只允许一条（重连即覆盖）
		var existing PlatformConnectionModel
		err := db.Where("athlete_id = ? AND platform = ?", c.AthleteID, c.Platform).First(&existing).Error
		if err == nil {
			c.ID = existing.ID
			c.SyncCursor = existing.SyncCursor // 保留游标
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if c.ID == 0 {
		return db.Create(c).Error
	}
	return db.Model(&PlatformConnectionModel{}).Where("id = ?", c.ID).
		Updates(map[string]any{
			"credential_enc": c.CredentialEnc,
			"enabled":        c.Enabled,
			"sync_cursor":    c.SyncCursor,
		}).Error
}

// ListPlatformConnections 列出档案的平台连接。
func ListPlatformConnections(db *gorm.DB, athleteID uint) ([]PlatformConnectionModel, error) {
	var out []PlatformConnectionModel
	if err := db.Where("athlete_id = ?", athleteID).Order("platform").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// GetPlatformConnection 取单个平台连接。
func GetPlatformConnection(db *gorm.DB, athleteID uint, platform string) (*PlatformConnectionModel, error) {
	var c PlatformConnectionModel
	err := db.Where("athlete_id = ? AND platform = ?", athleteID, platform).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPlatformNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// DeletePlatformConnection 删除平台连接。
func DeletePlatformConnection(db *gorm.DB, athleteID uint, platform string) error {
	res := db.Where("athlete_id = ? AND platform = ?", athleteID, platform).Delete(&PlatformConnectionModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrPlatformNotFound
	}
	return nil
}

// UpdateSyncState 更新同步结果（游标/时间/错误）。
func UpdateSyncState(db *gorm.DB, id uint, cursor string, at int64, lastErr string) error {
	var cursorPtr *string
	if cursor != "" {
		cursorPtr = &cursor
	}
	return db.Model(&PlatformConnectionModel{}).Where("id = ?", id).
		Updates(map[string]any{
			"sync_cursor":  cursorPtr,
			"last_sync_at": at,
			"last_error":   lastErr,
		}).Error
}
