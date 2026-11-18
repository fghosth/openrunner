package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fghosth/openrunner/server/store"
)

// 赛历接口测试：上游一律 httptest 回放，绝不碰真实站点。

// fakeRacesUpstream 回放两页赛历，并统计被请求了几次。
func fakeRacesUpstream(t *testing.T, hits *atomic.Int32, fail bool) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		if fail {
			io.WriteString(w, `<html>请求已被站点的安全策略拦截</html>`)
			return
		}
		var pageNo float64
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if v, ok := req["pageNo"].(float64); ok {
			pageNo = v
		}
		row := func(id, name, date, addr, item, grade string) string {
			return fmt.Sprintf(`{"raceId":%s,"raceName":%q,"raceGrade":%q,"raceTime":%q,`+
				`"raceAddress":%q,"raceItem":%q,"raceScale":null}`, id, name, grade, date, addr, item)
		}
		if pageNo <= 1 {
			fmt.Fprintf(w, `{"success":true,"data":{"pageNo":1,"pageCount":2,"results":[%s,%s]}}`,
				row("1", "广州马拉松", "2026-12-01", "广东省/广州市/天河区", `["全程","半程"]`, "A"),
				row("2", "深圳半马", "2026-11-01", "广东省/深圳市/", `["半程"]`, "B"))
			return
		}
		fmt.Fprintf(w, `{"success":true,"data":{"pageNo":2,"pageCount":2,"results":[%s,%s]}}`,
			row("3", "南京 10K", "2026-10-01", "江苏省/南京市/", `["10公里"]`, ""),
			row("4", "成都欢乐跑", "2026-09-01", "四川省/成都市/", `["5公里"]`, "A")) // 应被过滤掉
	}))
	t.Cleanup(srv.Close)
	old := racesBaseURL
	racesBaseURL = srv.URL
	t.Cleanup(func() { racesBaseURL = old })
}

func TestRacesRefreshSummaryAndList(t *testing.T) {
	h, token := setupWithToken(t)
	fakeRacesUpstream(t, nil, false)

	// 空表时 summary 是空的，但接口不该报错
	w := do(h, http.MethodGet, "/api/races/summary", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("空表 summary 期望 200, 得到 %d: %s", w.Code, w.Body.String())
	}

	t.Run("手动更新", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/races/refresh", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Imported   int   `json:"imported"`
			LastSyncAt int64 `json:"lastSyncAt"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		// 4 条里 5 公里那条被过滤，应写 3 条
		if res.Imported != 3 {
			t.Errorf("应导入 3 条（非马拉松项目被过滤），实际 %d", res.Imported)
		}
		if res.LastSyncAt == 0 {
			t.Error("应回写同步时间")
		}
	})

	t.Run("summary 给出省市区树", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/races/summary", token, "")
		var sum struct {
			Total      int   `json:"total"`
			LastSyncAt int64 `json:"lastSyncAt"`
			Years      []int `json:"years"`
			Provinces  []struct {
				Name   string `json:"name"`
				Count  int    `json:"count"`
				Cities []struct {
					Name      string `json:"name"`
					Count     int    `json:"count"`
					Districts []struct {
						Name  string `json:"name"`
						Count int    `json:"count"`
					} `json:"districts"`
				} `json:"cities"`
			} `json:"provinces"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &sum); err != nil {
			t.Fatal(err)
		}
		if sum.Total != 3 || sum.LastSyncAt == 0 {
			t.Errorf("总数/同步时间不对: %+v", sum)
		}
		if len(sum.Provinces) == 0 || sum.Provinces[0].Name != "广东省" || sum.Provinces[0].Count != 2 {
			t.Errorf("省树不对: %+v", sum.Provinces)
		}
		gz := sum.Provinces[0].Cities[0]
		if gz.Name != "广州市" || len(gz.Districts) != 1 || gz.Districts[0].Name != "天河区" {
			t.Errorf("市/区树不对: %+v", gz)
		}
		if len(sum.Years) == 0 || sum.Years[0] != 2026 {
			t.Errorf("年份列表不对: %v", sum.Years)
		}
	})

	t.Run("列表可按省与项目筛", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/races?province=%E5%B9%BF%E4%B8%9C%E7%9C%81", token, "")
		var list []store.RaceEventModel
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 2 {
			t.Fatalf("广东省应有 2 场，实际 %d: %s", len(list), w.Body.String())
		}
		// 日期倒序：最近的比赛在最前
		if list[0].Date != "2026-12-01" {
			t.Errorf("应按日期倒序，首条 %s", list[0].Date)
		}
		w2 := do(h, http.MethodGet, "/api/races?category=10k", token, "")
		var list2 []store.RaceEventModel
		_ = json.Unmarshal(w2.Body.Bytes(), &list2)
		if len(list2) != 1 || list2[0].Name != "南京 10K" {
			t.Errorf("按项目筛不对: %+v", list2)
		}
	})

	t.Run("列表与 summary 都认级别", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/races?grade=B", token, "")
		var list []store.RaceEventModel
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 1 || list[0].Name != "深圳半马" {
			t.Errorf("按级别筛不对: %+v", list)
		}
		// 级别取值由上游决定，所以清单由后端给（含空值占位「未标注」），前端不写死选项
		var sum struct {
			Grades []struct {
				Name  string `json:"name"`
				Count int    `json:"count"`
			} `json:"grades"`
		}
		_ = json.Unmarshal(do(h, http.MethodGet, "/api/races/summary", token, "").Body.Bytes(), &sum)
		if len(sum.Grades) != 3 || sum.Grades[len(sum.Grades)-1].Name != "未标注" {
			t.Fatalf("summary 的级别清单不对: %+v", sum.Grades)
		}
		// 「未标注」要能翻回空串真查到那条，否则树上点得到、列表却空
		w3 := do(h, http.MethodGet, "/api/races?grade=%E6%9C%AA%E6%A0%87%E6%B3%A8", token, "")
		var list3 []store.RaceEventModel
		_ = json.Unmarshal(w3.Body.Bytes(), &list3)
		if len(list3) != 1 || list3[0].Name != "南京 10K" {
			t.Errorf("按「未标注」筛不对: %+v", list3)
		}
	})

	t.Run("未登录不能读赛历", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/races", "", "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("期望 401，实际 %d", w.Code)
		}
	})
}

