package store

// 赛历（马拉松赛事日历）的存储。
//
// 与活动/成绩不同，赛历是**全局数据**，不挂档案：全国就那一份，多个档案共用。
// 所以它不进 athlete_id 的作用域，也不参与导出/导入。

import (
	"fmt"
	"sort"

	"github.com/fghosth/openrunner/core/races"
	"gorm.io/gorm"
)

// RaceEventModel 是一条赛历赛事。字段直接对齐上游归一后的形态，
// 地址三段存开（而不是存整串）是为了能在 SQL 里按省筛。
type RaceEventModel struct {
	ID     uint   `gorm:"primaryKey" json:"id"`
	RaceID string `gorm:"size:32;uniqueIndex;not null" json:"raceId"`
	Name   string `gorm:"size:191" json:"name"`
	Date   string `gorm:"size:10;index" json:"date"` // YYYY-MM-DD
	Year   int    `gorm:"index" json:"year"`
	Grade  string `gorm:"size:32" json:"grade"` // A / B / C（属地办赛）/ 系列赛
	// Province 是省级单位名（含自治区/直辖市/特别行政区），与前端 GeoJSON 的
	// properties.name 对齐后才能着色，所以**不做任何简称归一**。
	Province   string `gorm:"size:32;index" json:"province"`
	City       string `gorm:"size:32;index" json:"city"`
	District   string `gorm:"size:32" json:"district"`
	Categories string `gorm:"size:32" json:"categories"` // "full,half,10k"
	Scale      string `gorm:"size:64" json:"scale"`
	FetchedAt  int64  `json:"fetchedAt"`
	// Registration 是外挂的报名信息（最酷），**不是本表的列**：赛历整表替换会重建行，
	// 报名信息来自另一个源、按 race_id 单独存表，读列表时再挂上来。
	// gorm:"-" 让它不参与建表与读写，encoding/json 照常序列化。
	Registration *RaceRegistrationModel `gorm:"-" json:"registration,omitempty"`
}

func (RaceEventModel) TableName() string { return "race_events" }

// Categories 还原项目列表。
func (r RaceEventModel) CategoriesList() []races.Category { return races.SplitCategories(r.Categories) }

// Has 判断这条赛事是否含某项目。
func (r RaceEventModel) Has(c races.Category) bool {
	for _, x := range r.CategoriesList() {
		if x == c {
			return true
		}
	}
	return false
}

// RaceCalendarMetaModel 记录赛历的同步状态（单行，ID 固定为 1）。
type RaceCalendarMetaModel struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	LastSyncAt int64  `json:"lastSyncAt"`
	Total      int    `json:"total"`
	LastError  string `gorm:"size:512" json:"lastError"`
}

func (RaceCalendarMetaModel) TableName() string { return "race_calendar_meta" }

const raceCalendarMetaID = 1

// GetRaceCalendarMeta 取同步状态；没有记录时返回零值（不报错，新装实例就是这样）。
func GetRaceCalendarMeta(db *gorm.DB) (*RaceCalendarMetaModel, error) {
	var m RaceCalendarMetaModel
	err := db.First(&m, raceCalendarMetaID).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return &RaceCalendarMetaModel{ID: raceCalendarMetaID}, nil
		}
		return nil, err
	}
	return &m, nil
}

// SaveRaceCalendarMeta 更新同步状态（upsert 单行）。
func SaveRaceCalendarMeta(db *gorm.DB, lastSyncAt int64, total int, lastErr string) error {
	m := RaceCalendarMetaModel{ID: raceCalendarMetaID, LastSyncAt: lastSyncAt, Total: total, LastError: lastErr}
	var exists int64
	if err := db.Model(&RaceCalendarMetaModel{}).Where("id = ?", raceCalendarMetaID).Count(&exists).Error; err != nil {
		return err
	}
	if exists == 0 {
		return db.Create(&m).Error
	}
	return db.Model(&RaceCalendarMetaModel{}).Where("id = ?", raceCalendarMetaID).
		Updates(map[string]any{"last_sync_at": m.LastSyncAt, "total": m.Total, "last_error": m.LastError}).Error
}

