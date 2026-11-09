package ingest

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openrunner/core"
)

// GPX/TCX 解析恒等式（票 04）。fixture 值手工构造，断言与实现无关。
// GPX：5 个 trkpt，t+2s 故意缺失（空洞）；含 hr 扩展；无功率/踏频/距离。
// TCX：3 个 Trackpoint（t0/t1/t2）；含 DistanceMeters/Watts/Cadence/HR。

func TestParseGPX(t *testing.T) {
	f, err := os.Open("testdata/simple.gpx")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	activity, stream, err := ParseGPX(f)
	if err != nil {
		t.Fatalf("ParseGPX 失败: %v", err)
	}

	t.Run("summary：类型/开始时间/时长", func(t *testing.T) {
		if activity.Sport != core.SportRun {
			t.Errorf("期望 run（<type>running</type>）, 得到 %q", activity.Sport)
		}
		wantStart := time.Date(2026, 4, 10, 7, 0, 0, 0, time.UTC)
		if !activity.StartTime.Equal(wantStart) {
			t.Errorf("期望 %v, 得到 %v", wantStart, activity.StartTime)
		}
		// 5 点覆盖 0..5s（含 1 个空洞）→ 时长按覆盖跨度 5s
		if activity.Duration != 5*time.Second {
			t.Errorf("期望 5s, 得到 %v", activity.Duration)
		}
		if !activity.HasStream {
			t.Error("期望 HasStream=true")
		}
	})

	t.Run("stream：等间隔 + 空洞填 0 + hr 扩展", func(t *testing.T) {
		if stream.RecIntervalSecs != 1 {
			t.Fatalf("期望 1s, 得到 %d", stream.RecIntervalSecs)
		}
		if got := stream.Seconds(); got != 6 { // 0..5s 共 6 点
			t.Fatalf("期望 6 点, 得到 %d", got)
		}
		if stream.HeartRate[0] != 120 || stream.HeartRate[1] != 121 {
			t.Errorf("期望 hr [120,121], 得到 %v,%v", stream.HeartRate[0], stream.HeartRate[1])
		}
		if stream.HeartRate[2] != 0 {
			t.Errorf("期望空洞 hr=0（t+2s 缺失）, 得到 %v", stream.HeartRate[2])
		}
		if stream.HeartRate[3] != 123 {
			t.Errorf("期望 hr[3]=123, 得到 %v", stream.HeartRate[3])
		}
	})

	t.Run("stream：GPS 与海拔", func(t *testing.T) {
		if math.Abs(stream.Latitude[0]-31.2304) > 1e-9 {
			t.Errorf("期望 31.2304, 得到 %v", stream.Latitude[0])
		}
		if math.Abs(stream.Longitude[5]-121.4743) > 1e-9 {
			t.Errorf("期望 121.4743, 得到 %v", stream.Longitude[5])
		}
		if stream.AltitudeM[0] != 100 || stream.AltitudeM[5] != 102.5 {
			t.Errorf("期望海拔 [100,102.5], 得到 %v,%v", stream.AltitudeM[0], stream.AltitudeM[5])
		}
	})

	t.Run("爬升计算跳过空洞（回归：曾把缺测 0 当 -100m 落差）", func(t *testing.T) {
		// fixture 海拔 100→100.5→(缺测)→101.5→102→102.5 → 真实爬升 2.5m
		if math.Abs(activity.ElevationGain-2.5) > 1e-9 {
			t.Fatalf("期望爬升 2.5m, 得到 %v", activity.ElevationGain)
		}
	})

	t.Run("GPX 无功率/踏频 → 全 0", func(t *testing.T) {
		for i, p := range stream.Power {
			if p != 0 {
				t.Fatalf("期望功率全 0, @%d=%v", i, p)
			}
		}
	})
}