func TestRacesRefreshFailureRecordsError(t *testing.T) {
	h, token := setupWithToken(t)
	fakeRacesUpstream(t, nil, true)

	w := do(h, http.MethodPost, "/api/races/refresh", token, "")
	if w.Code != http.StatusBadGateway {
		t.Fatalf("上游被挡期望 502, 得到 %d: %s", w.Code, w.Body.String())
	}
	// 失败原因要能在 summary 里看到，否则用户只看到「数据没更新」却不知道为啥
	w2 := do(h, http.MethodGet, "/api/races/summary", token, "")
	if !strings.Contains(w2.Body.String(), `"lastError":"`) || strings.Contains(w2.Body.String(), `"lastError":""`) {
		t.Errorf("summary 应带 lastError: %s", w2.Body.String())
	}
}

func TestRaceSchedulerPullsEmptyTableImmediately(t *testing.T) {
	_, db := newTestServer(t)
	var hits atomic.Int32
	fakeRacesUpstream(t, &hits, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartRaceCalendarScheduler(ctx, db.Gorm(), time.Hour)

	// 空表：不等间隔，立刻拉
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if hits.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if hits.Load() == 0 {
		t.Fatal("空表应启动后立刻拉一次，而不是等一个周期")
	}
	if n, _ := store.CountRaceEvents(db.Gorm()); n != 3 {
		t.Errorf("首拉应写入 3 条，实际 %d", n)
	}
}

func TestRaceSchedulerWaitsWhenNotEmpty(t *testing.T) {
	_, db := newTestServer(t)
	fakeRacesUpstream(t, nil, false)
	if _, err := RefreshRaceCalendar(db.Gorm()); err != nil {
		t.Fatal(err)
	}

	var hits atomic.Int32
	fakeRacesUpstream(t, &hits, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 间隔给得比观测窗口长得多：表非空时不该立刻打上游
	StartRaceCalendarScheduler(ctx, db.Gorm(), time.Hour)
	time.Sleep(200 * time.Millisecond)
	if hits.Load() != 0 {
		t.Errorf("表非空时不该立刻更新，实际打了 %d 次", hits.Load())
	}
}

func TestRaceSchedulerStopsOnCancel(t *testing.T) {
	_, db := newTestServer(t)
	var hits atomic.Int32
	fakeRacesUpstream(t, &hits, false)

	ctx, cancel := context.WithCancel(context.Background())
	StartRaceCalendarScheduler(ctx, db.Gorm(), 50*time.Millisecond)
	time.Sleep(250 * time.Millisecond)
	cancel()
	before := hits.Load()
	// 取消后再等 6 个周期的长度。取消只能拦住**后续**触发，已经发出的那次请求
	// 会跑完，所以允许 +1；取消不工作的话这里会多出一把。
	time.Sleep(300 * time.Millisecond)
	if got := hits.Load(); got > before+1 {
		t.Errorf("取消后还在继续更新：%d → %d", before, got)
	}
}

func TestRacesRefreshRejectsUnknownCategory(t *testing.T) {
	h, token := setupWithToken(t)
	fakeRacesUpstream(t, nil, false)
	if w := do(h, http.MethodPost, "/api/races/refresh", token, ""); w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	// 非法 category 不能退化成「不过滤」把全部捞出来
	w := do(h, http.MethodGet, "/api/races?category=ultra", token, "")
	var list []store.RaceEventModel
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 3 {
		t.Errorf("非法项目应被忽略（等价于不限项目，共 3 条），实际 %d", len(list))
	}
}