// ReplaceRaceEvents **整表替换**赛历。
//
// 为什么不是增量 upsert：上游会删赛、改期、改项目，增量合并必然留下幽灵行，
// 而全量才三千行 —— 一个事务里换掉最便宜，也最不容易出错。
// 事务保证中途失败不会留下半张表（那会让地图只显示半个中国）。
func ReplaceRaceEvents(db *gorm.DB, rows []RaceEventModel) (int, error) {
	now := db.NowFunc().Unix()
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&RaceEventModel{}).Error; err != nil {
			return err
		}
		for i := range rows {
			rows[i].ID = 0
			rows[i].FetchedAt = now
		}
		// 分批插：一次三万行会让 SQLite 的语句变量数爆掉
		const batch = 200
		for i := 0; i < len(rows); i += batch {
			end := i + batch
			if end > len(rows) {
				end = len(rows)
			}
			if err := tx.Create(rows[i:end]).Error; err != nil {
				return fmt.Errorf("写入第 %d~%d 条失败: %w", i, end, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// CountRaceEventsBeforeYear 数出「早于某年」的残留行数。
//
// 存在的理由：core/races 的 keepYear 只过滤**新拉**的数据，而调度器只在表为空时补拉，
// 所以任何在过滤生效前同步过的实例，旧年份会一直挂到下一次整表替换（最长 7 天）。
// 启动时拿它判断要不要自愈，比等定时 tick 诚实。
func CountRaceEventsBeforeYear(db *gorm.DB, year int) (int64, error) {
	var n int64
	err := db.Model(&RaceEventModel{}).Where("year < ?", year).Count(&n).Error
	return n, err
}

// DeleteRaceEventsBeforeYear 删掉早于某年的赛历行，返回删除条数。
//
// 这是 keepYear 的**数据侧对应物**：过滤只管进、不管清旧，这里补上清旧。
// 删的正好是 keepYear 下一次也不会留下的行，所以重复执行是幂等的，
// 就算紧接着的整表替换把它们再带回来也无所谓。
func DeleteRaceEventsBeforeYear(db *gorm.DB, year int) (int64, error) {
	res := db.Where("year < ?", year).Delete(&RaceEventModel{})
	return res.RowsAffected, res.Error
}

// CountRaceEvents 返回当前库里的赛事条数（调度器用它判断「空表就先拉一次」）。
func CountRaceEvents(db *gorm.DB) (int64, error) {
	var n int64
	err := db.Model(&RaceEventModel{}).Count(&n).Error
	return n, err
}

// RaceFilter 是赛历查询条件；零值表示不限。
type RaceFilter struct {
	Province string
	City     string
	District string
	Year     int
	Category string // full / half / 10k
	// Grade 是上游的赛事级别原文（A / B / C（属地办赛）/ 系列赛），**精确匹配**：
	// 值里带「（）」和子串关系，LIKE 会误命中。空串表示不限。
	Grade string
	Name  string // 名称模糊匹配
	// Limit 只作用于 ListRaceEvents，**故意不进 apply**：apply 也被 RaceSummary 用，
	// 一旦把条数限制带进去，省/市/区的计数就会被悄悄截断。
	Limit int
}

func (f RaceFilter) apply(q *gorm.DB) *gorm.DB {
	if v, ok := matchValue(f.Province); ok {
		q = q.Where("province = ?", v)
	}
	if v, ok := matchValue(f.City); ok {
		q = q.Where("city = ?", v)
	}
	if v, ok := matchValue(f.District); ok {
		q = q.Where("district = ?", v)
	}
	if f.Year > 0 {
		q = q.Where("year = ?", f.Year)
	}
	// 项目过滤只认固定词表，然后直接 LIKE 子串：SQLite 与 MySQL 都支持 || 拼接的写法不统一，
	// 而 full/half/10k 三个 token 互不为子串，裸 LIKE 不会误命中。
	if _, ok := races.ParseCategory(f.Category); ok {
		q = q.Where("categories LIKE ?", "%"+f.Category+"%")
	}
	if v, ok := matchValue(f.Grade); ok {
		q = q.Where("grade = ?", v)
	}
	if f.Name != "" {
		q = q.Where("name LIKE ?", "%"+f.Name+"%")
	}
	return q
}

// matchValue 把筛选值翻成 SQL 条件值。空串表示「不限」；「未标注」是聚合树给缺字段
// 那批起的占位名，翻回空串才能真查到它们 —— 否则树上点得到的条目，列表却是空的。
func matchValue(v string) (string, bool) {
	if v == "" {
		return "", false
	}
	if v == unlabeled {
		return "", true
	}
	return v, true
}

// unlabeled 是上游缺字段时在聚合树里用的占位名（省 / 市 / 级别共用，前端原样显示）。
const unlabeled = "未标注"

// maxRaceListRows 是单次列表查询的硬上限（全量约 3000 条，全表吐给一个 HTTP 请求没意义）。
const maxRaceListRows = 2000

// ListRaceEvents 按条件取赛事，日期倒序（最近的在前 —— 赛历是拿来找下一场比赛的，
// 不是拿来考古的；配合 Limit 时截掉的也是陈年旧赛）。
func ListRaceEvents(db *gorm.DB, f RaceFilter) ([]RaceEventModel, error) {
	var out []RaceEventModel
	limit := f.Limit
	if limit <= 0 || limit > maxRaceListRows {
		limit = maxRaceListRows
	}
	err := f.apply(db.Model(&RaceEventModel{})).
		Order("date DESC, race_id DESC").Limit(limit).Find(&out).Error
	return out, err
}

// RaceDistrictCount 是一个区/县的场次。
type RaceDistrictCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// RaceCityCount 是一个城市的场次，带它下面的区。
//
// 注意 Count 是**该市全部**场次，包含没有区信息的那些；所以 Count ≥ Σ Districts.Count，
// 差值就是「上游没给区」的那批。刻意不把无区的塞成一个空名区 —— 那会让 UI 出现一个
// 点不动的假条目。
type RaceCityCount struct {
	Name      string              `json:"name"`
	Count     int                 `json:"count"`
	Districts []RaceDistrictCount `json:"districts"`
}

// RaceProvinceCount 是一个省级单位的场次，带它下面的市。
type RaceProvinceCount struct {
	Name   string          `json:"name"`
	Count  int             `json:"count"`
	Cities []RaceCityCount `json:"cities"`
}

// RaceNameCount 是「一个字符串取值 → 场次」，给级别这类由上游决定的枚举用。
type RaceNameCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// RaceMapSummary 是地图页一次要拿回的全部聚合信息。
type RaceMapSummary struct {
	Provinces []RaceProvinceCount `json:"provinces"`
	Years     []int               `json:"years"`
	// Grades 是级别 → 场次，按场次降序。级别取值由上游决定（会漂移），
	// 所以让前端跟着这里渲染，而不是写死一份选项。
	Grades     []RaceNameCount `json:"grades"`
	Total      int             `json:"total"`
	LastSyncAt int64           `json:"lastSyncAt"`
	LastError  string          `json:"lastError"`
}

// RaceSummary 算出省 → 市 → 区 的场次树。
//
// 全量才三千行，直接捞进内存聚合，不写方言相关的 GROUP BY ——
// 仓库同时支持 SQLite 与 MySQL，靠 SQL 拼三层下钻会把两边都写一遍。
// filter 里的省/市/区不参与（那是下钻用的），只按年份与项目筛。
func RaceSummary(db *gorm.DB, f RaceFilter) (*RaceMapSummary, error) {
	f.Province, f.City, f.District = "", "", ""
	var all []RaceEventModel
	if e := f.apply(db.Model(&RaceEventModel{})).Order("date ASC").Limit(5000).Find(&all).Error; e != nil {
		return nil, e
	}

	type cityAgg struct {
		count     int
		districts map[string]int
	}
	type agg struct {
		count  int
		cities map[string]*cityAgg
	}
	provs := map[string]*agg{}
	years := map[int]bool{}
	grades := map[string]int{}
	for _, r := range all {
		p := r.Province
		if p == "" {
			p = unlabeled
		}
		pa := provs[p]
		if pa == nil {
			pa = &agg{cities: map[string]*cityAgg{}}
			provs[p] = pa
		}
		pa.count++
		c := r.City
		if c == "" {
			c = unlabeled
		}
		ca := pa.cities[c]
		if ca == nil {
			ca = &cityAgg{districts: map[string]int{}}
			pa.cities[c] = ca
		}
		ca.count++
		if r.District != "" {
			ca.districts[r.District]++
		}
		if r.Year > 0 {
			years[r.Year] = true
		}
		g := r.Grade
		if g == "" {
			g = unlabeled
		}
		grades[g]++
	}

	out := &RaceMapSummary{Total: len(all), Years: []int{}}
	for y := range years {
		out.Years = append(out.Years, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(out.Years)))
	out.Grades = []RaceNameCount{}
	for g, n := range grades {
		out.Grades = append(out.Grades, RaceNameCount{Name: g, Count: n})
	}
	// 级别取值由上游决定，所以这里按场次排；空值（未标注）永远垫底
	sort.Slice(out.Grades, func(i, j int) bool {
		if out.Grades[i].Name == unlabeled {
			return false
		}
		if out.Grades[j].Name == unlabeled {
			return true
		}
		if out.Grades[i].Count != out.Grades[j].Count {
			return out.Grades[i].Count > out.Grades[j].Count
		}
		return out.Grades[i].Name < out.Grades[j].Name
	})

	byCount := func(a, b string, ca, cb int) bool {
		if ca != cb {
			return ca > cb
		}
		return a < b
	}
	for name, pa := range provs {
		pc := RaceProvinceCount{Name: name, Count: pa.count}
		for cname, ca := range pa.cities {
			cc := RaceCityCount{Name: cname, Count: ca.count}
			for dname, n := range ca.districts {
				cc.Districts = append(cc.Districts, RaceDistrictCount{Name: dname, Count: n})
			}
			sort.Slice(cc.Districts, func(i, j int) bool {
				return byCount(cc.Districts[i].Name, cc.Districts[j].Name, cc.Districts[i].Count, cc.Districts[j].Count)
			})
			pc.Cities = append(pc.Cities, cc)
		}
		sort.Slice(pc.Cities, func(i, j int) bool {
			return byCount(pc.Cities[i].Name, pc.Cities[j].Name, pc.Cities[i].Count, pc.Cities[j].Count)
		})
		out.Provinces = append(out.Provinces, pc)
	}
	sort.Slice(out.Provinces, func(i, j int) bool {
		return byCount(out.Provinces[i].Name, out.Provinces[j].Name, out.Provinces[i].Count, out.Provinces[j].Count)
	})
	if out.Provinces == nil {
		out.Provinces = []RaceProvinceCount{}
	}

	if meta, err := GetRaceCalendarMeta(db); err == nil {
		out.LastSyncAt = meta.LastSyncAt
		out.LastError = meta.LastError
	}
	return out, nil
}
