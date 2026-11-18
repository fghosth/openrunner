package races

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCategories(t *testing.T) {
	cases := []struct {
		in   []string
		want string // 逗号分隔，空串表示应被丢弃
	}{
		{[]string{"全程", "半程"}, "full,half"},
		{[]string{"半程"}, "half"},
		{[]string{"全程"}, "full"},
		{[]string{"10公里"}, "10k"},
		{[]string{"10公里", "5公里"}, "10k"},
		{[]string{"全程、半程、其他"}, "full,half"},
		{[]string{"半程马拉松"}, "half"},
		{[]string{"健康跑（10公里）"}, "10k"},
		{[]string{"十公里"}, "10k"},
		{[]string{"42.195公里"}, "full"},
		{[]string{"21.0975公里"}, "half"},
		// 只含这些的赛事不是「马拉松比赛」，必须丢
		{[]string{"5公里"}, ""},
		{[]string{"迷你"}, ""},
		{[]string{"亲子跑"}, ""},
		{[]string{"徒步"}, ""},
		// 裸「马拉松」不能判成全马：词表里有「迷你马拉松」「环岛马拉松31公里」
		{[]string{"迷你马拉松"}, ""},
		{[]string{"环岛马拉松31公里"}, ""},
		{[]string{"10英里"}, ""},
		{[]string{"100公里"}, ""},
		{nil, ""},
	}
	for _, tc := range cases {
		got := JoinCategories(Categories(tc.in))
		if got != tc.want {
			t.Errorf("Categories(%v) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestSplitAddress(t *testing.T) {
	cases := []struct {
		in      string
		p, c, d string
	}{
		{"广东省/梅州市/", "广东省", "梅州市", ""},
		{"广东省/梅州市/梅江区", "广东省", "梅州市", "梅江区"},
		{"重庆市/重庆市/巴南区", "重庆市", "重庆市", "巴南区"},
		{"  江苏省/南京市/玄武区 ", "江苏省", "南京市", "玄武区"},
		{"北京市", "北京市", "", ""},
		{"", "", "", ""},
		{"广东省//天河区", "广东省", "", "天河区"},
	}
	for _, tc := range cases {
		p, c, d := SplitAddress(tc.in)
		if p != tc.p || c != tc.c || d != tc.d {
			t.Errorf("SplitAddress(%q) = (%q,%q,%q)，期望 (%q,%q,%q)", tc.in, p, c, d, tc.p, tc.c, tc.d)
		}
	}
}

func TestParseItemsToleratesDirtyJSON(t *testing.T) {
	// 上游正常给 JSON 字符串数组，但词表里出现过裸串/全角分隔
	if got := parseItems(`["全程","半程"]`); len(got) != 2 {
		t.Errorf("正常 JSON 数组解错: %v", got)
	}
	if got := parseItems(`全程、半程`); len(got) != 2 {
		t.Errorf("全角顿号裸串应能切开: %v", got)
	}
	if got := parseItems(``); got != nil {
		t.Errorf("空串应为 nil: %v", got)
	}
	// 认不出的整串交给匹配器，而不是丢掉
	if got := parseItems(`全程接力`); len(got) != 1 {
		t.Errorf("裸串应原样返回一个 token: %v", got)
	}
}

func TestNormalizeDropsUnusableRows(t *testing.T) {
	cases := []struct {
		name string
		raw  rawRace
	}{
		{"无 id", rawRace{RaceTime: "2026-01-01", RaceAddress: "广东省/广州市/", RaceItem: `["全程"]`}},
		{"日期非法", rawRace{RaceID: jsonNum("1"), RaceTime: "待定", RaceAddress: "广东省/广州市/", RaceItem: `["全程"]`}},
		{"无地址", rawRace{RaceID: jsonNum("1"), RaceTime: "2026-01-01", RaceItem: `["全程"]`}},
		{"不含保留项目", rawRace{RaceID: jsonNum("1"), RaceTime: "2026-01-01", RaceAddress: "广东省/广州市/", RaceItem: `["5公里"]`}},
	}
	for _, tc := range cases {
		if _, ok := normalize(tc.raw); ok {
			t.Errorf("%s：应被丢弃", tc.name)
		}
	}
	r, ok := normalize(rawRace{
		RaceID: jsonNum("1000503000"), RaceName: "2026梅州马拉松", RaceGrade: "A",
		RaceTime: "2026-12-27", RaceAddress: "广东省/梅州市/", RaceItem: `["全程","半程"]`,
	})
	if !ok {
		t.Fatal("正常记录应保留")
	}
	if r.ID != "1000503000" || r.Province != "广东省" || r.City != "梅州市" || !r.Has(CategoryHalf) {
		t.Errorf("归一结果不对: %+v", r)
	}
	if r.Year() != 2026 {
		t.Errorf("年份应为 2026，实际 %d", r.Year())
	}
}

// --- ListAll ---

// raceRow 拼一条上游记录。
func raceRow(id, name, date, addr, item string) string {
	return fmt.Sprintf(`{"raceId":%s,"raceName":%q,"raceGrade":"A","raceTime":%q,`+
		`"raceAddress":%q,"raceItem":%q,"raceScale":null}`, id, name, date, addr, item)
}

func jsonNum(s string) json.Number { return json.Number(s) }
func TestListAllPaginates(t *testing.T) {
	var seen []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		pageNo := int(req["pageNo"].(float64))
		seen = append(seen, pageNo)
		if req["pageSize"].(float64) != float64(defaultPageSize) {
			// 断言常量而不是字面量：默认值会因上游调整分页上限而变
			// （2026-11-18 上游对 500 稳定返回 HTTP 500，见 defaultPageSize 注释），
			// 写死数字的话每次调默认值都得来改这里。
			t.Errorf("pageSize 应为 %d，实际 %v", defaultPageSize, req["pageSize"])
		}
		switch pageNo {
		case 1:
			fmt.Fprintf(w, `{"success":true,"code":0,"data":{"pageNo":1,"pageCount":2,"totalCount":3,"results":[%s,%s]}}`,
				raceRow("1", "A马", "2026-03-01", "广东省/广州市/", `["全程"]`),
				raceRow("2", "B马", "2026-01-01", "江苏省/南京市/", `["半程"]`))
		case 2:
			fmt.Fprintf(w, `{"success":true,"code":0,"data":{"pageNo":2,"pageCount":2,"totalCount":3,"results":[%s,%s]}}`,
				raceRow("3", "C马", "2026-05-01", "重庆市/重庆市/巴南区", `["10公里"]`),
				raceRow("4", "只有5公里", "2026-06-01", "四川省/成都市/", `["5公里"]`))
		default:
			t.Errorf("不该翻到第 %d 页", pageNo)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	got, err := c.ListAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Errorf("应只翻 2 页，实际 %v", seen)
	}
	if len(got) != 3 {
		t.Fatalf("应保留 3 条（5 公里那条被丢），实际 %d", len(got))
	}
	// 按日期升序
	if got[0].Date != "2026-01-01" || got[2].Date != "2026-05-01" {
		t.Errorf("排序不对: %s .. %s", got[0].Date, got[2].Date)
	}
	if got[2].District != "巴南区" {
		t.Errorf("直辖市区划没切开: %+v", got[2])
	}
}
func TestListAllRetriesTransportGlitch(t *testing.T) {
	// 上游偶发挂起/断连（实测过），这类抖动重试一次就该恢复；
	// 不重试的话整轮更新失败，地图要空一整周。
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			// 模拟连接被掐断
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		io.WriteString(w, `{"success":true,"data":{"pageNo":1,"pageCount":1,"results":[`+
			raceRow("1", "A马", "2026-03-01", "广东省/广州市/", `["全程"]`)+`]}}`)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	got, err := c.ListAll(context.Background())
	if err != nil {
		t.Fatalf("抖动后应重试成功，实际失败: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("应拿到 1 条，实际 %d", len(got))
	}
	if hits != 2 {
		t.Errorf("应重试一次（共 2 次请求），实际 %d", hits)
	}
}

func TestListAllKeepsCurrentYearOnward(t *testing.T) {
	// 只留当年及以后：历史赛历对「找下一场比赛」没用，却占了上游九成数据。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"success":true,"data":{"pageNo":1,"pageCount":1,"results":[`+
			raceRow("1", "去年的", "2025-12-31", "广东省/广州市/", `["全程"]`)+`,`+
			raceRow("2", "今年的", "2026-01-01", "广东省/深圳市/", `["半程"]`)+`,`+
			raceRow("3", "明年的", "2027-03-01", "江苏省/南京市/", `["10公里"]`)+`]}}`)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Now: func() time.Time { return time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) }}
	got, err := c.ListAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("应丢掉去年那条、留下今年与明年，实际 %d: %+v", len(got), got)
	}
	if got[0].Name != "今年的" || got[1].Name != "明年的" {
		t.Errorf("留下的不对: %+v", got)
	}
}

