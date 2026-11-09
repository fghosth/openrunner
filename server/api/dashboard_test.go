package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fghosth/openrunner/server/store"
)

func TestDashboardAndPersonalBest(t *testing.T) {
	h, token := setupWithToken(t)
	_ = createAthlete(t, h, token)

	t.Run("无活动时返回零值而非报错", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/dashboard", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		var d struct {
			Year   int   `json:"year"`
			Daily  []any `json:"daily"`
			Totals struct {
				All struct {
					Count int `json:"count"`
				} `json:"all"`
			} `json:"totals"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		if d.Year == 0 {
			t.Fatal("year 不应为 0")
		}
		if d.Totals.All.Count != 0 {
			t.Fatalf("空档案的活动数应为 0, 得到 %d", d.Totals.All.Count)
		}
	})

	t.Run("个人最佳在无速度流时返回空数组", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/1/personal-best", token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("期望 200, 得到 %d: %s", w.Code, w.Body.String())
		}
		var out []any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("响应不是合法 JSON 数组: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("没有跑步数据时应返回空数组, 得到 %d 项", len(out))
		}
	})

	t.Run("档案不存在返回 404", func(t *testing.T) {
		w := do(h, http.MethodGet, "/api/athletes/999/dashboard", token, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("期望 404, 得到 %d", w.Code)
		}
	})
}

func TestSyncDedupByBusinessKey(t *testing.T) {
	// 同一活动重复同步必须被「开始时间 + 类型」拦住：
	// FIT 字节每次下载都不同（content_hash 失效），业务键才稳定。
	h, dbw := newTestServer(t)
	token := setupAdmin(t, h)
	_ = createAthlete(t, h, token)

	db := dbw.Gorm()
	start := int64(1792569600) // 固定时间点
	m := store.ActivityModel{
		AthleteID: 1, Sport: "run", StartTime: start, DistanceM: 5000,
	}
	if err := db.Create(&m).Error; err != nil {
		t.Fatalf("插入活动失败: %v", err)
	}

	dup, err := store.FindActivityByStartAndSport(db, 1, start, "run")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if dup == nil || dup.ID != m.ID {
		t.Fatal("相同开始时间+类型应命中已有活动（去重生效）")
	}

	// 不同运动类型不算重复
	if other, err := store.FindActivityByStartAndSport(db, 1, start, "ride"); err != nil || other != nil {
		t.Fatalf("不同运动类型不应命中, got %v err %v", other, err)
	}
	// 不同档案不算重复
	if other, err := store.FindActivityByStartAndSport(db, 2, start, "run"); err != nil || other != nil {
		t.Fatalf("不同档案不应命中, got %v err %v", other, err)
	}
}
