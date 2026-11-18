package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fghosth/openrunner/server/store"
	"gorm.io/gorm"
)

// 报名数据接口测试：最酷一律 httptest 回放，绝不碰真实站点。

func regTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "reg.db"))
	if err != nil {
		t.Fatalf("建库: %v", err)
	}
	return db.Gorm()
}

func inDays(n int) string {
	return time.Now().AddDate(0, 0, n).Format("2006-01-02")
}

// 最酷列表页的比赛日是点分的（2026.12.08），不是 ISO 短横线 —— 假数据要像真的。
func dottedDays(n int) string {
	return time.Now().AddDate(0, 0, n).Format("2006.01.02")
}

func yearOf(date string) int {
	y, _ := strconv.Atoi(date[:4])
	return y
}

func seedRaces(t *testing.T, db *gorm.DB, rows ...store.RaceEventModel) {
	t.Helper()
	for i := range rows {
		rows[i].Year = yearOf(rows[i].Date)
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatalf("塞赛事: %v", err)
		}
	}
}

// 一页两张卡片：一张能在田协库里配上，一张配不上（最酷代理的赛事本来就只是子集）。
func fakeZuicool(t *testing.T) *httptest.Server {
	t.Helper()
	card := func(id, title, date, reg string) string {
		return `<div class="event-body">
<a href="https://zuicool.com/event/` + id + `" class="event-a">` + title + `</a>
<div class="info"><p>` + date + ` &middot; 浙江 舟山市</p></div>
<span>` + reg + `</span><a class="status_bth">点此报名</a></div>`
	}
	detail := func(price string) string {
		return `<html><head><meta name="description" content="赛事规模10000人；2026年10月28日10:00至11月18日12:00开放报名"></head>
<body><h1><a href="https://zuicool.com/event/1">2026舟山群岛马拉松</a></h1>
<div class="pkgs-list"><div class="pkg2">
<h4 class="name">马拉松（42.195公里）</h4>
<div class="price">` + price + `</div>
<a href="https://reg.zuicool.com/34604/233355" class="reg-btn">一键报名</a>
</div></div></body></html>`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/events":
			fmt.Fprint(w, `<html>`+
				card("84480", "2026舟山群岛马拉松", dottedDays(20)+" 07:00", "报名截止：11-18 12:00")+
				card("99999", "某个我们库里没有的越野赛", dottedDays(30)+" 07:00", "报名截止：12-01 18:00")+
				`</html>`)
		case r.URL.Path == "/event/84480":
			fmt.Fprint(w, detail("170.00"))
		case r.URL.Path == "/event/99999":
			fmt.Fprint(w, detail("300.00"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRefreshRaceRegistrations_matchesAndStores(t *testing.T) {
	db := regTestDB(t)
	seedRaces(t, db,
		store.RaceEventModel{RaceID: "r-zhoushan", Name: "2026舟山群岛马拉松", Date: inDays(20), Province: "浙江"},
		store.RaceEventModel{RaceID: "r-other", Name: "2026某场没上架的马拉松", Date: inDays(40), Province: "江苏"},
	)
	srv := fakeZuicool(t)
	zuicoolBaseURL = srv.URL
	t.Cleanup(func() { zuicoolBaseURL = "" })

	n, err := RefreshRaceRegistrations(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("配上 %d 场，应为 1（最酷只代理子集）", n)
	}
	reg, err := store.RaceRegistrationsByRaceIDs(db, []string{"r-zhoushan", "r-other"})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reg["r-zhoushan"]
	if !ok {
		t.Fatalf("没配上舟山：%+v", reg)
	}
	if got.MinPrice != 170 {
		t.Errorf("起价 %v，应为 170", got.MinPrice)
	}
	if got.RegEnd != "2026-11-18T12:00:00" {
		t.Errorf("报名截止应取详情页的完整窗口，实际 %s", got.RegEnd)
	}
	if got.RegStart != "2026-10-28T10:00:00" {
		t.Errorf("报名开始 %s", got.RegStart)
	}
	if got.RegURL != "https://reg.zuicool.com/34604/233355" {
		t.Errorf("报名链接应指向还能报的组别表单，实际 %s", got.RegURL)
	}
	if _, bad := reg["r-other"]; bad {
		t.Error("没上架的比赛不该有报名信息")
	}
}

// 挂到列表上：配上的有 registration，没配上的字段缺席（前端据此走复制入口）。
func TestAttachRaceRegistrations(t *testing.T) {
	db := regTestDB(t)
	seedRaces(t, db,
		store.RaceEventModel{RaceID: "r-zhoushan", Name: "2026舟山群岛马拉松", Date: inDays(20)},
		store.RaceEventModel{RaceID: "r-other", Name: "2026某场没上架的马拉松", Date: inDays(40)},
	)
	srv := fakeZuicool(t)
	zuicoolBaseURL = srv.URL
	t.Cleanup(func() { zuicoolBaseURL = "" })
	if _, err := RefreshRaceRegistrations(db); err != nil {
		t.Fatal(err)
	}

	list, err := store.ListRaceEvents(db, store.RaceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	attachRaceRegistrations(db, list)
	var hit, miss int
	for _, e := range list {
		switch e.RaceID {
		case "r-zhoushan":
			if e.Registration == nil {
				t.Error("舟山该挂上报名信息")
			} else if e.Registration.MinPrice != 170 {
				t.Errorf("挂上来的起价 %v", e.Registration.MinPrice)
			}
			hit++
		case "r-other":
			if e.Registration != nil {
				t.Error("没上架的比赛不该挂上报名信息")
			}
			miss++
		}
	}
	if hit != 1 || miss != 1 {
		t.Fatalf("列表内容不对：hit=%d miss=%d", hit, miss)
	}
}

// 一张都配不上时要清空旧数据 —— 否则会一直显示上周的截止日期。
func TestRefreshRaceRegistrations_clearsWhenNothingMatches(t *testing.T) {
	db := regTestDB(t)
	seedRaces(t, db, store.RaceEventModel{RaceID: "r-x", Name: "毫不相干的比赛", Date: inDays(20)})
	srv := fakeZuicool(t)
	zuicoolBaseURL = srv.URL
	t.Cleanup(func() { zuicoolBaseURL = "" })
	// 先塞一条旧的
	if _, err := store.ReplaceRaceRegistrations(db, []store.RaceRegistrationModel{
		{RaceID: "r-x", Source: "zuicool", RegEnd: "2020-01-01T00:00:00"},
	}); err != nil {
		t.Fatal(err)
	}
	n, err := RefreshRaceRegistrations(db)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("不该配上，实际 %d", n)
	}
	left, err := store.CountRaceRegistrations(db)
	if err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("旧数据没清掉，还剩 %d 条", left)
	}
}

// 「即将报名」的卡片给的是**报名开始**日，绝不能当截止显示 ——
// 那等于告诉用户「报名截止 11-22」而实际上 11-22 那天才刚开放。
func TestRefreshRaceRegistrations_startWindowIsNotDeadline(t *testing.T) {
	db := regTestDB(t)
	seedRaces(t, db, store.RaceEventModel{RaceID: "r-start", Name: "2027泉州石狮半程马拉松", Date: inDays(45)})
	card := `<div class="event-body">
<a href="https://zuicool.com/event/37017" class="event-a">2027泉州石狮半程马拉松</a>
<div class="info"><p>` + dottedDays(45) + ` 07:30 &middot; 福建 泉州市</p></div>
<span>报名开始：11-18 10:00</span><a class="status_bth">即将报名</a></div>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events":
			fmt.Fprint(w, "<html>"+card+"</html>")
		case "/event/37017":
			// 详情页没有「开放报名」那句，只有价格
			fmt.Fprint(w, `<html><head><meta name="description" content="半程马拉松"></head>
<body><h1><a href="https://zuicool.com/event/37017">2027泉州石狮半程马拉松</a></h1>
<div class="pkgs-list"><div class="pkg2"><h4 class="name">半程马拉松</h4>
<div class="price">150.00</div><a href="https://reg.zuicool.com/37017/1" class="reg-btn">一键报名</a>
</div></div></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	zuicoolBaseURL = srv.URL
	t.Cleanup(func() { zuicoolBaseURL = "" })

	if _, err := RefreshRaceRegistrations(db); err != nil {
		t.Fatal(err)
	}
	got, err := store.RaceRegistrationsByRaceIDs(db, []string{"r-start"})
	if err != nil {
		t.Fatal(err)
	}
	reg := got["r-start"]
	if reg.RegEnd != "" {
		t.Errorf("报名开始日被当成截止了：RegEnd=%s", reg.RegEnd)
	}
	if reg.RegStart == "" {
		t.Error("报名开始日没落到 RegStart")
	}
	// 「即将报名」不该给直达报名表单 —— 那个表单还没开
	if reg.RegURL == "https://reg.zuicool.com/37017/1" {
		t.Error("还没开放报名就给了组别表单直达链接")
	}
}

// 详情页挂了不能静默：列表页的截止是真的，价格拿不到要能在日志里看出来。
func TestRefreshRaceRegistrations_detailFailureKeepsListWindow(t *testing.T) {
	db := regTestDB(t)
	seedRaces(t, db, store.RaceEventModel{RaceID: "r-zhoushan", Name: "2026舟山群岛马拉松", Date: inDays(20)})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/events" {
			fmt.Fprint(w, `<html><div class="event-body">
<a href="https://zuicool.com/event/84480" class="event-a">2026舟山群岛马拉松</a>
<div class="info"><p>`+dottedDays(20)+` 07:00 &middot; 浙江 舟山市</p></div>
<span>报名截止：11-18 12:00</span><a class="status_bth">点此报名</a></div></html>`)
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError) // 详情页全挂
	}))
	defer srv.Close()
	zuicoolBaseURL = srv.URL
	t.Cleanup(func() { zuicoolBaseURL = "" })

	n, err := RefreshRaceRegistrations(db)
	if err != nil {
		t.Fatalf("详情页失败不该让整次刷新失败（列表数据是真的）：%v", err)
	}
	if n != 1 {
		t.Fatalf("写入 %d 条，应为 1", n)
	}
	got, _ := store.RaceRegistrationsByRaceIDs(db, []string{"r-zhoushan"})
	reg := got["r-zhoushan"]
	if reg.RegEnd != "2026-11-18T12:00:00" {
		t.Errorf("列表页的截止没兜住：RegEnd=%s", reg.RegEnd)
	}
	if reg.MinPrice != 0 {
		t.Errorf("详情页挂了还写出价格 %v，应为 0（前端据此不显示价格）", reg.MinPrice)
	}
}

