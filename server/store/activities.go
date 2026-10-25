package store

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/openrunner/core"
)

// CreateActivityWithStream 事务写入 Activity 与其 Stream。
func CreateActivityWithStream(db *gorm.DB, athleteID uint, act core.Activity, stream core.Stream, source, contentHash string) (uint, error) {
	m := ActivityModel{
		AthleteID:      athleteID,
		Sport:          string(act.Sport),
		StartTime:      act.StartTime.Unix(),
		DurationSecs:   int(act.Duration.Seconds()),
		DistanceM:      act.Distance,
		TimerSecs:      act.TimerSecs,
		ElevationGainM: act.ElevationGain,
		IsPlanned:      act.IsPlanned,
		AvgHr:          act.AvgHr,
		HasStream:      stream.Seconds() > 0,
		Source:         source,
		ContentHash:    contentHash,
	}
	var streamModel *ActivityStreamModel
	if m.HasStream {
		blob, err := stream.Marshal()
		if err != nil {
			return 0, fmt.Errorf("序列化 Stream 失败: %w", err)
		}
		streamModel = &ActivityStreamModel{RecIntervalSecs: stream.RecIntervalSecs, Data: blob}
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&m).Error; err != nil {
			return err
		}
		if streamModel != nil {
			streamModel.ActivityID = m.ID
			return tx.Create(streamModel).Error
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return m.ID, nil
}

// FindActivityByHash 按内容指纹查重。
func FindActivityByHash(db *gorm.DB, athleteID uint, hash string) (*ActivityModel, error) {
	var m ActivityModel
	err := db.Where("athlete_id = ? AND content_hash = ?", athleteID, hash).First(&m).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ListActivities 按开始时间倒序列出档案活动。
func ListActivities(db *gorm.DB, athleteID uint) ([]ActivityModel, error) {
	var out []ActivityModel
	if err := db.Where("athlete_id = ?", athleteID).Order("start_time DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// GetActivity 按 id 取单个活动（限定归属）。
func GetActivity(db *gorm.DB, athleteID, id uint) (*ActivityModel, error) {
	var m ActivityModel
	if err := db.Where("athlete_id = ? AND id = ?", athleteID, id).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// UpdateAthlete 更新名称与性别。
func UpdateAthlete(db *gorm.DB, id uint, name, sex string) error {
	updates := map[string]any{}
	if name != "" {
		updates["name"] = name
	}
	if sex == "M" || sex == "F" {
		updates["sex"] = sex
	}
	if len(updates) == 0 {
		return errors.New("无可更新字段")
	}
	return db.Model(&AthleteModel{}).Where("id = ?", id).Updates(updates).Error
}

// GetAthlete 按 id 取档案。
func GetAthlete(db *gorm.DB, id uint) (*AthleteModel, error) {
	var m AthleteModel
	if err := db.First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// GetStream 取活动的 Stream BLOB。
func GetStream(db *gorm.DB, activityID uint) (*ActivityStreamModel, error) {
	var m ActivityStreamModel
	if err := db.Where("activity_id = ?", activityID).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// AthleteExists 校验档案存在。
func AthleteExists(db *gorm.DB, athleteID uint) (bool, error) {
	var count int64
	if err := db.Model(&AthleteModel{}).Where("id = ?", athleteID).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
