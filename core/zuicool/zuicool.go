// Package zuicool 拉取「最酷」（zuicool.com）的**报名**数据：报名窗口与价格。
//
// 为什么是它：中国田协的赛历接口（见 core/races）只有比赛本身，**没有报名起止、
// 没有价格**（Round 44 查死过）。最酷是 nowrun 首页列出的官方报名渠道之一，
// 页面上这两样都是明文，而且 robots.txt 只禁了 /news/wp-content/，赛事页允许抓。
//
// 与 core/races 一样**只依赖标准库**，不碰数据库与 HTTP 框架，可整体移植。
//
// 代价是这里解析的是 HTML 而不是 JSON，所以有三条纪律：
//
//  1. **先剥 HTML 注释再解析**。列表页有 100 处一模一样的
//     `<!-- <span>报名时间：2018-08-06~2018-08-16</span> -->`，那是注释掉的死模板。
//     不剥注释就会拿到 100 条一模一样的假报名窗口 —— 比拿不到更糟。
//  2. **解析到 0 条要报错，不能静默返回空**。抓取器坏掉的样子就是「一切正常，结果为空」，
//     而空结果在 UI 上和「这个月没有比赛」长得一模一样。见 ErrNoCards。
//  3. **markup 变了要能被发现**。testdata/ 里是真实抓取的裁剪片段，模式一改就会红。
package zuicool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL 是站点根地址（测试指向 httptest）。
const DefaultBaseURL = "https://zuicool.com"

// defaultPerPage 是列表页每页条数。上游默认 20，实测接受 100，能把请求数压到 1/5。
const defaultPerPage = 100

// maxListPages 是翻页上限兜底。目录按比赛日期**倒序**（第 1 页是最近的），
// 正常在 10 页内就会翻到过去，留足余量防上游排序变了以后无限翻。
const maxListPages = 40

// listInterval 是列表翻页之间的间隔。详情页有 detailInterval，列表页同样不能免俗 ——
// 一次刷新翻十几页，零间隔连发在对方日志里和抓取器没区别。
const listInterval = 300 * time.Millisecond

// ErrBlocked 表示响应不像列表页 —— 多半是被风控拦成了验证页。
var ErrBlocked = errors.New("响应不是最酷赛事页（疑似被风控拦截）")

// ErrNoCards 表示页面抓到了但一张卡片都没解析出来。
//
// 这几乎总是 markup 变了而不是真的没有赛事：最酷的「报名中」列表长期上百场。
// 把它当错误抛出去，调度器就会把原因写进 meta 让页面显示出来；
// 当空结果返回的话，用户看到的是「今天没有比赛可报」。
var ErrNoCards = errors.New("页面里没解析到任何赛事卡片（最酷可能改了 markup）")

// ErrNoPackages 表示赛事页是好的，但最酷上这场没挂组别与报名窗口。
//
// 这是**正常数据形态**，不是解析失败：走抽签/预报名、或压根不在最酷报名的赛事都这样
// （2026-11-18 实测 51 场配上里有 20 场如此）。调用方应保留列表页拿到的窗口、
// 不显示价格，而不是当成故障上抛。
//
// 单独立一条而不是复用 ErrNoCards：后者写着「最酷可能改了 markup」，
// 每轮刷新刷出 20 条这种日志，会把人引去查一个根本不存在的 markup 变更。
var ErrNoPackages = errors.New("最酷上这场没有组别与报名窗口")

// 报名状态。
const (
	StatusOpen = "open" // 点此报名
	StatusSoon = "soon" // 即将报名（窗口还没开）
)

// ListEntry 是列表页上的一场比赛。
type ListEntry struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Date    string `json:"date"`    // YYYY-MM-DD（比赛日）
	RegKind string `json:"regKind"` // start = 报名开始（还没开）；end = 报名截止（正在报）
	RegAt   string `json:"regAt"`   // YYYY-MM-DDTHH:MM:00，年份由比赛日推（见 resolveYear）
	Status  string `json:"status"`
	URL     string `json:"url"`
}

