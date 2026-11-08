// Package coach 提供训练计划校验与配速/预测计算。
//
// week.go 的周负荷规则移植自 tomasstankovic/running-coach（MIT）的
// scripts/check_week.py，判定语义与其逐条对齐（金标准对照见
// openrunner/scripts/coach-check.sh）。各规则的文献出处标注在 Evidence 字段，
// 详见该 skill 的 references/sources.md（S 编号同源）。
package coach

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// Session 一节课。History 里的条目视为已完成跑步（运动类型过滤在调用方）；
// Plan 里的条目用 Type 区分课型。
type Session struct {
	Date           string   `json:"date"` // YYYY-MM-DD
	Type           string   `json:"type,omitempty"`
	Minutes        float64  `json:"minutes"`
	KM             float64  `json:"km"`
	QualityMinutes *float64 `json:"quality_minutes,omitempty"` // X/Z3+ 时间，缺省按课型估算
}

// WeekTotal 周总量（无逐课数据时的历史输入，对齐 check_week.py 的 history_weeks）。
type WeekTotal struct {
	WeekStart string
	Minutes   float64
	KM        float64
	LongMin   float64
}

// WeekCheckInput 周校验输入。
type WeekCheckInput struct {
	WeekRole     string // base|build|peak|deload|taper|race|return，空按 build
	Cautious     bool   // 新手或近 12 个月有伤：周增上限收紧
	History      []Session
	HistoryWeeks []WeekTotal
	Plan         []Session // 待校验周（周一~周日）
}

// Verdict 单条判定。Rule 是稳定 ID（供程序分支），Level ∈ OK|WARN|FAIL。
type Verdict struct {
	Level    string `json:"level"`
	Rule     string `json:"rule"`
	Detail   string `json:"detail"`
	Evidence string `json:"evidence,omitempty"`
}

const (
	spikeTolKM  = 0.3 // +10% 规则的公里舍入余量
	spikeTolMin = 3.0 // 分钟口径的舍入余量
)

var (
	hardTypes = map[string]bool{"tempo": true, "threshold": true, "intervals": true, "hills": true, "race": true}
	// 不算跑量的课型（对齐 check_week.py NON_RUN_TYPES）
	nonRunTypes = map[string]bool{"rest": true, "cross": true, "strength": true}
	// 缺 quality_minutes 时，各课型处于 X/Z3+ 的粗略占比（同源 DEFAULT_QUALITY_SHARE）
	defaultQualityShare = map[string]float64{"tempo": 0.4, "threshold": 0.4, "intervals": 0.3, "hills": 0.2, "race": 0.8, "strides": 0.03}
	evidence            = map[string]string{
		"volume":        "S3 Nielsen 2014 · S4 Damsted 2019",
		"deload-rhythm": "S6 practice",
		"spike":         "S1 Frandsen 2025 (Br J Sports Med 5200 人队列)",
		"long-share":    "S6 practice",
		"hard-days":     "practice",
		"strength":      "S35 Doma 2017",
		"intensity":     "S10–S13",
		"rest":          "practice",
	}
)

func isRunSession(s Session) bool { return !nonRunTypes[s.Type] }

func timeParse(s string) (time.Time, error) { return time.Parse("2006-01-02", s) }

func addDays(s string, n int) string {
	d, err := timeParse(s)
	if err != nil {
		return s
	}
	return d.AddDate(0, 0, n).Format("2006-01-02")
}

func monday(s string) string {
	d, err := timeParse(s)
	if err != nil {
		return s
	}
	return d.AddDate(0, 0, -int((d.Weekday()+6)%7)).Format("2006-01-02")
}

// dayDiff = a - b 的天数（ISO 日期字符串）。
func dayDiff(a, b string) int {
	da, ea := timeParse(a)
	db, eb := timeParse(b)
	if ea != nil || eb != nil {
		return 0
	}
	return int(da.Sub(db).Hours() / 24)
}

type weekBucket struct {
	start         string
	min, km, long float64
}

