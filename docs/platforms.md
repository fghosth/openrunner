# 平台接入协议（`openrunner-core/platform`）

> 目标：**任何 Go 项目都能直接复用**。这个包只依赖标准库，
> 不依赖数据库、HTTP 框架或本项目的领域模型。

## 为什么这样设计

运动数据平台的同步本质上只有三件事：**校验凭证 → 拉活动列表 → 下载原始文件**。
去重、格式解析、指标计算属于上层共性职责，放进来会让每个平台重复实现一遍。
所以接口只有三个方法，且都不含业务逻辑。

## 接口

```go
type Provider interface {
    Name() string
    Validate(cred Credential) error                                    // 不发网络请求
    ListActivities(ctx, cred, since time.Time) ([]ActivityRef, error)
    Download(ctx, cred, ref ActivityRef) (data []byte, filename string, err error)
}
```

- `Credential` 是 `map[string]string` —— 用 JSON 存库，**新增平台不需要改数据库 schema**。
- `ActivityRef` 只含「下载所需的最小信息」（ID / 开始时间 / 归一化运动类型 / 名称），
  刻意不含距离心率等摘要：解析是上层的事。
- 返回 `*AuthError` 表示凭证失效 → 上层提示重新授权，而不是当临时故障重试。

## 仓库内的实现

| 平台 | 认证 | 可靠性 | 备注 |
|---|---|---|---|
| `garmin` | API Key（国际站）/ OAuth1（中国站）| ✅ 官方 | 薄适配层，包装已有的 `core/garmin` 客户端 |
| `intervals` | HTTP Basic（`API_KEY` + personal key）| ✅ 官方 | 接入成本最低：用户只需一个 key |
| `strava` | OAuth2（refresh_token 自动续期）| ✅ 官方 | **无原始 FIT 下载端点** → 用 streams 重建 TCX |
| `coros` | 账号密码 → accessToken | ⚠️ 非官方 | 接口可能变动，失败时提示用户改用文件导入 |

### 明确未实现的平台

Keep / Nike / Joyrun（悦跑圈）/ Onelap（顽鹿）/ Komoot / iGPSPORT / OPPO / TulipSport
—— running_page 对它们全部是**逆向实现**（抓包 cookie、解析私有 polyline 格式）。
这类实现需要持续跟版本逆向、随时可能失效，且无法在无账号环境下测试。
**建议改用文件导入**（各 App 都支持导出 FIT/GPX）。

## 新增一个平台

三步，不动任何已有代码：

```go
// 1) 新建 platform/yourplatform/yourplatform.go
package yourplatform

func init() { platform.Register(Provider{}) }

type Provider struct{}

func (Provider) Name() string { return "yourplatform" }

func (Provider) Validate(cred platform.Credential) error {
    return cred.Require("apiKey")
}

func (Provider) ListActivities(ctx context.Context, cred platform.Credential, since time.Time) ([]platform.ActivityRef, error) {
    // 一句话：调平台 API，把结果映射成 []platform.ActivityRef
    // 地址请用 apiBase() = platform.BaseURL(name, defaultURL)，便于测试注入
}

func (Provider) Download(ctx context.Context, cred platform.Credential, ref platform.ActivityRef) ([]byte, string, error) {
    // 下载原始文件，必要时用 platform.NormalizeOriginal 解 gzip / 嗅探扩展名
}
```

```go
// 2) 在 server 侧空白导入，让 init() 执行
import _ "github.com/openrunner/core/platform/yourplatform"
```

```go
// 3) 在 server/api/platform_meta.go 的 platformMetas() 里加一条 UI 元信息
//    （字段定义驱动前端凭证表单，不需要改前端代码）
```

## 移植到其他项目

`platform/` 目录**零外部依赖**（只用 `context` / `net/http` / `encoding/json` 等标准库），
复制整个目录到任意 Go 模块即可使用：

```bash
cp -r openrunner-core/platform your-project/internal/platform
```

然后：

- 测试注入：`platform.SetBaseURL("yourplatform", httptestServer.URL)`
- 遍历已注册平台：`platform.Names()`
- 取实现：`platform.Get("strava")`

凭证存储由你的项目自己决定 —— 本包只管协议，不碰持久化与加密。
