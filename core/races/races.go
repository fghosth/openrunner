// Package races 拉取并归一「赛事日历」（赛历）数据。
//
// 设计目标与 core/platform 一致：**只依赖标准库**，不碰数据库与 HTTP 框架，可整体移植。
//
// 当前唯一来源是中国田协官网（runchina.org.cn）的赛历页背后的接口。与成绩查询那条路
// 不同，这个接口**无鉴权、无验证码**，所以赛历可以全自动定期拉取。
//
// 接口无公开文档，字段形态是逆向官网 SPA bundle 得到的，因此解析一律走宽容路线：
// 认不出的记录跳过而不是让整次拉取失败 —— 少一场比赛比地图空掉好。
package races

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL 是内容中心接口根地址（与成绩查询的 score-center 是同域不同服务）。
const DefaultBaseURL = "https://api-changzheng.chinaath.com/changzheng-content-center-api"

// defaultPageSize 是单页条数。
//
// 别照抄历史值：写这份注释时上游接受 500，2026-11-18 复测**500 稳定返回 HTTP 500**，
// 300 及以下正常（500/300/200/150/100 逐个试过）。上游会调它的分页上限，
// 而 HTTP 5xx 重试两次也救不回来 —— 默认值得留在上游上限之下，不能贴着天花板。
// 200 时全量约 3000 场要 15 页，多几页换稳定，值得。
const defaultPageSize = 200

// maxPages 是翻页上限的兜底。上游 totalCount 异常时，没有这个就会无限翻页。
const maxPages = 80

// ErrBlocked 表示响应不是 JSON —— 几乎总是被站点风控拦成了拦截页。
// 把它和「没有赛事」区分开很重要：前者该重试，后者是正常空结果。
var ErrBlocked = errors.New("响应不是 JSON（疑似被站点风控拦截）")

// APIError 是接口返回的业务错误（HTTP 200 但 success=false）。
type APIError struct {
	Code int
	Msg  string
}

func (e *APIError) Error() string { return fmt.Sprintf("races: %s (code %d)", e.Msg, e.Code) }

// Category 是一类被保留的路跑项目。刻意只认这三类 ——
// 赛历里还有 5 公里、亲子跑、徒步等几十个花样，那些不是「马拉松比赛」。
type Category string

const (
	CategoryFull Category = "full" // 全程 / 全马
	CategoryHalf Category = "half" // 半程 / 半马
	CategoryTen  Category = "10k"  // 10 公里
)

// AllCategories 是 UI 筛选项的固定顺序。
var AllCategories = []Category{CategoryFull, CategoryHalf, CategoryTen}

// categoryMatchers 按「规范词子串」匹配，而不是枚举全词表 ——
// 实测 raceItem 有 200+ 种写法（`全程、半程、其他`、`半程马拉松`、`健康跑（10公里）`…），
// 枚举永远追不完，子串能顺带吃掉这些变体。
//
// 刻意**不**用裸「马拉松」判全马：词表里有 `迷你马拉松`、`环岛马拉松31公里`，
// 用裸词会把它们误判成全马；实测没有只写「马拉松」的条目，所以不亏。
var categoryMatchers = map[Category][]string{
	CategoryFull: {"全程", "全马", "42.195", "42公里", "42km", "42接力"},
	CategoryHalf: {"半程", "半马", "21.0975", "21公里", "21km", "21接力"},
	CategoryTen:  {"10公里", "十公里", "10km", "10.0公里"},
}

// Categories 从 raceItem 的原始串里认出被保留的项目（返回顺序固定，便于比较与展示）。
//
// 入参是已经解好的字符串数组；上游偶尔把整个数组当字符串塞进来，那种由调用方兜。
func Categories(items []string) []Category {
	joined := strings.ToLower(strings.Join(items, "、"))
	var out []Category
	for _, cat := range AllCategories {
		for _, needle := range categoryMatchers[cat] {
			if strings.Contains(joined, strings.ToLower(needle)) {
				out = append(out, cat)
				break
			}
		}
	}
	return out
}

// Race 是一条归一后的赛事。地址三段来自 raceAddress，没有经纬度 ——
// 所以地图只能做到省级，市/区只能靠名称下钻。
type Race struct {
	ID       string
	Name     string
	Date     string // YYYY-MM-DD
	Grade    string // A / B / C（属地办赛）/ 系列赛
	Province string
	City     string
	District string
	// Categories 命中的项目，至少一个（否则这条不会出现在结果里）。
	Categories []Category
	Scale      string
}

