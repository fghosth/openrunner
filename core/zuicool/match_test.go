package zuicool

import "testing"

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"2026舟山群岛马拉松":                     "舟山群岛马拉松",
		"2026年长三角定向公开赛（无锡江阴站）":            "长三角定向公开赛无锡江阴站",
		"Biobor贝欧宝·2026“丽人18”女子10KM（深圳站）": "biobor贝欧宝丽人18女子10km深圳站",
		"WSE 10KM OPEN 桂林站":               "wse10kmopen桂林站",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// 赞助商前缀是最常见的差异：田协那边是干净名，最酷这边冠了名。
func TestMatchList_sponsorPrefix(t *testing.T) {
	entries := []ListEntry{{ID: "1", Title: "广汽传祺2026玉林马拉松", Date: "2026-12-27"}}
	targets := []MatchTarget{{RaceID: "r1", Name: "2026玉林马拉松", Date: "2026-12-27"}}
	got := MatchList(entries, targets, 0.75)
	if got["1"] != "r1" {
		t.Fatalf("带赞助商前缀没配上：%v", got)
	}
}

// 比赛日不同就绝不能配 —— 同名不同日多半是两场不同的比赛。
func TestMatchList_requiresSameDate(t *testing.T) {
	entries := []ListEntry{{ID: "1", Title: "2026海口马拉松", Date: "2026-12-27"}}
	targets := []MatchTarget{{RaceID: "r1", Name: "2026海口马拉松", Date: "2026-12-26"}}
	if got := MatchList(entries, targets, 0.75); len(got) != 0 {
		t.Fatalf("日期不同却配上了：%v", got)
	}
}

// 错配比漏配严重：短标题不能靠「都含马拉松」互相认领。
func TestMatchList_rejectsWeakSimilarity(t *testing.T) {
	entries := []ListEntry{{ID: "1", Title: "2026黄埔马拉松", Date: "2026-12-27"}}
	targets := []MatchTarget{{RaceID: "r1", Name: "2026海口马拉松", Date: "2026-12-27"}}
	if got := MatchList(entries, targets, 0.75); len(got) != 0 {
		t.Fatalf("两个不同的赛事被误配：%v", got)
	}
}

// 两张卡片抢同一个目标时，相似度高的赢（先到先得是错的）。
func TestMatchList_bestScoreWins(t *testing.T) {
	entries := []ListEntry{
		{ID: "weak", Title: "2026玉林马拉松赛", Date: "2026-12-27"},
		{ID: "exact", Title: "2026玉林马拉松", Date: "2026-12-27"},
	}
	targets := []MatchTarget{{RaceID: "r1", Name: "2026玉林马拉松", Date: "2026-12-27"}}
	got := MatchList(entries, targets, 0.7)
	if got["exact"] != "r1" {
		t.Fatalf("精确标题没拿到目标：%v", got)
	}
	if _, dup := got["weak"]; dup {
		t.Fatalf("一个目标被配了两次：%v", got)
	}
}

// similarity 自己的单测 —— 之前只被 MatchList 间接覆盖，
// 阈值一改就看不出是打分错了还是阈值卡错了。
func TestSimilarity(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want float64
	}{
		{"", "x", 0},
		{"玉林马拉松", "玉林马拉松", 1},
		{"马拉松", "马拉松", 1},
		// 包含关系走长度接近度：完全同名 1.0，带后缀略低
		{"广汽传祺玉林马拉松", "玉林马拉松", 0.9 + 0.1*5.0/9.0},
		{"玉林马拉松赛", "玉林马拉松", 0.9 + 0.1*5.0/6.0},
		// 公共词太短（<4 字）不该触发包含分支
		{"马拉松", "全程马拉松", 2 * 3.0 / (3 + 5)},
	} {
		got := similarity(Normalize(c.a), Normalize(c.b))
		if diff := got - c.want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("similarity(%q,%q)=%v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// 阈值实测：真实抓取语料上的假阳/假阴清单（2026-11-18 最酷目录 × 田协赛历）。
//
// AGENTS.md 的金标准纪律要求「偏离要量出数字」，0.75 不能是拍脑袋。
// 正例是当次真实配上并逐条人工核对过的；反例是同日、名字像但不是同一场的情形。
func TestMatchThreshold_measuredOnRealCorpus(t *testing.T) {
	const threshold = 0.75
	pos := [][2]string{
		{"2026舟山群岛马拉松", "2026舟山群岛马拉松"},
		{"2026上海马拉松", "2026上海马拉松"},
		{"2026深圳马拉松", "2026深圳马拉松"},
		{"2026宁德马拉松", "2026宁德马拉松"},
		{"2026玉林马拉松", "广汽传祺2026玉林马拉松"},
		{"2026中国田径协会10公里精英赛（蚌埠站）", "中国田径协会10公里精英赛蚌埠站"},
		{"“跑遍四川”总决赛·2026年雅安雨城半程马拉松", "跑遍四川总决赛2026年雅安雨城半程马拉松"},
	}
	neg := [][2]string{
		{"2026黄埔马拉松", "2026海口马拉松"},
		{"2026云浮新兴半程马拉松", "2026汾阳市贾家庄半程马拉松"},
		{"2026上海马拉松", "2026上海女子半程马拉松"},
		{"2026深圳马拉松", "2026深圳大G大利跑山赛"},
	}
	worstPos := 1.0
	for _, p := range pos {
		s := similarity(Normalize(p[0]), Normalize(p[1]))
		if s < threshold {
			t.Errorf("漏配（假阴）：%-28s ↔ %s = %.3f < %.2f", p[0], p[1], s, threshold)
		}
		if s < worstPos {
			worstPos = s
		}
	}
	bestNeg := 0.0
	for _, p := range neg {
		s := similarity(Normalize(p[0]), Normalize(p[1]))
		if s >= threshold {
			t.Errorf("误配（假阳）：%-28s ↔ %s = %.3f ≥ %.2f", p[0], p[1], s, threshold)
		}
		if s > bestNeg {
			bestNeg = s
		}
	}
	t.Logf("阈值 %.2f 实测：假阴最差 %.3f（余量 %.3f）· 假阳最好 %.3f（余量 %.3f）",
		threshold, worstPos, worstPos-threshold, bestNeg, threshold-bestNeg)
	if worstPos <= threshold {
		t.Errorf("正例贴着阈值没有余量（最差 %.3f），上游加个前缀就会漏", worstPos)
	}
}
