package core

import (
	"testing"
	"time"
)

func TestActivityValidate(t *testing.T) {
	start := time.Date(2026, 10, 24, 7, 30, 0, 0, time.Local)

	t.Run("合法的跑步 Activity", func(t *testing.T) {
		a := Activity{AthleteID: 1, Sport: SportRun, StartTime: start, Duration: 3600 * time.Second, Distance: 10000}
		if err := a.Validate(); err != nil {
			t.Fatalf("期望合法, 得到 %v", err)
		}
	})

	t.Run("未知运动类型不合法", func(t *testing.T) {
		a := Activity{AthleteID: 1, Sport: Sport("horse"), StartTime: start}
		if err := a.Validate(); err == nil {
			t.Fatal("期望校验失败")
		}
	})

	t.Run("缺少开始时间不合法", func(t *testing.T) {
		a := Activity{AthleteID: 1, Sport: SportRun}
		if err := a.Validate(); err == nil {
			t.Fatal("期望校验失败")
		}
	})

	t.Run("负距离不合法", func(t *testing.T) {
		a := Activity{AthleteID: 1, Sport: SportRun, StartTime: start, Distance: -1}
		if err := a.Validate(); err == nil {
			t.Fatal("期望校验失败")
		}
	})

	t.Run("负时长不合法", func(t *testing.T) {
		a := Activity{AthleteID: 1, Sport: SportRun, StartTime: start, Duration: -time.Second}
		if err := a.Validate(); err == nil {
			t.Fatal("期望校验失败")
		}
	})

	t.Run("负爬升不合法", func(t *testing.T) {
		a := Activity{AthleteID: 1, Sport: SportRun, StartTime: start, ElevationGain: -0.5}
		if err := a.Validate(); err == nil {
			t.Fatal("期望校验失败")
		}
	})
}

func TestIsCollectedSport(t *testing.T) {
	cases := map[Sport]bool{
		SportRun:   true,
		SportRide:  true,
		SportSwim:  false,
		SportOther: false,
	}
	for s, want := range cases {
		if got := IsCollectedSport(s); got != want {
			t.Fatalf("IsCollectedSport(%q) 期望 %v 得到 %v", s, want, got)
		}
	}
}
