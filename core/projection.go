package core

import "math"

// 成绩距离外推（race time projection）。
//
// 依据（见 DEVLOG 的调研记录）：
//   - Riegel 1977：t₂ = t₁ · (d₂/d₁)^1.06，固定指数，80% 左右准确，
//     但**对长距离偏乐观**（对业余跑者尤其明显）。
//   - Cameron 1998：对 Riegel 做距离相关修正，长距离略准，但增益仅 1–3%，
//     且实现依赖经验系数 —— 收益不抵复杂度，本项目不采用。
//   - **个人化幂律**（本项目采用）：log t = α·log d + log c。
//     用跑者自己的多距离成绩回归出个人 α，比固定 1.06 靠谱得多
//     （业余跑者 α 常在 1.08–1.15 之间，即长距离掉速比 Riegel 假设的更快）。
//
// 因此本文件提供两件事：
//   1. FitRiegelIndex —— 从用户成绩里拟合个人 α（log-log 最小二乘）；
//   2. ProjectRaceTimes —— 用某个基准成绩 + α 外推到任意距离。

// 默认指数（Riegel 1977）与合理区间。低于 1.02 意味着「越跑越省力」（不可能），
// 高于 1.20 意味着极度掉速（通常是数据错误或极端超马）。
const (
	DefaultRiegelAlpha = 1.06
	minRiegelAlpha     = 1.02
	maxRiegelAlpha     = 1.20
)

// RiegelFit 是个人幂律指数的拟合结果。
type RiegelFit struct {
	// Alpha 距离指数（1.06 = Riegel 教科书值）。
	Alpha float64 `json:"alpha"`
	// N 参与拟合的成绩数（≥2 才有意义）。
	N int `json:"n"`
	// OK 为 false 时调用方应退回 DefaultRiegelAlpha。
	OK bool `json:"ok"`
	// R2 拟合优度（log 空间）；N=2 时恒为 1（两点必然共线）。
	R2 float64 `json:"r2"`
}

// FitRiegelIndex 用多距离成绩拟合个人幂律指数 α。
//
// 模型：log(t) = α·log(d) + log(c)。只要求「距离互不相同」的成绩点
// （同距离的多条成绩对 α 没有信息量，只取最快那条）。
func FitRiegelIndex(pts []EffortPoint) RiegelFit {
	// 同一距离只保留最快
	best := map[float64]float64{}
	for _, p := range pts {
		if p.Seconds <= 0 || p.DistanceM <= 0 {
			continue
		}
		if cur, ok := best[p.DistanceM]; !ok || p.Seconds < cur {
			best[p.DistanceM] = p.Seconds
		}
	}
	out := RiegelFit{N: len(best)}
	if len(best) < 2 {
		return out
	}

	// log-log 最小二乘
	var n, sx, sy, sxx, sxy float64
	for d, t := range best {
		lx := math.Log(d)
		ly := math.Log(t)
		n++
		sx += lx
		sy += ly
		sxx += lx * lx
		sxy += lx * ly
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return out
	}
	alpha := (n*sxy - sx*sy) / den
	if math.IsNaN(alpha) || math.IsInf(alpha, 0) {
		return out
	}
	// 钳到物理合理区间：超出说明成绩数据异常（例如把骑行当跑步录进来）
	if alpha < minRiegelAlpha {
		alpha = minRiegelAlpha
	}
	if alpha > maxRiegelAlpha {
		alpha = maxRiegelAlpha
	}
	out.Alpha = alpha
	out.OK = true

	// R²（log 空间）
	intercept := (sy - alpha*sx) / n
	var ssRes, ssTot float64
	meanY := sy / n
	for d, t := range best {
		pred := alpha*math.Log(d) + intercept
		ly := math.Log(t)
		ssRes += (ly - pred) * (ly - pred)
		ssTot += (ly - meanY) * (ly - meanY)
	}
	if ssTot > 0 {
		out.R2 = 1 - ssRes/ssTot
	}
	return out
}

