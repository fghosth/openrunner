package api

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
)

// 金标准对照的 Go 侧输出：把生产路径算出的 PMC 序列写成 JSON，
// 交给 scripts/golden-check.py 与「照发表公式独立实现的 Python」逐项比对。
//
// 走的是 handleAthletePMC 同一条路径（loadDailyLoads → ListSeasons →
// buildPMCInput → core.ComputePMC），不是另写一份聚合逻辑——否则验的是
// 我对聚合的第二次理解，而不是真正上线的那条路。
//
// 默认跳过，只在显式给出库路径时运行：
//
//	OPENRUNNER_GOLDEN_DB=~/.openrunner/openrunner.db OPENRUNNER_GOLDEN_ATHLETE=1 \
//	OPENRUNNER_GOLDEN_OUT=/tmp/go_pmc.json \
//	go test ./api -run TestGoldenDumpPMC -count=1
func TestGoldenDumpPMC(t *testing.T) {
	dbPath := os.Getenv("OPENRUNNER_GOLDEN_DB")
	if dbPath == "" {
		t.Skip("未设置 OPENRUNNER_GOLDEN_DB，跳过金标准对照")
	}
	athleteID := 1
	if v := os.Getenv("OPENRUNNER_GOLDEN_ATHLETE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("OPENRUNNER_GOLDEN_ATHLETE 非法: %v", err)
		}
		athleteID = n
	}
	metric := os.Getenv("OPENRUNNER_GOLDEN_METRIC")
	if metric != "rtss" {
		metric = "trimp"
	}
	out := os.Getenv("OPENRUNNER_GOLDEN_OUT")
	if out == "" {
		out = "/tmp/go_pmc.json"
	}

	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	defer db.Close()
	gdb := db.Gorm()

	// 与 handler 完全一致的调用顺序与默认值
	rows, err := loadDailyLoads(gdb, uint(athleteID), metric)
	if err != nil {
		t.Fatalf("聚合失败: %v", err)
	}
	seasons, err := store.ListSeasons(gdb, uint(athleteID))
	if err != nil {
		t.Fatalf("赛季查询失败: %v", err)
	}
	today := time.Now().Local().Format("2006-01-02")
	in := buildPMCInput(today, rows, seasons)
	series := core.ComputePMC(in)

	payload := map[string]any{
		"today":      today,
		"metric":     metric,
		"athlete_id": athleteID,
		"from":       in.From,
		"to":         in.To,
		"series":     series,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatalf("写出失败: %v", err)
	}
	t.Logf("已写出 %s：%d 天，%s 负荷，区间 %s → %s", out, len(series.Dates), metric, in.From, in.To)
}

