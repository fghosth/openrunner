package coach

import (
	"math"
	"testing"
)

// 配速/预测移植的测试。数值金标准：zones.py / paces.py（MIT），
// 完整对照在 openrunner/scripts/coach-check.py；这里放锚点与形状断言。

func TestParseFormatPace(t *testing.T) {
	if v, err := ParsePace("4:10"); err != nil || v != 250 {
		t.Errorf(`ParsePace("4:10") = %v, %v；期望 250`, v, err)
	}
	if _, err := ParsePace("abc"); err == nil {
		t.Error("非法配速应报错")
	}
	if got := FormatPace(300.4); got != "5:00" {
		t.Errorf("FormatPace(300.4) = %s，期望 5:00", got)
	}
	if got := FormatPace(0); got != "--" {
		t.Errorf("FormatPace(0) = %s，期望 --", got)
	}
	if v, err := ParseTime("1:34:23"); err != nil || v != 5663 {
		t.Errorf(`ParseTime("1:34:23") = %v, %v；期望 5663`, v, err)
	}
	if v, err := ParseTime("48:30"); err != nil || v != 2910 {
		t.Errorf(`ParseTime("48:30") = %v, %v；期望 2910`, v, err)
	}
	if got := FormatTime(14425); got != "4:00:25" {
		t.Errorf("FormatTime(14425) = %s，期望 4:00:25", got)
	}
	if got := FormatTime(2910); got != "48:30" {
		t.Errorf("FormatTime(2910) = %s，期望 48:30", got)
	}
}

func TestVDOTAnchor(t *testing.T) {
	// 出版表锚点：10K 40:00 → VDOT ≈ 51.9（Daniels' Running Formula）
	vd := VDOT(10, 2400)
	if math.Abs(vd-51.9) > 0.15 {
		t.Errorf("VDOT(10K,40:00) = %.2f，期望 ≈51.9", vd)
	}
	// 往返一致性在全马距离上做（zones.py 的二分范围 60–600 分钟，
	// 10K 的 40 分钟在范围外——第一版测试在这里算错了期望）
	back := RaceSecsForVDOT(vd, 42.195)
	if back < 165*60 || back > 200*60 {
		t.Errorf("RaceSecsForVDOT(VDOT 51.9, 全马) = %.0fs，期望 165–200 分钟量级", back)
	}
	// 同距离往返（用时落在二分范围内时）
	rt := RaceSecsForVDOT(VDOT(42.195, 10800), 42.195)
	if math.Abs(rt-10800) > 2 {
		t.Errorf("全马往返 = %.1fs，期望 10800", rt)
	}
	// 单调：更快成绩 → 更高 VDOT
	if !(VDOT(10, 2300) > VDOT(10, 2400)) {
		t.Error("VDOT 应随成绩变快而升高")
	}
}

func TestDanielsRows(t *testing.T) {
	vd, rows := Daniels(10, 2400)
	if vd <= 0 || len(rows) != 5 {
		t.Fatalf("Daniels 应返回 VDOT 与 5 行，得 %v / %d 行", vd, len(rows))
	}
	byName := map[string]DanielsRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	// 配速次序：E 慢于 T，T 慢于 I，I 慢于 R（秒/公里越大越慢）
	if !(byName["E"].PaceSlow > byName["T"].PaceSlow) {
		t.Errorf("E 应慢于 T: E=%.0f T=%.0f", byName["E"].PaceSlow, byName["T"].PaceSlow)
	}
	if !(byName["T"].PaceSlow > byName["I"].PaceSlow) {
		t.Errorf("T 应慢于 I")
	}
	if !(byName["I"].PaceSlow > byName["R"].PaceSlow) {
		t.Errorf("I 应慢于 R")
	}
	// M 是单值（全马配速本身）
	if math.Abs(byName["M"].PaceFast-byName["M"].PaceSlow) > 1e-9 {
		t.Error("M 档 fast 应等于 slow")
	}
	// T 档应接近阈值（VDOT 51.9 的 T ≈ 4:45±10s/km，出版表 VDOT 52 行）
	// T 档：VDOT 51.9 的 86–88% VO2max → ≈4:10/km（250s 上下）。
	// 第一版期望 270–300 是手算错——zones.py 金标准对照才是数值裁判。
	if p := byName["T"].PaceSlow; p < 240 || p > 262 {
		t.Errorf("T 配速 %.0f s/km 超出 VDOT 51.9 的合理范围（≈4:00–4:22）", p)
	}
}

