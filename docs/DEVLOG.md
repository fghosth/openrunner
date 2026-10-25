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

## 票 05 · 心率区间 + TRIMP 家族

**交付**：core `hrmetrics.go`（TrimpPoints/Trimp100/TrimpZonal/HrTimeInZone，恒等式测试：阈值心率 1h → TRIMP(100)=100）；`hr_zones` 表（按日期区间生效，RestHR<LTHR≤MaxHR 校验）+ `activity_metrics` 快照表；导入时自动计算 + `recompute` 补算端点；档案 `sex` 字段（Ksex）；UI 心率区间编辑器 + 详情指标卡与 TiZ 分布条。

### 决策
- **时长口径**：TRIMP 用 `TimerSecs`（TotalTimerTime，不含停表），缺省回退 elapsed——评审 R3 决议的落地。
- **Edwards 系数**：区界 64/77/84/90/95% MHR，系数 0.84/1.65/2.57/4.01/5.91（GC TRIMP Zonal 注释口径）；<64% 记 zone1。
- **日期语义**：hr_zones 用 YYYY-MM-DD 字典序比较；活动日期暂取 UTC（本地时区细分挂账，跨午夜活动影响 ≤1 天）。
- **无配置语义**：无覆盖区间 → 不写 metrics（detail 为 null），UI 提示配置；不做估算（票 07）。

### 实测
big_activity（8.5h 骑行，avgHR 110）：TRIMP 284 / TRIMP(100) 199 / TiZ 全 zone1——数值合理（低强度长骑）。

## 票 06 · 配速区间 + rTSS

**交付**：core `pace_metrics.go`（Rtss/NormalizedSpeedKph 4次方均值/PaceZoneIndex/PaceTimeInZone，Skiba 跑步区界 %CV 6 区 80/87/95/105/119%；恒等式：阈值匀速 1h→100、时长线性、强度平方）；`pace_zones` 表（阈值配速 秒/km，UI mm:ss 输入）；metrics 双链重构——**心率链与配速链各自独立可选**，任一有产出即落库；详情页 rTSS + 配速 TiZ 条。

### 决策
- **配速链门控**：仅跑步（`Sport == run`）计算 rTSS/配速 TiZ——骑行 30kph 对 12kph 跑步阈值会产生荒谬数值（实测 rTSS 5356）；骑行强度留待功率链。S3 测试断言门控行为。
- **阈值配速存储**：秒/公里（跑者口径），UI mm:ss 双向转换；合法域 120–1200 s/km。
- **metrics 架构演化**：computeMetrics(db, athlete, activity, stream) 统一入口，各链独立可选——票 07 估算、票 09 功率链继续在此扩展。

## 评审记录 · 票 05+06（Review Round 4）

两轴结论：**OK with notes**（Spec 轴：两票验收逐条属实、无 scope creep；Standards 轴：core 零依赖、错误分流、恒等式测试齐）。已修：

| 发现 | 级 | 处置 |
|---|---|---|
| Edwards/Skiba 常数无论文出处（违 ADR-0001） | P1 | 代码注释标注"转引 GC + 原文待溯源"；02 规格文档补出处行；**溯源挂账见下** |
| 日期校验仅 len==10（"abcd-efgh" 可入库） | P1 | `time.Parse("2006-01-02")` 严校验（hr/pace 两处） |
| rTSS 链缺 TimerSecs→elapsed 回退 | P2 | 与心率链同回退 |
| 区间 Update 0 行仍 201 | P2 | RowsAffected==0 → ErrNotFound → 404 |
| 重叠区间取用规则未成文 | P2 | ORDER BY valid_from DESC, id DESC + 注释成文 |
| strconv 死码 / recompute 注释过时 / UI 无 sex 设置 / 默认日期硬编码 | P2 | 全清 |

### 挂账（新开）
1. **常数溯源**：Edwards 区系数 0.84/1.65/2.57/4.01/5.91 与 Skiba 跑步配速区界的原始论文出处待查证（当前仅能追溯到 GC 转引）；查证后回填 02 文档。
2. **NGP 坡度修正（Round 6 文档化决议）**：rTSS 的 4 次方均值用**原始速度**，无 GC aCoggan 的 NGP 坡度修正——山地跑（坡度 >±5%）偏差可达两位数百分比，该偏差经日聚合**直接进入 PMC 的 rTSS 口径**。接受该简化的理由：openrunner 目标用户为路跑/田径场场景为主；修正待山地用户反馈后作为独立指标（NGP-rTSS）引入，不回溯改 rTSS 定义。
3. **本地日期**：✅ Round 5 已解决——`timeToDateString` 改 `.Local()`（自托管单用户=服务器时区=用户时区）；手工活动取日错一天问题同源同修。
4. **TiZ 口径**：逐样本 TiZ 含停表段心率（继承 GC 口径），TRIMP 用 Timer——注释已标明；若要严格对齐可在停表段置 0。
5. **重叠区间**：允许并存，取用规则=起点最晚优先、同起点取后建者；UI 编辑/删除区间待补。

