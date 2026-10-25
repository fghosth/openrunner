package core

import (
	"math"
	"time"
)

// PMC（性能管理图）计算（票 10）。
// 常数出处：LTS 42 天 / STS 7 天与 e^(−1/days) 递推——GC PMCData.cpp 默认（GC_LTS_DAYS/GC_STS_DAYS），
// 规格文档 docs/goldencheetah-integration/03 §A2（金标准：GC PMCData.calculateMetrics）。
//
//	LTS[d] = stress[d]·(1−e^(−1/LTS_DAYS)) + LTS[d−1]·e^(−1/LTS_DAYS)   默认 42 天
//	STS[d] = 同式，默认 7 天
//	SB[d]  = LTS[d] − STS[d]（默认画在次日，SBToday 可改当天）
//	RR[d]  = STS 天数窗口内 LTS 日增量滚动和
//	播种：赛季起点 seed 直接作为该日 LTS/STS 初值（GC 负值标记法的等价实现）
//	三序列：actual（实际）/ planned（计划）/ expected（预期：过去=实际、今日特判、未来=计划）
//
// 与 GC 的两处有意 divergence（Round 6 评审决议）：
//  1. expected 序列同样接受播种（GC 只播 actual/planned）——播种日前 expected 会等于播种值，
//     使"过去 expected==actual"不变量在播种场景仍成立；
//  2. SB 数组输出 [:days] 截断（GC resize(days+1) 保留末日明日值）——末日 SB≈0 无数值影响。

type SeasonSeed struct {
	Date  string  `json:"date"`  // YYYY-MM-DD
	Value float64 `json:"value"` // 播种值（该日 LTS/STS 初值）
}

type PMCInput struct {
	From    string             `json:"from"` // YYYY-MM-DD
	To      string             `json:"to"`
	Today   string             `json:"today"` // 今日特判基准日；空 = 无特判（expected 按日期就近取）
	LtsDays int                `json:"ltsDays"`
	StsDays int                `json:"stsDays"`
	SBToday bool               `json:"sbToday"`
	Actual  map[string]float64 `json:"actual"`  // 日负荷（实测活动聚合）
	Planned map[string]float64 `json:"planned"` // 日负荷（计划活动聚合）
	Seeds   []SeasonSeed       `json:"seeds"`
}

type PMCSeries struct {
	Dates       []string  `json:"dates"`
	Stress      []float64 `json:"stress"` // 实际日负荷（tooltip 用）
	Lts         []float64 `json:"lts"`
	Sts         []float64 `json:"sts"`
	Sb          []float64 `json:"sb"`
	Rr          []float64 `json:"rr"`
	PlannedLts  []float64 `json:"plannedLts"`
	PlannedSts  []float64 `json:"plannedSts"`
	PlannedSb   []float64 `json:"plannedSb"`
	ExpectedLts []float64 `json:"expectedLts"`
	ExpectedSts []float64 `json:"expectedSts"`
	ExpectedSb  []float64 `json:"expectedSb"`
}

const (
	defaultLtsDays = 42
	defaultStsDays = 7
)

