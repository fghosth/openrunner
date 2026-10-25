package api

import (
	"math"
	"testing"
	"time"

	"github.com/openrunner/core"
	"github.com/openrunner/server/store"
)

// 功率链函数级测试（票 09①）：现有 FIT fixture 无功率数据，故用合成流验证
// NP / PowerLoad / PowerTiZ 的产出与数学正确性（S3 层门控由 csd_test 覆盖）。
func TestPowerChainMetricsSynthetic(t *testing.T) {
	h, db := newTestServer(t)
	_ = h

	athlete := &store.AthleteModel{ID: 1, Sex: "M"}
	if err := db.Gorm().Create(&store.CsZoneModel{
		AthleteID: 1, Domain: "power", ValidFrom: "2000-01-01", ValidTo: "2099-12-31",
		Cs: 250, Dprime: 20000, Source: "manual",
	}).Error; err != nil {
		t.Fatal(err)
	}

	start := time.Date(2026, 5, 1, 9, 0, 0, 0, time.Local)
	activity := &store.ActivityModel{
		ID: 1, AthleteID: 1, Sport: "ride",
		StartTime: start.Unix(), DurationSecs: 3600, TimerSecs: 3600,
		DistanceM: 30000,
	}

	// 恒定 250 W（=CP）× 1h：NP=250、IF=1、PowerLoad=100、TiZ 全 Z4（100%CV 落在 95-105 区）
	n := 3600
	power := make([]float64, n)
	for i := 0; i < n; i++ {
		power[i] = 250
	}
	stream := core.Stream{RecIntervalSecs: 1, Power: power}

	if err := computeMetrics(db.Gorm(), athlete, activity, &stream); err != nil {
		t.Fatalf("计算失败: %v", err)
	}
	m, err := getMetricsDTO(db.Gorm(), 1)
	if err != nil || m == nil {
		t.Fatalf("metrics 缺失: %v", err)
	}

	if math.Abs(m.Np-250) > 1e-6 {
		t.Errorf("期望 NP=250, 得到 %v", m.Np)
	}
	if math.Abs(m.PowerLoad-100) > 1e-6 {
		t.Errorf("期望 PowerLoad=100（1h@CP）, 得到 %v", m.PowerLoad)
	}
	if len(m.PowerTiZ) != 6 {
		t.Fatalf("期望 6 区, 得到 %v", m.PowerTiZ)
	}
	// 250/250 = 100% 落在 Z4（95-105%）
	if m.PowerTiZ[3] != 3600 {
		t.Errorf("期望 Z4=3600s, 得到 %v", m.PowerTiZ)
	}
	if m.IsEstimated {
		t.Error("有流活动不得标估算")
	}

	t.Run("2×CP 超强 → PowerLoad=400", func(t *testing.T) {
		if err := db.Gorm().Where("activity_id = ?", 1).Delete(&store.ActivityMetricsModel{}).Error; err != nil {
			t.Fatal(err)
		}
		hard := make([]float64, n)
		for i := range hard {
			hard[i] = 500
		}
		if err := computeMetrics(db.Gorm(), athlete, activity, &core.Stream{RecIntervalSecs: 1, Power: hard}); err != nil {
			t.Fatal(err)
		}
		m2, _ := getMetricsDTO(db.Gorm(), 1)
		if math.Abs(m2.PowerLoad-400) > 1e-6 {
			t.Errorf("期望 400（2² × 100）, 得到 %v", m2.PowerLoad)
		}
	})
}
