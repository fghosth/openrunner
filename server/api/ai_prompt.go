package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/openrunner/core"
	"github.com/openrunner/core/coach"
	"github.com/openrunner/server/store"
	"gorm.io/gorm"
)

// aiPromptDTO 生成的提示词与元信息。
type aiPromptDTO struct {
	Range      string `json:"range"`
	Goal       string `json:"goal"`
	From       string `json:"from"`
	To         string `json:"to"`
	Activities int    `json:"activities"`
	Markdown   string `json:"markdown"`
}

// rangeDays 预设范围（最近 N 天）。
var rangeDays = map[string]int{
	"week":    7,
	"month":   30,
	"quarter": 90,
	"year":    365,
}

// goalSpec 是训练目标（决定提示词里的目标描述与建议侧重）。
type goalSpec struct {
	Label    string
	Distance float64
	// Focus 针对该目标的训练侧重（写进提示词，引导 AI 给出对题的建议）
	Focus string
}

var goalSpecs = map[string]goalSpec{
	"5k": {
		Label: "5 公里", Distance: 5000,
		Focus: "速度耐力与 VO2max 间歇；关注 3-5 分钟的 I 强度课占比、腿部速度储备、以及体重/跑步经济性",
	},
	"10k": {
		Label: "10 公里", Distance: 10000,
		Focus: "阈值能力（T 强度）与配速控制；关注乳酸阈提升、节奏跑的时长与频率、以及后程掉速",
	},
	"half": {
		Label: "半程马拉松", Distance: 21097.5,
		Focus: "阈值到马拉松配速的转换与长距离耐力；关注每周长距离课、补给策略、以及 15-21km 的配速稳定性",
	},
	"marathon": {
		Label: "全程马拉松", Distance: 42195,
		Focus: "有氧基础与脂肪氧化能力；关注周跑量的渐进、长距离课的时长（建议 2.5-3.5h）、恢复与伤病预防",
	},
}

// handleAIPrompt 生成一段结构化的训练分析提示词（Markdown），供用户粘贴给任意 AI。
//
// GET /api/athletes/{athleteId}/ai-prompt?range=month&goal=10k[&activityId=123]
//
// 设计说明：不调外部 AI，只负责把「运动员档案 + 训练负荷 + 区间分布 + 活动明细 +
// 个人最佳 + 当前表现预测 + 目标」整理成专业提示词。用户复制到任意对话式 AI 即可。
func handleAIPrompt(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID, err := pathValueID(r, "athleteId")
		if err != nil {
			writeError(w, http.StatusBadRequest, "非法档案 id")
			return
		}
		athlete, err := store.GetAthlete(db, uint(athleteID))
		if err != nil {
			writeError(w, http.StatusNotFound, "Athlete 不存在")
			return
		}

		rng := r.URL.Query().Get("range")
		goalKey := r.URL.Query().Get("goal")
		goal, ok := goalSpecs[goalKey]
		if !ok {
			goal = goalSpecs["10k"]
			goalKey = "10k"
		}

		now := time.Now().Local()
		var from time.Time
		var single *store.ActivityModel

		if rng == "activity" {
			idStr := r.URL.Query().Get("activityId")
			aid, aerr := strconv.Atoi(idStr)
			if aerr != nil || aid <= 0 {
				writeError(w, http.StatusBadRequest, "选择单次分析时需提供 activityId")
				return
			}
			m, aerr := store.GetActivity(db, uint(athleteID), uint(aid))
			if aerr != nil {
				writeError(w, http.StatusNotFound, "活动不存在")
				return
			}
			single = m
			from = time.Unix(m.StartTime, 0).Local()
		} else {
			days, exists := rangeDays[rng]
			if !exists {
				days = 30
				rng = "month"
			}
			from = now.AddDate(0, 0, -days)
		}

		// 活动集合
		q := db.Where("athlete_id = ? AND is_planned = ?", uint(athleteID), false)
		if single != nil {
			q = q.Where("id = ?", single.ID)
		} else {
			q = q.Where("start_time >= ?", from.Unix())
		}
		var acts []store.ActivityModel
		if err := q.Order("start_time ASC").Find(&acts).Error; err != nil {
			writeError(w, http.StatusInternalServerError, "查询活动失败")
			return
		}
		if len(acts) == 0 {
			writeError(w, http.StatusBadRequest, "该范围内没有活动数据")
			return
		}

		// 指标
		metricsByID := map[uint]*metricsDTO{}
		for i := range acts {
			if m, err := getMetricsDTO(db, acts[i].ID); err == nil && m != nil {
				metricsByID[acts[i].ID] = m
			}
		}

		md := buildAIPrompt(db, uint(athleteID), athlete, acts, metricsByID, rng, goalKey, goal, from, now, single)

		writeJSON(w, http.StatusOK, aiPromptDTO{
			Range: rng, Goal: goalKey,
			From: from.Format("2006-01-02"), To: now.Format("2006-01-02"),
			Activities: len(acts), Markdown: md,
		})
	}
}

