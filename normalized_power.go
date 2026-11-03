package core

import "math"

// NPWindowSecs Coggan 定义里的滑动平均窗口长度（秒）。
const NPWindowSecs = 30

// NormalizedPower 计算 Coggan 标准化功率：先做 30 秒滑动平均，再取 4 次方均值。
//
//	ma(t) = mean(P[max(0, t−W+1) .. t])      W = 30s / 采样间隔
//	NP    = ( mean(ma⁴) )^(1/4)
//
// 与 NormalizedSpeedKph（裸 4 次方均值，IsoPower 同族）的差别就是那道滑动平均，
// 而它不是可选优化：4 次方会把单个尖峰放大上千倍。实测一条功率通道中位 308 W，
// 混进 1904 W 的单样本伪影，裸 4 次方均值算出 457 W（比 Coggan 口径高 49%）；
// PowerLoad 用 IF²，49% 会变成 123%。
//
// 非正值（停表段 / 无数据）先剔除再平均，否则等停时间会把 NP 拖向 0。
// 序列短于一个窗口时退化为裸 4 次方均值——没有足够样本做滑动平均。
//
// 出处：Coggan 的 NP 定义（30 秒滑动平均 + 4 次方均值）。
func NormalizedPower(samples []float64, intervalSecs int) float64 {
	if intervalSecs <= 0 {
		return 0
	}
	pos := make([]float64, 0, len(samples))
	for _, v := range samples {
		if v > 0 {
			pos = append(pos, v)
		}
	}
	if len(pos) == 0 {
		return 0
	}

	// 窗口按秒定义：30s ÷ 采样间隔，四舍五入到样本数。
	window := (NPWindowSecs + intervalSecs/2) / intervalSecs
	if window < 1 {
		window = 1
	}
	if len(pos) < window {
		return NormalizedSpeedKph(pos)
	}

	total := 0.0
	total4 := 0.0
	count := 0
	for i, v := range pos {
		total += v
		if i >= window {
			// 滑出窗口的那个样本（窗口起点前一个）
			total -= pos[i-window]
		}
		size := i + 1
		if size > window {
			size = window
		}
		// 开头不足一个窗口时用已有样本平均，否则前 30 秒会被整段丢掉
		ma := total / float64(size)
		total4 += ma * ma * ma * ma
		count++
	}
	if count == 0 {
		return 0
	}
	return math.Pow(total4/float64(count), 0.25)
}
