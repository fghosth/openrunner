package api

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"
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