func TestSpeedZoneTable(t *testing.T) {
	thr := 280.0 // 4:40/km
	rows := SpeedZoneTable(thr, 165)
	if len(rows) != 7 {
		t.Fatalf("80/20 七区应有 7 行，得 %d", len(rows))
	}
	// Z3 上界恰为阈值配速（100%）
	var z3, z5 PaceZoneRow
	for _, r := range rows {
		switch r.Name {
		case "Z3":
			z3 = r
		case "Z5":
			z5 = r
		}
	}
	if math.Abs(z3.PaceFast-thr) > 0.01 {
		t.Errorf("Z3 上界 = %.1f，期望阈值 %.1f", z3.PaceFast, thr)
	}
	if z5.PaceFast != 0 || z5.PctHi != 0 {
		t.Error("Z5 无上界")
	}
	if z5.HRHi != 0 {
		t.Error("Z5 心率无上界")
	}
	// HR 区间随 lthr 缩放：Z2 上界 = 0.902×165 ≈ 149
	if z2 := rows[1]; math.Abs(float64(z2.HRHi)-math.Round(0.902*165)) > 1 {
		t.Errorf("Z2 HR 上界 = %d，期望 %d", z2.HRHi, int(math.Round(0.902*165)))
	}
}

func TestThresholdFromRace(t *testing.T) {
	// 10K 48:48（292.8 s/km）× 系数 0.975 → 阈值 ≈ 300.3 s/km
	thr := ThresholdFromRace(10, 2928)
	if math.Abs(thr-300.3) > 0.5 {
		t.Errorf("ThresholdFromRace = %.1f，期望 ≈300.3", thr)
	}
	// 距离取最近档：15km 应贴 10 档而不是半马档
	thr15 := ThresholdFromRace(15, 15*300) // 配速 300
	if thr15 <= 300 || thr15 > 300/0.93+0.01 {
		t.Errorf("15km 系数选取异常: %f", thr15)
	}
}

func TestVickersAndTanda(t *testing.T) {
	// 金标准锚点（paces.py 实跑）：10K 48:48 + 50km/周 → Vickers1 = 4:00:25
	m1 := VickersModel1(10000, 2928, 50/1.609344)
	if math.Abs(m1-14425) > 2 {
		t.Errorf("VickersModel1 = %.0fs，期望 14425（4:00:25）", m1)
	}
	// Vickers2 需要两个不同距离成绩；单测验证形状与量纲
	m2 := VickersModel2(10000, 2928, 21097.5, 6300, 50/1.609344)
	if m2 < 12000 || m2 > 18000 {
		t.Errorf("VickersModel2 = %.0fs，量纲异常", m2)
	}
	// Tanda(50km/周, 6:00/km) ≈ 3:46:48（手算 13608s）
	td := Tanda(50, 360)
	if math.Abs(td-13608) > 5 {
		t.Errorf("Tanda = %.0fs，期望 ≈13608", td)
	}
}

func TestPredictMarathon(t *testing.T) {
	// 10K 48:48，周跑量 50km
	p := PredictMarathon(PredictInput{BaseKm: 10, BaseSecs: 2928, WeeklyKm: 50})
	// Riegel 固定 1.06：2928*(42.195/10)^1.06 ≈ 3:44:29（此前 paces.py 实跑值）
	if math.Abs(p.Riegel-13469) > 3 {
		t.Errorf("Riegel = %.0fs，期望 ≈13469（3:44:29）", p.Riegel)
	}
	if math.Abs(p.Vickers1-14425) > 2 {
		t.Errorf("Vickers1 = %.0fs，期望 14425", p.Vickers1)
	}
	// recommended = 最慢可信值（skill 纪律）
	if p.Recommended < p.Riegel {
		t.Errorf("recommended 应取最慢值: rec=%.0f riegel=%.0f", p.Recommended, p.Riegel)
	}
	// 无周跑量 → 只有 Riegel，recommended 即 Riegel
	p2 := PredictMarathon(PredictInput{BaseKm: 10, BaseSecs: 2928})
	if p2.Vickers1 != 0 || p2.Tanda != 0 {
		t.Error("无训练量输入时不应产出 Vickers/Tanda")
	}
	if math.Abs(p2.Recommended-p2.Riegel) > 1e-9 {
		t.Error("只有 Riegel 时 recommended 应为 Riegel")
	}
	// 基准已是全马或更长 → Vickers1 不适用（模型定义要求非全马成绩）
	p3 := PredictMarathon(PredictInput{BaseKm: 42.195, BaseSecs: 14000, WeeklyKm: 50})
	if p3.Vickers1 != 0 {
		t.Error("全马成绩不应产出 Vickers1")
	}
}
