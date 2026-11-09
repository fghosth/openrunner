package core

import "math"

// NPWindowSecs Coggan 定义里的滑动平均窗口长度（秒）。
const NPWindowSecs = 30

// NormalizedPower 计算 Coggan 标准化功率：先做 30 秒滑动平均，再取 4 次方均值。
//
//	ma(t) = mean(P[t−W+1 .. t])   W = 30s ÷ 采样间隔，只统计满窗口
//	NP    = ( mean(ma⁴) )^(1/4)
//
// 与 NormalizedSpeedKph（裸 4 次方均值，IsoPower 同族）的差别就是那道滑动平均，
// 而它不是可选优化：4 次方会把单个尖峰放大上千倍。实测活动 281 的功率通道
// 中位 308 W，混进 1904 W 与 1305 W 两个单样本伪影（占样本数 0.4%），裸 4 次方
// 均值算出 457 W，比正确口径的 306 W 高 49%；PowerLoad 用 IF²，49% 会变成 123%。
//
// 未满一个窗口的样本不参与平均。开头必须稀释：活动 281 的通道开头恰好就是
// 1904、1305 两个垃圾样本，若按「已有样本数」平均，第一个样本等于零稀释，
// 会算出 487 W——比裸均值还高。GC（src/Metrics/aCoggan.cpp）用零补齐环形缓冲
// 同样能稀释，但恒定功率的 NP 会偏低约 0.2%；跳过残缺窗口两头都避开。
//
// 非正值（停表段 / 无数据）先剔除再平均，否则等停时间会把 NP 拖向 0。
// 序列短于一个窗口时退化为裸 4 次方均值——没有足够样本做滑动平均。
//
// 出处：Coggan 的 NP 定义（30 秒滑动平均 + 4 次方均值）；开头窗口的处理
// 参照 GC aCoggan.cpp 并做了上述偏离。
func NormalizedPower(samples []float64, intervalSecs int) float64 {
	return smoothedFourthPower(samples, intervalSecs)
}

// NormalizedSpeed 速度流的同口径标准化值（30 秒滑动平均 + 4 次方均值）。
//
// 为什么不用裸 4 次方均值（NormalizedSpeedKph）：GPS 速度尖峰和功率伪影是同一类
// 垃圾，4 次方会把单样本放大上千倍。实测用户库 217 条跑步活动：活动 281 的原始
// 4 次方均值 14.82 km/h vs 平滑后 10.32（+43.6%），活动 151 +18.8%，rTSS 直接
// 被抬成 17.5/79.9（正确 8.5/56.6）；p90 抬升 2.2%。rTSS 里同样是 IF²，误差会被平方。
func NormalizedSpeed(speedsKph []float64, intervalSecs int) float64 {
	return smoothedFourthPower(speedsKph, intervalSecs)
}

// smoothedFourthPower 通用「30 秒滑动平均 → 4 次方均值 → 开 4 次方」。
// 功率（NP）与速度（rTSS 的 NS）必须同一口径，否则两条链的伪影敏感度不一致。
func smoothedFourthPower(samples []float64, intervalSecs int) float64 {
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

	// 未满一个窗口的样本不参与：此时窗口里缺历史，若按「已有样本数」平均，
	// 开头的尖峰等于零稀释（实测活动 281 通道开头就是 1904,1305 两个垃圾样本，
	// 那样算出 NP=487 W，比裸 4 次方均值还高）。若按 GC 那样用零补齐，
	// 恒定功率的 NP 又会偏低约 0.2%。直接跳过残缺窗口两头都避开。
	total := 0.0
	total4 := 0.0
	count := 0
	for i, v := range pos {
		total += v
		if i >= window {
			// 滑出窗口的那个样本（窗口起点前一个）
			total -= pos[i-window]
		}
		if i < window-1 {
			continue
		}
		ma := total / float64(window)
		total4 += ma * ma * ma * ma
		count++
	}
	if count == 0 {
		return 0
	}
	return math.Pow(total4/float64(count), 0.25)
}
