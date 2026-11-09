package core

import (
	"fmt"
	"html"
	"strings"
)

// SVG 年度海报（票 14）：纯函数生成可下载/可分享的 SVG 文档。
// 设计沿用 ui-ux-pro-max 的 Data-Dense Dashboard 基调（深底 + 蓝数据 + 琥珀强调）。

// PosterOptions 海报外观参数。
type PosterOptions struct {
	AthleteName string
	Accent      string // 主强调色；空则用默认蓝
}

const (
	posterWidth  = 800
	posterHeight = 600
	posterBg     = "#0f172a"
	posterCard   = "#111827"
	posterFg     = "#f8fafc"
	posterMuted  = "#cbd5e1"
	posterBorder = "#334155"
	posterFont   = `Fira Sans, Noto Sans SC, PingFang SC, -apple-system, sans-serif`
)

// RenderAnnualPoster 生成年度海报 SVG。
func RenderAnnualPoster(stat AnnualStat, opts PosterOptions) string {
	accent := opts.Accent
	if accent == "" {
		accent = "#3b82f6"
	}
	// 柱高归一化（空年份避免除零）
	maxDist := 1.0
	for _, m := range stat.Monthly {
		if m.DistanceM > maxDist {
			maxDist = m.DistanceM
		}
	}

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="%s 年度运动海报">`+"\n",
		posterWidth, posterHeight, posterWidth, posterHeight, esc(fmt.Sprintf("%d", stat.Year)))

	// 背景 + 卡片
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="%s"/>`+"\n", posterWidth, posterHeight, posterBg)
	fmt.Fprintf(&b, `<rect x="24" y="24" width="%d" height="%d" rx="16" fill="%s" stroke="%s"/>`+"\n",
		posterWidth-48, posterHeight-48, posterCard, posterBorder)

	// 标题区
	fmt.Fprintf(&b, `<text x="56" y="86" font-family="%s" font-size="30" font-weight="600" fill="%s" letter-spacing="1">%d 年度回顾</text>`+"\n",
		posterFont, posterFg, stat.Year)
	if opts.AthleteName != "" {
		fmt.Fprintf(&b, `<text x="56" y="118" font-family="%s" font-size="16" fill="%s">%s</text>`+"\n",
			posterFont, posterMuted, esc(opts.AthleteName))
	}

	// 四个 KPI
	kpis := []struct {
		label, value, unit string
	}{
		{"总距离", fmt.Sprintf("%.1f", stat.TotalDistanceM/1000), "km"},
		{"活动次数", fmt.Sprintf("%d", stat.ActivityCount), "次"},
		{"活跃天数", fmt.Sprintf("%d", stat.ActiveDays), "天"},
		{"总时长", fmt.Sprintf("%.0f", float64(stat.TotalDurationSecs)/3600), "小时"},
	}
	kx, ky, kw := 56.0, 150.0, 168.0
	for i, kpi := range kpis {
		x := kx + float64(i)*(kw+14)
		fmt.Fprintf(&b, `<rect x="%.0f" y="%.0f" width="%.0f" height="86" rx="10" fill="%s" stroke="%s"/>`+"\n",
			x, ky, kw, posterBg, posterBorder)
		fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" font-family="%s" font-size="13" fill="%s">%s</text>`+"\n",
			x+16, ky+26, posterFont, posterMuted, esc(kpi.label))
		fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" font-family="%s" font-size="28" font-weight="700" fill="%s">%s</text>`+"\n",
			x+16, ky+64, posterFont, posterFg, esc(kpi.value))
		fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" font-family="%s" font-size="12" fill="%s">%s</text>`+"\n",
			x+16+float64(len(kpi.value))*17, ky+64, posterFont, posterMuted, esc(kpi.unit))
	}

	// 月度柱状图
	const chartTop, chartH = 292.0, 190.0
	chartLeft, chartW := 56.0, 688.0
	fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" font-family="%s" font-size="13" fill="%s">月度距离（km）</text>`+"\n",
		chartLeft, chartTop-14, posterFont, posterMuted)
	// 基线
	fmt.Fprintf(&b, `<line x1="%.0f" y1="%.0f" x2="%.0f" y2="%.0f" stroke="%s"/>`+"\n",
		chartLeft, chartTop+chartH, chartLeft+chartW, chartTop+chartH, posterBorder)

	barW := chartW/12 - 8
	for i := 0; i < 12; i++ {
		m := MonthStat{Month: i + 1}
		if i < len(stat.Monthly) {
			m = stat.Monthly[i]
		}
		h := (m.DistanceM / maxDist) * chartH
		if m.DistanceM > 0 && h < 3 {
			h = 3 // 极小值也可见
		}
		x := chartLeft + float64(i)*(chartW/12) + 4
		y := chartTop + chartH - h
		fmt.Fprintf(&b, `<rect class="bar" x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="3" fill="%s" opacity="0.85"/>`+"\n",
			x, y, barW, h, accent)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-family="%s" font-size="10" fill="%s" text-anchor="middle">%d</text>`+"\n",
			x+barW/2, chartTop+chartH+16, posterFont, posterMuted, m.Month)
	}

	// 页脚
	fmt.Fprintf(&b, `<text x="56" y="%d" font-family="%s" font-size="12" fill="%s">Openrunner · 自托管训练分析</text>`+"\n",
		posterHeight-40, posterFont, posterMuted)

	b.WriteString(`</svg>` + "\n")
	return b.String()
}

// esc XML 文本转义（& < > " '）。
func esc(s string) string { return html.EscapeString(s) }
