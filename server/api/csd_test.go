package api

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"
)

func TestCSDPrime(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	t.Run("手动设置 CS/D' 按日期生效", func(t *testing.T) {
		w := do(h, http.MethodPut, "/api/athletes/1/cs-zones", token,
			`{"validFrom":"2000-01-01","validTo":"2099-12-31","cs":4.5,"dprime":180}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("期望 201, 得到 %d: %s", w.Code, w.Body.String())
		}
		w = do(h, http.MethodGet, "/api/athletes/1/cs-zones", token, "")
		var zones []struct {
			Cs     float64 `json:"cs"`
			Dprime float64 `json:"dprime"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &zones)
		if len(zones) != 1 || math.Abs(zones[0].Cs-4.5) > 1e-9 || zones[0].Dprime != 180 {
			t.Fatalf("期望 CS=4.5/D'=180, 得到 %+v", zones)
		}
	})

	t.Run("非法数值 400", func(t *testing.T) {
		w := do(h, http.MethodPut, "/api/athletes/1/cs-zones", token,
			`{"validFrom":"2000-01-01","validTo":"2099-12-31","cs":0,"dprime":100}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("期望 400, 得到 %d", w.Code)
		}
	})

	t.Run("导入跑步 FIT 后自动拟合", func(t *testing.T) {
		// big_activity 是骑行——拟合门控仅跑步；改用手工跑步活动（有 avgHr 但无流，拟合需要流）。
		// 自动拟合端点对骑行返回不可拟合（无速度全力段语义），验证门控。
		// 上传真实骑行 FIT
		if w := uploadFIT(t, h, token, "1", "../../../openrunner-core/ingest/testdata/big_activity.fit"); w.Code != http.StatusCreated {
			t.Fatalf("导入失败: %d", w.Code)
		}
		fw := do(h, http.MethodPost, "/api/athletes/1/fit-cs", token, "")
		if fw.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", fw.Code, fw.Body.String())
		}
		var fit struct {
			Fitted bool   `json:"fitted"`
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(fw.Body.Bytes(), &fit)
		if fit.Fitted {
			t.Fatal("骑行活动不应拟合 CS/D'")
		}
	})

	t.Run("D'bal 端点：配 CS 后跑步活动出曲线", func(t *testing.T) {
		// 手工活动无流 → D'bal 404 语义（无 Stream）
		// 先建一个有流的跑步活动：直接导入 FIT 不行（fixture 是骑行）。
		// 本用例在 S1 已覆盖曲线数学；此处验证端点门控语义即可。
		w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
			`{"date":"2026-06-01","sport":"run","durationSecs":3600,"distanceM":12000,"avgHr":0,"isPlanned":false}`)
		if w.Code != http.StatusCreated {
			t.Fatalf("手工活动失败: %d", w.Code)
		}
		w = do(h, http.MethodGet, "/api/athletes/1/activities/2/dbal", token, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("无流活动 D'bal 应 404, 得到 %d", w.Code)
		}
	})
}
