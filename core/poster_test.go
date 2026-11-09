package core

import (
	"strings"
	"testing"
)

// SVG 年度海报（票 14）：纯函数生成可下载的 SVG 文档。

func sampleStat() AnnualStat {
	return AnnualStat{
		Year:              2026,
		TotalDistanceM:    1248500, // 1248.5 km
		TotalDurationSecs: 372000,  // 103.3 h
		ActivityCount:     142,
		ActiveDays:        118,
		Monthly: []MonthStat{
			{Month: 1, DistanceM: 120000, Count: 12}, {Month: 2, DistanceM: 90000, Count: 9},
			{Month: 3, DistanceM: 150000, Count: 15}, {Month: 4, DistanceM: 180000, Count: 16},
			{Month: 5, DistanceM: 200000, Count: 18}, {Month: 6, DistanceM: 160000, Count: 15},
			{Month: 7, DistanceM: 140000, Count: 14}, {Month: 8, DistanceM: 100000, Count: 10},
			{Month: 9, DistanceM: 60000, Count: 8}, {Month: 10, DistanceM: 48500, Count: 6},
			{Month: 11, DistanceM: 0, Count: 0}, {Month: 12, DistanceM: 0, Count: 0},
		},
	}
}

func TestRenderAnnualPoster(t *testing.T) {
	svg := RenderAnnualPoster(sampleStat(), PosterOptions{
		AthleteName: "我-公路",
		Accent:      "#3b82f6",
	})

	t.Run("是合法 SVG 文档骨架", func(t *testing.T) {
		if !strings.HasPrefix(strings.TrimSpace(svg), "<?xml") && !strings.HasPrefix(strings.TrimSpace(svg), "<svg") {
			t.Fatalf("期望 SVG 文档, 得到前缀 %q", svg[:40])
		}
		for _, want := range []string{`<svg`, `xmlns="http://www.w3.org/2000/svg"`, `</svg>`} {
			if !strings.Contains(svg, want) {
				t.Errorf("缺少 %q", want)
			}
		}
		if !strings.Contains(svg, `viewBox=`) {
			t.Error("期望 viewBox（可缩放）")
		}
	})

	t.Run("核心数据出现在海报上", func(t *testing.T) {
		for _, want := range []string{"2026", "1248.5", "142", "118"} {
			if !strings.Contains(svg, want) {
				t.Errorf("缺少数据 %q", want)
			}
		}
	})

	t.Run("中文可渲染（字体族 + 文本未被转义破坏）", func(t *testing.T) {
		if !strings.Contains(svg, "我-公路") {
			t.Error("期望档案名出现在海报")
		}
		if !strings.Contains(svg, "Noto Sans SC") && !strings.Contains(svg, "PingFang") {
			t.Error("期望声明中文字体族")
		}
	})

	t.Run("12 个月柱全存在（含空月）", func(t *testing.T) {
		if n := strings.Count(svg, `class="bar"`); n != 12 {
			t.Fatalf("期望 12 个柱, 得到 %d", n)
		}
	})

	t.Run("强调色注入", func(t *testing.T) {
		if !strings.Contains(svg, "#3b82f6") {
			t.Error("期望强调色写入 SVG")
		}
	})

	t.Run("空统计不 panic", func(t *testing.T) {
		svg := RenderAnnualPoster(AnnualStat{Year: 2026}, PosterOptions{})
		if !strings.Contains(svg, "</svg>") {
			t.Fatal("空统计也应产出完整 SVG")
		}
	})

	t.Run("XML 特殊字符被转义", func(t *testing.T) {
		svg := RenderAnnualPoster(sampleStat(), PosterOptions{AthleteName: `A&B <test>`})
		if strings.Contains(svg, "<test>") {
			t.Fatal("原始 < > 未被转义，会破坏 XML")
		}
		if !strings.Contains(svg, "A&amp;B") {
			t.Error("期望 & 转义为 &amp;")
		}
	})
}
