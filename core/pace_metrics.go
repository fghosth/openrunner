package core

import "math"

// 配速链指标（票 06）。
//
// rTSS（Skiba 跑步版 TSS）= 100 × secs × NP² / (CV² × 3600)，
// NP 为配速流的 4 次方均值（IsoPower 同族），CV 为阈值速度（km/h）。
// 配速区间：Skiba 跑步配速分区（%CV，6 区 80/87/95/105/119%），转引自
// GoldenCheetah PaceZones.cpp（其注明 "based upon the Skiba run pace zones"）；原文溯源挂账 DEVLOG。

var paceZoneUpperBounds = [...]float64{0.80, 0.87, 0.95, 1.05, 1.19} // %CV 上界（Z1..Z5），Z6 无上界

// PaceZoneIndex 返回 0-5 区索引（Z1..Z6）；无效速度（≤0 或无阈值）返回 -1。
func PaceZoneIndex(speedKph, thresholdSpeedKph float64) int {
	if speedKph <= 0 || thresholdSpeedKph <= 0 {
		return -1
	}
	ratio := speedKph / thresholdSpeedKph
	for i := len(paceZoneUpperBounds) - 1; i >= 0; i-- {
		if ratio >= paceZoneUpperBounds[i] {
			return i + 1
		}
	}
	return 0
}

// PaceTimeInZone 返回 6 个配速区间的时长（秒）；缺测（0）样本不计。
func PaceTimeInZone(speeds []float64, thresholdSpeedKph float64, intervalSecs int) [6]float64 {
	var out [6]float64
	if intervalSecs <= 0 || thresholdSpeedKph <= 0 {
		return out
	}
	for _, sp := range speeds {
		if z := PaceZoneIndex(sp, thresholdSpeedKph); z >= 0 {
			out[z] += float64(intervalSecs)
		}
	}
	return out
}

// NormalizedSpeedKph 4 次方均值速度（与 IsoPower 同族，压抑低强度、放大高强度）。
func NormalizedSpeedKph(speeds []float64) float64 {
	if len(speeds) == 0 {
		return 0
	}
	total, count := 0.0, 0
	for _, sp := range speeds {
		if sp > 0 {
			total += sp * sp * sp * sp
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return math.Pow(total/float64(count), 0.25)
}

// Rtss 计算 rTSS；thresholdSpeedKph ≤ 0 或 NP ≤ 0 时为 0。
func Rtss(durationSecs, normalizedSpeedKph, thresholdSpeedKph float64) float64 {
	if durationSecs <= 0 || normalizedSpeedKph <= 0 || thresholdSpeedKph <= 0 {
		return 0
	}
	return 100 * durationSecs * normalizedSpeedKph * normalizedSpeedKph /
		(thresholdSpeedKph * thresholdSpeedKph * 3600)
}

// 功率区间上界（%CP，Coggan 七区：55/75/90/105/120/150）。
//
// 为什么不复用跑步配速的 %CV 界（0.80/0.87/0.95/1.05/1.19）：那是 Skiba 的
// 跑步配速分区，宽度分布与功率语义不同——按它切功率，耐力骑（70% CP）会整段
// 落在 W1，真正的功率分区（有氧 56-75%、节奏 76-90%）被压成一根线。
var powerZoneUpperBounds = [...]float64{0.55, 0.75, 0.90, 1.05, 1.20, 1.50} // 上界（Z1..Z6），Z7 无上界

// PowerZoneIndex 返回 0-6 区索引（W1..W7）；无效功率（≤0 或无 CP）返回 -1。
func PowerZoneIndex(power, cp float64) int {
	if power <= 0 || cp <= 0 {
		return -1
	}
	ratio := power / cp
	for i := len(powerZoneUpperBounds) - 1; i >= 0; i-- {
		if ratio >= powerZoneUpperBounds[i] {
			return i + 1
		}
	}
	return 0
}

// PowerTimeInZone 返回 7 个功率区间的时长（秒）；缺测（0）样本不计。
func PowerTimeInZone(powers []float64, cp float64, intervalSecs int) [7]float64 {
	var out [7]float64
	if intervalSecs <= 0 || cp <= 0 {
		return out
	}
	for _, p := range powers {
		if z := PowerZoneIndex(p, cp); z >= 0 {
			out[z] += float64(intervalSecs)
		}
	}
	return out
}
