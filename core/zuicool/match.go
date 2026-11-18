package zuicool

// 把最酷的赛事标题对到田协赛历的赛事上。
//
// 两边的名字**不是一套口径**：最酷带赞助商前缀（「Biobor贝欧宝·2026"丽人18"女子10KM」）、
// 年份写法不一（「2026年长三角…」/「2026舟山…」/年份在中间），田协那边则是干净名。
// 所以只能模糊匹配，而**错配的代价是给用户看错报名截止和价格** —— 比漏配严重。
// 因此这里的默认阈值偏保守（0.75），并且**比赛日必须完全相同**。
//
// 宁可漏：漏了前端会退到「去官方渠道报名」的复制入口，不会给出错误数字。

import (
	"sort"
	"strings"
)

// MatchTarget 是待匹配目标（田协赛历里的一场比赛）。
type MatchTarget struct {
	RaceID string
	Name   string
	Date   string // YYYY-MM-DD
}

// Normalize 归一标题：去年份、去空白与标点、拉丁字母转小写。
//
// 只保留汉字、字母和数字 —— 赞助商前缀里的「·」「（）」和全半角差异都在这里被抹平。
func Normalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
		case r >= 0x4e00 && r <= 0x9fff:
			b.WriteRune(r)
		default:
			// 空白、标点、全角符号、emoji 一律丢掉
		}
	}
	out := b.String()
	// 年份单独剥：两边的位置不一样（开头 / 中间都有）
	for _, y := range []string{"2016", "2017", "2018", "2019", "2020", "2021", "2022", "2023", "2024", "2025", "2026", "2027", "2028", "2029"} {
		out = strings.ReplaceAll(out, y, "")
	}
	return strings.ReplaceAll(out, "年", "")
}

// similarity 返回 [0,1]：最长公共子序列占两边平均长度的比例。
//
// 用子序列而不是子串，是因为赞助商前缀会把公共部分切成几段（「2026玉林马拉松」
// 对「广汽传祺2026玉林马拉松」没有连续公共段也能对上）。
func similarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	ra, rb := []rune(a), []rune(b)
	// 一方完整包含另一方 → 高分。赞助商前缀会把纯 LCS 占比拉低
	// 而包含本身就是最强的同一性信号，但仍按**长度接近度**打分，让完全同名赢过
	// 「玉林马拉松」vs「玉林马拉松赛」这种带后缀的近似名。下限 4 **字**
	// 是为了排除「马拉松」这种公共词互相认领。
	if len(ra) >= 4 && len(rb) >= 4 && (strings.Contains(a, b) || strings.Contains(b, a)) {
		lo, hi := len(ra), len(rb)
		if lo > hi {
			lo, hi = hi, lo
		}
		return 0.9 + 0.1*float64(lo)/float64(hi)
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			if ra[i-1] == rb[j-1] {
				cur[j] = prev[j-1] + 1
			} else if prev[j] >= cur[j-1] {
				cur[j] = prev[j]
			} else {
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, make([]int, len(rb)+1)
	}
	lcs := prev[len(rb)]
	return 2 * float64(lcs) / float64(len(ra)+len(rb))
}

// MatchList 把卡片对到目标赛事，返回 entry.ID → RaceID。
//
// 一张卡片最多配一个目标（取相似度最高者）；同一个目标被多张卡片抢时，
// 相似度高的那张赢 —— 所以先按 (日期) 分桶再逐张挑，避免 O(n·m) 全对。
func MatchList(entries []ListEntry, targets []MatchTarget, minRatio float64) map[string]string {
	byDate := make(map[string][]MatchTarget, 64)
	for _, t := range targets {
		byDate[t.Date] = append(byDate[t.Date], t)
	}
	// 先算出每张卡片的最佳候选，再按相似度降序认领目标，
	// 否则先来的低分卡片会把唯一的目标占走。
	type pick struct {
		cardID, raceID string
		score          float64
	}
	picks := make([]pick, 0, len(entries))
	for _, c := range entries {
		norm := Normalize(c.Title)
		if norm == "" {
			continue
		}
		best := pick{}
		for _, t := range byDate[c.Date] {
			s := similarity(norm, Normalize(t.Name))
			if s >= minRatio && s > best.score {
				best = pick{cardID: c.ID, raceID: t.RaceID, score: s}
			}
		}
		if best.raceID != "" {
			picks = append(picks, best)
		}
	}
	sort.SliceStable(picks, func(i, j int) bool { return picks[i].score > picks[j].score })
	out := make(map[string]string, len(picks))
	taken := make(map[string]bool, len(picks))
	for _, p := range picks {
		if taken[p.raceID] {
			continue
		}
		taken[p.raceID] = true
		out[p.cardID] = p.raceID
	}
	return out
}
