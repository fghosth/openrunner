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

// 导出 → 导入闭环：新档案全量恢复；重复导入全跳过（幂等）。
func TestExportImportRoundTrip(t *testing.T) {
	h, _ := newTestServer(t)
	token := setupAdmin(t, h)
	aid := createAthlete(t, h, token) // 档案 1
	// 源档案铺数据：心率区间 + 阈值 + 赛季 + 比赛成绩 + 两条活动（一条计划带课型）
	if w := do(h, http.MethodPut, "/api/athletes/1/hr-zones", token,
		`{"validFrom":"2026-01-01","validTo":"2026-12-31","maxHr":190,"restHr":60,"ltHr":170}`); w.Code != http.StatusCreated {
		t.Fatal("hr zone")
	}
	if w := do(h, http.MethodPut, "/api/athletes/1/pace-zones", token,
		`{"validFrom":"2026-01-01","validTo":"2026-12-31","thresholdPaceSecsPerKm":280}`); w.Code != http.StatusCreated {
		t.Fatal("pace zone")
	}
	if w := do(h, http.MethodPut, "/api/athletes/1/seasons", token,
		`{"name":"2026春","startDate":"2026-01-01","endDate":"2026-06-30","seed":60}`); w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("season %d", w.Code)
	}
	if w := do(h, http.MethodPost, "/api/athletes/1/race-results", token,
		`{"date":"2026-05-01","distanceM":10000,"timeSecs":2400,"name":"春季10K"}`); w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("race %d", w.Code)
	}
	if w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
		`{"date":"2026-06-01","sport":"run","durationSecs":3600,"distanceM":10000,"avgHr":150}`); w.Code != http.StatusCreated {
		t.Fatal("act1")
	}
	if w := do(h, http.MethodPost, "/api/athletes/1/activities", token,
		`{"date":"2026-06-02","sport":"run","durationSecs":1800,"distanceM":5000,"avgHr":0,"isPlanned":true,"type":"tempo"}`); w.Code != http.StatusCreated {
		t.Fatal("act2")
	}
	_ = aid

	// 导出
	w := do(h, http.MethodGet, "/api/athletes/1/export", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("export %d", w.Code)
	}
	archive := w.Body.String()

	// 新档案 2 导入
	aid2 := do(h, http.MethodPost, "/api/athletes", token, `{"name":"恢复档案"}`)
	if aid2.Code != http.StatusCreated && aid2.Code != http.StatusOK {
		t.Fatalf("athlete2 %d", aid2.Code)
	}
	w2 := do(h, http.MethodPost, "/api/athletes/2/import", token, archive)
	if w2.Code != http.StatusOK {
		t.Fatalf("import %d %s", w2.Code, w2.Body.String())
	}
	var res importResult
	_ = json.Unmarshal(w2.Body.Bytes(), &res)
	if res.ActivitiesImported != 2 || res.ZonesImported != 2 || res.SeasonsImported != 1 || res.RacesImported != 1 {
		t.Fatalf("导入计数不对: %+v", res)
	}
	// 计划课型带过来了
	lw := do(h, http.MethodGet, "/api/athletes/2/activities", token, "")
	var acts []map[string]any
	_ = json.Unmarshal(lw.Body.Bytes(), &acts)
	foundTempo := false
	for _, a := range acts {
		if a["isPlanned"] == true && a["planType"] == "tempo" {
			foundTempo = true
		}
	}
	if !foundTempo {
		t.Fatal("导入后计划课型 tempo 丢失")
	}
	// 幂等：同一档案再导一次全跳过
	w3 := do(h, http.MethodPost, "/api/athletes/2/import", token, archive)
	var res2 importResult
	_ = json.Unmarshal(w3.Body.Bytes(), &res2)
	if res2.ActivitiesImported != 0 || res2.ZonesImported != 0 || res2.SeasonsImported != 0 || res2.RacesImported != 0 {
		t.Fatalf("重复导入应全跳过: %+v", res2)
	}
	if res2.ActivitiesSkipped != 2 || res2.ZonesSkipped != 2 {
		t.Fatalf("跳过计数不对: %+v", res2)
	}
	// 坏 JSON 拒绝
	if w := do(h, http.MethodPost, "/api/athletes/2/import", token, `{not json`); w.Code != http.StatusBadRequest {
		t.Fatalf("坏 JSON 应 400，得到 %d", w.Code)
	}
}

// v1 归档（无目标/简介/头像字段）导入不得把档案设置清零（用户实测回归）。
func TestImportV1ArchiveKeepsProfile(t *testing.T) {
	h, _ := newTestServer(t)
	token := setupAdmin(t, h)
	_ = createAthlete(t, h, token)
	// 目标档案先有自己的设置
	if w := do(h, http.MethodPut, "/api/athletes/1", token,
		`{"name":"我","sex":"M","bio":"认真跑步","yearlyGoalKm":2860,"monthlyGoalKm":238,"weeklyGoalKm":55}`); w.Code != http.StatusOK {
		t.Fatalf("设置档案失败 %d", w.Code)
	}
	v1 := `{"schemaVersion":1,"exportedAt":"2026-01-01T00:00:00Z","athlete":{"id":9,"name":"别人","sex":"F"},"hrZones":[],"paceZones":[],"csZones":[],"seasons":[],"activities":[]}`
	w := do(h, http.MethodPost, "/api/athletes/1/import", token, v1)
	if w.Code != http.StatusOK {
		t.Fatalf("import %d %s", w.Code, w.Body.String())
	}
	lw := do(h, http.MethodGet, "/api/athletes", token, "")
	var list []map[string]any
	_ = json.Unmarshal(lw.Body.Bytes(), &list)
	a := list[0]
	if int(a["yearlyGoalKm"].(float64)) != 2860 || int(a["monthlyGoalKm"].(float64)) != 238 || int(a["weeklyGoalKm"].(float64)) != 55 {
		t.Fatalf("v1 导入把目标清零了: %v", a)
	}
	if a["bio"] != "认真跑步" {
		t.Fatalf("v1 导入把简介清了: %v", a["bio"])
	}
	// v2 显式带 0 目标 → 应生效（区分「没写」与「写了 0」）
	v2 := `{"schemaVersion":2,"athlete":{"id":9,"name":"别人","sex":"F","bio":"","yearlyGoalKm":0,"monthlyGoalKm":0,"weeklyGoalKm":0},"hrZones":[],"paceZones":[],"csZones":[],"seasons":[],"activities":[]}`
	if w := do(h, http.MethodPost, "/api/athletes/1/import", token, v2); w.Code != http.StatusOK {
		t.Fatalf("v2 import %d", w.Code)
	}
	lw = do(h, http.MethodGet, "/api/athletes", token, "")
	_ = json.Unmarshal(lw.Body.Bytes(), &list)
	if int(list[0]["yearlyGoalKm"].(float64)) != 0 {
		t.Fatalf("v2 显式 0 目标应覆盖，得到 %v", list[0]["yearlyGoalKm"])
	}
}
