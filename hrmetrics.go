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

// hrZoneIndex 返回 0-4 区索引；无效心率（≤0 或 >MaxHR）返回 -1。
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
	return 0 // <64% 记入 zone1（轻量活动仍计时间）
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
