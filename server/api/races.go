package api

// 赛历（马拉松赛事日历）接口。
//
// 与活动/成绩不同，赛历是**全局数据**：全国一份，所有档案共用，所以路由不挂在
// /athletes/{id} 下面。来源接口无鉴权无验证码，因此可以全自动定期拉取 ——
// 这点和成绩查询那条必须人来过验证码的路子完全不同。

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fghosth/openrunner/core/races"
	"github.com/fghosth/openrunner/server/store"
	"gorm.io/gorm"
)

// racesBaseURL 可被测试覆盖（指向 httptest）。生产为空 → 用 core 侧默认地址。
var racesBaseURL = ""

func newRacesClient() *races.Client {
	c := races.NewClient()
	if racesBaseURL != "" {
		c.BaseURL = racesBaseURL
	}
	return c
}

// raceRefreshTimeout 是一次全量拉取的预算。七页请求，正常几秒就完；
// 给 2 分钟是为了上游慢的时候不至于吊死连接。
const raceRefreshTimeout = 2 * time.Minute

// raceRefreshMu 保证同一时刻只有一次拉取在跑：手动点「立即更新」和每周定时
// 撞上的时候，打两次上游纯属浪费，还可能被风控。
var raceRefreshMu sync.Mutex

// ErrRefreshInProgress 是「已经有一次刷新在跑」的哨兵错误。
// 用 errors.Is 判断而不是 strings.Contains(err,"进行中") —— 后者把两条不同刷新
// 的文案耦合在一起，改一个字就静默把 409 降级成 502。
var ErrRefreshInProgress = errors.New("已有一次更新在进行中，请稍后再试")

// RaceRefreshResult 是一次刷新的结果。
type RaceRefreshResult struct {
	Imported   int   `json:"imported"`
	LastSyncAt int64 `json:"lastSyncAt"`
}

// RefreshRaceCalendar 拉一次全量并整表替换。手动与定时共用这一份实现。
//
// 失败时把原因写进 meta 但不覆盖上次的成功时间 —— 地图页要能同时说清
// 「数据是 3 天前更新的」和「最近一次更新失败了」。
func RefreshRaceCalendar(db *gorm.DB) (*RaceRefreshResult, error) {
	if !raceRefreshMu.TryLock() {
		return nil, ErrRefreshInProgress
	}
	defer raceRefreshMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), raceRefreshTimeout)
	defer cancel()

	list, err := newRacesClient().ListAll(ctx)
	meta, _ := store.GetRaceCalendarMeta(db)
	if err != nil {
		msg := strings.TrimPrefix(err.Error(), "races: ")
		if len(msg) > 480 {
			msg = msg[:480]
		}
		_ = store.SaveRaceCalendarMeta(db, meta.LastSyncAt, meta.Total, msg)
		return nil, err
	}
	rows := make([]store.RaceEventModel, 0, len(list))
	for _, r := range list {
		rows = append(rows, store.RaceEventModel{
			RaceID: r.ID, Name: r.Name, Date: r.Date, Year: r.Year(), Grade: r.Grade,
			Province: r.Province, City: r.City, District: r.District,
			Categories: races.JoinCategories(r.Categories), Scale: r.Scale,
		})
	}
	n, err := store.ReplaceRaceEvents(db, rows)
	if err != nil {
		_ = store.SaveRaceCalendarMeta(db, meta.LastSyncAt, meta.Total, "写入失败: "+err.Error())
		return nil, err
	}
	at := time.Now().Unix()
	if err := store.SaveRaceCalendarMeta(db, at, n, ""); err != nil {
		return nil, err
	}
	return &RaceRefreshResult{Imported: n, LastSyncAt: at}, nil
}

