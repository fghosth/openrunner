package core

import (
	"math"
	"testing"
)

// Coggan 标准化功率：先 30 秒滑动平均，再取 4 次方均值。
// 滑动平均不是可选优化——4 次方会把单个尖峰放大成灾难。实测数据里有一条
// 功率通道中位 308 W，混进 1904 W 的单样本伪影，裸 4 次方均值算出 457 W，
// 比 Coggan 口径高 49%；而 PowerLoad 用 IF²，49% 会变成 123%。

func almost(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

// 恒定功率：滑动平均不改变结果，NP 必须等于该功率。
func TestNormalizedPowerConstant(t *testing.T) {
	const p = 250.0
	s := make([]float64, 600) // 1s × 600 = 10 分钟
	for i := range s {
		s[i] = p
	}
	if got := NormalizedPower(s, 1); !almost(got, p, 1e-9) {
		t.Errorf("恒定 %.0f W 的 NP = %v，期望 %.0f", p, got, p)
	}
}

// 单个尖峰：这就是本函数存在的理由。NP 必须贴近基线，而不是被一个样本拖走。
func TestNormalizedPowerSuppressesSingleSpike(t *testing.T) {
	spike := make([]float64, 600)
	for i := range spike {
		spike[i] = 300
	}
	spike[320] = 1904 // 实测活动 281 里的伪影量级

	np := NormalizedPower(spike, 1)
	// 30 秒窗口会把这一个样本稀释到 1/30，NP 应当仍接近 300
	if np > 330 {
		t.Errorf("单个尖峰把 NP 抬到 %.1f W（基线 300 W）——30 秒滑动平均没起作用", np)
	}
	// 裸 4 次方均值（无滑动平均）会被抬得很高，用来确认测试真的在区分两者
	bare := NormalizedSpeedKph(spike)
	if !(bare > np*1.1) {
		t.Errorf("裸 4 次方均值 %.1f 未明显高于 Coggan NP %.1f，测试失去区分度", bare, np)
	}
}

// 真实数据的回归：实测活动 281 的功率通道开头就是 1904、1305 两个垃圾样本，
// 之后才是 ~90 W 起步的慢启动。上一版把开头按「已有样本数」平均，第一个样本
// 等于零稀释，算出 NP=487 W——比裸 4 次方均值（457 W）还高。
// 上面那条把尖峰放在 index 320，所以完全没覆盖到这个位置。
func TestNormalizedPowerSuppressesLeadingSpike(t *testing.T) {
	s := []float64{1904, 1305}
	for i := 0; i < 60; i++ {
		s = append(s, 90+float64(i)) // 慢启动
	}
	for i := 0; i < 600; i++ {
		s = append(s, 300) // 稳态
	}
	np := NormalizedPower(s, 1)
	if np > 330 {
		t.Errorf("开头的垃圾尖峰没被稀释：NP=%.1f W（稳态只有 300 W）", np)
	}
	if bare := NormalizedSpeedKph(s); !(bare > np*1.2) {
		t.Errorf("裸 4 次方均值 %.1f 未明显高于 NP %.1f，测试失去区分度", bare, np)
	}
}

// 4 次方均值 ≥ 算术均值（幂平均不等式），加了滑动平均也应成立。
func TestNormalizedPowerAtLeastMean(t *testing.T) {
	s := []float64{}
	for i := 0; i < 300; i++ {
		s = append(s, 150+100*math.Sin(float64(i)/37))
	}
	var sum float64
	for _, v := range s {
		sum += v
	}
	mean := sum / float64(len(s))
	if np := NormalizedPower(s, 1); np < mean {
		t.Errorf("NP %v < 均值 %v，违反幂平均不等式", np, mean)
	}
}

// 窗口是按秒定义的，不是按样本数：同一物理序列换采样间隔，NP 应当接近。
func TestNormalizedPowerWindowIsTimeBased(t *testing.T) {
	one := make([]float64, 600) // 1s × 600
	for i := range one {
		one[i] = 200 + 80*math.Sin(float64(i)/50)
	}
	// 同样的曲线按 2s 采样（隔一个取一个）
	var two []float64
	for i := 0; i < len(one); i += 2 {
		two = append(two, one[i])
	}
	a := NormalizedPower(one, 1)
	b := NormalizedPower(two, 2)
	if math.Abs(a-b)/a > 0.02 {
		t.Errorf("1s 采样 NP=%.2f 与 2s 采样 NP=%.2f 差 %.1f%%，窗口没有按秒算", a, b, math.Abs(a-b)/a*100)
	}
}

// 短于一个 30 秒窗口时退化为裸 4 次方均值（没有足够样本做滑动平均）。
func TestNormalizedPowerShortSeries(t *testing.T) {
	s := []float64{100, 200, 300, 400}
	if got, want := NormalizedPower(s, 1), NormalizedSpeedKph(s); !almost(got, want, 1e-9) {
		t.Errorf("短序列 NP=%v 应退化为裸 4 次方均值 %v", got, want)
	}
}

// 非正值（停表 / 无数据）不参与，否则会把 NP 拖向 0。
func TestNormalizedPowerIgnoresNonPositive(t *testing.T) {
	with := []float64{}
	for i := 0; i < 300; i++ {
		with = append(with, 250)
	}
	clean := append([]float64{}, with...)
	dirty := append([]float64{}, with...)
	for i := 10; i < 40; i++ {
		dirty[i] = 0 // 30 秒停表
	}
	a, b := NormalizedPower(clean, 1), NormalizedPower(dirty, 1)
	if !almost(a, b, 1e-9) {
		t.Errorf("掺入 0 之后 NP 从 %v 变成 %v，非正值应当被剔除", a, b)
	}
}

func TestNormalizedPowerDegenerate(t *testing.T) {
	if got := NormalizedPower(nil, 1); got != 0 {
		t.Errorf("空序列 NP = %v，期望 0", got)
	}
	s := []float64{200, 200}
	if got := NormalizedPower(s, 0); got != 0 {
		t.Errorf("intervalSecs=0 时 NP = %v，期望 0（无法定义时间窗口）", got)
	}
	if got := NormalizedPower([]float64{0, 0, 0}, 1); got != 0 {
		t.Errorf("全 0 序列 NP = %v，期望 0", got)
	}
}
