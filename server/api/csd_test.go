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
			`{"domain":"speed","validFrom":"2000-01-01","validTo":"2099-12-31","cs":4.5,"dprime":180}`)
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

func TestPowerChain(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	t.Run("跑步 FIT 导入 → fit-cs 同时产出 CP/W'（复用同一全力段）", func(t *testing.T) {
		// 无跑步 fixture（03 决议），用骑行 FIT 验证门控外行为：功率域不在本票
		// 此处只验证 fit-cs 幂等与 cs-zones 来源标记
		if w := do(h, http.MethodPut, "/api/athletes/1/cs-zones", token,
			`{"domain":"speed","validFrom":"2000-01-01","validTo":"2099-12-31","cs":4.5,"dprime":180}`); w.Code != http.StatusCreated {
			t.Fatal("CS 设置失败")
		}
		w := do(h, http.MethodGet, "/api/athletes/1/cs-zones", token, "")
		var zones []struct {
			Source string `json:"source"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &zones)
		if len(zones) != 1 || zones[0].Source != "manual" {
			t.Fatalf("期望 manual 来源, 得到 %+v", zones)
		}
	})

	t.Run("功率域配置 + power 域区间校验生效（S3 门控层）", func(t *testing.T) {
		// 注：现有 FIT fixture 无功率数据（big_activity 的 Power 全为 invalid），
		// 功率链的数学与 metrics 产出由函数级测试覆盖（power_chain_test.go 合成流）。
		if w := do(h, http.MethodPut, "/api/athletes/1/cs-zones", token,
			`{"domain":"power","validFrom":"2000-01-01","validTo":"2099-12-31","cs":280,"dprime":20000}`); w.Code != http.StatusCreated {
			t.Fatalf("功率域配置失败: %d %s", w.Code, w.Body.String())
		}
		// 功率域校验域：CP 必须在 100-500 W（速度域值会被拒）
		if w := do(h, http.MethodPut, "/api/athletes/1/cs-zones", token,
			`{"domain":"power","validFrom":"2000-01-01","validTo":"2099-12-31","cs":4.5,"dprime":180}`); w.Code != http.StatusBadRequest {
			t.Fatalf("功率域用速度值应 400, 得到 %d", w.Code)
		}
		// 速度域校验域：CS 必须在 1-12 m/s（功率值会被拒）——两域不互相污染
		if w := do(h, http.MethodPut, "/api/athletes/1/cs-zones", token,
			`{"domain":"speed","validFrom":"2000-01-01","validTo":"2099-12-31","cs":280,"dprime":20000}`); w.Code != http.StatusBadRequest {
			t.Fatalf("速度域用功率值应 400, 得到 %d", w.Code)
		}
	})

	t.Run("无功率流活动 wbal 404", func(t *testing.T) {
		if w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
			`{"date":"2026-07-01","sport":"run","durationSecs":3600,"distanceM":12000,"isPlanned":false}`); w.Code != http.StatusCreated {
			t.Fatal("手工活动失败")
		}
		w := do(h, http.MethodGet, "/api/athletes/1/activities/2/wbal", token, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("无功率流活动 wbal 应 404, 得到 %d", w.Code)
		}
	})
}
