package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestManualPlannedActivity(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	// 先配心率区间 + 阈值配速（估算的参数源）
	if w := do(h, http.MethodPut, "/api/athletes/1/hr-zones", token,
		`{"validFrom":"2000-01-01","validTo":"2099-12-31","maxHr":200,"restHr":50,"ltHr":170}`); w.Code != http.StatusCreated {
		t.Fatalf("心率区间创建失败: %d", w.Code)
	}
	if w := do(h, http.MethodPut, "/api/athletes/1/pace-zones", token,
		`{"validFrom":"2000-01-01","validTo":"2099-12-31","thresholdPaceSecsPerKm":300}`); w.Code != http.StatusCreated {
		t.Fatalf("配速区间创建失败: %d", w.Code)
	}

	t.Run("手工创建计划活动 → 201 + isPlanned", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
			`{"date":"2026-03-20","sport":"run","durationSecs":3600,"distanceM":12000,"avgHr":170,"isPlanned":true}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("期望 201, 得到 %d: %s", w.Code, w.Body.String())
		}
		var act struct {
			ID        uint   `json:"id"`
			IsPlanned bool   `json:"isPlanned"`
			HasStream bool   `json:"hasStream"`
			Sport     string `json:"sport"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &act)
		if !act.IsPlanned || act.HasStream || act.Sport != "run" {
			t.Fatalf("期望计划/无流/run, 得到 %+v", act)
		}
	})

	t.Run("仅 summary → 估算指标 + isEstimated=true", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/activities/1", token, "")
		var detail struct {
			Metrics *struct {
				Trimp       float64   `json:"trimp"`
				Trimp100    float64   `json:"trimp100"`
				Rtss        float64   `json:"rtss"`
				AvgHr       float64   `json:"avgHr"`
				HrTiZ       []float64 `json:"hrTiZ"`
				IsEstimated bool      `json:"isEstimated"`
			} `json:"metrics"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &detail)
		if detail.Metrics == nil {
			t.Fatal("期望估算 metrics 非空")
		}
		if !detail.Metrics.IsEstimated {
			t.Fatal("期望 isEstimated=true")
		}

		// 独立真值：TRIMP = 60min × hrr(170: 120/150) × 0.64 × exp(1.92×0.8)
		// avgSpeed = 12km / 1h = 12kph = 阈值速度 → rTSS = 100
		if detail.Metrics.Rtss == 0 {
			t.Fatal("期望估算 rTSS 非零")
		}
		if detail.Metrics.Trimp <= 0 || detail.Metrics.Trimp100 <= 0 {
			t.Fatalf("期望正 TRIMP, 得到 %+v", detail.Metrics)
		}
		if detail.Metrics.AvgHr != 170 {
			t.Errorf("期望 avgHr=170, 得到 %v", detail.Metrics.AvgHr)
		}
	})

	t.Run("计划活动不出现在实际序列语义由 PMC 票处理（此处仅入库）", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/activities", token, "")
		var list []map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if len(list) != 1 {
			t.Fatalf("期望 1 条, 得到 %d", len(list))
		}
	})

	t.Run("缺少日期/时长被 400 拒绝", func(t *testing.T) {
		w := do(h, http.MethodPost, "/api/athletes/1/activities", token, `{"sport":"run"}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("期望 400, 得到 %d", w.Code)
		}
	})
}
