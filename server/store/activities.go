package store

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/openrunner/core"
)

// CreateActivityWithStream 事务写入 Activity 与其 Stream。
// ErrSportNotCollected 表示该运动类型不在收录范围内（只收跑步与骑行）。
var ErrSportNotCollected = errors.New("sport not collected")

func CreateActivityWithStream(db *gorm.DB, athleteID uint, act core.Activity, stream core.Stream, source, contentHash string) (uint, error) {
	// 最后一道闸：无论从哪条路径进来（单文件导入/批量/云同步/未来的新入口），
	// 未收录的运动类型都不落库，避免规则在多个 handler 里漂移。
	if !core.IsCollectedSport(act.Sport) {
		return 0, ErrSportNotCollected
	}
	// 摘要心率：上游（FIT session）常常不给，就从心率通道算一个均值。
	// 汇总页的「平均心率」、资料卡、以及对无 stream 活动的估算都依赖它。
	avgHr := act.AvgHr
	if avgHr <= 0 && len(stream.HeartRate) > 0 {
		sum, n := 0.0, 0
		for _, v := range stream.HeartRate {
			if v > 0 {
				sum += v
				n++
			}
		}
		if n > 0 {
			avgHr = sum / float64(n)
		}
	}
	m := ActivityModel{
		AthleteID:      athleteID,
		Sport:          string(act.Sport),
		StartTime:      act.StartTime.Unix(),
		DurationSecs:   int(act.Duration.Seconds()),
		DistanceM:      act.Distance,
		TimerSecs:      act.TimerSecs,
		ElevationGainM: act.ElevationGain,
		IsPlanned:      act.IsPlanned,
		AvgHr:          avgHr,
		HasStream:      stream.Seconds() > 0,
		HasGps:         streamHasGps(stream),
		Source:         source,
	}
	if contentHash != "" {
		m.ContentHash = &contentHash
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

// streamHasGps 判断 Stream 是否含至少两个有效坐标点（画一条线的最低要求）。
func streamHasGps(stream core.Stream) bool {
	valid := 0
	n := len(stream.Latitude)
	if len(stream.Longitude) < n {
		n = len(stream.Longitude)
	}
	for i := 0; i < n; i++ {
		if core.IsValidCoordinate(stream.Latitude[i], stream.Longitude[i]) {
			valid++
			if valid >= 2 {
				return true
			}
		}
	}
	return false
}

// ErrActivityDuplicate 表示同一活动已存在（业务键去重命中）。
var ErrActivityDuplicate = errors.New("activity already exists")

// FindActivityByStartAndSport 按「开始时间 + 运动类型」查重。
//
// 云同步必须用它而不是 content_hash：实测同一活动每次下载的 FIT 字节并不相同
// （hash 每次都变），导致 content_hash 去重失效、同一活动被反复 imported，
// 数据无限膨胀。而「开始时间 + 类型」对同一活动是稳定的。
func FindActivityByStartAndSport(db *gorm.DB, athleteID uint, startTime int64, sport string) (*ActivityModel, error) {
	var m ActivityModel
	err := db.Where("athlete_id = ? AND start_time = ? AND sport = ?", athleteID, startTime, sport).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// FindActivityByHash 按内容指纹查重。
func FindActivityByHash(db *gorm.DB, athleteID uint, hash string) (*ActivityModel, error) {
	if hash == "" {
		return nil, nil // 无指纹不查（手工活动）
	}
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

// ErrActivityNotFound 档案下不存在该活动（删除/更新共用）。
var ErrActivityNotFound = errors.New("activity not found")

// DeleteActivity 删除活动及其 Stream 与指标快照（同事务）。
// 允许删除任何来源——自己的数据自己清理；平台活动删除后下次同步会按业务键重新导入。
func DeleteActivity(db *gorm.DB, athleteID, id uint) error {
	return db.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("athlete_id = ? AND id = ?", athleteID, id).Delete(&ActivityModel{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrActivityNotFound
		}
		if err := tx.Where("activity_id = ?", id).Delete(&ActivityStreamModel{}).Error; err != nil {
			return err
		}
		return tx.Where("activity_id = ?", id).Delete(&ActivityMetricsModel{}).Error
	})
}

// UpdateManualActivity 更新手工/计划活动的日期、时长、距离（仅 source=manual）。
// 返回更新后的行；指标重算由调用方负责（需要档案上下文）。
func UpdateManualActivity(db *gorm.DB, athleteID, id uint, startTimeUnix int64, durationSecs int, distanceM float64) (*ActivityModel, error) {
	var m ActivityModel
	err := db.Where("athlete_id = ? AND id = ? AND source = ?", athleteID, id, "manual").First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrActivityNotFound
		}
		return nil, err
	}
	m.StartTime = startTimeUnix
	m.DurationSecs = durationSecs
	m.TimerSecs = durationSecs // 手工活动无停表概念，两口径一致
	m.DistanceM = distanceM
	if err := db.Save(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}
