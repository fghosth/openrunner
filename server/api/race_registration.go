package api

// 报名数据（最酷）的刷新与挂载。
//
// 为什么需要它：田协赛历只有比赛本身，**没有报名窗口、没有价格**。
// 最酷是官方报名渠道之一，页面上这两样是明文，所以能补上——但只能补上它代理的那部分，
// 大田协赛常在数字心动/马拉马拉上架，这里就是空的。空的处理见前端：
// 不显示假数字，退到「复制赛事名去官方渠道」。
//
// 抓取顺序刻意是「先列表、匹配上了才抓详情」：详情页是一页一个请求，
// 全量抓几百页既慢又没礼貌，而没匹配上的详情页对我们毫无用处。

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/fghosth/openrunner/core/zuicool"
	"github.com/fghosth/openrunner/server/store"
	"gorm.io/gorm"
)

// zuicoolBaseURL 可被测试覆盖（指向 httptest）。生产为空 → 用 core 侧默认地址。
var zuicoolBaseURL = ""

func newZuicoolClient() *zuicool.Client {
	c := zuicool.NewClient()
	if zuicoolBaseURL != "" {
		c.BaseURL = zuicoolBaseURL
	}
	return c
}

// matchMinRatio 是标题相似度阈值。宁缺毋假：配错会把用户骗到另一场比赛的报名页去。
const matchMinRatio = 0.75

// detailFetchLimit 是一次刷新最多抓多少个详情页。
// 匹配上的场次本来就少，这个上限只在上游异常时兜底，避免无界抓取。
const detailFetchLimit = 150

// detailInterval 是详情页之间的间隔。批量抓别人的页面要留余地，
// 单用户自托管的应用没有理由把人家打出流量尖峰。
const detailInterval = 200 * time.Millisecond

const regRefreshTimeout = 5 * time.Minute

// regHTTPBudget 是**手动刷新**给报名这一步的时间预算。
//
// 后台调度可以慢慢跑（5 分钟无所谓），手动刷新是用户点着等结果的，
// 不能跟着上游一起拖：实测最酷被限流时单页就能卡 30s，整条链能挂到 7 分钟，
// 按钮转圈转到人以为页面死了。超预算就交付已经抓到的那部分（见 partial）。
const regHTTPBudget = 90 * time.Second

var regRefreshMu sync.Mutex

// RefreshRaceRegistrations 拉最酷的报名数据，配上 race_id 后整表替换。
//
// 返回配上并写入的条数。一条都没配上**不算错误**（最酷本来就不代理全部赛事），
// 但解析器整体失灵会由 zuicool.ErrNoCards 冒出来，不会被当成「今天没比赛在报」。
func RefreshRaceRegistrations(db *gorm.DB) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), regRefreshTimeout)
	defer cancel()
	n, _, err := refreshRaceRegistrations(ctx, db)
	return n, err
}

