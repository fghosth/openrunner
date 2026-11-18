package store

// 报名数据（报名窗口与价格）的存储。
//
// **为什么不直接给 race_events 加列**：赛历每次同步是整表替换
// （ReplaceRaceEvents 先 DELETE 再插），而报名信息来自另一个源、另一套节奏。
// 塞在同一行里，每周的全量替换会把它们抹掉；挂在自增主键上也不行，
// 替换之后自增 id 全变。所以按 **race_id（上游稳定键）** 外挂一张表。
//
// 覆盖率天生不满：最酷是报名平台不是全量赛历，大田协赛常在数字心动/马拉马拉上架。
// 没配上的比赛这里就没有行，前端据此退到「复制赛事名去官方渠道」而不是显示空价格。

import (
	"gorm.io/gorm"
)

// RaceRegistrationModel 是一场比赛的报名信息（来源：最酷）。
type RaceRegistrationModel struct {
	ID        uint    `gorm:"primaryKey" json:"-"`
	RaceID    string  `gorm:"size:32;uniqueIndex;not null" json:"-"`
	Source    string  `gorm:"size:16" json:"source"`   // 目前只有 zuicool
	SourceID  string  `gorm:"size:32" json:"sourceId"` // 最酷 event id
	RegStart  string  `gorm:"size:19" json:"regStart"` // YYYY-MM-DDTHH:MM:SS，空 = 上游没写
	RegEnd    string  `gorm:"size:19" json:"regEnd"`   // 报名截止
	MinPrice  float64 `json:"minPrice"`                // 各组别最低起价（元）；0 = 没解析到
	RegURL    string  `gorm:"size:255" json:"regUrl"`  // 报名页
	FetchedAt int64   `json:"fetchedAt"`
}

func (RaceRegistrationModel) TableName() string { return "race_registrations" }

// ReplaceRaceRegistrations 整表替换报名信息，返回写入条数。
//
// 与赛历同理做整表替换而不是逐条 upsert：来源是「当前在报的全集」，
// 下架的比赛必须跟着消失，否则会一直显示一个已经关掉的截止日期。
func ReplaceRaceRegistrations(db *gorm.DB, rows []RaceRegistrationModel) (int, error) {
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&RaceRegistrationModel{}).Error; err != nil {
			return err
		}
		const chunk = 200
		for i := 0; i < len(rows); i += chunk {
			end := i + chunk
			if end > len(rows) {
				end = len(rows)
			}
			if err := tx.Create(rows[i:end]).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// RaceRegistrationsByRaceIDs 按 race_id 批量取报名信息。
// 空入参直接返回空 map，避免拼出 `IN ()` 这种非法 SQL。
func RaceRegistrationsByRaceIDs(db *gorm.DB, ids []string) (map[string]RaceRegistrationModel, error) {
	out := make(map[string]RaceRegistrationModel, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []RaceRegistrationModel
	if err := db.Where("race_id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.RaceID] = r
	}
	return out, nil
}

// CountRaceRegistrations 是已配上报名信息的场次（给刷新结果与排障用）。
func CountRaceRegistrations(db *gorm.DB) (int64, error) {
	var n int64
	err := db.Model(&RaceRegistrationModel{}).Count(&n).Error
	return n, err
}
