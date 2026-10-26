package core

import (
	"math"
	"testing"
)

func TestFitRiegelIndex(t *testing.T) {
	t.Run("已知指数的合成数据能回收", func(t *testing.T) {
		// t = 0.06 · d^1.10（α=1.10）
		const wantAlpha = 1.10
		pts := []EffortPoint{
			{Seconds: 0.06 * math.Pow(5000, wantAlpha), DistanceM: 5000},
			{Seconds: 0.06 * math.Pow(10000, wantAlpha), DistanceM: 10000},
			{Seconds: 0.06 * math.Pow(21097.5, wantAlpha), DistanceM: 21097.5},
		}
		got := FitRiegelIndex(pts)
		if !got.OK {
			t.Fatal("应拟合成功")
		}
		if math.Abs(got.Alpha-wantAlpha) > 1e-6 {
			t.Fatalf("α 期望 %v, 得到 %v", wantAlpha, got.Alpha)
		}
		if got.R2 < 0.9999 {
			t.Fatalf("合成数据应完美拟合, R²=%v", got.R2)
		}
	})

	t.Run("同距离取最快且不计入点数", func(t *testing.T) {
		pts := []EffortPoint{
			{Seconds: 1500, DistanceM: 5000}, // 24:00
			{Seconds: 1400, DistanceM: 5000}, // 23:20 ← 更快
			{Seconds: 3000, DistanceM: 10000},
		}
		got := FitRiegelIndex(pts)
		if got.N != 2 {
			t.Fatalf("同距离应合并为 1 点 → 共 2 点, 得到 %d", got.N)
		}
		// α = log(3000/1400)/log(2) ≈ 1.0995
		want := math.Log(3000.0/1400.0) / math.Log(2)
		if math.Abs(got.Alpha-want) > 1e-6 {
			t.Fatalf("α 期望 %v, 得到 %v", want, got.Alpha)
		}
	})

	t.Run("成绩点不足", func(t *testing.T) {
		if got := FitRiegelIndex([]EffortPoint{{Seconds: 1200, DistanceM: 5000}}); got.OK {
			t.Fatal("只有 1 个距离时不应 OK")
		}
	})

	t.Run("异常数据被钳到合理区间", func(t *testing.T) {
		// 距离翻倍但时间只增加 10% → α≈0.14，物理上不可能
		got := FitRiegelIndex([]EffortPoint{
			{Seconds: 1000, DistanceM: 5000},
			{Seconds: 1100, DistanceM: 10000},
		})
		if !got.OK || got.Alpha < minRiegelAlpha {
			t.Fatalf("α 应被钳到 ≥%v, 得到 %v", minRiegelAlpha, got.Alpha)
		}
	})
}

func TestProjectRaceTimes(t *testing.T) {
	targets := []struct {
		Label     string
		DistanceM float64
	}{
		{"5K", 5000}, {"10K", 10000}, {"半马", 21097.5}, {"全马", 42195},
	}

	t.Run("从 10K 外推，Riegel 指数", func(t *testing.T) {
		// 10K 用 50:00 = 3000s，α=1.06
		got := ProjectRaceTimes(10000, 3000, targets, DefaultRiegelAlpha, 21097.5)
		if len(got) != 4 {
			t.Fatalf("期望 4 项, 得到 %d", len(got))
		}
		// 5K 应约 3000×(0.5)^1.06 ≈ 1439s（23:59）
		if math.Abs(got[0].TimeSecs-3000*math.Pow(0.5, 1.06)) > 1 {
			t.Fatalf("5K 预测不符: %v", got[0].TimeSecs)
		}
		// 半马 ≈ 3000×(2.1097)^1.06 ≈ 6595s
		if math.Abs(got[2].TimeSecs-3000*math.Pow(2.1097, 1.06)) > 2 {
			t.Fatalf("半马预测不符: %v", got[2].TimeSecs)
		}
		// 全马外推 4.2 倍 → 不可信（>2 倍基准距离）
		if got[3].Reliable {
			t.Fatal("全马从 10K 外推 4.2 倍，应标记不可信")
		}
		if got[3].Note == "" {
			t.Fatal("不可信时应给出说明")
		}
	})

	t.Run("已知最长距离限制可信度", func(t *testing.T) {
		// 用户最长只跑过 10K，则半马（2.1 倍）也不可信
		got := ProjectRaceTimes(10000, 3000, targets, 1.06, 10000)
		if got[2].Reliable {
			t.Fatal("最长只到 10K 时，半马应标记不可信")
		}
	})

	t.Run("跑过的距离不算外推", func(t *testing.T) {
		// 用户有半马成绩：半马预测应可信，全马仍不可信
		got := ProjectRaceTimes(10000, 3000, targets, 1.10, 21097.5)
		if !got[2].Reliable {
			t.Fatal("用户跑过半马，半马预测应判可信（不是外推）")
		}
		if got[3].Reliable {
			t.Fatal("全马（2× 已知最长）仍应判不可信")
		}
	})

	t.Run("基准非法返回空", func(t *testing.T) {
		if got := ProjectRaceTimes(0, 3000, targets, 1.06, 0); len(got) != 0 {
			t.Fatal("基准距离为 0 应返回空")
		}
	})
}