// weeklyHistory 把 history_weeks 与逐课 history 合并成按周分桶，
// 空周（生病/休息）保留为 0 —— 它们是真实数据点。
func weeklyHistory(in WeekCheckInput, planStart string) []weekBucket {
	weeks := map[string]*weekBucket{}
	for _, wk := range in.HistoryWeeks {
		start := monday(wk.WeekStart)
		weeks[start] = &weekBucket{start: start, min: wk.Minutes, km: wk.KM, long: wk.LongMin}
	}
	for _, w := range in.History {
		if !isRunSession(w) {
			continue
		}
		start := monday(w.Date)
		if start >= planStart {
			continue
		}
		b := weeks[start]
		if b == nil {
			b = &weekBucket{start: start}
			weeks[start] = b
		}
		b.min += w.Minutes
		b.km += w.KM
		if w.Minutes > b.long {
			b.long = w.Minutes
		}
	}
	if len(weeks) == 0 {
		return nil
	}
	keys := make([]string, 0, len(weeks))
	for k := range weeks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for d := keys[0]; d < planStart; d = addDays(d, 7) {
		if _, ok := weeks[d]; !ok {
			weeks[d] = &weekBucket{start: d}
		}
	}
	keys = keys[:0]
	for k := range weeks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]weekBucket, 0, len(keys))
	for _, k := range keys {
		if k < planStart {
			b := weeks[k]
			out = append(out, *b)
		}
	}
	return out
}

