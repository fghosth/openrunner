// Package platform 定义了「运动数据平台」的统一接入协议。
//
// 设计目标：**与 openrunner 解耦，可直接移植到任何 Go 项目**。
// 本包只依赖标准库 —— 不依赖数据库、HTTP 框架或本项目的领域模型。
//
// 接入一个新平台只需：
//  1. 实现 Provider 接口（三个方法：校验凭证 / 拉列表 / 下载原始文件）；
//  2. 在自己的 init() 里 Register(...)；
//  3. 上层用 platform.Get(name) 拿到实现即可。
package platform

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Sport 是归一化后的运动类型。各平台自己的类型串由实现负责映射。
type Sport string

const (
	SportRun   Sport = "run"
	SportRide  Sport = "ride"
	SportSwim  Sport = "swim"
	SportOther Sport = "other"
)

// Credential 是某平台的凭证键值对。
//
// 用 map 而不是每种平台一个 struct：上层只需按 JSON 存储/加密，
// 不需要为每个平台改数据库 schema；平台实现自己知道要哪些字段。
type Credential map[string]string

// Get 取字段（缺失返回空串）。
func (c Credential) Get(key string) string { return c[key] }

// Require 校验必填字段，缺失时返回可读错误。
func (c Credential) Require(keys ...string) error {
	var missing []string
	for _, k := range keys {
		if c[k] == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("缺少凭证字段: %v", missing)
	}
	return nil
}

// ActivityRef 是活动列表里的一条引用 —— 只含「下载原始文件」所需的最小信息。
//
// 刻意不包含距离/心率等摘要：本包的职责是「把原始文件拿回来」，
// 解析与指标计算属于上层（本项目由 ingest 包负责）。
type ActivityRef struct {
	// ID 平台侧的活动标识（字符串以兼容数字/哈希两种形式）。
	ID string
	// StartTime 活动开始时间（本地时区）。
	StartTime time.Time
	// Sport 归一化后的运动类型。
	Sport Sport
	// Name 活动名称（可空，仅用于日志/调试）。
	Name string
}

// Provider 是一个平台的数据源实现。
//
// 三个方法覆盖同步的全部需求，且都不含重试/去重/入库逻辑 ——
// 那些是上层的共性职责，放进来会让每个平台重复实现一遍。
type Provider interface {
	// Name 平台标识（存库、路由用），如 "intervals"、"strava"。
	Name() string

	// Validate 校验凭证字段是否齐全（**不发网络请求**，用于保存时即时反馈）。
	Validate(cred Credential) error

	// ListActivities 拉取 since 之后的活动引用。
	// 返回 AuthError 表示凭证失效（上层应提示用户重新授权）。
	ListActivities(ctx context.Context, cred Credential, since time.Time) ([]ActivityRef, error)

	// Download 下载一条活动的原始文件。
	// filename 是建议的文件名（含扩展名，如 "strava-12345.fit"）。
	// 返回的字节应当是可被 ingest 包解析的 FIT/TCX/GPX。
	Download(ctx context.Context, cred Credential, ref ActivityRef) (data []byte, filename string, err error)
}

// AuthError 表示凭证失效/被拒 —— 上层应提示用户重新授权，
// 而不是当作临时故障重试。
type AuthError struct{ Msg string }

func (e *AuthError) Error() string { return "认证失败: " + e.Msg }

// IsAuthError 判断是否为凭证失效。
func IsAuthError(err error) bool {
	var ae *AuthError
	return errors.As(err, &ae)
}

// baseURLOverrides 允许把某个平台的 API 根地址改掉 —— 测试时指向 httptest，
// 生产环境为空（各实现用自己的默认常量）。这是所有平台共用的测试注入点。
var baseURLOverrides sync.Map // name -> string

// SetBaseURL 覆盖某平台的 API 根地址（传空串恢复默认）。测试专用。
func SetBaseURL(name, url string) {
	if url == "" {
		baseURLOverrides.Delete(name)
		return
	}
	baseURLOverrides.Store(name, url)
}

// BaseURL 返回某平台应使用的 API 根地址：有覆盖用覆盖，否则用 fallback（实现里的默认值）。
func BaseURL(name, fallback string) string {
	if v, ok := baseURLOverrides.Load(name); ok {
		return v.(string)
	}
	return fallback
}

// registry 是全局注册表（平台在 init() 里注册）。
var (
	mu       sync.RWMutex
	registry = map[string]Provider{}
)

// Register 注册一个平台实现。重复注册同名平台会 panic ——
// 这属于编程错误，应在启动时就暴露。
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	if _, exists := registry[p.Name()]; exists {
		panic("platform: 重复注册平台 " + p.Name())
	}
	registry[p.Name()] = p
}

// Get 按名字取平台实现。
func Get(name string) (Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[name]
	return p, ok
}

// Names 返回已注册的平台名（字典序，便于固定 UI 顺序）。
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
