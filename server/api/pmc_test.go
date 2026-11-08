package api

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/openrunner/server/store"
)

func TestSeasonsCRUD(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	t.Run("PUT 创建赛季并 GET 返回", func(t *testing.T) {
		w := do(h, http.MethodPut, "/api/athletes/1/seasons", token,
			`{"name":"2026 公路季","startDate":"2026-01-01","endDate":"2026-10-31","seed":300}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("期望 201, 得到 %d: %s", w.Code, w.Body.String())
		}
		w = do(h, http.MethodGet, "/api/athletes/1/seasons", token, "")
		var seasons []struct {
			Name string  `json:"name"`
			Seed float64 `json:"seed"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &seasons)
		if len(seasons) != 1 || seasons[0].Name != "2026 公路季" || seasons[0].Seed != 300 {
			t.Fatalf("期望 1 个赛季 seed=300, 得到 %+v", seasons)
		}
	})

	t.Run("日期倒置被 400 拒绝", func(t *testing.T) {
		w := do(h, http.MethodPut, "/api/athletes/1/seasons", token,
			`{"name":"bad","startDate":"2026-10-31","endDate":"2026-01-01","seed":0}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("期望 400, 得到 %d", w.Code)
		}
	})
}

func TestPMCEndpoint(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	// 配置：心率区间 + 赛季播种 200 @ 2026-03-01
	if w := do(h, http.MethodPut, "/api/athletes/1/hr-zones", token,
		`{"validFrom":"2000-01-01","validTo":"2099-12-31","maxHr":200,"restHr":50,"ltHr":170}`); w.Code != http.StatusCreated {
		t.Fatal("心率区间失败")
	}
	if w := do(h, http.MethodPut, "/api/athletes/1/seasons", token,
		`{"name":"S1","startDate":"2026-03-01","endDate":"2026-10-31","seed":200}`); w.Code != http.StatusCreated {
		t.Fatal("赛季创建失败")
	}

	// 实际活动（过去）：一天 1h@170 → 估算 TRIMP=142.7（无流）
	if w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
		`{"date":"2026-03-05","sport":"run","durationSecs":3600,"distanceM":12000,"avgHr":170,"isPlanned":false}`); w.Code != http.StatusCreated {
		t.Fatalf("实际活动创建失败: %d", w.Code)
	}
	// 计划活动（未来）：同样 1h@170
	if w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
		`{"date":"2026-04-05","sport":"run","durationSecs":3600,"distanceM":12000,"avgHr":170,"isPlanned":true}`); w.Code != http.StatusCreated {
		t.Fatalf("计划活动创建失败: %d", w.Code)
	}

	w := do(h, http.MethodGet, "/api/athletes/1/pmc?metric=trimp", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
	}
	var series struct {
		Dates       []string  `json:"dates"`
		Lts         []float64 `json:"lts"`
		Sb          []float64 `json:"sb"`
		PlannedLts  []float64 `json:"plannedLts"`
		ExpectedLts []float64 `json:"expectedLts"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &series)

	today := time.Now().Local().Format("2006-01-02")
	todayIdx := -1
	for i, d := range series.Dates {
		if d == today {
			todayIdx = i
		}
	}
	if todayIdx < 0 {
		t.Fatal("区间未覆盖今天")
	}

	t.Run("播种生效：赛季起点 LTS=200", func(t *testing.T) {
		idx := -1
		for i, d := range series.Dates {
			if d == "2026-03-01" {
				idx = i
			}
		}
		if idx < 0 {
			t.Fatal("缺少赛季起点")
		}
		if math.Abs(series.Lts[idx]-200) > 1e-9 {
			t.Fatalf("期望播种 200, 得到 %v", series.Lts[idx])
		}
	})

	t.Run("实际活动日 LTS 相对衰减后的前一日上升", func(t *testing.T) {
		idx := -1
		for i, d := range series.Dates {
			if d == "2026-03-05" {
				idx = i
			}
		}
		// 反事实：无活动的纯衰减 = 前日 × e^(−1/42)；负荷 142.7 虽低于 LTS 水平，
		// 但其加权注入应使当日值高于纯衰减（IIR 吸收负荷的判据）
		counterfactual := series.Lts[idx-1] * math.Exp(-1.0/42)
		if series.Lts[idx] <= counterfactual {
			t.Fatalf("期望高于纯衰减 %v, 得到 %v", counterfactual, series.Lts[idx])
		}
	})

	t.Run("今日特判：expected 取实际序列", func(t *testing.T) {
		if math.Abs(series.ExpectedLts[todayIdx]-series.Lts[todayIdx]) > 1e-9 {
			t.Fatal("今日 expected 应与 actual 递推一致（无计划分叉）")
		}
	})

	t.Run("性能：全年序列应在 100ms 内返回", func(t *testing.T) {
		start := time.Now()
		w := do(h, http.MethodGet, "/api/athletes/1/pmc?metric=trimp", token, "")
		if w.Code != http.StatusOK {
			t.Fatal("请求失败")
		}
		if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
			t.Fatalf("期望 <100ms, 得到 %v", elapsed)
		}
	})
}

// 票 03：seed=0 赛季是有效锚点（从赛季首日 0 起链），旧代码 >0 门控把它整个忽略。
func TestPMCSeasonSeedZero(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)
	if w := do(h, http.MethodPut, "/api/athletes/1/hr-zones", token,
		`{"validFrom":"2000-01-01","validTo":"2099-12-31","maxHr":200,"restHr":50,"ltHr":170}`); w.Code != http.StatusCreated {
		t.Fatal("心率区间失败")
	}
	// 赛季起点比最早活动早 30 天，seed=0
	start := time.Now().AddDate(0, 0, -60).Format("2006-01-02")
	if w := do(h, http.MethodPut, "/api/athletes/1/seasons", token,
		`{"name":"S0","startDate":"`+start+`","endDate":"`+time.Now().Format("2006-01-02")+`","seed":0}`); w.Code != http.StatusCreated {
		t.Fatal("seed=0 赛季创建失败")
	}
	if w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
		`{"date":"`+time.Now().AddDate(0, 0, -30).Format("2006-01-02")+`","sport":"run","durationSecs":3600,"distanceM":12000,"avgHr":170}`); w.Code != http.StatusCreated {
		t.Fatal("活动创建失败")
	}
	w := do(h, http.MethodGet, "/api/athletes/1/pmc?metric=trimp", token, "")
	var series struct {
		Dates []string  `json:"dates"`
		Lts   []float64 `json:"lts"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &series)
	idx := -1
	for i, d := range series.Dates {
		if d == start {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("seed=0 赛季应把区间起点扩到赛季首日，dates[0]=%s", series.Dates[0])
	}
	if series.Lts[idx] != 0 {
		t.Errorf("赛季首日 LTS 应为 0（锚点），得 %v", series.Lts[idx])
	}
	// 衔接平滑：每日衰减不超过 τ=42 的指数因子（跳变会远超这个界）
	a := math.Exp(-1.0 / 42.0)
	for i := idx + 1; i < len(series.Dates) && series.Dates[i] <= time.Now().Format("2006-01-02"); i++ {
		if series.Lts[i] < series.Lts[i-1]*a-1e-9 {
			t.Errorf("锚点后衰减超指数界 %s: %v → %v", series.Dates[i], series.Lts[i-1], series.Lts[i])
			break
		}
	}
}

// 票 05：跨午夜活动的日期归属按本地时区（Round 5 挂账收尾）。
func TestActivityDateLocalAcrossMidnight(t *testing.T) {
	h, db := newTestServer(t)
	token := setupAdmin(t, h)
	_ = createAthlete(t, h, token)
	if w := do(h, http.MethodPut, "/api/athletes/1/hr-zones", token,
		`{"validFrom":"2000-01-01","validTo":"2099-12-31","maxHr":200,"restHr":50,"ltHr":170}`); w.Code != http.StatusCreated {
		t.Fatal("心率区间失败")
	}
	// 本地 23:50 与次日 00:10 各一条：应分别落进两个本地日期桶
	yesterday := time.Now().AddDate(0, 0, -1)
	late := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 50, 0, 0, time.Local)
	early := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 10, 0, 0, time.Now().Location())
	early = early.AddDate(0, 0, 1) // 次日 00:10
	var ath store.AthleteModel
	if err := db.Gorm().First(&ath, 1).Error; err != nil {
		t.Fatal(err)
	}
	for _, ts := range []time.Time{late, early} {
		a := &store.ActivityModel{
			AthleteID: 1, Sport: "run", StartTime: ts.Unix(),
			DurationSecs: 1800, TimerSecs: 1800, AvgHr: 170, DistanceM: 6000,
		}
		if err := db.Gorm().Create(a).Error; err != nil {
			t.Fatal(err)
		}
		// 走生产口径补算指标（直接插库不会触发 metrics，PMC 读的是 metrics 表）
		if err := recomputeMetrics(db.Gorm(), &ath, a); err != nil {
			t.Fatal(err)
		}
	}
	w := do(h, http.MethodGet, "/api/athletes/1/pmc?metric=trimp", token, "")
	var series struct {
		Dates  []string  `json:"dates"`
		Stress []float64 `json:"stress"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &series)
	wantLate := late.Format("2006-01-02")
	wantEarly := early.Format("2006-01-02")
	if wantLate == wantEarly {
		t.Fatal("用例构造错误：两条活动落在同一日期")
	}
	byDate := map[string]float64{}
	for i, d := range series.Dates {
		byDate[d] += series.Stress[i]
	}
	if byDate[wantLate] <= 0 {
		t.Errorf("本地 23:50 的活动应计入 %s，stress=%v", wantLate, byDate[wantLate])
	}
	if byDate[wantEarly] <= 0 {
		t.Errorf("本地次日 00:10 的活动应计入 %s，stress=%v", wantEarly, byDate[wantEarly])
	}
}