// refreshRaceRegistrations 在 ctx 的预算内跑一次。
//
// 第二个返回值 partial 表示「预算用完了，只抓完一部分」。此时**仍然把已抓到的写库**：
// 抓到了 40 场却因为第 41 场太慢就整批丢掉，等于把已经确定的事实扔掉，
// 比只更新一半更糟。调用方要如实告诉用户这是部分结果。
func refreshRaceRegistrations(ctx context.Context, db *gorm.DB) (int, bool, error) {
	if !regRefreshMu.TryLock() {
		return 0, false, fmt.Errorf("%w（报名数据）", ErrRefreshInProgress)
	}
	defer regRefreshMu.Unlock()

	entries, err := newZuicoolClient().List(ctx)
	if err != nil {
		return 0, false, err
	}
	// 只对还没开始的比赛配报名：过去的比赛不需要报名信息
	var targets []zuicool.MatchTarget
	today := time.Now().Format("2006-01-02")
	all, err := store.ListRaceEvents(db, store.RaceFilter{Limit: 2000})
	if err != nil {
		return 0, false, err
	}
	for _, e := range all {
		if e.Date >= today {
			targets = append(targets, zuicool.MatchTarget{RaceID: e.RaceID, Name: e.Name, Date: e.Date})
		}
	}
	matched := zuicool.MatchList(entries, targets, matchMinRatio)
	if len(matched) == 0 {
		// 一张都没配上：清掉旧数据，否则会一直显示上周的截止日期
		if _, err := store.ReplaceRaceRegistrations(db, nil); err != nil {
			return 0, false, err
		}
		return 0, false, nil
	}

	rows := make([]store.RaceRegistrationModel, 0, len(matched))
	partial := false
	n := 0
	for _, c := range entries {
		raceID, ok := matched[c.ID]
		if !ok {
			continue
		}
		if n >= detailFetchLimit {
			break
		}
		n++
		if n > 1 {
			time.Sleep(detailInterval)
		}
		ev, err := newZuicoolClient().Detail(ctx, c.ID)
		if err != nil && ctx.Err() != nil {
			// 预算用完了。已抓到的照写（见函数注释），只是要如实标成部分结果。
			partial = true
			break
		}
		// 列表页的窗口先兜底，详情页拿到就覆盖。**RegKind 决定它落在哪一边**：
		// 「报名开始：11-22」的卡片把 11-22 当截止显示，就是告诉用户一件他做不到的事。
		reg := store.RaceRegistrationModel{
			RaceID:    raceID,
			Source:    "zuicool",
			SourceID:  c.ID,
			RegURL:    c.URL, // 默认给赛事页；确认能报了才换成组别表单
			FetchedAt: time.Now().Unix(),
		}
		if c.RegKind == "end" {
			reg.RegEnd = c.RegAt
		} else {
			reg.RegStart = c.RegAt
		}
		if err != nil {
			// 详情页抓不到不等于没有报名：列表页的窗口是真的，只是拿不到价格。
			// **必须记日志** —— 静默退回会让「全都缺价格」看起来像正常现象。
			// 但「最酷上这场没挂组别」是正常数据形态（实测 51 场里 20 场如此），
			// 和真的抓失败分开记，否则每轮 20 条假警报会把真故障埋掉。
			if errors.Is(err, zuicool.ErrNoPackages) {
				log.Printf("报名详情：%s(%s) 最酷没挂组别与窗口，沿用列表页窗口", c.Title, c.ID)
			} else {
				log.Printf("报名详情失败 %s(%s): %v", c.Title, c.ID, err)
			}
		} else {
			if ev.RegEnd != "" {
				reg.RegEnd = ev.RegEnd
				reg.RegStart = ev.RegStart // 详情页给了完整窗口，列表页那份就作废
			}
			reg.MinPrice = ev.MinPrice
			// 只有真的在报名中才给直达表单；「即将报名」给表单会把人送到一个还没开的页
			if c.Status == zuicool.StatusOpen {
				for _, p := range ev.Packages {
					if p.Open && p.RegURL != "" {
						reg.RegURL = p.RegURL
						break
					}
				}
			}
		}
		rows = append(rows, reg)
	}
	if _, err := store.ReplaceRaceRegistrations(db, rows); err != nil {
		return 0, false, err
	}
	return len(rows), partial, nil
}

// refreshRaceRegistrationsLogged 是调度/串联用的日志包装。
func refreshRaceRegistrationsLogged(db *gorm.DB, why string) {
	n, err := RefreshRaceRegistrations(db)
	if err != nil {
		log.Printf("报名数据%s失败: %v", why, err)
		return
	}
	log.Printf("报名数据%s完成：配上 %d 场", why, n)
}

// attachRaceRegistrations 把报名信息挂到列表上（一次 IN 查询，不是 N+1）。
func attachRaceRegistrations(db *gorm.DB, list []store.RaceEventModel) {
	ids := make([]string, 0, len(list))
	for _, e := range list {
		ids = append(ids, e.RaceID)
	}
	reg, err := store.RaceRegistrationsByRaceIDs(db, ids)
	if err != nil {
		// 挂不上不影响主数据能看，退化成「没有报名信息」即可
		log.Printf("读取报名信息失败: %v", err)
		return
	}
	for i := range list {
		if r, ok := reg[list[i].RaceID]; ok {
			p := r
			list[i].Registration = &p
		}
	}
}
