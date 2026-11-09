package api

import (
	"math"
	"testing"
	"time"

	"github.com/fghosth/openrunner/core"
	"github.com/fghosth/openrunner/server/store"
)

// 双入口不变量（票 07 验收 4 的函数级验证，S3 缝内侧的 computeMetrics 单测）：
// 恒定数据下，"有 Stream 实测" 与 "仅 summary 估算" 两入口产出相同指标——
// 恒速时 4 次方均值=平均速度、逐秒 avgHR=均值，估算不再是近似。
func TestComputeMetricsDualEntryConstantData(t *testing.T) {
	h, db := newTestServer(t)
	_ = h
	athlete := &store.AthleteModel{ID: 1, Sex: "M"}
	// 区间 fixture（覆盖活动日期）
	if err := db.Gorm().Create(&store.HrZoneModel{AthleteID: 1, ValidFrom: "2000-01-01", ValidTo: "2099-12-31", MaxHr: 200, RestHr: 50, LtHr: 170}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Gorm().Create(&store.PaceZoneModel{AthleteID: 1, ValidFrom: "2000-01-01", ValidTo: "2099-12-31", ThresholdPaceSecsPerKm: 300}).Error; err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 6, 1, 8, 0, 0, 0, time.Local)
	activity := &store.ActivityModel{
		ID: 1, AthleteID: 1, Sport: "run",
		StartTime: start.Unix(), DurationSecs: 3600, TimerSecs: 3600, AvgHr: 170,
		DistanceM: 12000, // 12 kph 恒速 × 1h
	}

	// 恒定数据流：3600s，HR=170，速度=12kph
	n := 3600
	hr := make([]float64, n)
	sp := make([]float64, n)
	for i := 0; i < n; i++ {
		hr[i] = 170
		sp[i] = 12
	}
	stream := core.Stream{RecIntervalSecs: 1, HeartRate: hr, SpeedKph: sp}

	if err := computeMetrics(db.Gorm(), athlete, activity, &stream); err != nil {
		t.Fatalf("实测入口失败: %v", err)
	}
	exact, err := getMetricsDTO(db.Gorm(), 1)
	if err != nil || exact == nil {
		t.Fatalf("实测 metrics 缺失: %v", err)
	}
	if exact.IsEstimated {
		t.Fatal("有 Stream 不得标估算")
	}

	// 清掉实测结果，跑估算入口（同数据仅 summary）
	if err := db.Gorm().Where("activity_id = ?", 1).Delete(&store.ActivityMetricsModel{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := computeMetrics(db.Gorm(), athlete, activity, nil); err != nil {
		t.Fatalf("估算入口失败: %v", err)
	}
	est, err := getMetricsDTO(db.Gorm(), 1)
	if err != nil || est == nil {
		t.Fatalf("估算 metrics 缺失: %v", err)
	}
	if !est.IsEstimated {
		t.Fatal("summary 入口应标估算")
	}

	if math.Abs(est.Trimp-exact.Trimp) > 1e-6 {
		t.Fatalf("恒定数据下 TRIMP 应相等: 实测 %v vs 估算 %v", exact.Trimp, est.Trimp)
	}
	if math.Abs(est.Rtss-exact.Rtss) > 1e-6 {
		t.Fatalf("恒定数据下 rTSS 应相等: 实测 %v vs 估算 %v", exact.Rtss, est.Rtss)
	}
}