// buildAIPrompt 组装 Markdown 提示词。纯函数式拼接，便于测试与调整。
func buildAIPrompt(
	db *gorm.DB,
	athleteID uint,
	athlete *store.AthleteModel,
	acts []store.ActivityModel,
	metricsByID map[uint]*metricsDTO,
	rng, goalKey string,
	goal goalSpec,
	from, now time.Time,
	single *store.ActivityModel,
) string {
	var b strings.Builder

	b.WriteString("# 跑步训练数据分析请求\n\n")
	b.WriteString("你是一位有 20 年经验的中长跑教练兼运动生理学专家。下面是运动员的真实训练数据，请做专业分析。\n\n")

	// ---- 运动员档案 ----
	b.WriteString("## 一、运动员档案\n\n")
	fmt.Fprintf(&b, "- 姓名：%s\n", athlete.Name)
	sex := "男"
	if athlete.Sex == "F" {
		sex = "女"
	}
	fmt.Fprintf(&b, "- 性别：%s\n", sex)
	// 心率区间
	if hz, err := store.HrZoneForDate(db, athleteID, now.Format("2006-01-02")); err == nil {
		fmt.Fprintf(&b, "- 心率参数：最大心率 %d bpm · 静息心率 %d bpm · 乳酸阈心率 %d bpm\n",
			hz.MaxHr, hz.RestHr, hz.LtHr)
	}
	// 阈值配速
	if pz, err := store.PaceZoneForDate(db, athleteID, now.Format("2006-01-02")); err == nil && pz.ThresholdPaceSecsPerKm > 0 {
		fmt.Fprintf(&b, "- 阈值配速：%s /km\n", secToPace(float64(pz.ThresholdPaceSecsPerKm)))
	}
	// 临界速度
	if cz, err := store.CsZoneForDate(db, athleteID, "speed", now.Format("2006-01-02")); err == nil && cz.Cs > 0 {
		fmt.Fprintf(&b, "- 临界速度 CS：%.2f m/s（%s /km）· D'：%.0f m\n", cz.Cs, secToPace(1000/cz.Cs), cz.Dprime)
	}
	b.WriteString("\n")

	// ---- 分析范围 ----
	b.WriteString("## 二、分析范围\n\n")
	rangeLabel := map[string]string{
		"activity": "单次训练", "week": "最近一周", "month": "最近一个月",
		"quarter": "最近一个季度", "year": "最近一年",
	}[rng]
	if single != nil {
		fmt.Fprintf(&b, "- 范围：%s（%s）\n", rangeLabel, from.Format("2006-01-02 15:04"))
	} else {
		fmt.Fprintf(&b, "- 范围：%s（%s ~ %s）\n", rangeLabel, from.Format("2006-01-02"), now.Format("2006-01-02"))
	}

	var totalDist, totalSecs, totalTrimp, totalRtss, totalElev float64
	var hrCount int
	var hrSum float64
	var hrTiZ [5]float64
	for i := range acts {
		a := &acts[i]
		totalDist += a.DistanceM
		secs := float64(a.DurationSecs)
		if a.TimerSecs > 0 {
			secs = float64(a.TimerSecs)
		}
		totalSecs += secs
		totalElev += a.ElevationGainM
		if a.AvgHr > 0 {
			hrSum += a.AvgHr
			hrCount++
		}
		if m := metricsByID[a.ID]; m != nil {
			totalTrimp += m.Trimp
			totalRtss += m.Rtss
			for z := 0; z < 5 && z < len(m.HrTiZ); z++ {
				hrTiZ[z] += m.HrTiZ[z]
			}
		}
	}

	fmt.Fprintf(&b, "- 训练次数：%d 次\n", len(acts))
	fmt.Fprintf(&b, "- 总距离：%.1f km · 总时长：%.1f 小时 · 累计爬升：%.0f m\n",
		totalDist/1000, totalSecs/3600, totalElev)
	if totalDist > 0 {
		fmt.Fprintf(&b, "- 平均配速：%s /km\n", secToPace(totalSecs/(totalDist/1000)))
	}
	if hrCount > 0 {
		fmt.Fprintf(&b, "- 平均心率：%.0f bpm\n", hrSum/float64(hrCount))
	}
	// 周均（帮助 AI 判断周跑量是否递进合理）
	weekSpan := now.Sub(from).Hours() / 24 / 7
	if weekSpan < 1 {
		weekSpan = 1
	}
	fmt.Fprintf(&b, "- 折算周跑量：%.1f km/周\n\n", totalDist/1000/weekSpan)

	// ---- 训练负荷 ----
	b.WriteString("## 三、训练负荷（TRIMP / rTSS 体系）\n\n")
	fmt.Fprintf(&b, "- 累计 TRIMP：%.0f（周均 %.0f）\n", totalTrimp, totalTrimp/weekSpan)
	if totalRtss > 0 {
		fmt.Fprintf(&b, "- 累计 rTSS：%.0f（周均 %.0f）\n", totalRtss, totalRtss/weekSpan)
	}
	// PMC 期末值（仅预设范围有意义）
	if single == nil {
		rows, err := loadDailyLoads(db, athleteID, "trimp")
		if err == nil {
			seasons, _ := store.ListSeasons(db, athleteID)
			in := buildPMCInput(now.Format("2006-01-02"), rows, seasons)
			series := core.ComputePMC(in)
			if n := len(series.Dates); n > 0 {
				// 找 <= today 的最后一个点
				idx := -1
				for i := 0; i < n; i++ {
					if series.Dates[i] <= now.Format("2006-01-02") {
						idx = i
					}
				}
				if idx >= 0 {
					fmt.Fprintf(&b, "- PMC 期末：体能 LTS %.1f · 疲劳 STS %.1f · 状态 SB %.1f\n",
						series.Lts[idx], series.Sts[idx], series.Sb[idx])
					if series.Sb[idx] < -30 {
						b.WriteString("  - ⚠️ SB 低于 -30，处于过度疲劳区间\n")
					} else if series.Sb[idx] > 10 {
						b.WriteString("  - SB 高于 +10，休息充分，适合比赛或测验\n")
					}
				}
			}
		}
	}
	b.WriteString("\n")

	// ---- 强度分布 ----
	var tizTotal float64
	for _, v := range hrTiZ {
		tizTotal += v
	}
	if tizTotal > 0 {
		b.WriteString("## 四、强度分布（心率区间时间占比）\n\n")
		names := [5]string{"Z1 恢复", "Z2 有氧", "Z3 节奏", "Z4 阈值", "Z5 无氧"}
		b.WriteString("| 区间 | 时长 | 占比 |\n|---|---|---|\n")
		for z := 0; z < 5; z++ {
			fmt.Fprintf(&b, "| %s | %.0f 分钟 | %.1f%% |\n", names[z], hrTiZ[z]/60, hrTiZ[z]/tizTotal*100)
		}
		easy := (hrTiZ[0] + hrTiZ[1]) / tizTotal * 100
		hard := (hrTiZ[3] + hrTiZ[4]) / tizTotal * 100
		fmt.Fprintf(&b, "\n- 低强度（Z1+Z2）占比 %.1f%%，高强度（Z4+Z5）占比 %.1f%%\n", easy, hard)
		if easy > 80 && hard > 15 {
			b.WriteString("- 结构接近**极化训练**（80/20），通常是耐力项目最理想的结构\n")
		} else if easy < 70 {
			b.WriteString("- 低强度占比偏低（<70%），可能存在「中等强度过多」的问题（灰区训练）\n")
		}
		b.WriteString("\n")
	}

	// ---- 活动明细 ----
	b.WriteString("## 五、活动明细\n\n")
	if single != nil {
		b.WriteString("| 项目 | 数值 |\n|---|---|\n")
		a := single
		b.WriteString("| 日期 | " + time.Unix(a.StartTime, 0).Local().Format("2006-01-02 15:04") + " |\n")
		fmt.Fprintf(&b, "| 距离 | %.2f km |\n", a.DistanceM/1000)
		fmt.Fprintf(&b, "| 时长 | %.0f 分钟 |\n", float64(a.DurationSecs)/60)
		if a.DistanceM > 0 && a.DurationSecs > 0 {
			fmt.Fprintf(&b, "| 平均配速 | %s /km |\n", secToPace(float64(a.DurationSecs)/(a.DistanceM/1000)))
		}
		if a.AvgHr > 0 {
			fmt.Fprintf(&b, "| 平均心率 | %.0f bpm |\n", a.AvgHr)
		}
		fmt.Fprintf(&b, "| 爬升 | %.0f m |\n", a.ElevationGainM)
		if m := metricsByID[a.ID]; m != nil {
			fmt.Fprintf(&b, "| TRIMP | %.0f |\n", m.Trimp)
			fmt.Fprintf(&b, "| TRIMP(100) | %.0f |\n", m.Trimp100)
			fmt.Fprintf(&b, "| rTSS | %.0f |\n", m.Rtss)
			if m.Np > 0 {
				fmt.Fprintf(&b, "| NP 功率 | %.0f W |\n", m.Np)
			}
		}
		// 步频步幅
		if sm, err := store.GetStream(db, a.ID); err == nil {
			if st, err := core.UnmarshalStream(sm.Data); err == nil {
				ss := core.SummarizeStream(st)
				if ss.AvgCadenceSpm > 0 {
					fmt.Fprintf(&b, "| 平均步频 | %.0f spm |\n", ss.AvgCadenceSpm)
					fmt.Fprintf(&b, "| 平均步幅 | %.2f m |\n", ss.AvgStrideM)
				}
			}
		}
		b.WriteString("\n")
	} else {
		b.WriteString("| 日期 | 距离 | 时长 | 配速 | 均心率 | 爬升 | TRIMP | rTSS |\n")
		b.WriteString("|---|---|---|---|---|---|---|---|\n")
		for i := range acts {
			a := &acts[i]
			pace := "—"
			if a.DistanceM > 100 && a.DurationSecs > 0 {
				pace = secToPace(float64(a.DurationSecs) / (a.DistanceM / 1000))
			}
			hr := "—"
			if a.AvgHr > 0 {
				hr = fmt.Sprintf("%.0f", a.AvgHr)
			}
			var trimp, rtss float64
			if m := metricsByID[a.ID]; m != nil {
				trimp, rtss = m.Trimp, m.Rtss
			}
			fmt.Fprintf(&b, "| %s | %.2f km | %.0f′ | %s | %s | %.0f | %.0f | %.0f |\n",
				time.Unix(a.StartTime, 0).Local().Format("01-02"), a.DistanceM/1000,
				float64(a.DurationSecs)/60, pace, hr, a.ElevationGainM, trimp, rtss)
		}
		b.WriteString("\n")
	}

	// ---- 个人最佳（扫速度流，可能稍慢）
	best, err := computePersonalBests(db, athleteID)
	if err == nil && len(best) > 0 {
		b.WriteString("## 六、个人最佳（活动内最快分段）\n\n")
		b.WriteString("| 距离 | 成绩 | 配速 | 来源 |\n|---|---|---|---|\n")
		for _, pb := range best {
			if pb.Seconds <= 0 {
				continue
			}
			fmt.Fprintf(&b, "| %s | %s | %s /km | %s |\n", pb.Label,
				secToHMS(pb.Seconds), secToPace(pb.PaceSecsPerKm),
				time.Unix(pb.ActivityStart, 0).Local().Format("2006-01-02"))
		}
		b.WriteString("\n")
	}

	// ---- 训练目标 ----
	b.WriteString("## 七、训练目标\n\n")
	fmt.Fprintf(&b, "- 目标距离：**%s**\n", goal.Label)
	fmt.Fprintf(&b, "- 该目标的训练重点：%s\n\n", goal.Focus)

	// ---- 系统确定性计算（coach-engine 票 07）----
	if sec := coachDeterministicSection(db, athleteID, now, single); sec != "" {
		b.WriteString(sec)
	}

	// ---- 提问 ----
	b.WriteString("## 九、请回答\n\n")
	b.WriteString("请基于以上数据，用中文给出：\n\n")
	b.WriteString("1. **训练结构评估**：当前强度分布是否合理？属于极化、金字塔还是灰区训练？有什么问题？\n")
	b.WriteString("2. **负荷递进分析**：周跑量和 TRIMP 的递进是否安全？有没有过量或不足的迹象？结合 SB/STS 判断恢复状态。\n")
	fmt.Fprintf(&b, "3. **针对 %s 目标的差距分析**：以当前能力（见个人最佳与负荷），距离目标还有多远？最短板是什么（有氧基础/阈值/速度/跑步经济性）？\n", goal.Label)
	b.WriteString("4. **未来 4-8 周的训练计划**：给出周计划框架（每周几次、各次的主项与强度区间、周跑量建议），并说明递进节奏。计划课强度请引用第八节的配速表。\n")
	b.WriteString("5. **风险提示**：数据里有哪些伤病或过度训练的预警信号？需要监控什么指标？\n\n")
	b.WriteString("要求：结论要有数据支撑（引用上面的具体数字），避免泛泛而谈；如果数据不足以支撑某个结论，请直接说明缺什么数据。\n")

	return b.String()
}

