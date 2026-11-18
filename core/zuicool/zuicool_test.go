package zuicool

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("读 fixture %s: %v", name, err)
	}
	return string(b)
}

// 真实列表页裁剪片段。断言写死具体值而不是「非空」——
// 解析器最容易出的错是「抓到了但抓错字段」，非空断言拦不住。
func TestParseList_realFixture(t *testing.T) {
	entries := ParseList(fixture(t, "list_newreg.html"))
	if len(entries) != 3 {
		t.Fatalf("解析到 %d 张卡片，应为 3：%+v", len(entries), entries)
	}
	want := []ListEntry{
		{ID: "66020", Title: "2027安踏冠军大连100越野赛", Date: "2027-05-22", RegKind: "start", RegAt: "2026-11-22T18:00:00", Status: StatusSoon},
		{ID: "16055", Title: "2026年长三角定向公开赛（无锡江阴站）", Date: "2026-12-05", RegKind: "end", RegAt: "2026-11-26T23:59:00", Status: StatusOpen},
		{ID: "35270", Title: "WSE 10KM OPEN 桂林站", Date: "2027-03-14", RegKind: "end", RegAt: "2027-03-01T23:59:00", Status: StatusOpen},
	}
	for i, w := range want {
		got := entries[i]
		if got.ID != w.ID || got.Title != w.Title || got.Date != w.Date ||
			got.RegKind != w.RegKind || got.RegAt != w.RegAt || got.Status != w.Status {
			t.Errorf("卡片 %d\n  got %+v\n want %+v", i, got, w)
		}
	}
}

// 死模板回归：列表页注释里躺着 100 条一模一样的「报名时间：2018-08-06~2018-08-16」。
// 谁哪天图省事改成「从整页文本里抓报名时间」，这条就会红。
func TestParseList_ignoresCommentedOutTemplate(t *testing.T) {
	for _, c := range ParseList(fixture(t, "list_newreg.html")) {
		if strings.Contains(c.RegAt, "2018") {
			t.Errorf("抓到了注释里的死模板：%+v", c)
		}
	}
	if !strings.Contains(fixture(t, "list_newreg.html"), "2018-08-06") {
		t.Fatal("fixture 里必须保留那段注释，否则这条测试没有意义")
	}
}

// 上游只给「月-日」，年份靠比赛日推；跨年的窗口必须退一年。
func TestResolveYear_crossesYearBackwards(t *testing.T) {
	race := time.Date(2027, 1, 2, 7, 0, 0, 0, time.Local)
	// 1 月的比赛，12 月截止 → 上一年的 12 月
	if got := resolveYear("12", "20", "18", "00", race); got != "2026-12-20T18:00:00" {
		t.Errorf("跨年未退一年：%s", got)
	}
	// 同年的正常截止
	if got := resolveYear("11", "26", "23", "59", race); got != "2026-11-26T23:59:00" {
		t.Errorf("同年窗口算错：%s", got)
	}
	// 早于比赛日的窗口不该退年
	// 线上赛的截止晚于比赛日 10 天，是真实形态，不能被退到前一年
	// （实测「2026 跑遍中国线上系列赛-上海站」比赛 11-19、报名截止 11-29）
	virtual := time.Date(2026, 11, 19, 0, 0, 0, 0, time.Local)
	if got := resolveYear("11", "29", "23", "59", virtual); got != "2026-11-29T23:59:00" {
		t.Errorf("线上赛的正常截止被退了一年：%s", got)
	}
	same := time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local)
	if got := resolveYear("10", "15", "00", "00", same); got != "2026-10-15T00:00:00" {
		t.Errorf("不该退年却退了：%s", got)
	}
}

func TestParseDetail_realFixture(t *testing.T) {
	ev := ParseDetail(fixture(t, "detail_84480.html"))
	if ev.ID != "84480" || ev.Title != "2026舟山群岛马拉松" {
		t.Fatalf("标题/ID 解析错：%+v", ev)
	}
	if ev.RegStart != "2026-10-28T10:00:00" || ev.RegEnd != "2026-11-18T12:00:00" {
		t.Errorf("报名窗口解析错：start=%s end=%s", ev.RegStart, ev.RegEnd)
	}
	if len(ev.Packages) != 4 {
		t.Fatalf("组别数 %d，应为 4：%+v", len(ev.Packages), ev.Packages)
	}
	type pk struct {
		name  string
		price float64
		open  bool
	}
	want := []pk{
		{"马拉松（42.195公里）", 170, true},
		{"半程马拉松（21.0975公里）", 150, false}, // 已报满
		{"马拉松（42.195公里）+领物日班车", 210, true},
		{"半程马拉松（21.0975公里）+领物日班车", 190, false},
	}
	for i, w := range want {
		g := ev.Packages[i]
		if g.Name != w.name || g.Price != w.price || g.Open != w.open {
			t.Errorf("组别 %d\n  got %+v\n want %+v", i, g, w)
		}
	}
	if ev.MinPrice != 150 {
		t.Errorf("起价 %v，应为 150", ev.MinPrice)
	}
	if !strings.HasPrefix(ev.Packages[0].RegURL, "https://reg.zuicool.com/34604") {
		t.Errorf("报名链接解析错：%s", ev.Packages[0].RegURL)
	}
}