// 预算用完时**已抓到的必须落库**。抓到了 40 场却因为第 41 场太慢就整批丢掉，
// 等于把已经确定的事实扔掉，比只更新一半更糟；同时必须如实标成部分结果。
func TestRefreshRaceRegistrations_partialOnBudgetExhausted(t *testing.T) {
	db := regTestDB(t)
	seedRaces(t, db,
		store.RaceEventModel{RaceID: "r-a", Name: "2026甲马拉松", Date: inDays(20)},
		store.RaceEventModel{RaceID: "r-b", Name: "2026乙马拉松", Date: inDays(21)},
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/events" {
			fmt.Fprint(w, `<html><div class="event-body">
<a href="https://zuicool.com/event/1" class="event-a">2026甲马拉松</a>
<div class="info"><p>`+dottedDays(20)+` 07:00 &middot; 浙江 甲市</p></div>
<span>报名截止：11-18 12:00</span><a class="status_bth">点此报名</a></div>
<div class="event-body">
<a href="https://zuicool.com/event/2" class="event-a">2026乙马拉松</a>
<div class="info"><p>`+dottedDays(21)+` 07:00 &middot; 江苏 乙市</p></div>
<span>报名截止：11-19 12:00</span><a class="status_bth">点此报名</a></div></html>`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/2") {
			time.Sleep(1500 * time.Millisecond) // 第二场慢到超出预算
			return
		}
		fmt.Fprint(w, `<html><h1><a href="https://zuicool.com/event/1">2026甲马拉松</a></h1>
<div class="pkg2"><h4 class="name">全程</h4><div class="price">180.00</div></div></html>`)
	}))
	defer srv.Close()
	zuicoolBaseURL = srv.URL
	t.Cleanup(func() { zuicoolBaseURL = "" })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	n, partial, err := refreshRaceRegistrations(ctx, db)
	if err != nil {
		t.Fatalf("超预算不该当错误抛出：%v", err)
	}
	if !partial {
		t.Fatalf("必须标成部分结果，否则前端会把 1 场说成全量")
	}
	if n != 1 {
		t.Fatalf("已抓到的 %d 场应照常写入", n)
	}
	got, _ := store.RaceRegistrationsByRaceIDs(db, []string{"r-a", "r-b"})
	if got["r-a"].MinPrice != 180 {
		t.Errorf("第一场详情已抓到却没落库：%+v", got["r-a"])
	}
	if _, bad := got["r-b"]; bad {
		t.Errorf("第二场没抓到，不该有记录：%+v", got["r-b"])
	}
}