// CheckWeek 校验计划周。判定顺序与 check_week.py 输出顺序一致，便于对照。
func CheckWeek(in WeekCheckInput) ([]Verdict, error) {
	if len(in.Plan) == 0 {
		return nil, errors.New("coach: plan 为空，无法校验")
	}
	role := in.WeekRole
	if role == "" {
		role = "build"
	}
	for _, s := range append(append([]Session{}, in.Plan...), in.History...) {
		if _, err := timeParse(s.Date); err != nil {
			return nil, fmt.Errorf("coach: 日期非法 %q: %w", s.Date, err)
		}
	}

	minDate := ""
	for _, w := range in.Plan {
		if minDate == "" || w.Date < minDate {
			minDate = w.Date
		}
	}
	planStart := monday(minDate)
	prior := weeklyHistory(in, planStart)

	var vs []Verdict
	report := func(level, rule, detail string) {
		vs = append(vs, Verdict{Level: level, Rule: rule, Detail: detail, Evidence: evidence[rule]})
	}

	// ---- 计划周总量 ----
	var runs []Session
	for _, w := range in.Plan {
		if isRunSession(w) {
			runs = append(runs, w)
		}
	}
	var totalMin, longMin float64
	for _, w := range runs {
		totalMin += w.Minutes
		if w.Minutes > longMin {
			longMin = w.Minutes
		}
	}

	// ---- 周增量进展 ----
	if len(prior) == 0 {
		report("WARN", "volume", "no history given; cannot check progression (pass history or history_weeks)")
	} else {
		last := prior[len(prior)-1]
		recent := prior[max(0, len(prior)-3):]
		var sumRecent, reference float64
		for _, p := range recent {
			sumRecent += p.min
			if p.min > reference {
				reference = p.min
			}
		}
		avg3 := sumRecent / float64(len(recent))
		base := max(last.min, 1)
		change := (totalMin - base) / base * 100
		changeAvg := (totalMin - avg3) / max(avg3, 1) * 100

		switch role {
		case "deload", "taper", "race", "return":
			if (role == "deload" || role == "taper") && totalMin > base*0.9 {
				report("WARN", "volume", fmt.Sprintf("%s week but volume is %+.0f%% vs last week; aim for −20 to −40%%", role, change))
			} else {
				report("OK", "volume", fmt.Sprintf("%s week volume %+.0f%% vs last week", role, change))
			}
		default:
			small := totalMin-base <= 30 && base < 240
			note := ""
			if last.min < 0.85*reference {
				baseRef := reference
				changeRef := (totalMin - baseRef) / max(baseRef, 1) * 100
				note = fmt.Sprintf(" (last week was lighter; %+.0f%% vs the %.0f-min week before)", changeRef, baseRef)
				change = changeRef
				small = totalMin-baseRef <= 30 && baseRef < 240
			}
			warnAt, failAt := 15.0, 20.0
			if in.Cautious {
				warnAt, failAt = 10, 15
			}
			twoWeeks := 0.0
			if len(prior) >= 2 {
				prev2 := prior[len(prior)-2]
				twoWeeks = (totalMin - prev2.min) / max(prev2.min, 1) * 100
			}
			switch {
			case change > failAt && !small:
				report("FAIL", "volume", fmt.Sprintf("volume %+.0f%% (%.0f → %.0f min)%s; keep ≤ +%.0f%% (hard limit +%.0f%%)",
					change, base, totalMin, note, warnAt, failAt))
			case twoWeeks > 30 && !small && note == "":
				report("FAIL", "volume", fmt.Sprintf("volume %+.0f%% over two weeks; keep two-week growth ≤ +30%%", twoWeeks))
			case change > warnAt && !small:
				report("WARN", "volume", fmt.Sprintf("volume %+.0f%%%s; +%.0f%% is the usual ceiling", change, note, warnAt))
			default:
				report("OK", "volume", fmt.Sprintf("volume %+.0f%%%s, %+.0f%% vs 3-week average", change, note, changeAvg))
			}
		}

		// ---- 减量节奏：连续 3 周爬升且无轻周 ----
		if (role == "build" || role == "peak" || role == "base") && len(prior) >= 3 {
			last3 := prior[len(prior)-3:]
			var mx, mn, peakBefore float64
			mn = last3[0].min
			for _, p := range last3 {
				if p.min > mx {
					mx = p.min
				}
				if p.min < mn {
					mn = p.min
				}
			}
			for _, p := range prior[:len(prior)-3] {
				if p.min > peakBefore {
					peakBefore = p.min
				}
			}
			noLighter := mn >= 0.85*max(mx, peakBefore)
			rising := last3[2].min >= 1.05*last3[0].min && totalMin >= last3[2].min
			if noLighter && rising {
				report("WARN", "deload-rhythm", "3 weeks of rising load without a lighter week; consider making this a deload")
			}
		}
	}

	// ---- 单次 +10%（S1）----
	spikeChecks(in, runs, planStart, prior, report)

	// ---- 长课占比 ----
	if totalMin > 0 && role != "race" && role != "taper" {
		share := longMin / totalMin * 100
		limit := 45.0
		if len(runs) > 4 {
			limit = 40
			if totalMin >= 300 {
				limit = 35
			}
		}
		level := "OK"
		if share > limit {
			level = "WARN"
		}
		report(level, "long-share", fmt.Sprintf("long run is %.0f%% of weekly time (limit ~%.0f%% for %d runs)", share, limit, len(runs)))
	}

	// ---- 硬课数量与间隔 ----
	hardSet := map[string]bool{}
	var longDates []string
	for _, w := range in.Plan {
		if hardTypes[w.Type] {
			hardSet[w.Date] = true
		}
		if w.Type == "long" {
			longDates = append(longDates, w.Date)
		}
	}
	var hardDates []string
	for d := range hardSet {
		hardDates = append(hardDates, d)
	}
	sort.Strings(hardDates)
	maxHard := 2
	if role == "peak" {
		maxHard = 3
	}
	if role == "deload" || role == "return" {
		maxHard = 1
	}
	level := "OK"
	if len(hardDates) > maxHard {
		level = "WARN"
	}
	report(level, "hard-days", fmt.Sprintf("%d hard days (max %d for a %s week)", len(hardDates), maxHard, role))
	for i := 0; i+1 < len(hardDates); i++ {
		if dayDiff(hardDates[i+1], hardDates[i]) == 1 {
			report("FAIL", "hard-days", fmt.Sprintf("hard days back to back: %s and %s", hardDates[i], hardDates[i+1]))
		}
	}
	for _, ld := range longDates {
		for _, hd := range hardDates {
			if abs(dayDiff(ld, hd)) == 1 {
				report("WARN", "hard-days", fmt.Sprintf("long run %s is next to a hard day %s", ld, hd))
			}
		}
	}

	// ---- 力量课位置 ----
	keyDays := map[string]bool{}
	for _, d := range hardDates {
		keyDays[d] = true
	}
	for _, d := range longDates {
		keyDays[d] = true
	}
	for _, w := range in.Plan {
		if w.Type == "strength" && keyDays[addDays(w.Date, 1)] {
			report("WARN", "strength", fmt.Sprintf("strength on %s is the day before a hard session or long run", w.Date))
		}
	}

	// ---- 强度分布 ----
	var quality float64
	for _, w := range runs {
		if w.QualityMinutes != nil {
			quality += *w.QualityMinutes
		} else {
			quality += w.Minutes * defaultQualityShare[w.Type]
		}
	}
	if totalMin > 0 {
		q := quality / totalMin * 100
		level := "OK"
		if q > 25 {
			level = "WARN"
		}
		report(level, "intensity", fmt.Sprintf("~%.0f%% of time at X/Z3 or harder (target ≤ 20%%, warn > 25%%)", q))
	}

	// ---- 休息日 ----
	runDateSet := map[string]bool{}
	for _, w := range runs {
		runDateSet[w.Date] = true
	}
	restDays := 7 - len(runDateSet)
	level = "OK"
	if restDays < 1 {
		level = "WARN"
	}
	report(level, "rest", fmt.Sprintf("%d day(s) without running", restDays))

	return vs, nil
}