// Has 判断这条赛事是否含某项目。
func (r Race) Has(c Category) bool {
	for _, x := range r.Categories {
		if x == c {
			return true
		}
	}
	return false
}

// SplitAddress 把 "广东省/梅州市/梅江区" 切成三段。
//
// 实测形态：区可以为空（`广东省/梅州市/`）；直辖市省市同名（`重庆市/重庆市/巴南区`）。
// 照原样返回，不做「直辖市只留一层」的合并 —— 那是展示层的事，在这里合并会让数据说不清。
func SplitAddress(s string) (province, city, district string) {
	parts := strings.Split(strings.TrimSpace(s), "/")
	get := func(i int) string {
		if i < len(parts) {
			return strings.TrimSpace(parts[i])
		}
		return ""
	}
	return get(0), get(1), get(2)
}

// Client 是赛历客户端。零值不可用，请用 NewClient。
type Client struct {
	// BaseURL 覆盖接口根地址（测试指向 httptest）。空则用 DefaultBaseURL。
	BaseURL string
	// HTTP 自定义客户端；nil 时用带 60s 超时的默认实例（全量要翻好几页）。
	HTTP *http.Client
	// PageSize 单页条数；<=0 用 defaultPageSize。
	PageSize int
	// Now 注入「今天」，测试要能钉住它。nil 时用 time.Now。
	Now func() time.Time
}

// NewClient 返回指向线上接口的客户端。
func NewClient() *Client { return &Client{} }

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return DefaultBaseURL
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	// 30s：正常单页几秒就回来，超过这个数基本是上游卡住，交给上层重试而不是干等
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// keepYear 决定一条比赛留不留：**只留当年及以后**。
// 历史赛历对「找下一场比赛」毫无用处，却占了上游九成数据（实测 3043 行里
// 2026 年只有 415 行），存下来既拖慢整表替换，也让地图和年份筛选全是陈年旧赛。
func (c *Client) keepYear(date string) bool {
	y, err := strconv.Atoi(date[:4])
	if err != nil {
		return false
	}
	return y >= c.now().Year()
}

func (c *Client) pageSize() int {
	if c.PageSize > 0 {
		return c.PageSize
	}
	return defaultPageSize
}

type rawRace struct {
	RaceID      json.Number `json:"raceId"`
	RaceName    string      `json:"raceName"`
	RaceGrade   string      `json:"raceGrade"`
	RaceTime    string      `json:"raceTime"`
	RaceAddress string      `json:"raceAddress"`
	RaceItem    string      `json:"raceItem"` // JSON 字符串数组，偶尔是脏数据
	RaceScale   string      `json:"raceScale"`
}

type envelope struct {
	Success bool   `json:"success"`
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Data    *page  `json:"data"`
}

type page struct {
	Results    []rawRace `json:"results"`
	PageNo     int       `json:"pageNo"`
	PageSize   int       `json:"pageSize"`
	PageCount  int       `json:"pageCount"`
	TotalCount int       `json:"totalCount"`
}

