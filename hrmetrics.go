package core

import "math"

// TRIMP 家族（规格：Morton/Banister 训练冲量 + Green 系数，见 docs/goldencheetah-integration/02 §4）。
//
//	TRIMP   = 分钟 × %HRR × 0.64 × exp(Ksex × %HRR)      Ksex: 男 1.92 / 女 1.67
//	%HRR    = (AvgHR − RestHR) / (MaxHR − RestHR)
// 常数出处（ADR-0001）：0.64/1.92/1.67 为 Green et al 系数（转引自
// GoldenCheetah TRIMPPoints.cpp 注释 "Morton/Banister with Green et al"）。
// 时长口径：调用方传 TimerSecs（不含停表段），不传 elapsed（DEVLOG 票 03 评审决议）。

// TrimpPoints 计算 TRIMP；avgHr ≤ restHr 时为 0。
func TrimpPoints(durationSecs, avgHr, maxHr, restHr, ksex float64) float64 {
	if durationSecs <= 0 || avgHr <= restHr || maxHr <= restHr {
		return 0
	}
	hrr := (avgHr - restHr) / (maxHr - restHr)
	return (durationSecs / 60) * hrr * 0.64 * math.Exp(ksex*hrr)
}

// Trimp100 把 TRIMP 归一化为"阈值心率 1 小时 = 100 分"。
func Trimp100(trimp, maxHr, restHr, ltHr, ksex float64) float64 {
	if trimp == 0 || maxHr <= restHr || ltHr <= restHr {
		return 0
	}
	hrrLT := (ltHr - restHr) / (maxHr - restHr)
	denominator := 60 * hrrLT * 0.64 * math.Exp(ksex*hrrLT)
	if denominator == 0 {
		return 0
	}
	return 100 * trimp / denominator
}

// 心率区间系数（Edwards 系训练冲量分区法，按 %MaxHR 分界）。
// 出处：转引自 GoldenCheetah TRIMPZonalPoints.cpp 注释；原始论文溯源挂账 DEVLOG（ADR-0001）。
var hrZoneBounds = [...]float64{0.64, 0.77, 0.84, 0.90, 0.95}
var hrZoneCoefficients = [...]float64{0.84, 1.65, 2.57, 4.01, 5.91}

// hrZoneIndex 返回 0-4 区索引；无效心率（≤0、>MaxHR）或低于 zone1 下界（<64% MaxHR）
// 返回 -1。
//
// 「低于 zone1 不计」是 GC 口径（TRIMPZonalPoints 走 time_in_zone_H*，不落在任何
// 区间的心率样本不进任何桶）。早期版本把 <64% 记进 zone1（系数 0.84），会让低强度
// 课的分区 TRIMP 偏高——1 小时 <64% maxHR 在本实现是 50 分、GC 是 0 分。
func hrZoneIndex(hr, maxHr float64) int {
	if hr <= 0 || maxHr <= 0 || hr > maxHr {
		return -1
	}
	ratio := hr / maxHr
	for i := len(hrZoneBounds) - 1; i >= 0; i-- {
		if ratio >= hrZoneBounds[i] {
			return i
		}
	}
	return -1 // <64% MaxHR：不进任何区间（GC 口径）
}

// TrimpZonal 逐样本心率区间系数累加（Edwards 式）；缺测样本（0）跳过。
func TrimpZonal(hrs []float64, maxHr float64, intervalSecs int) float64 {
	if intervalSecs <= 0 || maxHr <= 0 {
		return 0
	}
	sum := 0.0
	for _, hr := range hrs {
		if z := hrZoneIndex(hr, maxHr); z >= 0 {
			sum += hrZoneCoefficients[z] * float64(intervalSecs) / 60
		}
	}
	return sum
}

// HrTimeInZone 返回 5 个心率区间的时长（秒）；缺测样本不计。
func HrTimeInZone(hrs []float64, maxHr float64, intervalSecs int) [5]float64 {
	var out [5]float64
	if intervalSecs <= 0 {
		return out
	}
	for _, hr := range hrs {
		if z := hrZoneIndex(hr, maxHr); z >= 0 {
			out[z] += float64(intervalSecs)
		}
	}
	return out
}

// maxPlausibleHR 是心率传感器的合理上界；超过此值的读数视为噪声
// （光学腕表在干燥/抖动时偶发 200+ 的孤立尖峰），不参与观测 MaxHR。
const maxPlausibleHR = 230.0

// HrPeak 是一段心率数据的峰值统计。
type HrPeak struct {
	// MaxHr 是观察到的最小心率（bpm）；无有效样本时为 0。
	MaxHr float64
	// Samples 是参与统计的有效样本数（已剔除缺测与越界噪声）。
	Samples int
}

// PeakHeartRate 求心率通道的峰值。剔除 ≤0 的缺测点与 >230 的传感器噪声。
// 用途：用历史活动数据反推 Athlete 的观测最大心率，作为 MaxHR 的默认建议值。
func PeakHeartRate(hrs []float64) HrPeak {
	var out HrPeak
	for _, hr := range hrs {
		if hr <= 0 || hr > maxPlausibleHR {
			continue
		}
		out.Samples++
		if hr > out.MaxHr {
			out.MaxHr = hr
		}
	}
	return out
}

// HrObservation 是对一段心率通道的观测结果。
type HrObservation struct {
	// Peak 是单点峰值（已剔除缺测与噪声）。
	Peak float64
	// Sustained 是持续 WindowSecs 秒的最高均值 —— 抗单点伪影，更能代表真实最大心率。
	Sustained float64
	// Quality 是有效样本占比（0~1）。光电心率在力量训练/球类时腕部受压，
	// 常出现大段缺测与孤立尖峰，Quality 低的活动不应采信其峰值。
	Quality float64
	// Samples 是有效样本数。
	Samples int
}

// minQualityForPeak 是采信心率峰值的有效样本占比门槛。
const minQualityForPeak = 0.8

// MinQualityForPeak 暴露采信门槛，供上层统计时过滤低质量活动。
func MinQualityForPeak() float64 { return minQualityForPeak }

// ObserveHeartRate 观测一段心率通道：给出单点峰值、持续峰值与数据质量。
//
// 为什么不能只用单点峰值：光电腕表在力量训练、球类等手腕发力的运动中会偶发
// 远高于真实心率的孤立尖峰（实测某次 0.2km 的「other」活动读数 207，而同期
// 11km 跑步的峰值只有 186）。用「持续 windowSecs 秒的最高均值」可消除这类伪影。
func ObserveHeartRate(hrs []float64, intervalSecs, windowSecs int) HrObservation {
	peak := PeakHeartRate(hrs)
	out := HrObservation{Peak: peak.MaxHr, Samples: peak.Samples}
	if len(hrs) == 0 {
		return out
	}
	out.Quality = float64(peak.Samples) / float64(len(hrs))
	if intervalSecs <= 0 || windowSecs <= 0 {
		return out
	}

	win := windowSecs / intervalSecs
	if win < 1 {
		win = 1
	}
	if win > len(hrs) {
		return out
	}
	// 窗口内有效样本也需达到同样比例，否则跨过整段缺测的窗口会给出虚高的均值。
	needValid := int(float64(win)*minQualityForPeak + 0.5)
	for i := 0; i+win <= len(hrs); i++ {
		sum, valid := 0.0, 0
		for _, hr := range hrs[i : i+win] {
			if hr > 0 && hr <= maxPlausibleHR {
				sum += hr
				valid++
			}
		}
		if valid < needValid {
			continue
		}
		if m := sum / float64(valid); m > out.Sustained {
			out.Sustained = m
		}
	}
	return out
}