type runRec struct {
	date    string
	km, min float64
}

// spikeChecks 移植 check_week.py spike_checks：计划课逐条与「它之前 30 天内最长」比，
// 参考值随旧课滑出窗口而移动；已完成的近两周超标课在回顾模式下报 WARN。
func spikeChecks(in WeekCheckInput, planRuns []Session, planStart string, prior []weekBucket, report func(string, string, string)) {
	var runs []runRec
	for _, w := range in.History {
		if isRunSession(w) && w.Date < planStart {
			runs = append(runs, runRec{w.Date, w.KM, w.Minutes})
		}
	}
	byKm := len(runs) > 0
	for _, r := range runs {
		if r.km <= 0 {
			byKm = false
			break
		}
	}
	unit := "min"
	val := func(r runRec) float64 { return r.min }
	pval := func(s Session) float64 { return s.Minutes }
	tol := spikeTolMin
	if byKm {
		unit = "km"
		val = func(r runRec) float64 { return r.km }
		pval = func(s Session) float64 { return s.KM }
		tol = spikeTolKM
	}

	if len(runs) == 0 {
		if len(prior) > 0 { // 只有周总量：与近 4 周最长比
			var ref, longest float64
			for _, p := range prior[max(0, len(prior)-4):] {
				if p.long > ref {
					ref = p.long
				}
			}
			for _, w := range planRuns {
				if w.Minutes > longest {
					longest = w.Minutes
				}
			}
			if ref > 0 && longest > ref*1.10+spikeTolMin {
				report("FAIL", "spike", fmt.Sprintf("longest run %.0f min is %.0f%% above the recent longest (%.0f min); keep each run within +10%%",
					longest, longest/ref*100-100, ref))
			}
		}
		return
	}

	// 回顾模式：近两周已完成但破了 +10% 的课
	for _, r := range runs {
		if dayDiff(planStart, r.date) > 14 {
			continue
		}
		var ref float64
		for _, x := range runs {
			d := dayDiff(r.date, x.date)
			if d > 0 && d <= 30 {
				if v := val(x); v > ref {
					ref = v
				}
			}
		}
		if ref > 0 {
			v := val(r)
			if v > ref*1.10+tol {
				report("WARN", "spike", fmt.Sprintf("done %s: %.0f %s was %.0f%% longer than the 30-day longest (%.0f %s); a race or an unplanned spike?",
					r.date, v, unit, v/ref*100-100, ref, unit))
			}
		}
	}

	seen := append([]runRec{}, runs...)
	flagged := false
	planned := append([]Session{}, planRuns...)
	sort.SliceStable(planned, func(i, j int) bool { return planned[i].Date < planned[j].Date })
	for _, w := range planned {
		var ref float64
		for _, x := range seen {
			d := dayDiff(w.Date, x.date)
			if d > 0 && d <= 30 {
				if v := val(x); v > ref {
					ref = v
				}
			}
		}
		v := pval(w)
		if ref > 0 {
			over := v/ref*100 - 100
			switch {
			case w.Type == "race":
				report("OK", "spike", fmt.Sprintf("%s: race is %+.0f%% vs the 30-day longest run (%.0f %s); races are exempt, but aim for a half marathon <= ~+20%%",
					w.Date, over, ref, unit))
			case v > ref*1.10+tol:
				report("FAIL", "spike", fmt.Sprintf("%s: %.0f %s is %.0f%% longer than the longest run of the last 30 days (%.0f %s); keep every run within +10%%",
					w.Date, v, unit, over, ref, unit))
				flagged = true
			}
		}
		seen = append(seen, runRec{w.Date, w.KM, w.Minutes})
	}
	if !flagged {
		report("OK", "spike", "no run is more than 10 % longer than the longest of the previous 30 days")
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