func TestListAllDedupesRaceID(t *testing.T) {
	// 真实上游会吐重复行（3043 行里 4 个 raceId 重复），库里 race_id 是唯一键，
	// 漏了这一步整批写入会 constraint failed。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		pageNo := int(req["pageNo"].(float64))
		if pageNo == 1 {
			fmt.Fprintf(w, `{"success":true,"data":{"pageNo":1,"pageCount":2,"results":[%s,%s]}}`,
				raceRow("1", "A马", "2026-03-01", "广东省/广州市/", `["全程"]`),
				raceRow("1", "A马", "2026-03-01", "广东省/广州市/", `["全程"]`))
			return
		}
		fmt.Fprintf(w, `{"success":true,"data":{"pageNo":2,"pageCount":2,"results":[%s,%s]}}`,
			raceRow("2", "B马", "2026-01-01", "江苏省/南京市/", `["半程"]`),
			raceRow("1", "A马", "2026-03-01", "广东省/广州市/", `["全程"]`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	got, err := c.ListAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("重复 raceId 应只保留一条，实际 %d: %+v", len(got), got)
	}
}

func TestListAllStopsWhenPageCountMissing(t *testing.T) {
	// pageCount 缺失时退化成「本页不满就到底」，不能无限翻
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"success":true,"data":{"results":[`+
			raceRow("1", "A马", "2026-03-01", "广东省/广州市/", `["全程"]`)+`]}}`)
	}))
	defer srv.Close()
	got, err := (&Client{BaseURL: srv.URL}).ListAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("应 1 条，实际 %d", len(got))
	}
}

func TestListAllErrorClassification(t *testing.T) {
	t.Run("风控拦截页", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `<html>请求已被站点的安全策略拦截</html>`)
		}))
		defer srv.Close()
		_, err := (&Client{BaseURL: srv.URL}).ListAll(context.Background())
		if !errors.Is(err, ErrBlocked) {
			t.Fatalf("非 JSON 要判为被拦截，实际 %v", err)
		}
	})
	t.Run("业务失败", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"success":false,"code":500,"msg":"服务异常"}`)
		}))
		defer srv.Close()
		_, err := (&Client{BaseURL: srv.URL}).ListAll(context.Background())
		var ae *APIError
		if !errors.As(err, &ae) || ae.Code != 500 {
			t.Fatalf("业务错误要带 code，实际 %v", err)
		}
	})
	t.Run("空赛事不是错误", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"success":true,"data":{"pageCount":1,"results":[]}}`)
		}))
		defer srv.Close()
		got, err := (&Client{BaseURL: srv.URL}).ListAll(context.Background())
		if err != nil || len(got) != 0 {
			t.Fatalf("空结果应正常返回，实际 %v / %v", got, err)
		}
	})
}

func TestJoinSplitCategories(t *testing.T) {
	in := []Category{CategoryFull, CategoryTen}
	s := JoinCategories(in)
	if s != "full,10k" {
		t.Fatalf("拼接不对: %s", s)
	}
	out := SplitCategories(s + ",bogus,")
	if len(out) != 2 || out[0] != CategoryFull || out[1] != CategoryTen {
		t.Errorf("拆分应丢掉不认识的片段，实际 %v", out)
	}
	if !strings.Contains(s, "full") {
		t.Error("存储串应可读")
	}
}