## 票 07 · Estimated Metric + 计划 Activity

**交付**：手工创建端点 `POST /api/athletes/{id}/activities`（date/sport/durationSecs/distanceM/avgHr/isPlanned）；computeMetrics 支持 `stream == nil` 估算路径——avgHR 代入 TRIMP 公式、平均速度代入 rTSS（同一公式，IsEstimated=true；Zonal/TiZ 无逐秒数据不产出）；recompute 对无流活动也走估算；UI 手工录入表单（折叠式）+ 列表/详情"计划"徽标 + 估算说明文案。

### 决策
- **估算=同公式、均值输入**：TRIMP 估算用 avgHR 直代（非新公式）；rTSS 估算用平均速度代 NP 位——4 次方均值需要逐秒数据，均值是其保守近似；两路差异已在 S3 断言（估算 Rtss=100 @ 12kph/1h）。
- **Zonal/TiZ 估算链不产出**：无逐秒数据无法分区——metrics 中保持 0 值数组，UI 只在有值时渲染。
- **计划活动**：`isPlanned` 落库即语义；"实际/计划/预期"三序列的拆分属票 10 PMC（今日特判已在 03 文档 A3 规格中）。
- 领域字段 `core.Activity.AvgHr` 仅 summary-only 语义（有 Stream 时恒 0，由流计算）。

### 实测
手工计划 run（12km/1h/avgHR170）→ TRIMP 142.7 / rTSS 100（阈值速度）/ isEstimated=true / isPlanned=true；未配置的链不影响另一链。

## 评审记录 · 票 07 + Round 4 修复验证（Review Round 5）

两轴结论：**OK with notes，无 P1**（Standards：出处注释诚实达标、严校验覆盖、upsert 语义正确；Spec：票 07 验收 ①②③✓、④部分→本轮补齐）。已修：

| 发现 | 处置 |
|---|---|
| S3 断言只验非零（与 DEVLOG"断言 Rtss=100"不符） | 手工活动测试改**独立真值等值断言**：TRIMP=60×0.8×0.64×e^1.536（手算）、rTSS=100（精确） |
| 双入口对照测试缺失（票 07 验收④部分） | 新增 `metrics_dualentry_test.go`：恒定数据下实测/估算两入口 TRIMP、rTSS **逐等相等**（恒速时 NP=平均速度，估算非近似）|
| 手工活动 zone 取日错一天（local 午夜 → UTC 转换） | timeToDateString 改 `.Local()`——**同修挂账 #3** |
| TimerSecs 回退块两链重复 + 估算 np 用 elapsed 的口径混合 | 抽 `effectiveDurationSecs()` 共用；估算 np=elapsed 基准已在注释声明（summary-only 无 Timer 概念） |
| avgHr=0 部分链产出 UI 语义 / DEVLOG 表述 | 估算文案已声明"无逐秒数据"口径；TRIMP=0 为合法值（无有效心率），保持 |

**发现过程坑**：测试数据自洽性（43.2km@12kph/1h 与恒速 12kph 矛盾导致 rTSS 1296）——双入口等值测试同时是数据自洽性的哨兵。

## 票 10 · PMC 趋势纵深

**交付**：core `pmc.go`（ComputePMC：三序列 IIR 递推 + 播种 + SB 位移 + RR + 今日特判，恒等式 7 组）；server `seasons` CRUD + `GET /api/athletes/{id}/pmc?metric=trimp|rtss`（activities×metrics JOIN 聚合日负荷、GC 区间口径 from−1/to+365）；UI Trends 页（ECharts：LTS/STS/SB/RR + 日负荷柱 + 计划/预期虚线 + SB<−25 红色 markArea + dataZoom + 赛季编辑器 + 度量切换）。

