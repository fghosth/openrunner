// Package ingest 把外部来源（FIT 文件、后续的平台 API）转换为 openrunner 领域对象。
// clean-room：仅依据公开的 FIT 协议与 muktihari/fit 库实现，公式常数不涉及。
package ingest

import (
	"fmt"
	"io"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/filedef"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"

	"github.com/openrunner/core"
)

// ParseFIT 解析 FIT 活动文件为 Activity summary 与归一化 Stream。
// 归一化约定见 core.Stream：等间隔、按秒对齐、缺测填 0。
func ParseFIT(r io.Reader) (core.Activity, core.Stream, error) {
	listener := filedef.NewListener()
	dec := decoder.New(r, decoder.WithMesgListener(listener))
	if _, err := dec.Decode(); err != nil {
		return core.Activity{}, core.Stream{}, fmt.Errorf("FIT 解析失败: %w", err)
	}

	file := listener.File()
	act, ok := file.(*filedef.Activity)
	if !ok {
		return core.Activity{}, core.Stream{}, fmt.Errorf("不是活动文件（file type: %T）", file)
	}
	if len(act.Sessions) == 0 {
		return core.Activity{}, core.Stream{}, fmt.Errorf("FIT 文件缺少 Session")
	}

	session := act.Sessions[0]
	// Session 级 invalid 哨兵归 0（如水上运动的距离/爬升常为 invalid）
	activity := core.Activity{
		Sport:     mapSport(session.Sport),
		StartTime: session.StartTime,
		HasStream: true,
	}
	if session.TotalElapsedTime != basetype.Uint32Invalid {
		activity.Duration = time.Duration(session.TotalElapsedTime) * time.Millisecond
	}
	if session.TotalDistance != basetype.Uint32Invalid {
		activity.Distance = float64(session.TotalDistance) / 100 // scale 100 → m
	}
	if session.TotalAscent != basetype.Uint16Invalid {
		activity.ElevationGain = float64(session.TotalAscent)
	}
	if activity.Distance == 0 && activity.Duration == 0 {
		return core.Activity{}, core.Stream{}, fmt.Errorf("Session 缺少有效的时长与距离")
	}

	samples := make([]core.Sample, 0, len(act.Records))
	for _, rec := range act.Records {
		samples = append(samples, sampleFromRecord(rec))
	}
	stream := core.NormalizeStream(1, samples)
	if stream.Seconds() == 0 {
		return core.Activity{}, core.Stream{}, fmt.Errorf("FIT 文件没有 Record 数据")
	}
	return activity, stream, nil
}

func sampleFromRecord(rec *mesgdef.Record) core.Sample {
	s := core.Sample{T: rec.Timestamp}

	if rec.HeartRate != basetype.Uint8Invalid {
		s.HeartRate = float64(rec.HeartRate)
	}
	if rec.Cadence != basetype.Uint8Invalid {
		s.Cadence = float64(rec.Cadence)
	}
	if rec.Power != basetype.Uint16Invalid {
		s.Power = float64(rec.Power)
	}
	// 速度：优先 enhanced（scale 1000, m/s），否则传统 speed；→ km/h
	switch {
	case rec.EnhancedSpeed != basetype.Uint32Invalid && rec.EnhancedSpeed != 0:
		s.SpeedKph = float64(rec.EnhancedSpeed) / 1000 * 3.6
	case rec.Speed != basetype.Uint16Invalid:
		s.SpeedKph = float64(rec.Speed) / 1000 * 3.6
	}
	// 距离：scale 100 → m
	if rec.Distance != basetype.Uint32Invalid {
		s.DistanceM = float64(rec.Distance) / 100
	}
	// 海拔：优先 enhanced（scale 5, offset 500）；enhanced 缺席或 raw=0（=−500m，物理上不可信）
	// 时回退传统字段（scaled helper 自带 offset）。
	switch {
	case rec.EnhancedAltitude != basetype.Uint32Invalid && rec.EnhancedAltitude != 0:
		s.AltitudeM = float64(rec.EnhancedAltitude)/5 - 500
	case rec.Altitude != basetype.Uint16Invalid && rec.Altitude != 0:
		s.AltitudeM = rec.AltitudeScaled()
	}
	// GPS：semicircles → degrees（FIT 规范公式：semi × 180 / 2^31）；invalid（±2^31-1）保持 0
	if rec.PositionLat != basetype.Sint32Invalid && rec.PositionLong != basetype.Sint32Invalid {
		s.Latitude = float64(rec.PositionLat) * 180.0 / (1 << 31)
		s.Longitude = float64(rec.PositionLong) * 180.0 / (1 << 31)
	}
	return s
}

func mapSport(s typedef.Sport) core.Sport {
	switch s {
	case typedef.SportRunning:
		return core.SportRun
	case typedef.SportCycling:
		return core.SportRide
	case typedef.SportSwimming:
		return core.SportSwim
	default:
		return core.SportOther
	}
}
