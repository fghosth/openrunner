package api

// 认证端点速率限流（debt-sprint 票 06，Round 3 安全挂账收尾）。
//
// 内存令牌桶、无新依赖。局限（有意接受，单用户自托管场景）：
//   - 重启清零；多实例不共享计数。
//   - 键取 X-Forwarded-For 首项（Docker 部署常在反代后，否则全员共享一个桶），
//     没有 XFF 时用 RemoteAddr。直连暴露时攻击者可伪造 XFF 换桶——
//     防爆破成本门槛而已，不是边界防御；反代部署请自行剥掉外部 XFF。
//   - 只在登录「成功」时清零计数（见 resetOnSuccess 注释）：
//     爆破全是失败尝试，正常用户登录成功即回到满桶（见 rateLimitAuth）。

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type tokenBucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter 是按 key 分桶的令牌桶。now 可注入，测试免等窗口。
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rate    float64 // 每秒补充的令牌数
	burst   float64 // 桶容量
	now     func() time.Time
}

func newRateLimiter(perMinute, burst float64) *rateLimiter {
	return &rateLimiter{
		buckets: map[string]*tokenBucket{},
		rate:    perMinute / 60,
		burst:   burst,
		now:     time.Now,
	}
}

// allow 消耗一个令牌；被拒时返回建议的 Retry-After 秒数（向上取整）。
func (l *rateLimiter) allow(key string) (retryAfterSecs int, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b := l.buckets[key]
	if b == nil {
		b = &tokenBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+secondsOr0(now.Sub(b.last))*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return 0, true
	}
	need := (1 - b.tokens) / l.rate
	return int(math.Ceil(need)), false
}

// reset 清空某 key 的欠账（登录成功后调用）。
func (l *rateLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}

// secondsOr0 防时钟回拨产生负间隔。
func secondsOr0(d time.Duration) float64 {
	if d < 0 {
		return 0
	}
	return d.Seconds()
}

// clientIP 取限流键用的来源地址（见文件头 XFF 权衡说明）。
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, _ := strings.Cut(xff, ","); first != "" {
			return strings.TrimSpace(first)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// statusProbe 只为了看响应码，不缓冲 body。
type statusProbe struct {
	http.ResponseWriter
	status int
}

func (s *statusProbe) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// rateLimitAuth 把限流挂在 auth 处理器上（不引中间件框架，mux 直接包）。
func rateLimitAuth(l *rateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := clientIP(r)
		if retry, ok := l.allow(key); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeError(w, http.StatusTooManyRequests, "尝试过于频繁，请稍后再试")
			return
		}
		probe := &statusProbe{ResponseWriter: w, status: http.StatusOK}
		next(probe, r)
		// 成功（2xx）说明是本人登录，清零失败计数；
		// 401/409 这类失败保留欠账，继续累计到 429
		if probe.status >= 200 && probe.status < 300 {
			l.reset(key)
		}
	}
}
