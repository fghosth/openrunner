package core

import (
	"encoding/json"
	"time"
)

// Stream 是 Activity 内按固定采样间隔记录的连续数据通道（见 CONTEXT.md）。
// 约定（沿用 01 文档 §1.2）：所有通道等长、按秒对齐、缺测填 0；距离为累计值（米）；
// 速度 km/h；功率 W；海拔 m；经纬度十进制度。
type Stream struct {
	RecIntervalSecs int       `json:"recIntervalSecs"`
	HeartRate       []float64 `json:"hr,omitempty"`       // bpm
	SpeedKph        []float64 `json:"speed,omitempty"`    // km/h
	DistanceM       []float64 `json:"distance,omitempty"` // 累计, m
	AltitudeM       []float64 `json:"alt,omitempty"`      // m
	Power           []float64 `json:"power,omitempty"`    // W
	Cadence         []float64 `json:"cad,omitempty"`      // rpm
	Latitude        []float64 `json:"lat,omitempty"`      // degrees
	Longitude       []float64 `json:"lon,omitempty"`      // degrees
}

// Seconds 返回流长度（采样点数 × 间隔 = 覆盖秒数）。
func (s Stream) Seconds() int {
	for _, ch := range [][]float64{
		s.HeartRate, s.SpeedKph, s.DistanceM, s.AltitudeM,
		s.Power, s.Cadence, s.Latitude, s.Longitude,
	} {
		if len(ch) > 0 {
			return len(ch)
		}
	}
	return 0
}

// Sample 是归一化前的单点样本；T 为绝对时间，字段缺省即缺测。
type Sample struct {
	T         time.Time
	HeartRate float64
	SpeedKph  float64
	DistanceM float64
	AltitudeM float64
	Power     float64
	Cadence   float64
	Latitude  float64
	Longitude float64
}

// NormalizeStream 把乱序、含空洞的样本归一化为等间隔 Stream：
// 按时间排序后，以 interval 秒为步长从首点到末点展开，缺失秒填 0。
func NormalizeStream(intervalSecs int, samples []Sample) Stream {
	if len(samples) == 0 {
		return Stream{RecIntervalSecs: intervalSecs}
	}
	sorted := make([]Sample, len(samples))
	copy(sorted, samples)
	// 插入排序足够：FIT 记录近序，乱序仅局部
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j].T.Before(sorted[j-1].T); j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	first, last := sorted[0].T, sorted[len(sorted)-1].T
	steps := int(last.Sub(first).Seconds()/float64(intervalSecs)) + 1
	byIndex := make(map[int]Sample, len(sorted))
	for _, s := range sorted {
		idx := int(s.T.Sub(first).Seconds() / float64(intervalSecs))
		if _, exists := byIndex[idx]; !exists { // 同秒重复取先到者
			byIndex[idx] = s
		}
	}

	out := Stream{
		RecIntervalSecs: intervalSecs,
		HeartRate:       make([]float64, steps),
		SpeedKph:        make([]float64, steps),
		DistanceM:       make([]float64, steps),
		AltitudeM:       make([]float64, steps),
		Power:           make([]float64, steps),
		Cadence:         make([]float64, steps),
		Latitude:        make([]float64, steps),
		Longitude:       make([]float64, steps),
	}
	for i := 0; i < steps; i++ {
		s, ok := byIndex[i]
		if !ok {
			continue // 空洞保持 0
		}
		out.HeartRate[i] = s.HeartRate
		out.SpeedKph[i] = s.SpeedKph
		out.DistanceM[i] = s.DistanceM
		out.AltitudeM[i] = s.AltitudeM
		out.Power[i] = s.Power
		out.Cadence[i] = s.Cadence
		out.Latitude[i] = s.Latitude
		out.Longitude[i] = s.Longitude
	}
	return out
}

// Marshal 序列化为 JSON BLOB（存储形态见 ADR/票 12 决策）。
func (s Stream) Marshal() ([]byte, error) {
	return json.Marshal(s)
}

// UnmarshalStream 还原 Stream。
func UnmarshalStream(b []byte) (Stream, error) {
	var s Stream
	if err := json.Unmarshal(b, &s); err != nil {
		return Stream{}, err
	}
	return s, nil
}

// AvgHeartRate 非零心率样本的均值；无心率数据返回 0。
func (s Stream) AvgHeartRate() float64 {
	sum, n := 0.0, 0
	for _, hr := range s.HeartRate {
		if hr > 0 {
			sum += hr
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// StreamStats 单条 Stream 的派生统计（详情页展示用，不入库）。
type StreamStats struct {
	// AvgCadenceSpm 平均步频（步/分，双侧）。FIT 的 cadence 对跑步是**单脚**值，
	// 这里已 ×2 换算成 Garmin Connect 那种 steps/min。
	AvgCadenceSpm float64 `json:"avgCadenceSpm"`
	// MaxCadenceSpm 最大步频（同样已 ×2）。
	MaxCadenceSpm float64 `json:"maxCadenceSpm"`
	// TotalSteps 总步数。
	TotalSteps int `json:"totalSteps"`
	// AvgStrideM 平均步幅（米）。= 距离 / 总步数。
	AvgStrideM float64 `json:"avgStrideM"`
}

// SummarizeStream 计算步频/步幅等派生指标。
//
// 步幅不用「速度/步频」逐点算再平均（那样会被停顿点拉偏），而是用
// 总距离 ÷ 总步数 —— 与 Garmin 的算法口径一致。
func SummarizeStream(s Stream) StreamStats {
	var out StreamStats
	if s.RecIntervalSecs <= 0 || len(s.Cadence) == 0 {
		return out
	}
	sum, n := 0.0, 0
	maxCad := 0.0
	totalSteps := 0.0
	for _, c := range s.Cadence {
		if c <= 0 {
			continue
		}
		sum += c
		n++
		if c > maxCad {
			maxCad = c
		}
		totalSteps += c * 2 * float64(s.RecIntervalSecs) / 60
	}
	if n == 0 {
		return out
	}
	out.AvgCadenceSpm = sum / float64(n) * 2
	out.MaxCadenceSpm = maxCad * 2
	out.TotalSteps = int(totalSteps + 0.5)

	// 优先用累计距离流末值；缺失时退回速度积分
	dist := 0.0
	if len(s.DistanceM) > 0 {
		dist = s.DistanceM[len(s.DistanceM)-1]
	}
	if dist <= 0 && len(s.SpeedKph) > 0 {
		for _, v := range s.SpeedKph {
			if v > 0 {
				dist += v / 3.6 * float64(s.RecIntervalSecs)
			}
		}
	}
	if totalSteps > 0 && dist > 0 {
		out.AvgStrideM = dist / totalSteps
	}
	return out
}
