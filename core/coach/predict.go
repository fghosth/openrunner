package coach

// 全马预测：Riegel（S27）、Vickers & Vertosick 模型 1/2（S25）、Tanda（S26）。
// 移植自 tomasstankovic/running-coach（MIT）scripts/paces.py 的 predict 段，
// 系数与论文 Additional file 一致，金标准对照见 coach-check.py。

import "math"

const milesPerKm = 1.0 / 1.609344

// VickersModel1 Vickers & Vertosick 2016 模型 1（S25）：
// 一个非全马比赛成绩 + 典型周跑量（英里，论文单位）。返回全马秒数。
func VickersModel1(dMeters, tSecs, weeklyMiles float64) float64 {
	vRiegel := 42195 / (tSecs * math.Pow(42195/dMeters, 1.07))
	v := 0.16018617 + 0.83076202*vRiegel + 0.06423826*(weeklyMiles/10)
	return 42195 / v
}

// VickersModel2 模型 2：两个比赛（d1 < d2）+ 周跑量。返回全马秒数。
// 论文里最准的形态；调用方需保证 d1 < d2 且都是非全马距离。
func VickersModel2(d1, t1, d2, t2, weeklyMiles float64) float64 {
	k21 := math.Log(t2/t1) / math.Log(d2/d1)
	kMar := 1.4510756 - 0.23797948*k21 - 0.01410023*(weeklyMiles/10)
	return t2 * math.Pow(42195/d2, kMar)
}

// Tanda Tanda 2011（S26）：周均跑量（km）+ 训练平均配速（秒/公里，第 -9~-1 周）
// → 全马配速，返回全马秒数。样本验证范围约 2:47–3:36。
func Tanda(weeklyKm, trainingPaceSecsPerKm float64) float64 {
	pm := 17.1 + 140.0*math.Exp(-0.0053*weeklyKm) + 0.55*trainingPaceSecsPerKm
	return 42.195 * pm
}

// PredictInput 全马预测输入。零值字段表示不可用，对应模型自动跳过。
type PredictInput struct {
	BaseKm   float64 // 基准比赛距离（km）
	BaseSecs float64 // 基准成绩（秒）
	// 第二场比赛（Vickers 模型 2 用；距离必须与基准不同）
	Race2Km   float64
	Race2Secs float64
	WeeklyKm  float64 // 典型周跑量（km）
	// TrainingPaceSecs 第 -9~-1 周所有跑步的平均配速（秒/公里，Tanda 用）
	TrainingPaceSecs float64
	// WeeksObserved 周跑量的样本周数；<4 时 Vickers/Tanda 标低可信
	WeeksObserved int
}

// MarathonPrediction 各模型的全马预测（秒）。0 表示该模型输入不足、未计算。
type MarathonPrediction struct {
	Riegel      float64 `json:"riegel"`
	Vickers1    float64 `json:"vickers1,omitempty"`
	Vickers2    float64 `json:"vickers2,omitempty"`
	Tanda       float64 `json:"tanda,omitempty"`
	Recommended float64 `json:"recommended"`
	// LowConfidence 周跑量样本 <4 周时 true（Vickers/Tanda 的跑量代表性不足）
	LowConfidence bool `json:"lowConfidence"`
	// Notes 各模型适用性说明（缺输入的原因等）
	Notes []string `json:"notes,omitempty"`
}

// PredictMarathon 汇总四个模型并给出 recommended。
//
// 纪律（paces.py 原文）：「Plan from the slowest credible estimate」——
// recommended 取所有可用预测里最慢（最大秒数）的那个。Riegel 对全马
// 系统性乐观（Vickers 2016：常快 10 分钟以上），有训练量输入时几乎
// 不会成为 recommended。
func PredictMarathon(in PredictInput) MarathonPrediction {
	var p MarathonPrediction
	if in.BaseKm <= 0 || in.BaseSecs <= 0 {
		return p
	}
	// Riegel 固定 1.06（与 paces.py predict 一致；个人 α 投影在 core.ProjectRaceTimes，
	// 是另一条链路，两者在 UI 分开呈现）
	p.Riegel = in.BaseSecs * math.Pow(42.195/in.BaseKm, 1.06)
	available := []float64{p.Riegel}

	if in.WeeklyKm > 0 {
		if in.WeeksObserved > 0 && in.WeeksObserved < 4 {
			p.LowConfidence = true
			p.Notes = append(p.Notes, "周跑量样本不足 4 周，Vickers/Tanda 代表性有限")
		}
		miles := in.WeeklyKm * milesPerKm
		// 模型定义要求非全马基准成绩
		if in.BaseKm < 42 {
			p.Vickers1 = VickersModel1(in.BaseKm*1000, in.BaseSecs, miles)
			available = append(available, p.Vickers1)
		} else {
			p.Notes = append(p.Notes, "基准成绩已是全马距离，Vickers 模型 1 不适用")
		}
		if in.Race2Km > 0 && in.Race2Secs > 0 && in.Race2Km != in.BaseKm {
			d1, t1 := in.BaseKm*1000, in.BaseSecs
			d2, t2 := in.Race2Km*1000, in.Race2Secs
			if d1 > d2 {
				d1, t1, d2, t2 = d2, t2, d1, t1
			}
			p.Vickers2 = VickersModel2(d1, t1, d2, t2, miles)
			available = append(available, p.Vickers2)
		}
		if in.TrainingPaceSecs > 0 {
			p.Tanda = Tanda(in.WeeklyKm, in.TrainingPaceSecs)
			available = append(available, p.Tanda)
		}
	} else {
		p.Notes = append(p.Notes, "无周跑量：仅 Riegel（对全马偏乐观）")
	}

	worst := available[0]
	for _, v := range available[1:] {
		if v > worst {
			worst = v
		}
	}
	p.Recommended = worst
	return p
}