// RaceProjection 一个目标距离的预测。
type RaceProjection struct {
	Label string `json:"label"`
	// LabelKey 是 i18n key（如 race.half）；前端优先按它取本地文案，取不到才回落
	// Label。服务端不该把中文当数据发给英文界面。
	LabelKey  string  `json:"labelKey,omitempty"`
	DistanceM float64 `json:"distanceM"`
	TimeSecs  float64 `json:"timeSecs"`
	// PaceSecsPerKm 便于直接展示（不会被单位换算影响，前端再转）
	PaceSecsPerKm float64 `json:"paceSecsPerKm"`
	// ExtrapolationRatio = 目标距离 / 基准距离。越大越不可靠。
	ExtrapolationRatio float64 `json:"extrapolationRatio"`
	// Reliable 外推倍数 ≤2 且 ≤ 用户已知最长成绩的 2 倍时视为可信。
	Reliable bool `json:"reliable"`
	// Note 不可信时的中文说明（保底，旧客户端可用）。
	Note string `json:"note,omitempty"`
	// NoteCode 是稳定 code（note.tooFar / note.tooFarMarathon），前端优先按它取本地文案。
	NoteCode string `json:"noteCode,omitempty"`
}

// ProjectRaceTimes 从「基准距离 + 基准成绩」外推到各目标距离。
//
// maxKnownDistM 是用户已知的最长成绩距离（用于判断外推是否离数据太远）；
// 传 0 表示未知。
func ProjectRaceTimes(
	baseDistM, baseTimeSecs float64,
	targets []struct {
		Label     string
		LabelKey  string
		DistanceM float64
	},
	alpha float64,
	maxKnownDistM float64,
) []RaceProjection {
	out := make([]RaceProjection, 0, len(targets))
	if baseDistM <= 0 || baseTimeSecs <= 0 {
		return out
	}
	if alpha <= 0 {
		alpha = DefaultRiegelAlpha
	}
	for _, tg := range targets {
		if tg.DistanceM <= 0 {
			continue
		}
		ratio := tg.DistanceM / baseDistM
		t := baseTimeSecs * math.Pow(ratio, alpha)
		p := RaceProjection{
			Label:              tg.Label,
			LabelKey:           tg.LabelKey,
			DistanceM:          tg.DistanceM,
			TimeSecs:           t,
			PaceSecsPerKm:      t / (tg.DistanceM / 1000),
			ExtrapolationRatio: ratio,
		}
		// 可信度分三种情况：
		//   1. 用户**实际跑过**这个距离（≤ 最长已知成绩）→ 不是外推，可信；
		//   2. 略超已知最长距离（≤1.5 倍）→ 仍可参考；
		//   3. 更远 → 不可信（Cameron 1998 的提醒：全马预测必须假设有足够的
		//      专项长距离训练，否则会乐观 5–15%）。
		// 另加一条绝对上限：相对基准距离外推超过 3 倍时一律不可信
		// （Riegel 自己只保证 3.5–230 分钟的适用范围）。
		reliable := true
		if maxKnownDistM > 0 {
			switch {
			case tg.DistanceM <= maxKnownDistM:
				reliable = true // 跑过，不算外推
			case tg.DistanceM <= maxKnownDistM*1.5:
				reliable = true // 略超，可参考
			default:
				reliable = false
			}
		}
		if ratio > 3 {
			reliable = false
		}
		tooFar := !reliable
		if tooFar {
			p.Reliable = false
			if tg.DistanceM >= 42195 {
				p.Note = "外推距离远超你的已知成绩，全马预测需要足够的专项长距离训练作支撑，误差可能很大"
				p.NoteCode = "note.tooFarMarathon"
			} else {
				p.Note = "外推距离较远，误差会放大"
				p.NoteCode = "note.tooFar"
			}
		} else {
			p.Reliable = true
		}
		out = append(out, p)
	}
	return out
}