### 决策
- **播种语义**：seed 直接作为该日 LTS/STS 初值（GC 用负值标记法，openrunner 用显式初值——数学等价，语义更直白）。
- **RR 窗口减法**：与 GC 一致 `d−stsDays−1 ≥ 0` 才减（我最初写成 `d−stsDays ≥ 0`，越界 panic——测试抓出）。
- **PMC 区间**：from=min(最早活动/赛季)−1，to=max(最晚活动,今天)+365（GC 口径 A3）。
- **度量切换**：PMC 用 metric 列直查（trimp/rtss），无表达式引擎（GC DataFilter 路径砍掉）。

### 教训（IIR 直觉陷阱）
- 测试预期两次写错：①60 天未到稳态（IIR 需 ~4τ≈170 天才收敛 99%）→ 改 365 天；②"活动日 LTS 应抬升"错误——负荷 142.7 **低于**当前 LTS 186 时，IIR 向负荷靠拢反而下降。正确判据=**高于纯衰减反事实** `prev×e^(−1/42)`。教训：滤波器行为用反事实断言，别用单调直觉。
- ECharts 色带用 markArea（SB<−25 过劳红区）；echarts@6 类型对 `season.id?: number` 需显式收窄。

### 实测
播种 200→活动日 185.2（>纯衰减）、今日 expected==actual、未来计划日 planned>0、664 天序列 <100ms。

### Round 6 双轴结论
Standards 轴：公式出处链诚实、metric 白名单封死注入、时间语义三层无混用、ECharts 生命周期干净、Round 5 修复逐条属实。硬违规 2×P1 已修：①seasons DTO 化（store 模型不再直接 Decode）；②SB 红区阈值回填 03 文档（GC sbColor 真值 **−40**，原 −25 无出处、文档示例 −30 失真——单一事实源修正）。P2 已清：死码、addDays/daysBetween 移回测试文件（core 生产代码不留测试专用函数）、markArea 改双点对格式（防 echarts@6 静默不渲染）。挂账更新：**金标准数据对照自动化**（Spec 轴 P2：恒等式全合成数据）与 **pmc_test 未来日期用例过期**（今日特判断言空转）记入挂账。

### Round 6（Spec 轴）结论
与 GC PMCData.cpp **逐行核对**：RR 窗口边界、今日特判、SB 位移、from/to 区间全部一致（Correct 判定）；验收 ①②全过、③金标准对照无自动化测试（恒等式全合成数据）挂账、④性能 664 天实测 O(n) 余量足。修复：expected 播种与 SB 截断两处有意 divergence 已在 core 注释声明；RAG 红区阈值校准为 GC sbColor 的 **SB<−40**（原 −25 无出处）；挂账 #2 NGP 已按上文文档化。

## 票 08 · CS/D' 拟合 + D'bal 曲线

**交付**：core `critical_speed.go`（BestEfforts 多时长滑窗全力段 + FitCriticalSpeed 最小二乘 OLS 回归 + DBal 微分式，钳 [0,D']；恒等式：合成数据精确回收 CS=4.5/D'=180 R²≈1、噪声鲁棒、消耗速率精确 2m/s×60s=120m、恢复按剩余比例）；`cs_zones` 表（手动/fit 双来源，合法域 CS 1-12 m/s、D' 10-2000m）；`POST fit-cs`（跑步活动流聚合 → 全力段 → 拟合，R²<0.9 拒绝）；`GET …/activities/{id}/dbal`（有流+有 CS 配置）；UI CsZoneEditor（手动+拟合按钮+配速展示）+ 详情 D'bal ECharts 面积图。

### 决策
- **拟合=线性回归**：D(t)=D'+CS·t 对 (t, D) 是线性的——OLS 解析解即可，无需 LM（区别于功率域 CP 的非线性）。
- **全力段时长**：3/5/7/10/12/15 分钟（GC deriveCPParameters 挑点口径的跑步版）。
- **拟合门控**：仅跑步活动的速度流参与；骑行功率域属票 09（fit-cs 对骑行库返回 fitted=false）。
- **质量门槛**：R²<0.9 或参数非正 → 拒绝写入（UI 显示原因）；手动覆盖永远可用。
- **fixture 缺口**：现有 FIT fixture 均非跑步（big_activity=骑行、SUP=other），跑步端到端拟合靠 S1 合成数据 + S3 门控语义；待真实跑步数据导入后补一条金标准用例。

### 坑
- python heredoc 写 JSX 含全角括号+&apos; 的组合会提前终结三引号——大 JSX 块用 write 工具整文件重写，不用 heredoc。
- echarts@6 valueFormatter 回调参数需显式 `(v: number | string)`（strict 模式）。
