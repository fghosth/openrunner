package ingest

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/mesgdef"

	"github.com/fghosth/openrunner/core"
)

// sampleFromRecord 的换算规则单测（scale/offset/invalid → 领域单位）。
// 期望值是手工换算的独立真值，与实现写法无关。
func TestSampleFromRecordConversion(t *testing.T) {
	t.Run("速度 scale 1000: 5000 → 18 km/h", func(t *testing.T) {
		s := sampleFromRecord(&mesgdef.Record{Speed: 5000})
		if math.Abs(s.SpeedKph-18) > 1e-9 {
			t.Fatalf("期望 18, 得到 %v", s.SpeedKph)
		}
	})

	t.Run("enhanced speed 8330 → 29.988 km/h", func(t *testing.T) {
		s := sampleFromRecord(&mesgdef.Record{EnhancedSpeed: 8330, Speed: 1})
		if math.Abs(s.SpeedKph-8330.0/1000*3.6) > 1e-9 {
			t.Fatalf("期望 %v, 得到 %v", 8330.0/1000*3.6, s.SpeedKph)
		}
	})

	t.Run("距离 scale 100: 40581 → 405.81 m", func(t *testing.T) {
		s := sampleFromRecord(&mesgdef.Record{Distance: 40581})
		if math.Abs(s.DistanceM-405.81) > 1e-9 {
			t.Fatalf("期望 405.81, 得到 %v", s.DistanceM)
		}
	})

	t.Run("enhanced 海拔 scale 5 offset 500: 3000 → 100 m", func(t *testing.T) {
		s := sampleFromRecord(&mesgdef.Record{EnhancedAltitude: 3000})
		if s.AltitudeM != 100 {
			t.Fatalf("期望 100, 得到 %v", s.AltitudeM)
		}
	})

	t.Run("传统海拔 scale 5 offset 500: 3000 → 100 m", func(t *testing.T) {
		s := sampleFromRecord(&mesgdef.Record{Altitude: 3000})
		if s.AltitudeM != 100 {
			t.Fatalf("期望 100, 得到 %v", s.AltitudeM)
		}
	})

	t.Run("心率/功率/踏频直通", func(t *testing.T) {
		s := sampleFromRecord(&mesgdef.Record{HeartRate: 126, Power: 250, Cadence: 90})
		if s.HeartRate != 126 || s.Power != 250 || s.Cadence != 90 {
			t.Fatalf("得到 %+v", s)
		}
	})

	t.Run("GPS 半圆转度", func(t *testing.T) {
		// 半圆常量按 FIT 规范公式独立计算（2^31 圆 = 180°）
		lat := int32(math.Round(31.2304 * (1 << 31) / 180))
		lon := int32(math.Round(121.4737 * (1 << 31) / 180))
		s := sampleFromRecord(&mesgdef.Record{PositionLat: lat, PositionLong: lon})
		if math.Abs(s.Latitude-31.2304) > 1e-6 {
			t.Fatalf("期望 ≈31.2304, 得到 %v", s.Latitude)
		}
		if math.Abs(s.Longitude-121.4737) > 1e-6 {
			t.Fatalf("期望 ≈121.4737, 得到 %v", s.Longitude)
		}
	})

	t.Run("invalid 哨兵值 → 0（缺测填 0 的前置）", func(t *testing.T) {
		s := sampleFromRecord(&mesgdef.Record{
			HeartRate: 255, Power: 65535, Cadence: 255,
			Distance: 4294967295, Speed: 65535,
			EnhancedAltitude: 4294967295, EnhancedSpeed: 4294967295,
			PositionLat: 2147483647, PositionLong: 2147483647,
		})
		if s.HeartRate != 0 || s.Power != 0 || s.Cadence != 0 || s.DistanceM != 0 ||
			s.SpeedKph != 0 || s.AltitudeM != 0 || s.Latitude != 0 || s.Longitude != 0 {
			t.Fatalf("期望全部缺测为 0, 得到 %+v", s)
		}
	})
}

func TestParseFITBigActivity(t *testing.T) {
	// 官方 FIT SDK 样本（库 testdata 复分发，MIT + FIT SDK 许可）
	f, err := os.Open("testdata/big_activity.fit")
	if err != nil {
		t.Fatalf("打开 fixture 失败: %v", err)
	}
	defer f.Close()

	activity, stream, err := ParseFIT(f)
	if err != nil {
		t.Fatalf("ParseFIT 失败: %v", err)
	}

	if activity.Sport != core.SportRide {
		t.Errorf("期望 ride, 得到 %q", activity.Sport)
	}
	if activity.StartTime.IsZero() || activity.Duration <= 0 {
		t.Errorf("期望有效时间, 得到 start=%v duration=%v", activity.StartTime, activity.Duration)
	}
	// 99998 条记录、约 30877s → stream 覆盖到末点
	if stream.Seconds() < 30000 {
		t.Errorf("期望长 stream, 得到 %d 点", stream.Seconds())
	}
	// rec[0] 实测: speed raw 8330 → 29.988 km/h; distance raw 40581 → 405.81 m
	if math.Abs(stream.SpeedKph[0]-8330.0/1000*3.6) > 1e-6 {
		t.Errorf("期望首点速度 %v, 得到 %v", 8330.0/1000*3.6, stream.SpeedKph[0])
	}
	if math.Abs(stream.DistanceM[0]-405.81) > 1e-6 {
		t.Errorf("期望首点距离 405.81, 得到 %v", stream.DistanceM[0])
	}
	if stream.HeartRate[0] != 110 {
		t.Errorf("期望首点心率 110, 得到 %v", stream.HeartRate[0])
	}

	// BLOB 往返（票面验收：读取可还原）
	b, err := stream.Marshal()
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	restored, err := core.UnmarshalStream(b)
	if err != nil {
		t.Fatalf("UnmarshalStream 失败: %v", err)
	}
	if len(restored.SpeedKph) != len(stream.SpeedKph) || restored.SpeedKph[0] != stream.SpeedKph[0] {
		t.Fatal("BLOB 往返不一致")
	}
}

func TestParseFITSessionInvalidFields(t *testing.T) {
	// real_sup.fit（原 poolswim 样本）：实际运动为 SUP，Session 的距离/爬升是 invalid 哨兵
	f, err := os.Open("testdata/real_sup.fit")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	activity, stream, err := ParseFIT(f)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if activity.Distance != 0 {
		t.Errorf("invalid 总距离应归 0, 得到 %v", activity.Distance)
	}
	if activity.ElevationGain != 0 {
		t.Errorf("invalid 爬升应归 0, 得到 %v", activity.ElevationGain)
	}
	if activity.Duration != 3601*time.Second {
		t.Errorf("期望 3601s, 得到 %v", activity.Duration)
	}
	if stream.Seconds() == 0 {
		t.Error("期望非空 stream")
	}
}

func TestParseFITRejects(t *testing.T) {
	t.Run("损坏文件给出可读错误", func(t *testing.T) {
		f, err := os.Open("testdata/corrupt.fit")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, _, err = ParseFIT(f); err == nil {
			t.Fatal("期望报错")
		}
	})

	t.Run("仅含 FileId 的文件报错", func(t *testing.T) {
		f, err := os.Open("testdata/fileid_only.fit")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		_, _, err = ParseFIT(f)
		if err == nil {
			t.Fatal("期望报错（无 Session/Record）")
		}
	})

	t.Run("空输入报错", func(t *testing.T) {
		if _, _, err := ParseFIT(strings.NewReader("")); err == nil {
			t.Fatal("期望报错")
		}
	})
}
