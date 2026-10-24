package core

import "time"

// Sport 是 Activity 的运动类型。运动类型是 Activity 的属性而非独立概念（见 CONTEXT.md）。
type Sport string

const (
	SportRun   Sport = "run"
	SportRide  Sport = "ride"
	SportSwim  Sport = "swim"
	SportOther Sport = "other"
)

// Activity 是一次运动训练的记录，是所有度量计算的原子单元（见 CONTEXT.md）。
type Activity struct {
	ID            int64
	AthleteID     int64
	Sport         Sport
	StartTime     time.Time // 活动开始的本地时间；跨午夜归开始日
	Duration      time.Duration
	Distance      float64 // 米，恒公制存储
	ElevationGain float64
	IsPlanned     bool
	HasStream     bool
}

// Validate 检查 Activity 的领域不变量。
func (a Activity) Validate() error {
	if err := validateSport(a.Sport); err != nil {
		return err
	}
	if a.StartTime.IsZero() {
		return &ValidationError{Field: "StartTime", Reason: "不能为空"}
	}
	if a.Distance < 0 {
		return &ValidationError{Field: "Distance", Reason: "不能为负"}
	}
	return nil
}

func validateSport(s Sport) error {
	switch s {
	case SportRun, SportRide, SportSwim, SportOther:
		return nil
	default:
		return &ValidationError{Field: "Sport", Reason: "未知运动类型 " + string(s)}
	}
}
