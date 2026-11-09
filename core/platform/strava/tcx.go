package strava

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"time"

	"github.com/fghosth/openrunner/core/platform"
)

// TCX 结构（只覆盖本项目 ingest 会读的字段）。
// 参考 Garmin TrainingCenterDatabase v2 schema。

type tcxDB struct {
	XMLName    xml.Name    `xml:"TrainingCenterDatabase"`
	Xmlns      string      `xml:"xmlns,attr"`
	Activities tcxActivity `xml:"Activities>Activity"`
}

type tcxActivity struct {
	Sport string   `xml:"Sport,attr"`
	ID    string   `xml:"Id"`
	Lap   []tcxLap `xml:"Lap"`
}

type tcxLap struct {
	StartTime    string     `xml:"StartTime,attr"`
	TotalTimeSec float64    `xml:"TotalTimeSeconds"`
	DistanceM    float64    `xml:"DistanceMeters"`
	Tracks       []tcxTrack `xml:"Track"`
}

type tcxTrack struct {
	Points []tcxPoint `xml:"Trackpoint"`
}

type tcxPoint struct {
	Time     string       `xml:"Time"`
	Position *tcxPosition `xml:"Position,omitempty"`
	Altitude *float64     `xml:"AltitudeMeters,omitempty"`
	Distance *float64     `xml:"DistanceMeters,omitempty"`
	HR       *float64     `xml:"HeartRateBpm>Value,omitempty"`
	Cadence  *float64     `xml:"Cadence,omitempty"`
	Power    *float64     `xml:"Extensions>TPX>Watts,omitempty"`
	Speed    *float64     `xml:"Extensions>TPX>Speed,omitempty"`
}

type tcxPosition struct {
	Lat float64 `xml:"LatitudeDegrees"`
	Lng float64 `xml:"LongitudeDegrees"`
}

// buildTCX 把 Strava 的并列 stream 数组转成 TCX。
func buildTCX(
	detail apiActivity, ref platform.ActivityRef,
	times, dists, alts, hrs, cads, speeds, watts []float64,
	latlng [][2]float64,
) ([]byte, error) {
	n := len(times)
	if n == 0 {
		// 没有 time stream 时用最小长度推断
		for _, c := range [][]float64{dists, hrs, latlngToLen(latlng)} {
			if len(c) > n {
				n = len(c)
			}
		}
	}
	if n == 0 {
		return nil, fmt.Errorf("该活动没有可用的逐点数据（可能未记录 GPS/心率）")
	}

	start := ref.StartTime
	if start.IsZero() {
		start = time.Now()
	}

	pts := make([]tcxPoint, 0, n)
	for i := 0; i < n; i++ {
		var t time.Time
		if i < len(times) && times[i] > 0 {
			t = start.Add(time.Duration(times[i] * float64(time.Second)))
		} else {
			t = start.Add(time.Duration(i) * time.Second)
		}
		p := tcxPoint{Time: t.Format(time.RFC3339)}
		if i < len(latlng) && (latlng[i][0] != 0 || latlng[i][1] != 0) {
			p.Position = &tcxPosition{Lat: latlng[i][0], Lng: latlng[i][1]}
		}
		if i < len(alts) && alts[i] > 0 {
			v := alts[i]
			p.Altitude = &v
		}
		if i < len(dists) && dists[i] >= 0 {
			v := dists[i]
			p.Distance = &v
		}
		if i < len(hrs) && hrs[i] > 0 {
			v := hrs[i]
			p.HR = &v
		}
		if i < len(cads) && cads[i] > 0 {
			v := cads[i]
			p.Cadence = &v
		}
		if i < len(watts) && watts[i] > 0 {
			v := watts[i]
			p.Power = &v
		}
		if i < len(speeds) && speeds[i] > 0 {
			v := speeds[i]
			p.Speed = &v
		}
		pts = append(pts, p)
	}

	sport := "Running"
	switch ref.Sport {
	case platform.SportRide:
		sport = "Biking"
	case platform.SportSwim:
		sport = "Swimming"
	}

	doc := tcxDB{
		Xmlns: "http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2",
		Activities: tcxActivity{
			Sport: sport,
			ID:    start.Format(time.RFC3339),
			Lap: []tcxLap{{
				StartTime:    start.Format(time.RFC3339),
				TotalTimeSec: float64(detail.MovingTime),
				DistanceM:    detail.Distance,
				Tracks:       []tcxTrack{{Points: pts}},
			}},
		},
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", " ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("生成 TCX 失败: %w", err)
	}
	return buf.Bytes(), nil
}

// latlngToLen 用占位切片表达 latlng 的长度（仅为统一取最大长度）。
func latlngToLen(l [][2]float64) []float64 { return make([]float64, len(l)) }