// ComputePMC 从 From 到 To 逐日递推三序列。输入日期与输出日期均为 YYYY-MM-DD。
func ComputePMC(in PMCInput) PMCSeries {
	if in.LtsDays <= 0 {
		in.LtsDays = defaultLtsDays
	}
	if in.StsDays <= 0 {
		in.StsDays = defaultStsDays
	}

	from, err1 := time.ParseInLocation("2006-01-02", in.From, time.UTC)
	to, err2 := time.ParseInLocation("2006-01-02", in.To, time.UTC)
	if err1 != nil || err2 != nil || from.After(to) {
		return PMCSeries{}
	}
	days := int(to.Sub(from).Hours()/24) + 1

	dates := make([]string, days)
	for i := 0; i < days; i++ {
		dates[i] = from.AddDate(0, 0, i).Format("2006-01-02")
	}

	stress := make([]float64, days)
	plannedStress := make([]float64, days)
	expectedStress := make([]float64, days)

	todayIdx := -1
	if in.Today != "" {
		todayIdx = indexOf(dates, in.Today)
	}

	for i, d := range dates {
		stress[i] = in.Actual[d]
		plannedStress[i] = in.Planned[d]
		// 预期序列：过去=实际；今日=有实际用实际、无实际用计划；未来=计划；无 Today 时=就近取用
		switch {
		case todayIdx >= 0:
			switch {
			case i < todayIdx:
				expectedStress[i] = in.Actual[d]
			case i == todayIdx:
				if in.Actual[d] > 0 {
					expectedStress[i] = in.Actual[d]
				} else {
					expectedStress[i] = in.Planned[d]
				}
			default:
				expectedStress[i] = in.Planned[d]
			}
		default:
			if in.Actual[d] > 0 {
				expectedStress[i] = in.Actual[d]
			} else {
				expectedStress[i] = in.Planned[d]
			}
		}
	}

	// 播种索引（区间内）
	seedByIdx := func() map[int]float64 {
		m := map[int]float64{}
		for _, s := range in.Seeds {
			if idx := indexOf(dates, s.Date); idx >= 0 {
				m[idx] = s.Value
			}
		}
		return m
	}

	lts := recurse(stress, seedByIdx(), float64(in.LtsDays))
	sts := recurse(stress, seedByIdx(), float64(in.StsDays))
	plts := recurse(plannedStress, seedByIdx(), float64(in.LtsDays))
	psts := recurse(plannedStress, seedByIdx(), float64(in.StsDays))
	elts := recurse(expectedStress, seedByIdx(), float64(in.LtsDays))
	ests := recurse(expectedStress, seedByIdx(), float64(in.StsDays))

	offset := 0
	if !in.SBToday {
		offset = 1 // 默认画在次日
	}
	sb := make([]float64, days+1)
	for i := 0; i < days; i++ {
		sb[i+offset] = lts[i] - sts[i]
	}
	psb := make([]float64, days+1)
	for i := 0; i < days; i++ {
		psb[i+offset] = plts[i] - psts[i]
	}
	esb := make([]float64, days+1)
	for i := 0; i < days; i++ {
		esb[i+offset] = elts[i] - ests[i]
	}

	rr := rampRate(lts, in.StsDays)

	return PMCSeries{
		Dates: dates, Stress: stress,
		Lts: lts, Sts: sts, Sb: sb[:days], Rr: rr,
		PlannedLts: plts, PlannedSts: psts, PlannedSb: psb[:days],
		ExpectedLts: elts, ExpectedSts: ests, ExpectedSb: esb[:days],
	}
}

// recurse 一阶 IIR 递推；seed 索引处的值直接作为初值。
func recurse(stress []float64, seeds map[int]float64, daysConst float64) []float64 {
	out := make([]float64, len(stress))
	a := math.Exp(-1.0 / daysConst)
	for d := range stress {
		if seed, ok := seeds[d]; ok {
			out[d] = seed
			continue
		}
		prev := 0.0
		if d > 0 {
			prev = out[d-1]
		}
		out[d] = stress[d]*(1-a) + prev*a
	}
	return out
}

// rampRate STS 窗口内 LTS 日增量滚动和（GC 口径）。
func rampRate(lts []float64, stsDays int) []float64 {
	out := make([]float64, len(lts))
	rolling := 0.0
	for d := range lts {
		if d == 0 {
			continue
		}
		rolling += lts[d] - lts[d-1]
		if d-stsDays-1 >= 0 { // 与 GC 一致：窗口滑出后才做减法（d > stsDays）
			rolling -= lts[d-stsDays] - lts[d-stsDays-1]
		}
		out[d] = rolling
	}
	return out
}

func indexOf(ss []string, v string) int {
	for i, s := range ss {
		if s == v {
			return i
		}
	}
	return -1
}
