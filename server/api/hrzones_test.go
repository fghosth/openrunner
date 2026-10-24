package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHrZonesCRUD(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	t.Run("PUT 创建区间并 GET 返回", func(t *testing.T) {
		w := do(h, http.MethodPut, "/api/athletes/1/hr-zones", token,
			`{"validFrom":"2026-01-01","validTo":"2026-12-31","maxHr":190,"restHr":50,"ltHr":160}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("期望 201, 得到 %d: %s", w.Code, w.Body.String())
		}
		w = do(h, http.MethodGet, "/api/athletes/1/hr-zones", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d", w.Code)
		}
		var zones []struct {
			MaxHr  int `json:"maxHr"`
			RestHr int `json:"restHr"`
			LtHr   int `json:"ltHr"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &zones)
		if len(zones) != 1 || zones[0].MaxHr != 190 || zones[0].LtHr != 160 {
			t.Fatalf("期望 1 组区间(190/50/160), 得到 %+v", zones)
		}
	})

	t.Run("非法数值被 400 拒绝", func(t *testing.T) {
		w := do(h, http.MethodPut, "/api/athletes/1/hr-zones", token,
			`{"validFrom":"2026-01-01","validTo":"2026-12-31","maxHr":190,"restHr":200,"ltHr":160}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("期望 400, 得到 %d", w.Code)
		}
	})
}

func TestImportComputesTrimpMetrics(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	t.Run("未配置心率区间 → metrics 为 null", func(t *testing.T) {
		w := uploadFIT(t, h, token, "1", "../../../openrunner-core/ingest/testdata/big_activity.fit")
		if w.Code != http.StatusCreated {
			t.Fatalf("导入失败: %d", w.Code)
		}
		w = do(h, http.MethodGet, "/api/athletes/1/activities/1", token, "")
		var detail struct {
			Metrics *struct {
				Trimp float64 `json:"trimp"`
			} `json:"metrics"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &detail)
		if detail.Metrics != nil {
			t.Fatal("未配置区间时期望 metrics=null")
		}
	})

	t.Run("配置区间后重算 → trimp/trimp100/tiz 出现", func(t *testing.T) {
		w := do(h, http.MethodPut, "/api/athletes/1/hr-zones", token,
			`{"validFrom":"2000-01-01","validTo":"2099-12-31","maxHr":200,"restHr":50,"ltHr":170}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("区间创建失败: %d", w.Code)
		}
		w = do(h, http.MethodPost, "/api/athletes/1/activities/1/recompute", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d", w.Code)
		}
		w = do(h, http.MethodGet, "/api/athletes/1/activities/1", token, "")
		var detail struct {
			Metrics *struct {
				Trimp       float64   `json:"trimp"`
				Trimp100    float64   `json:"trimp100"`
				AvgHr       float64   `json:"avgHr"`
				IsEstimated bool      `json:"isEstimated"`
				HrTiZ       []float64 `json:"hrTiZ"`
			} `json:"metrics"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &detail)
		if detail.Metrics == nil {
			t.Fatal("期望 metrics 非空")
		}
		if detail.Metrics.Trimp <= 0 || detail.Metrics.Trimp100 <= 0 {
			t.Fatalf("期望正的 TRIMP/TRIMP100, 得到 %+v", detail.Metrics)
		}
		if detail.Metrics.AvgHr <= 0 {
			t.Errorf("期望平均心率 > 0, 得到 %v", detail.Metrics.AvgHr)
		}
		if detail.Metrics.IsEstimated {
			t.Error("有 Stream 的活动不得标记估算")
		}
		if len(detail.Metrics.HrTiZ) != 5 {
			t.Errorf("期望 5 区 TiZ, 得到 %v", detail.Metrics.HrTiZ)
		}
	})
}