// Pkg 是一个报名组别（最酷按组别分别定价、分别报满）。
type Pkg struct {
	Name   string  `json:"name"`
	Price  float64 `json:"price"`
	Open   bool    `json:"open"`
	RegURL string  `json:"regUrl"`
}

// Registration 是详情页解析出来的报名详情。
type Registration struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	URL      string  `json:"url"`
	RegStart string  `json:"regStart"` // 空 = 上游没写
	RegEnd   string  `json:"regEnd"`
	Packages []Pkg   `json:"packages"`
	MinPrice float64 `json:"minPrice"` // 各组别最低起价；0 = 一个价都没解析到
}

// Client 是最酷客户端。零值不可用，请用 NewClient。
type Client struct {
	BaseURL string
	HTTP    *http.Client
	// Now 注入「今天」，测试要能钉住它。nil 时用 time.Now。
	Now func() time.Time
	// PerPage 覆盖每页条数；<=0 用 defaultPerPage。
	PerPage int
}

// NewClient 返回指向线上站点的客户端。
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
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) perPage() int {
	if c.PerPage > 0 {
		return c.PerPage
	}
	return defaultPerPage
}

var (
	reComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	// 卡片起点。列表页每场比赛一个 event-body。
	reCardStart = regexp.MustCompile(`(?s)<div class="event-body">`)
	// 标题链接带 class="event-a"，这是卡片里唯一带这个 class 的链接。
	reTitle = regexp.MustCompile(`(?s)<a href="https?://[^/"]+/event/(\d+)"[^>]*class="event-a"[^>]*>\s*([^<]+?)\s*</a>`)
	reDate  = regexp.MustCompile(`(\d{4})\.(\d{2})\.(\d{2})`)
	// 报名窗口。**月-日 时:分，上游不给年份**。
	reReg = regexp.MustCompile(`报名(开始|截止)：(\d{1,2})-(\d{1,2}) (\d{1,2}):(\d{2})`)
	// 详情页：meta description 里那句「…；2026年10月28日10:00至11月18日12:00开放报名…」
	// 比正文散文稳定，优先用它。
	reDesc     = regexp.MustCompile(`(?s)<meta name="description" content="([^"]*)"`)
	reH1       = regexp.MustCompile(`(?s)<h1>\s*<a href="https?://[^/"]+/event/(\d+)"[^>]*>\s*([^<]+?)\s*</a>`)
	reWin      = regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日(\d{1,2}):(\d{2})至(\d{1,2})月(\d{1,2})日(\d{1,2}):(\d{2})开放报名`)
	rePkgStart = regexp.MustCompile(`(?s)<div class="pkg2">`)
	rePkgName  = regexp.MustCompile(`(?s)<h4 class="name"[^>]*>\s*([^<]+?)\s*</h4>`)
	rePrice    = regexp.MustCompile(`(?s)<div class="price">\s*([0-9]+(?:\.[0-9]+)?)\s*</div>`)
	rePkgURL   = regexp.MustCompile(`href="(https?://[^"]*reg[^"]*/\d+(?:/\d+)?)"`)
)

// stripComments 剥掉 HTML 注释。见包注释第 1 条：列表页的死模板全在注释里。
func stripComments(s string) string {
	return reComment.ReplaceAllString(s, "")
}

// resolveYear 给「MM-DD HH:MM」补年份。上游只给月日，而报名窗口可能跨年
// （1 月的比赛，报名截止在上年 12 月）。规则：按比赛日同年拼，**只有晚出比赛日
// 60 天以上才退一年**。
//
// 60 天不是拍脑袋：跨年窗口一般差 300 天以上（1 月比赛 / 上年 12 月截止），
// 而**线上赛的报名截止确实可以晚于比赛日**（实测「跑遍中国线上系列赛」比赛 11-19、
// 截止 11-29）。用「晚于比赛日就退年」会把这种正常数据判到前一年去。
func resolveYear(mm, dd, hh, mi string, raceDate time.Time) string {
	m, e1 := strconv.Atoi(mm)
	d, e2 := strconv.Atoi(dd)
	h, e3 := strconv.Atoi(hh)
	i, e4 := strconv.Atoi(mi)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return ""
	}
	t := time.Date(raceDate.Year(), time.Month(m), d, h, i, 0, 0, raceDate.Location())
	if t.After(raceDate.Add(60 * 24 * time.Hour)) {
		t = t.AddDate(-1, 0, 0)
	}
	return t.Format("2006-01-02T15:04:05")
}

// get 取一个页面并做基本核验。
func (c *Client) get(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	// 带 UA：不带的时候不少站点直接给拦截页。
	req.Header.Set("User-Agent", "openrunner/1.0 (self-hosted running dashboard; race registration lookup)")
	body, err := c.http().Do(req)
	if err != nil {
		return "", err
	}
	defer body.Body.Close()
	if body.StatusCode != http.StatusOK {
		return "", fmt.Errorf("最酷返回 HTTP %d", body.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(body.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ParseList 从列表页 HTML 解析卡片。导出是为了能用真实 fixture 直接测。
func ParseList(html string) []ListEntry {
	h := stripComments(html)
	units := reCardStart.Split(h, -1)
	out := make([]ListEntry, 0, len(units))
	for _, u := range units[1:] {
		tm := reTitle.FindStringSubmatch(u)
		if tm == nil {
			continue
		}
		dm := reDate.FindStringSubmatch(u)
		rm := reReg.FindStringSubmatch(u)
		if dm == nil || rm == nil {
			// 没有比赛日或没有报名窗口的卡片直接跳过：宁缺毋假。
			continue
		}
		race, err := time.Parse("2006.01.02", dm[0])
		if err != nil {
			continue
		}
		kind := "end"
		if rm[1] == "开始" {
			kind = "start"
		}
		status := StatusOpen
		if strings.Contains(u, "即将报名") {
			status = StatusSoon
		}
		id := tm[1]
		out = append(out, ListEntry{
			ID:      id,
			Title:   strings.TrimSpace(tm[2]),
			Date:    race.Format("2006-01-02"),
			RegKind: kind,
			RegAt:   resolveYear(rm[2], rm[3], rm[4], rm[5], race),
			Status:  status,
			URL:     c_eventURL("", id),
		})
	}
	return out
}

func c_eventURL(base, id string) string {
	if base == "" {
		return "https://zuicool.com/event/" + id
	}
	return base + "/event/" + id
}

// ListPage 取一页赛事（page 从 1 开始）。
func (c *Client) ListPage(ctx context.Context, page int) ([]ListEntry, error) {
	url := fmt.Sprintf("%s/events?page=%d&per-page=%d", c.baseURL(), page, c.perPage())
	raw, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(raw, "event-body") {
		return nil, ErrBlocked
	}
	entries := ParseList(raw)
	if len(entries) == 0 {
		return nil, ErrNoCards
	}
	return entries, nil
}

// List 翻目录直到比赛日整体早于今天。
//
// 目录按日期倒序，所以「这一页最晚的比赛都比今天早」就可以停了 ——
// 再往后翻是历史，对「找下一场能报的比赛」没有意义。
// 翻页之间留 listInterval：目录一次要翻好几页，别把人家打出流量尖峰。
func (c *Client) List(ctx context.Context) ([]ListEntry, error) {
	today := c.now()
	seen := make(map[string]bool, 512)
	out := make([]ListEntry, 0, 256)
	for page := 1; page <= maxListPages; page++ {
		if page > 1 {
			// 翻页节流；ctx 取消时立刻退出而不是干等
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			case <-time.After(listInterval):
			}
		}
		entries, err := c.ListPage(ctx, page)
		if err != nil {
			if errors.Is(err, ErrNoCards) && page > 1 {
				// 第 2 页起空 = 翻到头了，第 1 页空才是真出事
				break
			}
			return nil, err
		}
		newOnPage := 0
		maxDate := ""
		floor := today.Format("2006-01-02")
		for _, cd := range entries {
			if cd.Date > maxDate {
				maxDate = cd.Date
			}
			// 过去的比赛不再需要报名信息，收进来只会让下游白抓详情页
			if cd.Date < floor {
				continue
			}
			if seen[cd.ID] {
				continue
			}
			seen[cd.ID] = true
			out = append(out, cd)
			newOnPage++
		}
		if newOnPage == 0 || (maxDate != "" && maxDate < today.Format("2006-01-02")) {
			break
		}
	}
	return out, nil
}

// ParseDetail 从详情页 HTML 解析报名详情。
func ParseDetail(html string) *Registration {
	h := stripComments(html)
	ev := &Registration{}
	if m := reH1.FindStringSubmatch(h); m != nil {
		ev.ID, ev.Title = m[1], strings.TrimSpace(m[2])
		ev.URL = c_eventURL("", m[1])
	}
	desc := ""
	if m := reDesc.FindStringSubmatch(h); m != nil {
		desc = m[1]
	}
	if w := reWin.FindStringSubmatch(desc); w != nil {
		y, _ := strconv.Atoi(w[1])
		mo, _ := strconv.Atoi(w[2])
		d, _ := strconv.Atoi(w[3])
		hh, _ := strconv.Atoi(w[4])
		mi, _ := strconv.Atoi(w[5])
		ev.RegStart = time.Date(y, time.Month(mo), d, hh, mi, 0, 0, time.Local).Format("2006-01-02T15:04:05")
		mo2, _ := strconv.Atoi(w[6])
		d2, _ := strconv.Atoi(w[7])
		h2, _ := strconv.Atoi(w[8])
		m2, _ := strconv.Atoi(w[9])
		end := time.Date(y, time.Month(mo2), d2, h2, m2, 0, 0, time.Local)
		if end.Before(time.Date(y, time.Month(mo), d, hh, mi, 0, 0, time.Local)) {
			end = end.AddDate(1, 0, 0) // 窗口跨年
		}
		ev.RegEnd = end.Format("2006-01-02T15:04:05")
	}
	for _, blk := range rePkgStart.Split(h, -1)[1:] {
		nm := rePkgName.FindStringSubmatch(blk)
		pm := rePrice.FindStringSubmatch(blk)
		if nm == nil || pm == nil {
			continue
		}
		price, err := strconv.ParseFloat(pm[1], 64)
		if err != nil {
			continue
		}
		reg := ""
		if u := rePkgURL.FindStringSubmatch(blk); u != nil {
			reg = u[1]
		}
		// 组别是否还能报：按钮写「已截止」或名字旁标「(已报满)」的都算不能报。
		open := !strings.Contains(blk, "已截止") && !strings.Contains(blk, "已报满")
		ev.Packages = append(ev.Packages, Pkg{
			Name: strings.TrimSpace(nm[1]), Price: price, Open: open, RegURL: reg,
		})
		if price < ev.MinPrice || ev.MinPrice == 0 {
			ev.MinPrice = price
		}
	}
	return ev
}

// Detail 取一场比赛的报名详情。
func (c *Client) Detail(ctx context.Context, id string) (*Registration, error) {
	raw, err := c.get(ctx, c_eventURL(c.baseURL(), id))
	if err != nil {
		return nil, err
	}
	ev := ParseDetail(raw)
	if len(ev.Packages) == 0 && ev.RegStart == "" {
		// 分两种情况，别都推给「markup 变了」——那是会让人去追幽灵 bug 的报错。
		// 标题解析得出来，说明这是一张正常的赛事页，只是最酷上没挂组别和报名窗口
		// （实测 20 场如此，如 2026温州马拉松：走预报名+抽签，详情页、/categories
		// 页都没有 price 节点）。调用方拿列表页的窗口兜底即可，不是解析器坏了。
		if ev.ID != "" {
			return nil, fmt.Errorf("%w：%s", ErrNoPackages, id)
		}
		// 连标题都没有：页面形态可能真的变了，或者这页根本不是赛事页
		return nil, fmt.Errorf("%w：%s 没解析到组别与报名窗口", ErrNoCards, id)
	}
	if ev.ID == "" {
		ev.ID = id
		ev.URL = c_eventURL(c.baseURL(), id)
	}
	return ev, nil
}
