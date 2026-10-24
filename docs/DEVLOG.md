# 开发日志（DEVLOG）

> 用途：记录实现过程中的**工程事实与坑**——ADR 只记不可逆决策，这里记"当时怎么做的、为什么、踩了什么"。
> 每完成一张票追加一节；坑与决策随做随记。新会话开工前先读本文件与 `CONTEXT.md`、`docs/adr/`。

## 状态快照

更新于票 03 完成时（2026-10-25）。

| 仓库 | HEAD | 说明 |
|---|---|---|
| `openrunner`（本仓） | 见 `git log` | server（Go）+ 文档 + 票务 |
| `openrunner-core` | 同上 | 纯 Go 领域库：core 类型 + ingest |
| `openrunner-ui` | 同上 | React 19 + Vite + Tailwind v4 |

测试接缝状态：S1（core 领域 API）✅ 已建立（恒等式+换算单测）；S2（fixture 回放）✅ 已建立（FIT）；S3（HTTP 黑盒）✅ 已建立（server/api/*_test.go）。UI 无自动化缝（人工验收）。

fixture 清单（`openrunner-core/ingest/testdata/`）：`big_activity.fit`（官方 FIT SDK 样本，2.4MB，骑行 99998 记录）、`real_sup.fit`（原 poolswim 样本，SUP，Session 距离/爬升为 invalid 哨兵）、`corrupt.fit`、`fileid_only.fit`。来源 muktihari/fit 库 testdata（MIT + FIT SDK 双许可，可复分发）。

---

## 票 01 · 走通骨架

**交付**：三仓骨架；server `/health`（S3 TDD）；GORM+SQLite（glebarez 纯 Go 驱动，CGO_ENABLED=0）；`go:embed` UI；Makefile；Dockerfile + compose（实机验证）；CI；MIT。

### 决策
- **模块路径**：core = `github.com/openrunner/core`，server = `github.com/openrunner/server`；server/go.mod 用 `replace => ../../openrunner-core` 指向本地。⚠️ 这要求 checkout 布局为同级目录——CI 已按此布局（主仓 `path: openrunner` + core `path: openrunner-core`），发布公开 remote 前红是预期。
- **嵌入策略**：UI 构建产物复制进 `server/web/dist`；占位 index.html **入库**（fresh clone 可编译），`assets/` 忽略由 `make ui-dist` 刷新。`.gitignore` 的 `dist/` 必须锚定为 `/dist/`（评审抓出的真 P1：未锚定会误伤 server/web/dist）。
- **compose 构建上下文**：主上下文=本仓库，core/ui 经 `additional_contexts` 注入（相对路径基于 compose 文件目录，写 `../../`）。注意 BuildKit stage 名不得与 additional context 名相同（`FROM … AS ui-build` 而非 `ui`）。

### 坑
- Go 1.26 **移除小数常量→整型截断**：`int32(3.7)` 编译错；常量表达式含小数结果转整型一律 `math.Round` 后转（票 03 GPS 常量踩过）。
- `*gorm.DB` 无 `.Close()`；要经 `db.DB()` 取 `*sql.DB`。store 包封装为 `store.DB{Gorm(), Close()}`。
- lmfit 式全局单例不存在的问题这里没有；但 Go 常量折叠 + shift + float 混合表达式的报错信息会给出"最终值"，容易误判——隔离最小复现是唯一解。

---

## 票 02 · 鉴权与 Athlete 档案

**交付**：首启 setup（409 防重）+ JWT 登录（bcrypt、HS256、7 天、密钥落 `secrets` 表重启后 token 仍有效）；`requireAuth` 中间件（业务 API 全量鉴权）；athletes CRUD；UI：Setup/Login/档案切换页；单管理员。

### 决策
- **单 mux + SPA 回退**：`api.NewMux(db, version, spa http.Handler)`——显式注册 `/health` 与 `/api/*`，`spa != nil` 时挂 `/`。教训：最初 main 里 `StripPrefix("/api")` + 内部带前缀路由，测试直连 NewMux 全绿、生产 404（测试盲区）——**路由只在生产等价形态下测**（补了 /api 前缀用例后简化为单 mux）。
- **DTO 契约**：store 模型不直接出 API；GORM 模型加小驼峰 json tag，`PasswordHash`/`SecretModel` 标 `json:"-"` 永不出 API（评审抓出"大写字段泄漏"后立的规矩）。
- **错误分流**：store 定义 `ValidationError`（→400）/ `ErrUserExists`（→409）/ 其余（→500 不带内部细节）；登录失败统一"账密错误"防账号枚举；用户不存在时执行 dummy bcrypt 抹时序差。
- **「切换 Athlete」= 客户端状态**（localStorage + 请求携带），无 switch 端点——已注记进 spec.md API 契约行。

### 坑（评审发现）
- 401 后 UI 卡死：`api.ts` 收 401 清 token + 抛 `ApiError(401)`，App 在 `refreshAthletes` 的 catch 里判 401 → 回登录页（验收 2 的 UI 引导）。
- UI mutations 必须 try/catch → setError，否则 unhandled rejection 零反馈。
- TODO(安全) 挂账：登录/setup 无速率限流（单用户自托管接受，多用户前必补）。

---

## 票 03 · FIT 导入单文件纵深

**交付**：core `Stream`（等长通道/缺测填 0/JSON BLOB）+ `NormalizeStream`；`core/ingest.ParseFIT`（muktihari/fit v0.28.5）；server：ActivityModel + Stream BLOB（事务写入）、`POST /api/athletes/{id}/activities/import`（multipart + sha256 去重 409）、list/detail/stream；UI：react-router 化 + 上传/列表/详情页。

### 决策
- **FIT 库选型**：`github.com/muktihari/fit`（MIT + FIT SDK 许可、持续维护、自带官方 SDK 测试样本可复分发）。tormoder/fit 为备选。
- **fixture 策略**：放弃自造 FIT（ToMesg 用法踩坑，生成的字段错位），**直接提交库内官方 SDK 样本**做 S2 fixture；scale/offset 换算用 `sampleFromRecord` 纯单元测试钉死（期望值为手工换算的独立真值，非往返自证）。
- **mesgdef 字段语义**：全是**原始缩放值**——Distance scale 100、Speed/EnhancedSpeed scale 1000 (m/s)、Altitude scale 5 offset 500、PositionLat/Long 半圆。invalid 哨兵 = 各 basetype Max 值，必须显式判并归 0（真实 SUP 文件的 Session 距离/爬升就是 invalid）。
- **enhanced vs 传统字段回退链**：Enhanced* 缺席（invalid）或 raw=0（海拔 -500m 物理不可信）时回退传统字段；GPS 用规范公式 `semi × 180/2^31` 自实现（弃 lib 的 semicircles 包，行为存疑）。
- **去重**：文件内容 sha256 → `activities.content_hash`（athlete 内唯一语义），重复上传 409。票 04 批量导入沿用。
- **Stream BLOB**：JSON 序列化整包存 `activity_streams.data`（01 文档决策），通道命名小驼峰（hr/speed/distance/alt/power/cad/lat/lon）。

### 坑
- 上文 Go 1.26 常量截断（GPS 半圆常量）。
- filedef.Listener 处理 chained FIT（多段）文件时 `File()` 返回合并结果；Session[0] 可能不是整段——票 04 批量时注意。
- 测试里 fixture 相对路径以 **CWD=包目录** 计：server/api 测试引 core 的 fixture 用 `../../../openrunner-core/...`。
- multipart 测试走 `io.Pipe` + goroutine 写（httptest 无现成 multipart helper）。

---

## 工程备忘（跨票有效）

1. **提交纪律**：每票独立 commit（三仓各一），评审修复单独 commit；票验收完成后勾选 `.scratch/openrunner-v1/issues/NN-*.md` 并提交。
2. **TDD 循环**：红→绿只在 S1/S2/S3 缝上；禁止在缝外测内部实现（tdd skill）。
3. **评审节奏**：每 1-2 张票跑一次 code-review skill 两轴并行（Standards/Spec 子代理），P1 当场修。
4. **服务启动**：`make run`（数据 ./data）；单二进制 `--data` 默认 `~/.openrunner`；端口 `--addr`。
5. **go.mod replace** 在 core 有公开 remote 后应收紧（CI 已用 `${{ env.CORE_REPO }}` 占位 `fghosth/openrunner-core`，推送后改真名）。
6. **review 结论沉淀**：两轴报告要点回写本文件对应票节，防丢失。

---

## 评审记录 · 票 03（Review Round 3）

两轴结论：**OK with notes**（无 P1；Standards 轴确认 core 零 Web/DB 红线、DTO 纪律、S1/S2/S3 缝纪律全部通过；Spec 轴 5 项验收逐条属实）。P2 已全部处理：

| 发现 | 处置 |
|---|---|
| 去重/Stream 端点吞 DB 错误（err==nil&&dup!=nil 模式） | 全面改为 err→500 / NotFound→404 分流；AthleteExists 同模式一并修 |
| content_hash 无唯一索引（并发竞态） | migrate 建 partial unique index `(athlete_id,content_hash) WHERE content_hash != ''`；⚠️ GORM 默认表名 `activity_models`，显式 `TableName()` 定为 `activities`/`activity_streams` |
| multipart 无大小限制/临时文件滞留 | MaxBytesReader 64MB + LimitReader + MultipartForm.RemoveAll |
| durationSecs=elapsed 口径（TRIMP 会高估） | **新增 `TimerSecs`**（TotalTimerTime，缺省回退 elapsed）；DTO 暴露 timerSecs；**票 05 的 TRIMP 时长优先 TimerSecs** |
| startTime 注释矛盾 | 统一为"绝对时刻 + 展示层本地渲染"（core 与 store 两处） |
| NormalizeStream 硬编码 1s（smart recording 会把 3-5s 间隔当"停车"） | ingest 按**记录时间差中位数**推断 RecIntervalSecs |
| sportNames/fmtDuration 双处重复 | 提取 `ui/src/format.ts` |
| 导航 `<a>` 整页刷新 | 改 `<Link>` |

**范围说明（Spec 轴判定）**：Home 内的档案胶囊实际吸收了票 02 的"档案切换页"（App.tsx 已无独立页）——属合理粘合，记档；票面验收不受影响。

**票 05 注意**：TRIMP 的时长取 `TimerSecs`（不是 durationSecs）。
