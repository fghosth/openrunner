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

// 上面那条用恒定功率——两种口径都得 250，对「有没有 30 秒滑动平均」完全瞎，
// 所以它当年对「NP 其实是裸 4 次方均值」这个 bug 没有任何防御力。
// 这条用带伪影尖峰的序列，才能把 Coggan NP 与裸 4 次方均值区分开。
func TestPowerChainNpUsesCogganWindow(t *testing.T) {
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

	// 250 W × 1h，中途混一个 1904 W 的单样本伪影（实测活动 281 的量级）
	n := 3600
	power := make([]float64, n)
	for i := range power {
		power[i] = 250
	}
	power[320] = 1904

	if err := computeMetrics(db.Gorm(), athlete, activity, &core.Stream{RecIntervalSecs: 1, Power: power}); err != nil {
		t.Fatalf("计算失败: %v", err)
	}
	m, err := getMetricsDTO(db.Gorm(), 1)
	if err != nil || m == nil {
		t.Fatalf("metrics 缺失: %v", err)
	}

	// 30 秒窗口把这一个样本稀释成 (29×250+1904)/30 ≈ 305，NP 应当仍贴着 250
	if m.Np > 280 {
		t.Errorf("单个伪影把 NP 抬到 %.1f W（基线 250 W）——落库的可能不是 Coggan 口径", m.Np)
	}
	// 必须与裸 4 次方均值明显不同，否则这条测试没有区分度
	bare := core.NormalizedSpeedKph(power)
	if !(bare > m.Np*1.1) {
		t.Errorf("裸 4 次方均值 %.1f 未明显高于落库 NP %.1f，测试失去区分度", bare, m.Np)
	}
	// PowerLoad 必须由落库的 NP 推导（用 IF²，NP 偏 40% 就是负荷偏 ~100%）
	if want := core.Rtss(float64(activity.TimerSecs), m.Np, 250); math.Abs(m.PowerLoad-want) > 1e-6 {
		t.Errorf("PowerLoad=%v 与落库 NP 推导值 %v 不符", m.PowerLoad, want)
	}
}

// 存量脏值必须能被重算覆盖——这是本次 NP 口径修正对已有数据生效的唯一路径。
// 上面那些测试都走「新导入」，没有任何一条证明 recompute 会把旧的裸 4 次方均值
// 改写成当前口径；如果 recompute 走的是另一条分支，修复合成就等于没修。
func TestRecomputeOverwritesStaleNp(t *testing.T) {
	_, db := newTestServer(t)

	athlete := &store.AthleteModel{ID: 1, Sex: "M"}
	if err := db.Gorm().Create(&store.CsZoneModel{
		AthleteID: 1, Domain: "power", ValidFrom: "2000-01-01", ValidTo: "2099-12-31",
		Cs: 250, Dprime: 20000, Source: "manual",
	}).Error; err != nil {
		t.Fatal(err)
	}

	activity := &store.ActivityModel{
		ID: 1, AthleteID: 1, Sport: "ride",
		StartTime:    time.Date(2026, 5, 1, 9, 0, 0, 0, time.Local).Unix(),
		DurationSecs: 600, TimerSecs: 600, DistanceM: 5000,
	}
	if err := db.Gorm().Create(activity).Error; err != nil {
		t.Fatal(err)
	}

	// 功率流：开头两个垃圾尖峰 + 稳态 250 W（复刻真实活动 281 的形状）
	power := []float64{1904, 1305}
	for i := 2; i < 600; i++ {
		power = append(power, 250)
	}
	blob, err := core.Stream{RecIntervalSecs: 1, Power: power}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Gorm().Create(&store.ActivityStreamModel{
		ActivityID: 1, RecIntervalSecs: 1, Data: blob,
	}).Error; err != nil {
		t.Fatal(err)
	}

	// 模拟旧口径留下的存量值：裸 4 次方均值 + 一个明显不对的负荷
	stale := core.NormalizedSpeedKph(power)
	if err := db.Gorm().Create(&store.ActivityMetricsModel{
		ActivityID: 1, Np: stale, PowerLoad: 999,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := recomputeMetrics(db.Gorm(), athlete, activity); err != nil {
		t.Fatalf("重算失败: %v", err)
	}
	m, err := getMetricsDTO(db.Gorm(), 1)
	if err != nil || m == nil {
		t.Fatalf("metrics 缺失: %v", err)
	}

	want := core.NormalizedPower(power, 1)
	if math.Abs(m.Np-want) > 1e-9 {
		t.Errorf("重算后 NP=%v，应为当前口径 %v", m.Np, want)
	}
	if math.Abs(m.Np-stale) < 1e-6 {
		t.Errorf("NP 仍是存量脏值 %v，重算没有覆盖", m.Np)
	}
	if want := core.Rtss(float64(activity.TimerSecs), m.Np, 250); math.Abs(m.PowerLoad-want) > 1e-6 {
		t.Errorf("PowerLoad=%v 未按重算后的 NP 更新（期望 %v）", m.PowerLoad, want)
	}
}
