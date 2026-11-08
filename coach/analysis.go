package coach

// 单课分析：Pa:Hr 解耦、效率因子 EF、心率漂移（coach-engine 票 06）。
// 移植自 tomasstankovic/running-coach（MIT）scripts/analyze_session.py 的
// series_halves / EF 段；心率漂移窗口（稳态后前 15 分钟 vs 末 15 分钟，
// <5% 为佳）按票面 S50 判据。证据标签：S21 Pa:Hr、S22 耐久性、S50 漂移。

import "math"

const (
	minSteadySecs   = 3600 // 短于 60 分钟谈解耦意义不大（MIN_STEADY_MIN）
	warmupSkipSecs  = 600  // 前 10 分钟热身剔除（WARMUP_SKIP_S）
	decouplingGood  = 5.0  // % （Friel / TrainingPeaks 口径）
	decouplingHigh  = 10.0
	driftWindowSecs = 900 // 漂移窗口 ±15 分钟
	driftGoodPct    = 5.0 // % （S50）
	minHalfPoints   = 20  // series_halves 的样本下限
	minDriftPoints  = 10
)

// SessionAnalysis 单课耐久性分析结果。Applicable=false 时 Reason 给明确原因。
type SessionAnalysis struct {
	Applicable bool    `json:"applicable"`
	Reason     string  `json:"reason,omitempty"`
	DurationS  float64 `json:"durationSecs"`

	DecouplingPct     float64 `json:"decouplingPct,omitempty"`
	DecouplingVerdict string  `json:"decouplingVerdict,omitempty"` // ok | moderate | large
	FirstHalfHR       float64 `json:"firstHalfHr,omitempty"`
	SecondHalfHR      float64 `json:"secondHalfHr,omitempty"`

	EFPerBpm float64 `json:"efPerBpm,omitempty"` // 平均速度 m/min 每 bpm

	HrDriftPct     float64 `json:"hrDriftPct,omitempty"`
	HrDriftVerdict string  `json:"hrDriftVerdict,omitempty"` // ok | high
	EarlyHR        float64 `json:"earlyHr,omitempty"`
	LateHR         float64 `json:"lateHr,omitempty"`
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// AnalyzeSession 由双流（bpm、km/h，等间隔 intervalSecs）计算三指标。
// durationSecs 用活动计时时长（≥60 分钟才适用）。
//
// 口径说明（与金标准对照时记录）：analyze_session.py 用 NGP（坡度修正配速），
// openrunner streams 无梯度通道，这里用原始速度——平路一致，坡路解耦会偏大。
func AnalyzeSession(durationSecs float64, intervalSecs int, hr, speedKph []float64) SessionAnalysis {
	a := SessionAnalysis{DurationS: durationSecs}
	switch {
	case durationSecs < minSteadySecs:
		a.Reason = "时长不足 60 分钟，解耦/漂移读数无意义"
		return a
	case intervalSecs <= 0:
		a.Reason = "流采样间隔非法"
		return a
	case len(hr) == 0 || len(speedKph) == 0:
		a.Reason = "缺少心率或速度流，无法分析"
		return a
	}
	a.Applicable = true

	n := len(hr)
	if len(speedKph) < n {
		n = len(speedKph)
	}
	// 有效点：双流都 >0，且跳过前 10 分钟热身（与 series_halves 的过滤一致）
	type pt struct{ t, spdMS, hr float64 }
	var pts []pt
	for i := 0; i < n; i++ {
		t := float64(i) * float64(intervalSecs)
		// 过滤口径与 series_halves 对齐：hr>0 且 0<速度（NGP≤15min/km ⇔ ≥4km/h）
		if hr[i] > 0 && speedKph[i] >= 4.0 && t >= warmupSkipSecs {
			pts = append(pts, pt{t, speedKph[i] / 3.6, hr[i]})
		}
	}
	if len(pts) < minHalfPoints {
		a.Reason = "热身后有效双流样本不足 20 点"
		a.Applicable = false
		return a
	}

	// —— Pa:Hr 解耦（series_halves 同式）：后半 vs 前半的 (速度/心率) 比下降 %
	mid := (pts[0].t + pts[len(pts)-1].t) / 2
	var aSpd, aHr, bSpd, bHr []float64
	for _, p := range pts {
		if p.t < mid {
			aSpd = append(aSpd, p.spdMS)
			aHr = append(aHr, p.hr)
		} else {
			bSpd = append(bSpd, p.spdMS)
			bHr = append(bHr, p.hr)
		}
	}
	if len(aHr) > 0 && len(bHr) > 0 {
		ra := mean(aSpd) / mean(aHr)
		rb := mean(bSpd) / mean(bHr)
		a.DecouplingPct = (ra - rb) / ra * 100
		a.FirstHalfHR = mean(aHr)
		a.SecondHalfHR = mean(bHr)
		switch {
		case a.DecouplingPct < decouplingGood:
			a.DecouplingVerdict = "ok"
		case a.DecouplingPct < decouplingHigh:
			a.DecouplingVerdict = "moderate"
		default:
			a.DecouplingVerdict = "large"
		}
	}

	// —— EF：全程平均速度（m/min）/ 平均心率（含热身，与脚本 totals 口径一致）
	var allSpd, allHr []float64
	for i := 0; i < n; i++ {
		if hr[i] > 0 && speedKph[i] > 0 {
			allSpd = append(allSpd, speedKph[i]/3.6*60) // m/min
			allHr = append(allHr, hr[i])
		}
	}
	if len(allHr) > 0 {
		a.EFPerBpm = mean(allSpd) / mean(allHr)
	}

	// —— 心率漂移：热身后前 15 分钟 vs 末 15 分钟（S50 <5% 为佳）
	end := pts[len(pts)-1].t
	var early, late []float64
	for _, p := range pts {
		if p.t < warmupSkipSecs+driftWindowSecs {
			early = append(early, p.hr)
		}
		if p.t > end-driftWindowSecs {
			late = append(late, p.hr)
		}
	}
	if len(early) >= minDriftPoints && len(late) >= minDriftPoints && mean(early) > 0 {
		a.EarlyHR = mean(early)
		a.LateHR = mean(late)
		a.HrDriftPct = (a.LateHR - a.EarlyHR) / a.EarlyHR * 100
		if math.Abs(a.HrDriftPct) < driftGoodPct {
			a.HrDriftVerdict = "ok"
		} else {
			a.HrDriftVerdict = "high"
		}
	}
	return a
}
