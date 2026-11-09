package coach

// 训练配速表：80/20 七区（S48）、Daniels-Gilbert VDOT（S46）、比赛→阈值估算。
// 移植自 tomasstankovic/running-coach（MIT）的 scripts/paces.py 与 scripts/zones.py，
// 数值金标准对照见 openrunner/scripts/coach-check.py。

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var paceRe = regexp.MustCompile(`^\s*(\d+):(\d{1,2})\s*$`)

// ParsePace 把 "4:10" 解析为秒/公里。
func ParsePace(text string) (float64, error) {
	m := paceRe.FindStringSubmatch(text)
	if m == nil {
		return 0, fmt.Errorf("coach: 非法配速 %q（应为 m:ss）", text)
	}
	mm, err1 := strconv.Atoi(m[1])
	ss, err2 := strconv.Atoi(m[2])
	if err1 != nil || err2 != nil {
		return 0, fmt.Errorf("coach: 非法配速 %q", text)
	}
	return float64(mm*60 + ss), nil
}

// ParseTime 把 "48:30" 或 "1:34:23" 解析为秒。
func ParseTime(text string) (float64, error) {
	parts := strings.Split(strings.TrimSpace(text), ":")
	if len(parts) == 0 || len(parts) > 3 {
		return 0, fmt.Errorf("coach: 非法用时 %q", text)
	}
	secs := 0
	for _, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return 0, fmt.Errorf("coach: 非法用时 %q", text)
		}
		secs = secs*60 + v
	}
	return float64(secs), nil
}

