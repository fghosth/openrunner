package core

import (
	"testing"
	"time"
)

func TestNormalizeStream(t *testing.T) {
	base := time.Date(2026, 10, 25, 7, 0, 0, 0, time.Local)

	t.Run("等间隔无空洞：原样保留", func(t *testing.T) {
		s := NormalizeStream(1, []Sample{
			{T: base, HeartRate: 100, SpeedKph: 18},
			{T: base.Add(time.Second), HeartRate: 101, SpeedKph: 18},
		})
		if len(s.HeartRate) != 2 || s.HeartRate[0] != 100 || s.HeartRate[1] != 101 {
			t.Fatalf("期望 [100 101], 得到 %v", s.HeartRate)
		}
		if s.RecIntervalSecs != 1 {
			t.Fatalf("期望间隔 1s, 得到 %d", s.RecIntervalSecs)
		}
	})

	t.Run("空洞填 0（票面验收：固定采样间隔、空洞填 0）", func(t *testing.T) {
		s := NormalizeStream(1, []Sample{
			{T: base, HeartRate: 100, SpeedKph: 18, DistanceM: 0},
			// 缺 t+1s
			{T: base.Add(2 * time.Second), HeartRate: 102, SpeedKph: 20, DistanceM: 40},
		})
		if len(s.HeartRate) != 3 {
			t.Fatalf("期望 3 个采样（含 1 个空洞）, 得到 %d", len(s.HeartRate))
		}
		if s.HeartRate[1] != 0 || s.SpeedKph[1] != 0 || s.DistanceM[1] != 0 {
			t.Fatalf("期望空洞填 0, 得到 %+v", s)
		}
		if s.HeartRate[2] != 102 {
			t.Fatalf("期望 [2]=102, 得到 %v", s.HeartRate)
		}
	})

	t.Run("乱序输入被排序", func(t *testing.T) {
		s := NormalizeStream(1, []Sample{
			{T: base.Add(time.Second), HeartRate: 101},
			{T: base, HeartRate: 100},
		})
		if s.HeartRate[0] != 100 || s.HeartRate[1] != 101 {
			t.Fatalf("期望排序后 [100 101], 得到 %v", s.HeartRate)
		}
	})

	t.Run("GPS 通道随样本携带", func(t *testing.T) {
		s := NormalizeStream(1, []Sample{
			{T: base, Latitude: 31.23, Longitude: 121.47},
			{T: base.Add(time.Second)}, // 该秒无 GPS → 0
		})
		if s.Latitude[0] != 31.23 || s.Latitude[1] != 0 {
			t.Fatalf("期望 GPS 缺测填 0, 得到 %v", s.Latitude)
		}
	})
}

func TestStreamMarshalRoundTrip(t *testing.T) {
	s := Stream{
		RecIntervalSecs: 1,
		HeartRate:       []float64{100, 0, 102},
		SpeedKph:        []float64{18, 0, 20},
		DistanceM:       []float64{0, 0, 40},
		AltitudeM:       []float64{100, 100.5, 101},
		Power:           []float64{250, 250, 0},
		Cadence:         []float64{90, 0, 88},
		Latitude:        []float64{31.2304, 0, 31.2305},
		Longitude:       []float64{121.4737, 0, 121.4738},
	}
	b, err := s.Marshal()
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	got, err := UnmarshalStream(b)
	if err != nil {
		t.Fatalf("UnmarshalStream 失败: %v", err)
	}
	if got.RecIntervalSecs != s.RecIntervalSecs || len(got.HeartRate) != 3 || got.AltitudeM[1] != 100.5 {
		t.Fatalf("往返不一致: %+v", got)
	}
}

func TestStreamSeconds(t *testing.T) {
	s := Stream{RecIntervalSecs: 1, HeartRate: make([]float64, 180)}
	if s.Seconds() != 180 {
		t.Fatalf("期望 180, 得到 %d", s.Seconds())
	}
}

func TestSummarizeStream(t *testing.T) {
	t.Run("单脚步频 ×2 与步幅", func(t *testing.T) {
		// 单脚 90 rpm ×2 = 180 spm；10 秒 → 30 步；距离 24 米 → 步幅 0.8m
		s := Stream{
			RecIntervalSecs: 1,
			Cadence:         []float64{90, 90, 90, 90, 90, 90, 90, 90, 90, 90},
			DistanceM:       []float64{2.4, 4.8, 7.2, 9.6, 12, 14.4, 16.8, 19.2, 21.6, 24},
		}
		got := SummarizeStream(s)
		if got.AvgCadenceSpm != 180 {
			t.Fatalf("平均步频期望 180 spm, 得到 %v", got.AvgCadenceSpm)
		}
		if got.TotalSteps != 30 {
			t.Fatalf("总步数期望 30, 得到 %d", got.TotalSteps)
		}
		if got.AvgStrideM < 0.79 || got.AvgStrideM > 0.81 {
			t.Fatalf("步幅期望约 0.8m, 得到 %v", got.AvgStrideM)
		}
	})

	t.Run("无步频数据时返回零值", func(t *testing.T) {
		got := SummarizeStream(Stream{RecIntervalSecs: 1, DistanceM: []float64{0, 100}})
		if got.AvgCadenceSpm != 0 || got.AvgStrideM != 0 {
			t.Fatalf("期望零值, 得到 %+v", got)
		}
	})

	t.Run("忽略缺测点", func(t *testing.T) {
		s := Stream{
			RecIntervalSecs: 1,
			Cadence:         []float64{0, 90, 0, 90},
			DistanceM:       []float64{0, 6, 12, 18},
		}
		got := SummarizeStream(s)
		if got.AvgCadenceSpm != 180 {
			t.Fatalf("缺测点不应拉低均值, 期望 180, 得到 %v", got.AvgCadenceSpm)
		}
	})
}
