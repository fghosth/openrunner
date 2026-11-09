package ingest

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/fghosth/openrunner/core"
)

// GPX / TCX 解析（票 04）。两者都是 XML，用 encoding/xml 手工反序列化最小字段集：
//   GPX 1.1：trk/trkseg/trkpt（lat/lon/ele/time）+ Garmin gpxtpx:TrackPointExtension/hr
//   TCX   v2：Activities/Activity[@Sport]/Lap/Track/Trackpoint
//           （Time/Position/AltitudeMeters/DistanceMeters/HeartRateBpm/Cadence/Watts）
//
// 共同约定：输出经 core.NormalizeStream 归一化（等间隔、空洞填 0）。

// ---------------- GPX ----------------

type gpxFile struct {
	XMLName xml.Name   `xml:"gpx"`
	Tracks  []gpxTrack `xml:"trk"`
}

type gpxTrack struct {
	Name     string        `xml:"name"`
	Type     string        `xml:"type"`
	Segments []gpxTrackSeg `xml:"trkseg"`
}

type gpxTrackSeg struct {
	Points []gpxPoint `xml:"trkpt"`
}

type gpxPoint struct {
	Lat  float64 `xml:"lat,attr"`
	Lon  float64 `xml:"lon,attr"`
	Ele  float64 `xml:"ele"`
	Time string  `xml:"time"`
	HR   int     `xml:"extensions>TrackPointExtension>hr"`
	// 部分实现把 extensions 里的命名空间去掉，多路兜底
	HRPlain int `xml:"extensions>hr"`
}

// ParseGPX 解析 GPX 文件为 Activity summary 与归一化 Stream。
func ParseGPX(r io.Reader) (core.Activity, core.Stream, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return core.Activity{}, core.Stream{}, fmt.Errorf("读取 GPX 失败: %w", err)
	}
	// 命名空间处理：去前缀后按本地名匹配（encoding/xml 对 xmlns 前缀不友善）
	normalized := stripNamespacePrefixes(raw)

	var g gpxFile
	if err := xml.Unmarshal(normalized, &g); err != nil {
		return core.Activity{}, core.Stream{}, fmt.Errorf("GPX 解析失败: %w", err)
	}

	var samples []core.Sample
	sport := core.SportOther
	for _, trk := range g.Tracks {
		if sport == core.SportOther {
			sport = mapGPXType(trk.Type)
		}
		for _, seg := range trk.Segments {
			for _, p := range seg.Points {
				t, err := parseXMLTime(p.Time)
				if err != nil {
					continue // 无时间的点无法对齐
				}
				hr := p.HR
				if hr == 0 {
					hr = p.HRPlain
				}
				samples = append(samples, core.Sample{
					T:         t,
					Latitude:  p.Lat,
					Longitude: p.Lon,
					AltitudeM: p.Ele,
					HeartRate: float64(hr),
				})
			}
		}
	}
	if len(samples) == 0 {
		return core.Activity{}, core.Stream{}, fmt.Errorf("GPX 中没有可用的轨迹点")
	}

	stream := core.NormalizeStream(1, samples)
	activity := core.Activity{
		Sport:     sport,
		StartTime: samples[0].T,
		Duration:  time.Duration(stream.Seconds()-1) * time.Second, // 覆盖跨度
		HasStream: true,
	}
	activity.Distance = streamPathLengthM(stream)
	activity.ElevationGain = streamElevationGain(stream)
	return activity, stream, nil
}

func mapGPXType(s string) core.Sport {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "running", "run", "trail_running":
		return core.SportRun
	case "cycling", "biking", "bike", "road_cycling", "mountain_biking":
		return core.SportRide
	case "swimming", "swim":
		return core.SportSwim
	default:
		return core.SportOther
	}
}

// ---------------- TCX ----------------

type tcxFile struct {
	XMLName    xml.Name      `xml:"TrainingCenterDatabase"`
	Activities []tcxActivity `xml:"Activities>Activity"`
}

type tcxActivity struct {
	Sport string   `xml:"Sport,attr"`
	ID    string   `xml:"Id"`
	Laps  []tcxLap `xml:"Lap"`
}

type tcxLap struct {
	StartTime string     `xml:"StartTime,attr"`
	TotalTime float64    `xml:"TotalTimeSeconds"`
	Distance  float64    `xml:"DistanceMeters"`
	Tracks    []tcxTrack `xml:"Track"`
}

type tcxTrack struct {
	Points []tcxTrackpoint `xml:"Trackpoint"`
}

type tcxTrackpoint struct {
	Time         string  `xml:"Time"`
	AltitudeM    float64 `xml:"AltitudeMeters"`
	DistanceM    float64 `xml:"DistanceMeters"`
	HeartRate    int     `xml:"HeartRateBpm>Value"`
	Cadence      int     `xml:"Cadence"`
	Watts        float64 `xml:"Extensions>TPX>Watts"`
	LatitudeDeg  float64 `xml:"Position>LatitudeDegrees"`
	LongitudeDeg float64 `xml:"Position>LongitudeDegrees"`
}