// FormatPace 秒/公里 → "4:10"；非法值返回 "--"（与 paces.py 同）。
func FormatPace(secPerKm float64) string {
	if secPerKm <= 0 || secPerKm > 3600 {
		return "--"
	}
	sec := int(math.Round(secPerKm))
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

// FormatTime 秒 → "h:mm:ss"（不足一小时 "m:ss"）。
func FormatTime(secs float64) string {
	s := int(math.Round(secs))
	h, rem := s/3600, s%3600
	m, sec := rem/60, rem%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}

// ZoneBound 区间界（阈值速度 / LTHR 的分数）。
type ZoneBound struct {
	Name   string
	Lo, Hi float64
}

// DefaultSpeedZones 80/20 Endurance 七区（Z1,Z2,X,Z3,Y,Z4,Z5；S48，
// TrainingPeaks 的 80/20 计算器同款比例）。
var DefaultSpeedZones = []ZoneBound{
	{"Z1", 0.5995, 0.7599},
	{"Z2", 0.7599, 0.8711},
	{"X", 0.8711, 0.9294},
	{"Z3", 0.9294, 1.0000},
	{"Y", 1.0000, 1.0204},
	{"Z4", 1.0204, 1.1521},
	{"Z5", 1.1521, 1.40},
}

// DefaultHRZones 与速度区对齐的心率比例（LTHR 基准，S49 同族）。
var DefaultHRZones = []ZoneBound{
	{"Z1", 0.718, 0.810},
	{"Z2", 0.810, 0.902},
	{"X", 0.902, 0.948},
	{"Z3", 0.948, 1.000},
	{"Y", 1.000, 1.017},
	{"Z4", 1.017, 1.052},
	{"Z5", 1.052, 1.10},
}

// PaceZoneRow 配速区间表一行。PaceFast/PctHi/HRHi 为 0 表示无上界（Z5）。
type PaceZoneRow struct {
	Name     string  `json:"name"`
	PaceSlow float64 `json:"paceSlow"` // 秒/公里
	PaceFast float64 `json:"paceFast"`
	PctLo    int     `json:"pctLo"`
	PctHi    int     `json:"pctHi"`
	HRLo     int     `json:"hrLo,omitempty"`
	HRHi     int     `json:"hrHi,omitempty"`
	Use      string  `json:"use"`
}

// zoneUse 与 zones.py 的 80/20 用途文案逐字一致（服务端英文，UI 负责本地化）。
var zoneUse = map[string]string{
	"Z1": "recovery, warm-up and cool-down",
	"Z2": "easy and long runs (upper half: steady)",
	"X":  "marathon pace, steady: only on purpose",
	"Z3": "tempo, cruise intervals",
	"Y":  "threshold",
	"Z4": "VO2 intervals (5k-3k pace)",
	"Z5": "strides, short fast reps",
}

// SpeedZoneTable 由阈值配速（秒/公里）生成 80/20 七区表；lthr>0 时附心率列。
// 注意 TrainingPeaks 口径是「阈值速度的百分比」：越快数值越大，
// 配速 = 阈值配速 / 分数。
func SpeedZoneTable(thresholdSecPerKm float64, lthr float64) []PaceZoneRow {
	rows := make([]PaceZoneRow, 0, len(DefaultSpeedZones))
	for i, z := range DefaultSpeedZones {
		top := z.Name == "Z5"
		r := PaceZoneRow{
			Name:     z.Name,
			PaceSlow: thresholdSecPerKm / z.Lo,
			PctLo:    int(math.Round(z.Lo * 100)),
			Use:      zoneUse[z.Name],
		}
		if !top {
			r.PaceFast = thresholdSecPerKm / z.Hi
			r.PctHi = int(math.Round(z.Hi * 100))
		}
		if lthr > 0 {
			h := DefaultHRZones[i]
			r.HRLo = int(math.Round(h.Lo * lthr))
			if !top {
				r.HRHi = int(math.Round(h.Hi * lthr))
			}
		}
		rows = append(rows, r)
	}
	return rows
}

// --- Daniels-Gilbert VDOT（Daniels' Running Formula，S46）---

func vo2At(vMPerMin float64) float64 {
	return -4.60 + 0.182258*vMPerMin + 0.000104*vMPerMin*vMPerMin
}

func shareOfMax(minutes float64) float64 {
	return 0.8 + 0.1894393*math.Exp(-0.012778*minutes) + 0.2989558*math.Exp(-0.1932605*minutes)
}

// VDOT 由比赛距离（km）与用时（秒）算 VDOT 值。
func VDOT(raceKm, secs float64) float64 {
	t := secs / 60
	return vo2At(raceKm*1000/t) / shareOfMax(t)
}

func speedAtVO2(vo2 float64) float64 {
	a, b, c := 0.000104, 0.182258, -4.60-vo2
	return (-b + math.Sqrt(b*b-4*a*c)) / (2 * a)
}

// RaceSecsForVDOT 由 VDOT 反解该距离的比赛用时（zones.py 同款二分）。
func RaceSecsForVDOT(vd, km float64) float64 {
	lo, hi := 60.0, 600.0 // 分钟
	for i := 0; i < 60; i++ {
		mid := (lo + hi) / 2
		if VDOT(km, mid*60) > vd {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2 * 60
}

// DanielsRow E/M/T/I/R 一行；配速为秒/公里。
type DanielsRow struct {
	Name     string  `json:"name"`
	PaceFast float64 `json:"paceFast"`
	PaceSlow float64 `json:"paceSlow"`
	Use      string  `json:"use"`
}

var danielsPcts = []struct {
	name     string
	lo, hi   float64
	use      string
	marathon bool
}{
	{"E", 59, 74, "easy and long runs", false},
	{"M", 0, 0, "marathon pace", true},
	{"T", 86, 88, "threshold, tempo", false},
	{"I", 95, 100, "intervals 3-5 min", false},
	{"R", 105, 105, "repetitions 200-400 m", false},
}

// Daniels 由任一比赛成绩生成 VDOT 训练配速五档。
func Daniels(raceKm, secs float64) (float64, []DanielsRow) {
	vd := VDOT(raceKm, secs)
	rows := make([]DanielsRow, 0, len(danielsPcts))
	for _, d := range danielsPcts {
		if d.marathon {
			p := RaceSecsForVDOT(vd, 42.195) / 42.195
			rows = append(rows, DanielsRow{Name: d.name, PaceFast: p, PaceSlow: p, Use: d.use})
			continue
		}
		fast := 1000 / speedAtVO2(vd*d.hi/100) * 60
		slow := 1000 / speedAtVO2(vd*d.lo/100) * 60
		rows = append(rows, DanielsRow{Name: d.name, PaceFast: fast, PaceSlow: slow, Use: d.use})
	}
	return vd, rows
}

// raceToThreshold 比赛均速中能长期维持的份额（practice 系数，paces.py 同源）。
var raceToThreshold = []ZoneBound{
	{Name: "5", Lo: 5.0, Hi: 0.93},
	{Name: "10", Lo: 10.0, Hi: 0.975},
	{Name: "21.0975", Lo: 21.0975, Hi: 1.02},
	{Name: "42.195", Lo: 42.195, Hi: 1.075},
}

// ThresholdFromRace 由比赛成绩估算阈值配速（秒/公里）。距离取最近档。
// 起伏赛道与越野会低估能力（paces.py 同款提醒）。
func ThresholdFromRace(raceKm, secs float64) float64 {
	pace := secs / raceKm
	nearest := raceToThreshold[0]
	best := math.Abs(raceKm - nearest.Lo)
	for _, r := range raceToThreshold[1:] {
		if d := math.Abs(raceKm - r.Lo); d < best {
			best = d
			nearest = r
		}
	}
	return pace / nearest.Hi
}