func TestParseTCX(t *testing.T) {
	f, err := os.Open("testdata/simple.tcx")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	activity, stream, err := ParseTCX(f)
	if err != nil {
		t.Fatalf("ParseTCX 失败: %v", err)
	}

	t.Run("summary：Sport=Biking → ride", func(t *testing.T) {
		if activity.Sport != core.SportRide {
			t.Errorf("期望 ride, 得到 %q", activity.Sport)
		}
		wantStart := time.Date(2026, 4, 11, 8, 0, 0, 0, time.UTC)
		if !activity.StartTime.Equal(wantStart) {
			t.Errorf("期望 %v, 得到 %v", wantStart, activity.StartTime)
		}
		if activity.Duration != 6*time.Second {
			t.Errorf("期望 6s（TotalTimeSeconds）, 得到 %v", activity.Duration)
		}
		if math.Abs(activity.Distance-30) > 1e-9 {
			t.Errorf("期望 30m, 得到 %v", activity.Distance)
		}
	})

	t.Run("stream：功率/踏频/HR/距离/海拔换算", func(t *testing.T) {
		if stream.Seconds() != 3 {
			t.Fatalf("期望 3 点, 得到 %d", stream.Seconds())
		}
		// Watts 扩展
		if stream.Power[0] != 200 || stream.Power[2] != 220 {
			t.Errorf("期望功率 [200,…,220], 得到 %v", stream.Power)
		}
		if stream.Cadence[1] != 86 {
			t.Errorf("期望踏频 86, 得到 %v", stream.Cadence[1])
		}
		if stream.HeartRate[2] != 132 {
			t.Errorf("期望 hr 132, 得到 %v", stream.HeartRate[2])
		}
		if stream.DistanceM[2] != 10 {
			t.Errorf("期望距离 10m, 得到 %v", stream.DistanceM[2])
		}
		if stream.AltitudeM[2] != 101 {
			t.Errorf("期望海拔 101m, 得到 %v", stream.AltitudeM[2])
		}
		// TCX 无速度字段 → 由距离差分推导 5 m/s = 18 km/h
		if math.Abs(stream.SpeedKph[1]-18) > 0.01 {
			t.Errorf("期望由距离差推导 18 km/h, 得到 %v", stream.SpeedKph[1])
		}
	})
}

func TestParseGPXTracksMultipleSegments(t *testing.T) {
	// 两个 trkseg 应合并为一条流
	xml := `<?xml version="1.0"?><gpx version="1.1"><trk><trkseg>
<trkpt lat="31.0" lon="121.0"><time>2026-04-10T07:00:00Z</time></trkpt>
<trkpt lat="31.1" lon="121.1"><time>2026-04-10T07:00:01Z</time></trkpt>
</trkseg><trkseg>
<trkpt lat="31.2" lon="121.2"><time>2026-04-10T07:00:02Z</time></trkpt>
</trkseg></trk></gpx>`
	_, stream, err := ParseGPX(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if stream.Seconds() != 3 || stream.Latitude[2] != 31.2 {
		t.Fatalf("期望 3 点合并, 得到 %d 点 lat2=%v", stream.Seconds(), stream.Latitude[2])
	}
}

func TestParseGPXTCXRejects(t *testing.T) {
	t.Run("非法 XML", func(t *testing.T) {
		if _, _, err := ParseGPX(strings.NewReader("not xml at all")); err == nil {
			t.Fatal("期望报错")
		}
	})
	t.Run("无 trkpt", func(t *testing.T) {
		if _, _, err := ParseGPX(strings.NewReader(`<?xml version="1.0"?><gpx></gpx>`)); err == nil {
			t.Fatal("期望报错")
		}
	})
	t.Run("TCX 无 Activity", func(t *testing.T) {
		if _, _, err := ParseTCX(strings.NewReader(`<?xml version="1.0"?><TrainingCenterDatabase></TrainingCenterDatabase>`)); err == nil {
			t.Fatal("期望报错")
		}
	})
}