// StartRaceCalendarScheduler 启动赛历定期更新循环（由 main 调用一次，ctx 取消即停）。
//
// 循环体放在 api 包而不是 main，是为了能连着 racesBaseURL 一起测 ——
// 调度逻辑最容易出错的地方正是「什么时候真的去打上游」。
func StartRaceCalendarScheduler(ctx context.Context, db *gorm.DB, interval time.Duration) {
	if interval <= 0 {
		interval = defaultRaceRefreshInterval
	}
	go func() {
		// 旧年份自愈：keepYear 只过滤新拉的数据，而下面只在**空表**时补拉，
		// 所以升级前同步过的实例会把旧年份挂到下一次定时替换（最长 7 天）。
		// 启动时直接清掉 —— 删的正是 keepYear 下次也不会留下的行，幂等且不用打上游。
		if n, err := store.CountRaceEventsBeforeYear(db, time.Now().Year()); err == nil && n > 0 {
			if del, err := store.DeleteRaceEventsBeforeYear(db, time.Now().Year()); err == nil {
				log.Printf("赛历自愈：清掉早于 %d 年的残留 %d 行", time.Now().Year(), del)
			} else {
				log.Printf("赛历自愈失败（清旧年份）: %v", err)
			}
		}
		// 空表先拉一次：否则新装实例的地图要空整整一周，看起来像坏了
		if n, err := store.CountRaceEvents(db); err == nil && n == 0 {
			refreshRaceCalendarLogged(db, "首次拉取")
		}
		for {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			refreshRaceCalendarLogged(db, "定时更新")
		}
	}()
}

// defaultRaceRefreshInterval 是赛历的更新周期。上游是官方赛历，
// 一周足够 —— 更勤快只会增加被风控的概率，赛事也不会变那么快。
const defaultRaceRefreshInterval = 7 * 24 * time.Hour

func refreshRaceCalendarLogged(db *gorm.DB, why string) {
	res, err := RefreshRaceCalendar(db)
	if err != nil {
		log.Printf("赛历%s失败: %v", why, err)
		return
	}
	log.Printf("赛历%s完成：写入 %d 场", why, res.Imported)
}

// raceFilterFromQuery 把 URL 查询参数翻成 store 层筛选条件。
func raceFilterFromQuery(r *http.Request) store.RaceFilter {
	f := store.RaceFilter{
		Province: strings.TrimSpace(r.URL.Query().Get("province")),
		City:     strings.TrimSpace(r.URL.Query().Get("city")),
		District: strings.TrimSpace(r.URL.Query().Get("district")),
		Category: strings.TrimSpace(r.URL.Query().Get("category")),
		Name:     strings.TrimSpace(r.URL.Query().Get("name")),
		// 级别是上游原文，精确匹配；「未标注」是 summary 里空值的占位名，翻回 IS NULL/空
		Grade: strings.TrimSpace(r.URL.Query().Get("grade")),
	}
	if y, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("year"))); err == nil && y > 0 {
		f.Year = y
	}
	// 默认 500 条（一个省全年的场次在这个量级）。非法值保持默认，
	// 不能退化成「不限量」把整表吐出去。
	f.Limit = 500
	if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			f.Limit = n
		}
	}
	return f
}

// handleRacesSummary 返回省→市→区场次树 + 同步状态，地图页一次取回。
func handleRacesSummary(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sum, err := store.RaceSummary(db, raceFilterFromQuery(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusOK, sum)
	}
}

// handleRacesList 返回筛选后的赛事列表（日期倒序，默认最多 500 条）。
func handleRacesList(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := store.ListRaceEvents(db, raceFilterFromQuery(r))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "查询失败")
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

// handleRacesRefresh 手动更新赛历。
func handleRacesRefresh(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := RefreshRaceCalendar(db)
		if err != nil {
			// 上游被挡/限流与本地写库失败要分开：前者重试有用，后者重试没用
			status := http.StatusBadGateway
			if errors.Is(err, ErrRefreshInProgress) {
				status = http.StatusConflict
			}
			writeError(w, status, strings.TrimPrefix(err.Error(), "races: "))
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}
