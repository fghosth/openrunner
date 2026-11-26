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
	// 金标准锚点（paces.py 实跑）：10K 48:48 + 半马 1:45 + 50km/周 → 14107s
	m2 := VickersModel2(10000, 2928, 21097.5, 6300, 50/1.609344)
	if math.Abs(m2-14107) > 2 {
		t.Errorf("VickersModel2 = %.0fs，期望 14107", m2)
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

func TestSameRaceDistance(t *testing.T) {
	// 同一场半马的两种记法：相对差 0.012%
	if !SameRaceDistance(21.0975, 21.1) {
		t.Error("21.0975 与 21.1 应视为同一距离")
	}
	// 20km 与半马差 5.2%，是真正不同距离，不能被容差吞掉
	if SameRaceDistance(20, 21.0975) {
		t.Error("20km 与半马不应视为同一距离")
	}
	if SameRaceDistance(0, 21.1) || SameRaceDistance(10, 0) {
		t.Error("零距离不应判为同一距离")
	}
}

func TestVickersModel2Guards(t *testing.T) {
	miles := 55 / 1.609344
	// 事故复现：同一场半马被记成 21097.5 与 21100，ln(d2/d1)≈0 使 k21 冲到 -1343，
	// 幂运算溢出成 2.6e+100 秒。上游 paces.py 同样会炸，这是两边共有的缺陷。
	if v := VickersModel2(21097.5, 7000, 21100, 5970, miles); v != 0 {
		t.Errorf("同一距离的两场应返回 0，实得 %v", v)
	}
	// 较长一场已是全马 → 模型定义不适用（d2=42195 时结果退化成 t2 本身）
	if v := VickersModel2(10000, 2641, 42195, 12000, miles); v != 0 {
		t.Errorf("全马距离作 d2 应返回 0，实得 %v", v)
	}
	for _, bad := range [][5]float64{{0, 100, 20000, 100, 10}, {10000, 0, 20000, 100, 10}, {-1, 100, 20000, 100, 10}} {
		if v := VickersModel2(bad[0], bad[1], bad[2], bad[3], bad[4]); v != 0 {
			t.Errorf("非法参数 %v 应返回 0，实得 %v", bad, v)
		}
	}
	// 与 paces.py 的有意偏离：参照要求调用方把距离排好序，本实现自己排。
	// 量出来的偏离：paces.vickers_model2(21097.5,5970,10000,2641)=13689s，
	// 排序后=13183s，差 506s（8 分半）—— 所以排序是必要的，不是可选的。
	ordered := VickersModel2(10000, 2641, 21097.5, 5970, miles)
	swapped := VickersModel2(21097.5, 5970, 10000, 2641, miles)
	if math.Abs(ordered-swapped) > 1e-9 {
		t.Errorf("输入顺序不应影响结果: %.0f vs %.0f", ordered, swapped)
	}
	// 金标准锚点（paces.py 实跑）：10K 44:01 + 半马 1:39:30 + 55km/周 → 13183s
	if math.Abs(ordered-13183) > 2 {
		t.Errorf("VickersModel2 = %.0fs，期望 13183", ordered)
	}
}

func TestPredictMarathonDuplicateHalfDistance(t *testing.T) {
	// 事故场景：基准=半马 21.1(1:39:30)，库里另有一条 21.0975(1:56:40) 的半马。
	// 修复前 Race2 会取那条 21.0975，Vickers2 溢出，推荐值（取最慢）跟着变成垃圾数字。
	p := PredictMarathon(PredictInput{
		BaseKm: 21.1, BaseSecs: 5970, Race2Km: 21.0975, Race2Secs: 7000, WeeklyKm: 55,
	})
	if p.Vickers2 != 0 {
		t.Errorf("同一距离的第二场不应产出 Vickers2，实得 %v", p.Vickers2)
	}
	if !plausibleMarathonSecs(p.Recommended) {
		t.Errorf("推荐值必须落在可信区间，实得 %v", p.Recommended)
	}
	// 换成真正不同距离的第二场 → 金标准 13183s
	p2 := PredictMarathon(PredictInput{
		BaseKm: 21.1, BaseSecs: 5970, Race2Km: 10, Race2Secs: 2641, WeeklyKm: 55,
	})
	if math.Abs(p2.Vickers2-13183) > 2 {
		t.Errorf("Vickers2 = %.0fs，期望 13183", p2.Vickers2)
	}
	// 推荐值 = 所有可信预测里最慢的那个（skill 纪律）
	want := p2.Riegel
	for _, v := range []float64{p2.Vickers1, p2.Vickers2} {
		if v > want {
			want = v
		}
	}
	if math.Abs(p2.Recommended-want) > 1e-6 {
		t.Errorf("推荐值应取最慢可信值 %.0fs，实得 %.0f", want, p2.Recommended)
	}
}
