package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/openrunner/server/store"
)

// 票 07：确定性计算段（第八节）的含/不含形态——8 种组合快照断言。

func promptMarkdown(t *testing.T, h http.Handler, token string, q string) string {
	t.Helper()
	w := do(h, http.MethodGet, "/api/athletes/1/ai-prompt?"+q, token, "")
	if w.Code != 200 {
		t.Fatalf("ai-prompt %s → %d: %s", q, w.Code, w.Body.String())
	}
	var resp struct {
		Markdown string `json:"markdown"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Markdown
}

func TestAIPromptDeterministicSections(t *testing.T) {
	h, db := newTestServer(t)
	token := setupAdmin(t, h)
	aid := createAthlete(t, h, token)

	// 范围内至少一条活动（否则端点 400）
	insertRun(t, db, aid, time.Now().AddDate(0, 0, -2), 60, 12, false)

	has := func(md, sub string) bool { return strings.Contains(md, sub) }
	check := func(name, md string, want81, want82, want83 bool) {
		t.Helper()
		for _, c := range []struct {
			tag  string
			want bool
			got  bool
		}{
			{"### 8.1", want81, has(md, "### 8.1")},
			{"### 8.2", want82, has(md, "### 8.2")},
			{"### 8.3", want83, has(md, "### 8.3")},
		} {
			if c.want != c.got {
				t.Errorf("%s：%s 期望 %v，实际 %v", name, c.tag, c.want, c.got)
			}
		}
		if !has(md, "## 九、请回答") {
			t.Errorf("%s：提问节应为第九节", name)
		}
		if want81 || want82 || want83 {
			if !has(md, "不得重新计算或修改这些数值") {
				t.Errorf("%s：应含「不得重算」指令", name)
			}
			if !has(md, "## 八、系统确定性计算结果") {
				t.Errorf("%s：应有第八节标题", name)
			}
		} else if has(md, "## 八、") {
			t.Errorf("%s：全缺数据时第八节应整体省略", name)
		}
	}

	// 组合 1：全空
	check("①无 coach 数据", promptMarkdown(t, h, token, "range=month&goal=10k"), false, false, false)

	seedPace := func() {
		if err := db.Gorm().Create(&store.PaceZoneModel{
			AthleteID: aid, ValidFrom: "2000-01-01", ValidTo: "9999-12-31", ThresholdPaceSecsPerKm: 280,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	seedRace := func() {
		if err := db.Gorm().Create(&store.RaceResultModel{
			AthleteID: aid, Date: "2026-01-10", DistanceM: 10000, TimeSecs: 2400,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	seedPlan := func() {
		nm := nextMonday()
		insertRun(t, db, aid, nm, 60, 12, true)
	}

	// 组合 2：仅阈值 → 8.2
	seedPace()
	check("②仅 pace_zones", promptMarkdown(t, h, token, "range=month&goal=10k"), false, true, false)

	// 组合 3：仅比赛 → 8.2（race 反推）+ 8.3
	seedRace()
	check("③仅比赛", promptMarkdown(t, h, token, "range=month&goal=half"), false, true, true)

	// 组合 4：仅计划课 → 8.1
	seedPlan()
	check("④含下周计划", promptMarkdown(t, h, token, "range=month&goal=5k"), true, true, true)

	// 组合 5：quarter 范围同样带全三段
	check("⑤quarter", promptMarkdown(t, h, token, "range=quarter&goal=marathon"), true, true, true)

	// 组合 6：year
	check("⑥year", promptMarkdown(t, h, token, "range=year&goal=10k"), true, true, true)

	// 组合 7：week 范围
	check("⑦week", promptMarkdown(t, h, token, "range=week&goal=10k"), true, true, true)

	// 组合 8：单次活动范围 → 8.1 省略（周校验不适用），8.2/8.3 仍在
	var act store.ActivityModel
	db.Gorm().Where("is_planned = ?", false).First(&act)
	md := promptMarkdown(t, h, token, "range=activity&goal=10k&activityId="+strconv.Itoa(int(act.ID)))
	check("⑧单次活动", md, false, true, true)

	// 内容抽查：配速表含 Daniels 与 80/20 行、预测含推荐
	if !strings.Contains(md, "Daniels VDOT") || !strings.Contains(md, "80/20 七区") {
		t.Error("8.2 应含 Daniels 与 80/20 行")
	}
	if !strings.Contains(md, "推荐（最慢可信值）") {
		t.Error("8.3 应含推荐值行")
	}
}