// ListAll 拉取全部赛事，只保留含 全程/半程/10公里 的，按日期升序返回。
//
// 上游 totalCount 实测 3000 出头，pageSize=500 时 7 页拉完。翻页以 pageCount 为准
// （totalCount 除法会因边界差一页），并用 maxPages 兜底防死循环。
func (c *Client) ListAll(ctx context.Context) ([]Race, error) {
	out := make([]Race, 0, 512)
	// 上游会吐出完全相同的行（实测 3043 行里有 4 个 raceId 重复），而 race_id
	// 在库里是唯一键 —— 不去重，整批写入会直接失败。
	seen := make(map[string]bool, 512)
	for pageNo := 1; pageNo <= maxPages; pageNo++ {
		p, err := c.fetchPage(ctx, pageNo)
		if err != nil {
			return nil, err
		}
		for _, raw := range p.Results {
			r, ok := normalize(raw)
			// 顺序有讲究：先 normalize（保证 Date 是 YYYY-MM-DD，keepYear 才能安全切片）
			if !ok || seen[r.ID] || !c.keepYear(r.Date) {
				continue
			}
			seen[r.ID] = true
			out = append(out, r)
		}
		// 以 pageCount 为准；pageCount 缺失时退化成「本页不满就到底」
		last := pageNo >= p.PageCount
		if p.PageCount <= 0 {
			last = len(p.Results) < c.pageSize()
		}
		if last {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// fetchPage 拉一页。上游偶发挂起（实测有单次请求几十秒不回头），这类抖动重试一次
// 通常就好了；业务拒绝（限流、被安全策略挡）重试没有意义，直接往上抛。
func (c *Client) fetchPage(ctx context.Context, pageNo int) (*page, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		p, err := c.fetchPageOnce(ctx, pageNo)
		if err == nil {
			return p, nil
		}
		var ae *APIError
		if errors.As(err, &ae) || errors.Is(err, ErrBlocked) {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

func (c *Client) fetchPageOnce(ctx context.Context, pageNo int) (*page, error) {
	body, err := json.Marshal(map[string]any{
		"provinceId":    "",
		"cityId":        "",
		"districtId":    "",
		"raceName":      "",
		"raceGrade":     "",
		"raceStartTime": "",
		"pageNo":        pageNo,
		"pageSize":      c.pageSize(),
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL()+"/api/homePage/official/searchCompetitionMls", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("races: 请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("races: 读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("races: HTTP %d", resp.StatusCode)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("races: %w: %.120s", ErrBlocked, bytes.TrimSpace(raw))
	}
	if !env.Success {
		msg := strings.TrimSpace(env.Msg)
		if msg == "" {
			msg = "查询被拒"
		}
		return nil, &APIError{Code: env.Code, Msg: msg}
	}
	if env.Data == nil {
		return nil, &APIError{Code: env.Code, Msg: "响应没有数据"}
	}
	return env.Data, nil
}

// normalize 把一条原始记录归一；返回 ok=false 表示这条不可用（缺关键字段或不含保留项目）。
func normalize(raw rawRace) (Race, bool) {
	id := strings.TrimSpace(raw.RaceID.String())
	if id == "" || id == "0" {
		return Race{}, false
	}
	date := strings.TrimSpace(raw.RaceTime)
	if _, err := time.Parse("2006-01-02", date); err != nil {
		// 日期是列表的主排序键与「今年有哪些赛」的依据，认不出来就丢这条
		return Race{}, false
	}
	prov, city, dist := SplitAddress(raw.RaceAddress)
	if prov == "" {
		return Race{}, false
	}
	cats := Categories(parseItems(raw.RaceItem))
	if len(cats) == 0 {
		return Race{}, false
	}
	return Race{
		ID:         id,
		Name:       strings.TrimSpace(raw.RaceName),
		Date:       date,
		Grade:      strings.TrimSpace(raw.RaceGrade),
		Province:   prov,
		City:       city,
		District:   dist,
		Categories: cats,
		Scale:      strings.TrimSpace(raw.RaceScale),
	}, true
}

// parseItems 解 raceItem。上游给的是 JSON 字符串数组（`"[\"全程\",\"半程\"]"`），
// 但词表里出现过带全角逗号、裸串的情况，解不出来就整串当一个 token 去匹配。
func parseItems(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err == nil {
		out := make([]string, 0, len(arr))
		for _, v := range arr {
			if v = strings.TrimSpace(v); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	// 退化路径：按常见分隔符切，实在切不开就整串交给匹配器
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == '，' || r == '、' || r == '/' || r == '[' || r == ']' || r == '"'
	})
	if len(fields) == 0 {
		return []string{s}
	}
	return fields
}

// String 便于日志与测试断言。
func (c Category) String() string { return string(c) }

// ParseCategory 把存储里的字符串还原成 Category。
func ParseCategory(s string) (Category, bool) {
	switch strings.TrimSpace(s) {
	case string(CategoryFull):
		return CategoryFull, true
	case string(CategoryHalf):
		return CategoryHalf, true
	case string(CategoryTen):
		return CategoryTen, true
	}
	return "", false
}

// JoinCategories / SplitCategories 是存储层与领域层之间的小转换器。
// 用逗号而不是 JSON 存：列表短、要参与 SQL 过滤，可读性比结构更重要。

// JoinCategories 把项目列表拼成存储用的字符串。
func JoinCategories(cs []Category) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, string(c))
	}
	return strings.Join(parts, ",")
}

// SplitCategories 解存储里的项目串，认不出的片段直接丢掉。
func SplitCategories(s string) []Category {
	var out []Category
	for _, part := range strings.Split(s, ",") {
		if c, ok := ParseCategory(part); ok {
			out = append(out, c)
		}
	}
	return out
}

// Year 取比赛年份（非法日期返回 0）。
func (r Race) Year() int {
	if len(r.Date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(r.Date[:4])
	if err != nil {
		return 0
	}
	return y
}