// 解析到 0 条必须报错而不是返回空 —— 空结果在 UI 上和「没有比赛」长得一样。
func TestListPage_zeroCardsIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "events") {
			// 有 event-body 但没有可解析的卡片：markup 变了的形态
			_, _ = w.Write([]byte(`<html><div class="event-body">changed</div></html>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL}
	if _, err := c.ListPage(context.Background(), 1); !errors.Is(err, ErrNoCards) {
		t.Fatalf("应报 ErrNoCards，实际 %v", err)
	}
}

func TestListPage_blocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body>安全验证</body></html>`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL}
	if _, err := c.ListPage(context.Background(), 1); !errors.Is(err, ErrBlocked) {
		t.Fatalf("应报 ErrBlocked，实际 %v", err)
	}
}

// 目录按日期倒序，翻到「整页都比今天早」就该停，不能把 3000 条历史全拉回来。
func TestList_stopsAtPastDates(t *testing.T) {
	// 每页一张卡片，日期逐页往过去走（真实目录就是按比赛日倒序）
	page := func(n int) string {
		d := time.Date(2026, 11, 20, 0, 0, 0, 0, time.Local).AddDate(0, 0, -20*(n-1))
		return `<div class="event-body"><a href="https://zuicool.com/event/` +
			strconv.Itoa(n) + `" class="event-a">赛 ` + d.Format("2006.01.02") +
			`</a><span>报名截止：11-26 23:59</span></div>`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			n = 1
		}
		_, _ = w.Write([]byte(`<html>` + page(n) + `</html>`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Now: func() time.Time { return time.Date(2026, 11, 18, 9, 0, 0, 0, time.Local) }}
	got, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 3 {
		t.Fatalf("翻了 %d 页还没停，日期倒序的早停没生效", len(got))
	}
	if len(got) == 0 {
		t.Fatal("一页都没解析到")
	}
	for _, cd := range got {
		if cd.Date < "2026-11-18" {
			t.Errorf("收进了过去的比赛：%+v", cd)
		}
	}
}

// serveFixture 用 httptest 把 fixture 当真实页面发出去，好覆盖 Client.Detail
// 的错误分支（ParseDetail 本身只返回结构，判断在 Detail 里）。
func serveFixture(t *testing.T, name string) *Client {
	t.Helper()
	body := fixture(t, name)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL}
}

// 最酷上没挂组别的赛事（走预报名+抽签的那批）是**正常数据形态**，
// 不是解析器坏了。报 ErrNoCards（"最酷可能改了 markup"）会让每轮刷新
// 刷出 20 条假警报，把人引去查一个不存在的 markup 变更。
func TestDetailNoPackages(t *testing.T) {
	c := serveFixture(t, "detail_68782_nopackages.html")
	ev, err := c.Detail(context.Background(), "68782")
	if !errors.Is(err, ErrNoPackages) {
		t.Fatalf("应报 ErrNoPackages，实际 %v", err)
	}
	if errors.Is(err, ErrNoCards) {
		t.Fatalf("不该报 ErrNoCards（那等于说 markup 变了）：%v", err)
	}
	if ev != nil {
		t.Fatalf("出错时不该返回半截结果，实际 %+v", ev)
	}
}

// 详情页里混着**相关赛事**的卡片，每张自带「报名截止」。
// 报名窗口只能来自本页的 meta description；从正文扫日期会把别人的
// 截止日期配给本场——那是编造数据，比缺失更糟。
func TestDetailRelatedCardsDoNotLeak(t *testing.T) {
	c := serveFixture(t, "detail_68782_nopackages.html")
	if _, err := c.Detail(context.Background(), "68782"); !errors.Is(err, ErrNoPackages) {
		t.Fatalf("相关卡片不该被当成本场的组别解析：%v", err)
	}
	ev := ParseDetail(fixture(t, "detail_68782_nopackages.html"))
	if len(ev.Packages) != 0 {
		t.Fatalf("相关赛事卡片漏进本场组别了：%+v", ev.Packages)
	}
	if ev.RegStart != "" || ev.RegEnd != "" {
		t.Fatalf("相关赛事的报名截止漏进本场窗口了：%q ~ %q", ev.RegStart, ev.RegEnd)
	}
	if ev.ID != "68782" {
		t.Fatalf("标题应照常解析出本页 ID，实际 %q", ev.ID)
	}
}
