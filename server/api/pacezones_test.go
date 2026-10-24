package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestPaceZonesAndRtss(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	t.Run("PUT 创建阈值配速并 GET 返回", func(t *testing.T) {
		w := do(h, http.MethodPut, "/api/athletes/1/pace-zones", token,
			`{"validFrom":"2000-01-01","validTo":"2099-12-31","thresholdPaceSecsPerKm":300}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("期望 201, 得到 %d: %s", w.Code, w.Body.String())
		}
		w = do(h, http.MethodGet, "/api/athletes/1/pace-zones", token, "")
		var zones []struct {
			ThresholdPaceSecsPerKm int `json:"thresholdPaceSecsPerKm"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &zones)
		if len(zones) != 1 || zones[0].ThresholdPaceSecsPerKm != 300 {
			t.Fatalf("期望 300 s/km, 得到 %+v", zones)
		}
	})

	t.Run("非法阈值被 400 拒绝", func(t *testing.T) {
		w := do(h, http.MethodPut, "/api/athletes/1/pace-zones", token,
			`{"validFrom":"2000-01-01","validTo":"2099-12-31","thresholdPaceSecsPerKm":60}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("期望 400, 得到 %d", w.Code)
		}
	})

	t.Run("配速链门控：骑行活动不出 rTSS（仅跑步）", func(t *testing.T) {
		// big_activity 是骑行；rTSS 的数值正确性由 core 恒等式测试覆盖
		// 同时配置心率区间：验证心率链不受配速链门控影响
		if w := do(h, http.MethodPut, "/api/athletes/1/hr-zones", token,
			`{"validFrom":"2000-01-01","validTo":"2099-12-31","maxHr":200,"restHr":50,"ltHr":170}`); w.Code != http.StatusCreated {
			t.Fatalf("心率区间创建失败: %d", w.Code)
		}
		w := do(h, http.MethodPut, "/api/athletes/1/pace-zones", token,
			`{"validFrom":"2000-01-01","validTo":"2099-12-31","thresholdPaceSecsPerKm":300}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("区间创建失败: %d", w.Code)
		}
		if w := uploadFIT(t, h, token, "1", "../../../openrunner-core/ingest/testdata/big_activity.fit"); w.Code != http.StatusCreated {
			t.Fatalf("导入失败: %d %s", w.Code, w.Body.String())
		}
		w = do(h, http.MethodGet, "/api/athletes/1/activities/1", token, "")
		var detail struct {
			Metrics *struct {
				Rtss    float64   `json:"rtss"`
				PaceTiZ []float64 `json:"paceTiZ"`
				Trimp   float64   `json:"trimp"`
			} `json:"metrics"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &detail)
		if detail.Metrics == nil {
			t.Fatal("期望 metrics 非空（心率链仍应产出）")
		}
		if detail.Metrics.Rtss != 0 {
			t.Fatalf("骑行活动期望 rTSS=0, 得到 %v", detail.Metrics.Rtss)
		}
		if detail.Metrics.Trimp <= 0 {
			t.Errorf("期望心率链 TRIMP 正常, 得到 %v", detail.Metrics.Trimp)
		}
	})
}
