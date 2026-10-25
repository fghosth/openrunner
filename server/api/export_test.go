package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestExportArchive(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	// 造数据：区间 + 赛季 + CS + 导入活动（含 stream/metrics）+ 手工计划活动
	if w := do(h, http.MethodPut, "/api/athletes/1/hr-zones", token,
		`{"validFrom":"2000-01-01","validTo":"2099-12-31","maxHr":200,"restHr":50,"ltHr":170}`); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}
	if w := do(h, http.MethodPut, "/api/athletes/1/pace-zones", token,
		`{"validFrom":"2000-01-01","validTo":"2099-12-31","thresholdPaceSecsPerKm":300}`); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}
	if w := do(h, http.MethodPut, "/api/athletes/1/cs-zones", token,
		`{"domain":"speed","validFrom":"2000-01-01","validTo":"2099-12-31","cs":4.5,"dprime":180}`); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}
	if w := do(h, http.MethodPut, "/api/athletes/1/seasons", token,
		`{"name":"S1","startDate":"2026-01-01","endDate":"2026-12-31","seed":100}`); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}
	if w := uploadFIT(t, h, token, "1", tdFiles+"simple.gpx"); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}
	if w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
		`{"date":"2026-05-01","sport":"run","durationSecs":3600,"distanceM":10000,"avgHr":150,"isPlanned":true}`); w.Code != http.StatusCreated {
		t.Fatal(w.Code)
	}

	t.Run("导出包含 schema 版本与全部实体", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/export", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("期望 JSON Content-Type, 得到 %q", ct)
		}
		if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
			t.Errorf("期望 attachment, 得到 %q", cd)
		}

		var dump struct {
			SchemaVersion int    `json:"schemaVersion"`
			ExportedAt    string `json:"exportedAt"`
			Athlete       struct {
				ID   uint   `json:"id"`
				Name string `json:"name"`
			} `json:"athlete"`
			Activities []struct {
				ID        uint   `json:"id"`
				IsPlanned bool   `json:"isPlanned"`
				HasStream bool   `json:"hasStream"`
				Metrics   any    `json:"metrics"`
				Stream    []byte `json:"stream"` // base64
			} `json:"activities"`
			HrZones   []any `json:"hrZones"`
			PaceZones []any `json:"paceZones"`
			CsZones   []any `json:"csZones"`
			Seasons   []any `json:"seasons"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &dump); err != nil {
			t.Fatalf("导出不是合法 JSON: %v", err)
		}
		if dump.SchemaVersion < 1 {
			t.Errorf("期望 schemaVersion≥1, 得到 %d", dump.SchemaVersion)
		}
		if dump.ExportedAt == "" {
			t.Error("期望 exportedAt")
		}
		if dump.Athlete.Name != "测试档案" {
			t.Errorf("期望档案名, 得到 %q", dump.Athlete.Name)
		}
		if len(dump.Activities) != 2 {
			t.Fatalf("期望 2 个活动, 得到 %d", len(dump.Activities))
		}
		// 有 stream 的活动带 base64 数据；手工活动无
		streamCount := 0
		plannedCount := 0
		for _, a := range dump.Activities {
			if len(a.Stream) > 0 {
				streamCount++
			}
			if a.IsPlanned {
				plannedCount++
			}
			if a.HasStream && a.Metrics == nil {
				t.Error("有 stream 的活动应带 metrics")
			}
		}
		if streamCount != 1 {
			t.Errorf("期望 1 个活动带 stream, 得到 %d", streamCount)
		}
		if plannedCount != 1 {
			t.Errorf("期望 1 个计划活动, 得到 %d", plannedCount)
		}
		if len(dump.HrZones) != 1 || len(dump.PaceZones) != 1 || len(dump.CsZones) != 1 || len(dump.Seasons) != 1 {
			t.Errorf("期望各配置 1 条, 得到 hr=%d pace=%d cs=%d season=%d",
				len(dump.HrZones), len(dump.PaceZones), len(dump.CsZones), len(dump.Seasons))
		}
	})

	t.Run("重复导出内容稳定（可 diff）", func(t *testing.T) {
		w1 := do(h, http.MethodGet, "/api/athletes/1/export", token, "")
		w2 := do(h, http.MethodGet, "/api/athletes/1/export", token, "")
		var a, b struct {
			SchemaVersion int                 `json:"schemaVersion"`
			Activities    []struct{ ID uint } `json:"activities"`
		}
		_ = json.Unmarshal(w1.Body.Bytes(), &a)
		_ = json.Unmarshal(w2.Body.Bytes(), &b)
		if a.SchemaVersion != b.SchemaVersion || len(a.Activities) != len(b.Activities) {
			t.Fatal("两次导出结构不一致")
		}
	})

	t.Run("无活动的档案也能导出", func(t *testing.T) {
		if w := do(h, http.MethodPost, "/api/athletes", token, `{"name":"空档案"}`); w.Code != http.StatusCreated {
			t.Fatal(w.Code)
		}
		w := do(h, http.MethodGet, "/api/athletes/2/export", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d", w.Code)
		}
		var dump struct {
			Activities []any `json:"activities"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &dump)
		if dump.Activities == nil {
			t.Error("期望 activities 为空数组而非 null")
		}
	})

	t.Run("不存在的档案 404", func(t *testing.T) {
		if w := do(h, http.MethodGet, "/api/athletes/999/export", token, ""); w.Code != http.StatusNotFound {
			t.Fatalf("期望 404, 得到 %d", w.Code)
		}
	})
}