// ParseTCX 解析 TCX 文件为 Activity summary 与归一化 Stream。
func ParseTCX(r io.Reader) (core.Activity, core.Stream, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return core.Activity{}, core.Stream{}, fmt.Errorf("读取 TCX 失败: %w", err)
	}
	normalized := stripNamespacePrefixes(raw)

	var f tcxFile
	if err := xml.Unmarshal(normalized, &f); err != nil {
		return core.Activity{}, core.Stream{}, fmt.Errorf("TCX 解析失败: %w", err)
	}
	if len(f.Activities) == 0 {
		return core.Activity{}, core.Stream{}, fmt.Errorf("TCX 中没有 Activity")
	}

	act := f.Activities[0]
	var samples []core.Sample
	var totalTime float64
	var totalDistance float64
	for _, lap := range act.Laps {
		totalTime += lap.TotalTime
		if lap.Distance > 0 {
			totalDistance = lap.Distance // 最后一圈的累计距离即总距离
		}
		for _, tr := range lap.Tracks {
			for _, p := range tr.Points {
				t, err := parseXMLTime(p.Time)
				if err != nil {
					continue
				}
				samples = append(samples, core.Sample{
					T:         t,
					Latitude:  p.LatitudeDeg,
					Longitude: p.LongitudeDeg,
					AltitudeM: p.AltitudeM,
					DistanceM: p.DistanceM,
					HeartRate: float64(p.HeartRate),
					Cadence:   float64(p.Cadence),
					Power:     p.Watts,
				})
			}
		}
	}
	if len(samples) == 0 {
		return core.Activity{}, core.Stream{}, fmt.Errorf("TCX 中没有 Trackpoint")
	}

	stream := core.NormalizeStream(1, samples)
	// TCX 无速度字段 → 由累计距离差分推导
	deriveSpeedFromDistance(&stream)

	start := samples[0].T
	if id, err := parseXMLTime(act.ID); err == nil {
		start = id
	}
	duration := time.Duration(totalTime) * time.Second
	if duration <= 0 {
		duration = time.Duration(stream.Seconds()-1) * time.Second
	}
	activity := core.Activity{
		Sport:         mapTCXSport(act.Sport),
		StartTime:     start,
		Duration:      duration,
		TimerSecs:     int(duration.Seconds()),
		Distance:      totalDistance,
		ElevationGain: streamElevationGain(stream),
		HasStream:     true,
	}
	return activity, stream, nil
}

func mapTCXSport(s string) core.Sport {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "running":
		return core.SportRun
	case "biking", "cycling":
		return core.SportRide
	case "swimming", "other":
		if strings.EqualFold(s, "swimming") {
			return core.SportSwim
		}
		return core.SportOther
	default:
		return core.SportOther
	}
}

// ---------------- 共享工具 ----------------

// parseXMLTime 解析 ISO8601（GPX/TCX 均为 UTC 'Z' 或带偏移）。
func parseXMLTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("空时间")
	}
	for _, layout := range []string{
		time.RFC3339Nano, time.RFC3339,
		"2006-01-02T15:04:05.000Z", "2006-01-02T15:04:05Z",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析时间 %q", s)
}

// stripNamespacePrefixes 把 <ns:Tag> 变成 <Tag>、去掉 xmlns:* 声明属性。
// encoding/xml 的 struct tag 无法表达"任意前缀"，标准化后按本地名匹配最稳。
func stripNamespacePrefixes(raw []byte) []byte {
	s := string(raw)
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '<':
			inTag = true
			b.WriteByte(c)
		case c == '>':
			inTag = false
			b.WriteByte(c)
		case inTag && c == ':':
			// 删掉前缀（回退已写入的标识符字符）
			out := b.String()
			j := len(out) - 1
			for j >= 0 && (isNameChar(out[j])) {
				j--
			}
			b.Reset()
			b.WriteString(out[:j+1])
		default:
			b.WriteByte(c)
		}
	}
	return []byte(b.String())
}

func isNameChar(c byte) bool {
	return c == '_' || c == '-' || c == '.' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// deriveSpeedFromDistance 由累计距离差分推导速度（km/h），填补 TCX 无速度字段的空白。
func deriveSpeedFromDistance(s *core.Stream) {
	n := s.Seconds()
	hasDistance := false
	for _, d := range s.DistanceM {
		if d > 0 {
			hasDistance = true
			break
		}
	}
	if !hasDistance {
		return
	}
	hasSpeed := false
	for _, v := range s.SpeedKph {
		if v > 0 {
			hasSpeed = true
			break
		}
	}
	if hasSpeed {
		return // 已有速度字段则不覆盖
	}
	for i := 1; i < n; i++ {
		delta := s.DistanceM[i] - s.DistanceM[i-1]
		if delta > 0 {
			s.SpeedKph[i] = delta / float64(s.RecIntervalSecs) * 3.6
		}
	}
	if n > 1 {
		s.SpeedKph[0] = s.SpeedKph[1]
	}
}

// streamPathLengthM 由 GPS 点计算轨迹长度（Haversine）。
func streamPathLengthM(s core.Stream) float64 {
	total := 0.0
	for i := 1; i < s.Seconds(); i++ {
		lat1, lon1 := s.Latitude[i-1], s.Longitude[i-1]
		lat2, lon2 := s.Latitude[i], s.Longitude[i]
		if lat1 == 0 && lon1 == 0 {
			continue
		}
		if lat2 == 0 && lon2 == 0 {
			continue
		}
		total += haversineM(lat1, lon1, lat2, lon2)
	}
	return total
}

func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusM = 6371000.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// streamElevationGain 累计爬升（只计正增量）。
// 空洞（NormalizeStream 填 0 的缺测点）必须跳过——否则 0 会被误判为 -100m 落差，
// 随后回到真实海拔的跳变全部计入爬升（Round 8 实测 bug：2.5m 爬升被算成 103m）。
func streamElevationGain(s core.Stream) float64 {
	gain := 0.0
	last := 0.0
	hasLast := false
	for i := 0; i < s.Seconds(); i++ {
		alt := s.AltitudeM[i]
		if alt == 0 { // 缺测（真实海拔恰为 0 的海平面活动无法区分，可接受）
			continue
		}
		if hasLast && alt > last {
			gain += alt - last
		}
		last = alt
		hasLast = true
	}
	return gain
}