// secToPace 秒/公里 → "m:ss"。
func secToPace(secsPerKm float64) string {
	if secsPerKm <= 0 {
		return "—"
	}
	m := int(secsPerKm) / 60
	s := int(secsPerKm) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}

// secToHMS 秒 → "h:mm:ss" 或 "m:ss"。
func secToHMS(secs float64) string {
	t := int(secs + 0.5)
	h := t / 3600
	m := (t % 3600) / 60
	s := t % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// coachDeterministicSection 第八节：系统确定性计算（coach-engine 票 07）。
// 三个块（周校验 / 配速表 / 预测）任一缺数据即整块省略，全缺则整节省略；
// 每块行数有上限，超出截断并标注——提示词长度增长有界。
func coachDeterministicSection(db *gorm.DB, athleteID uint, now time.Time, single *store.ActivityModel) string {
	var blocks []string

	// 8.1 下周计划校验（规则引擎；单次活动范围不适用）
	if single == nil {
		mon := mondayOf(now).AddDate(0, 0, 7)
		if vs := weekCheckFromCalendar(db, athleteID, mon); len(vs) > 0 {
			var s strings.Builder
			s.WriteString("### 8.1 下周计划校验（规则引擎判定，S3/S4 等已发表规则）\n\n")
			const maxVerdicts = 12
			for i, vd := range vs {
				if i >= maxVerdicts {
					fmt.Fprintf(&s, "- （其余 %d 条判定省略）\n", len(vs)-i)
					break
				}
				ev := vd.Evidence
				if ev != "" {
					ev = " [" + ev + "]"
				}
				fmt.Fprintf(&s, "- **%s** %s：%s%s\n", vd.Level, vd.Rule, vd.Detail, ev)
			}
			blocks = append(blocks, s.String()+"\n")
		}
	}

	// 8.2 训练配速表
	today := now.Format("2006-01-02")
	if thr, src := thresholdForDate(db, athleteID, today); thr > 0 {
		var s strings.Builder
		fmt.Fprintf(&s, "### 8.2 训练配速表（阈值 %s /km，来源：%s）\n\n", secToPace(thr), src)
		if km, secs, ok := longestRace(db, athleteID); ok {
			vd, rows := coach.Daniels(km, secs)
			parts := make([]string, 0, len(rows))
			for _, r := range rows {
				if r.PaceFast == r.PaceSlow {
					parts = append(parts, fmt.Sprintf("%s %s", r.Name, secToPace(r.PaceSlow)))
				} else {
					parts = append(parts, fmt.Sprintf("%s %s–%s", r.Name, secToPace(r.PaceFast), secToPace(r.PaceSlow)))
				}
			}
			fmt.Fprintf(&s, "- Daniels VDOT %.1f（基准 %.1fkm）：%s\n", vd, km, strings.Join(parts, " · "))
		}
		var lthr float64
		if hz, err := store.HrZoneForDate(db, athleteID, today); err == nil && hz != nil {
			lthr = float64(hz.LtHr)
		}
		parts := make([]string, 0, 7)
		for _, r := range coach.SpeedZoneTable(thr, lthr) {
			parts = append(parts, fmt.Sprintf("%s ≤%s", r.Name, secToPace(r.PaceSlow)))
		}
		fmt.Fprintf(&s, "- 80/20 七区（配速不慢于）：%s\n", strings.Join(parts, " · "))
		blocks = append(blocks, s.String()+"\n")
	}

	// 8.3 全马预测
	if in, ok := predictInputsFromDB(db, athleteID); ok {
		p := coach.PredictMarathon(in)
		items := []string{"Riegel " + secToHMS(p.Riegel)}
		if p.Vickers1 > 0 {
			items = append(items, "Vickers1 "+secToHMS(p.Vickers1))
		}
		if p.Vickers2 > 0 {
			items = append(items, "Vickers2 "+secToHMS(p.Vickers2))
		}
		if p.Tanda > 0 {
			items = append(items, "Tanda "+secToHMS(p.Tanda))
		}
		s := fmt.Sprintf("### 8.3 全马预测（基准 %.1fkm %s，周跑量 %.1fkm/%d 周）\n\n- %s\n- **推荐（最慢可信值）：%s** —— Riegel 对全马系统性乐观，按推荐值配速\n\n",
			in.BaseKm, secToHMS(in.BaseSecs), in.WeeklyKm, in.WeeksObserved,
			strings.Join(items, " · "), secToHMS(p.Recommended))
		blocks = append(blocks, s)
	}

	if len(blocks) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## 八、系统确定性计算结果\n\n")
	b.WriteString("以下数值由系统按已发表公式确定性计算。你的职责是解读与建议，" +
		"**不得重新计算或修改这些数值**；引用时请原样引用。\n\n")
	for _, blk := range blocks {
		b.WriteString(blk)
	}
	return b.String()
}
