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

## 票 09 · 功率链 + W'bal

**交付**：core `wbal.go`（TauFromData Skiba τ 公式、WBal 前向积分、AvgPowerBelowCP、FitCPWPrime 2 参数线性化拟合——P=CP+W'/t 对 (1/t,P) OLS）；`GET …/activities/{id}/wbal` 端点（有功率流 + CS 配置时）；UI 详情页 W'bal ECharts 面积图（无功率流静默隐藏）。

### 决策
- **跑步功率域复用 cs_zones 存储**：CP↔CS、W'↔D'——票 08 决策的延续（功率域=速度域的 Watt 换算），避免第二张配置表。
- **FitCPWPrime**：GC CP2Model 的线性化（P 对 1/t 线性回归），复用票 08 OLS——再次避免 LM。
- **W'bal 前向积分**：04 文档工程优化语义（I += exp(t/τ)·(P−CP)+，消耗=exp(−t/τ)·I）；GC 逐点语义下当前秒超功率全额计入。

### 教训（对拍测试的价值）
- S1 恒等式初版用**连续解析解**对拍离散实现，差 ~92J 不匹配——根因是 GC 逐点语义（当前秒全额计入）与连续积分的固有差异。改用**朴素 O(n²) 定义对拍**（04 文档原文语义独立实现），优化实现与朴素定义逐点相等——这才是"独立真值"的正确形态。第二轮又抓出对拍实现自己的指数符号写反——**对拍测试抓的是两边，不只抓被测方**。

## 评审记录 · 票 08+09（Review Round 7）— BLOCK → 修复

两轴判定 **BLOCK**（两 P0 均为我执行走样的决策，评审抓得准）：

| 发现 | 级 | 处置 |
|---|---|---|
| **cs_zones 功率域复用单位错配**：同字段双单位（CS m/s ↔ CP W），校验域让真实 CP/W' 根本写不进，wbal 必产出 CP≈4.5W 假曲线 | P0 | **加 `domain` 列**（speed/power）+ 分域校验（speed: CS 1-12 m/s / D' 10-2000m；power: CP 100-500W / W' 4000-40000J）；`CsZoneForDate` 按域查询；S3 断言两域互不污染 |
| **票 09① 功率链指标未实现**：metrics.go 只有心率/配速链 | P0 | 新增功率链：NP（4 次方均值）+ PowerLoad（类 BikeScore = Rtss 公式复用）+ PowerTiZ（6 区）；模型加 3 列；合成流测试验证 1h@CP→PowerLoad=100、2×CP→400 |
| **FitCPWPrime 返回值对调**（斜率=W' 却赋给 cp），零调用零测试 | P1 | 修正为 `return intercept, slope` + 新增合成数据回收测试（CP=250/W'=20000 精确） |
| fit-cs 旧 fit 行遮蔽新拟合 | P1 | 拟合前删同域旧 `source=fit` 行；acts 按 start_time ASC（"最早活动日期"成立） |
| 票 09② τ 单活动覆盖未实现 | P1 | `?tau=` 查询参数覆盖 |
| hasPower 死代码 / BestEfforts 复杂度注释错 / 单测名不副实 / UI 残留文案 | P2 | 全修；`hasPower` 也加 `stream==nil` 守卫（估算路径曾 panic） |

### 关键教训
1. **"复用存储"必须带域判别**——我原以为 CP↔CS 只是单位换算，实际是两套独立物理量（速度域 vs 功率域），同表无 domain 列 = 语义污染。Round 7 用 domain 列 + 分域校验彻底分离。
2. **声明交付 ≠ 实际交付**：DEVLOG 写"功率域=Watt 换算落地"，实际 metrics 里根本没功率链、FitCPWPrime 零调用——评审逐条核对验收框才抓出。教训：勾票前对着验收框逐条跑一遍。
3. **fixture 能力边界要早发现**：big_activity 的 Power 全是 invalid（99998/99998）——早该像 tick 03 那样先探针确认数据能力，而不是写完才发现端到端测不了。功率链最终用合成流函数级测试覆盖。

## 票 04 · GPX/TCX + 批量导入 + 去重

**交付**：core `ingest/xml.go`（ParseGPX/ParseTCX 手工 encoding/xml 反序列化 + 命名空间前缀剥离 + Haversine 轨迹长度 + 距离差分推导速度）；server `parseActivityFile` 内容嗅探（`<` 开头判 XML 再分 GPX/TCX，否则 FIT）+ `POST …/import-batch`（多 file 字段，逐文件独立处理，imported/skipped/failed 汇总 + 逐文件结果）+ 单文件导入同步升级为嗅探；UI 多选上传（.fit/.gpx/.tcx）+ 批量结果清单。

### 决策
- **xml 命名空间处理**：encoding/xml 的 struct tag 无法表达"任意前缀"（`gpxtpx:TrackPointExtension`），实现 `stripNamespacePrefixes` 预处理——删标签名前缀、保留本地名。比给每个 tag 写多套变体稳。
- **格式嗅探而非扩展名**：`<` 开头 → XML（根元素含 TrainingCenterDatabase=TCX / `<gpx`=GPX）；否则 FIT。扩展名可伪造，内容不会。
- **批量语义**：单文件失败不影响其余（用户一次拖入 100 个文件，不该因 1 个坏文件全废）；重复=skipped 而非 failed。
- **GPX 无距离/功率**：距离用 Haversine 轨迹长度推导；TCX 无速度用累计距离差分推导（`deriveSpeedFromDistance`，已有速度字段则不覆盖）。

### 坑（实测抓到的真 bug）
- **爬升计算被空洞污染**：`streamElevationGain` 把 NormalizeStream 填 0 的缺测海拔当成"掉到海平面"，再回到真实海拔的跳变全计入 → GPX 2.5m 爬升算成 103m。修法：跳过 0 值点（海平面活动无法区分，可接受），加回归测试锁定。
- 命名空间剥离用逐字符扫描 + 回退路径写法略绕，但比正则可靠（避免误伤文本内容）。

## 票 11 · 地图轨迹 + 年度统计

**交付**：core `stats.go`（AnnualStats 年/月汇总 + Streaks 最长/当前连续 + DownsampleTrack 等距抽稀并跳过无效坐标）；server `GET …/activities/tracks?max=300`（只返回有 GPS 的活动）+ `GET …/stats/annual?year=`（日级聚合 + 连续天数）；UI 地图页（**Leaflet + OpenStreetMap 免 token 瓦片**，按运动类型着色折线，popup 链详情，自动 fitBounds）+ 年度统计页（5 项 KPI + 12 月柱图 + 年份切换）。

### 决策
- **地图库选 Leaflet 而非 Mapbox/MapLibre**：running_page 用 Mapbox GL + MapCN 瓦片；openrunner 自托管场景下 OSM 标准瓦片最省事（真正免 token、无第三方 SDK 体积），代价是无 3D/矢量样式——对轨迹展示足够。
- **降采样在服务端**：1000 个活动 × 每活动 1 万点的原始轨迹会撑爆前端；服务端每条抽稀到 ≤300 点（等距，保首尾），前端只画线。
- **年度统计放 core**：纯函数（日级摘要 → 年/月/连续），server 只做 SQL 聚合 → 便于 S1 精确测试（同日多次合并、跨月连续、空年份）。
- **无 GPS 活动不进地图**：手工/无轨迹活动在地图上无意义，静默排除（`len(pts) < 2` 过滤）。

### 坑
- Leaflet 的 marker 图标在打包环境常丢（vite 未处理图片资源），本次只用 polyline 避开；若后续要加 marker 需显式配置 iconUrl 或用 divIcon。
- GPX fixture 的 5 个 trkpt 全带 lat/lon（空洞只缺 hr/海拔采样）——测试预期最初写成 4 点，实测 5 点后修正（**空洞 ≠ 缺 GPS**）。

## 票 12 · 数据导出 JSON 归档

**交付**：`GET /api/athletes/{id}/export`（流式导出：schemaVersion + exportedAt + athlete + 四类配置 + activities 逐条含 metrics 与 base64 stream）；UI 档案区「导出 JSON」按钮（blob 下载带 Authorization）。

### 决策
- **流式写出**：header 先 Marshal 一次，去掉结尾 `}` 后续接 `,"activities":[`，活动用 `json.Encoder` 逐条写——大档案不驻留内存。代价是拼接略 hack，但比全量 Marshal 更符合"大数据量不超时"验收。
- **结构体而非 map 做 header**：map 的 key 顺序随机 → 导出文件无法 diff。这是验收项「可 diff/校验」的直接要求。
- **stream 用 base64**：核心 Stream 是任意长度浮点数组，base64(JSON) 保证导出仍是单一合法 JSON 文档（不用 NDJSON 分文件）。
- **空数组而非 null**：`nonNilSlice` 泛型助手——`null` 会让导入端到处判空。

### 坑
- 拼接 JSON 时最初误写了两次 header（先 `w.Write(hb)` 再写 prefix），导致 `invalid character ':' after top-level value`——**流式拼接必须想清楚"哪一段只写一次"**。

## 票 13 · 交付打磨（embed + compose + MySQL）

**交付**：MySQL 支持（`OPENRUNNER_DB_DSN` 环境变量 → `store.OpenFromEnv`；GORM mysql 驱动）；方言感知索引（SQLite partial index / MySQL 先查 information_schema）；Canonical 表名补全；CI 双库矩阵（SQLite job + MySQL service job）；Dockerfile 增加可覆盖 `GOPROXY` build arg；README 补数据库选择与首启流程。

### 真 MySQL 容器实测抓出三个 bug（SQLite 放行、MySQL 报错）
1. **`key` 是 MySQL 保留字** → `secrets.key` 的 `WHERE key = ?` 报 1064。修：列名改 `secret_key`。
2. **`CREATE INDEX IF NOT EXISTS` MySQL 不支持** → 重启时索引已存在报 1061（第二次启动才炸，更隐蔽）。修：MySQL 路径先查 `information_schema.statistics`。
3. **表名不统一** → `athlete_models`/`user_models`/`secret_models` 是 GORM 默认复数，与 spec 的 `athletes`/`users` 不符（Round 3 只修了 activity 两张表）。修：三张表补 `TableName()`。

### 决策
- **MySQL 用普通唯一索引 + NULL 语义**：MySQL 无 partial index，`content_hash` 手工活动写空串会撞唯一约束——改为手工活动不写该列（NULL），MySQL 允许多 NULL。
- **Dockerfile GOPROXY 可覆盖**：本地/国内网络构建需要（默认仍是官方 proxy，不改变默认行为）。
- **Docker Desktop 的 `network_mode: host` 不生效于端口映射** → 用 `extra_hosts: host-gateway` 连宿主 MySQL 更可靠。

### 实测
MySQL 8.4 容器：10 张表全建、setup/login/档案/活动导入全通、数据确认落在 MySQL；SQLite 全量测试绿；单二进制 24MB 内嵌 UI 正常。

## 票 14 · SVG 年度海报（可选票）

**交付**：core `RenderAnnualPoster`（纯函数生成 800×600 SVG：年份标题 + 4 个 KPI + 12 月柱图 + 页脚，沿用 Data-Dense Dashboard 配色）；`GET /api/athletes/{id}/poster.svg?year=&accent=`；UI 年度统计页「下载海报 SVG」。

### 决策
- **纯函数生成 SVG 字符串**（不引绘图库）：海报是固定版式，手写 SVG 反而比图表库可控（字体族、间距、配色全显式）。
- **XML 转义必须自己做**：`html.EscapeString` 处理档案名中的 `& < >`——否则名字带 `&` 就破坏整个 SVG（测试锁定）。
- **字体族声明中文字体**（Noto Sans SC / PingFang SC）：验收项「中文字体渲染正常」。
- **`Content-Disposition: inline`** 而非 attachment：SVG 浏览器可直接预览，用户再另存。

### 实测
端到端 6389 字节、`ET.parse` 校验为合法 XML、根元素 `{http://www.w3.org/2000/svg}svg`。

## 评审记录 · 票 11-14（Review Round 8）— BLOCK → 修复

两轴判定 **BLOCK**。P0×1 + P1×4 + P2×4，全部修复并用回归测试锁住：

| 发现 | 级 | 处置 |
|---|---|---|
| **手工活动 content_hash 写空串**：MySQL 普通唯一索引下同档案第二个手工活动必撞 1062 → 500（DEVLOG 声称改 NULL 但代码未落地——Round 7 教训复发） | **P0** | `ContentHash` 改 `*string`，空串写 NULL；`FindActivityByHash` 对空指纹直接返回；**真 MySQL 容器验证 3 个手工活动全成功** |
| `activity_streams.Data` 是 `blob`：MySQL 上限 64KB，1h@1s 多通道 JSON 超限 → 1406 | P1 | 改 `longblob`；**真 MySQL 导入 2.4MB FIT 验证** |
| `?accent` 未白名单直入 SVG 属性 → 可注入 `<script>` | P1 | `isHexColor` 校验（仅 #RGB/#RRGGBB），非法 400；注入向量测试锁定 |
| 导出对 metrics/stream 取数错误静默跳过 → 返回"合法但不完整"归档 | P1 | 改为中途失败直接断开连接（客户端据不完整 JSON 判定失败），绝不产出缺数据归档 |
| **index.html 引 Google Fonts 外链**（违反票 13「无外部静态资源依赖」） | P1 | 移除外链，改用全系统字体栈；S3 测试断言内嵌 HTML 无 `fonts.googleapis` |
| 地图端点 N+1（每活动一次 GetStream） | P2 | 批量 `WHERE activity_id IN ?` 一次取全 |
| 年度统计把计划活动计入距离/次数/连续天数 | P2 | `WHERE is_planned = false`；测试锁定 |
| Dockerfile 未 `chown /data`（非 root + 命名卷写不进） | P2 | `mkdir -p /data && chown openrunner` |
| 旧库无迁移代码 | P2 | 本次为 0.1 前唯一一次 schema 变动，**不写迁移**；README/DEVLOG 声明升级需清库重建 |

### 教训（第二次复发）
「DEVLOG 声明 ≠ 代码落地」在 Round 7 已记为教训，本轮又在 `content_hash NULL` 上复发。**根因**：写 DEVLOG 时把"决定要做"写成"已做"。改进：DEVLOG 只在**代码+测试都完成后**记录已完成项，计划项写进「挂账」段。

### 一条 python replace 静默失配
`longblob` 修改第一次没生效（缩进不同，`str.replace` 无声跳过），靠真 MySQL 测试才暴露。教训：**批量文本替换后必须 grep 验证**，别信 exit code。

### Round 8 修复后验证（全部实测通过）
- **MySQL 8.4 真容器**：3 个手工活动全成功（P0 修复生效）、2.4MB FIT 导入成功（longblob 生效）
- **Docker compose**：语法 ✓、health ✓、内嵌 HTML 零外链 ✓、**重启后 data 卷数据仍在** ✓
- 全量测试 4 个包全绿
- compose 修复：`args` 曾被误放在 service 层级（应在 build 下），已修正并加 GOPROXY 透传

## 票 15 · 云同步框架 + Garmin（首个平台）

**关键发现（改变了方案选型）**：Garmin 已推出官方 **Personal API**（2026），专为个人/hobby 项目设计，
**不需要**企业开发者账号或三脚 OAuth——用户在 Garmin Connect 网页自助生成 API Key 即可。

### 端点（核对自官方示例客户端 github.com/garmin/connect-personal-api-python）
```
POST https://personal.connectapi.garmin.com/personalapitoken-service/personal-api/personal-api-key/token
     Header: Authorization: ApiKey <personal-api-key>
     → { access_token: <JWT>, expires_in: 86400 }   # 24h，无 refresh token，用 Key 重换

GET /activitylist-service/activities/search/activities?start&limit&startDate&endDate&activityType
GET /download-service/files/activity/{id}   → ZIP（内含 .fit）
```
`download-service` 返回 **ZIP**（不是裸 FIT）——解包后复用我们已有的 FIT 解析器，零新增解析逻辑。

### 交付
- **core/garmin**：`NewClient/ExchangeToken/ListActivitiesSince（自动分页）/DownloadActivityFIT（ZIP→FIT）/TokenExpiry（JWT exp 解析）` + `AuthError/APIError` 分类
- **server**：`platform_connections`（凭证 AES-256-GCM 加密，密钥从 secrets 的 jwt_secret 经标签隔离派生）+ `sync_runs`（同步运行记录）；CRUD + `POST …/sync` + **cron 调度**（默认 6h，`OPENRUNNER_SYNC_INTERVAL` 可调、`OPENRUNNER_SYNC_ENABLED=false` 关闭）
- **同步流程**：解密凭证 → 换 token → 拉增量列表（游标或 30 天）→ 逐个下载 ZIP→FIT → 复用 `importOne`（content_hash 幂等）→ 推进游标
- **UI**：平台连接页（API Key 输入不回显、立即同步、结果统计、错误提示、指向 Garmin 生成页）

### 决策
- **凭证加密复用 JWT 根密钥但标签隔离**（`sha256(root + "openrunner/credentials/v1")`）——避免密钥复用风险又不引入第二个需要管理的密钥。
- **同步失败不中断整批**：单个活动下载失败只计入 failed（Garmin 部分活动无原始文件属正常）。
- **11 个 S2 子测试**覆盖票面要求的三路径：流数据（ZIP→FIT→入库）、summary-only（下载 404 只计数）、凭证失效（API Key 错误 → 可读中文 + 502）。

### 实测
- 凭证加密：DB 中 `credential_enc` 为密文（前 8 字节非明文），API 响应零明文泄漏
- **真实打到 Garmin 官方端点**：假 Key → 401 → 转为「API Key 无效或已过期，请在 Garmin Connect 重新生成」+ HTTP 502
- **待验证**：真实 API Key 的全链路（需用户提供，本地无法伪造有效凭证）

### 挂账
- 真实 Key 端到端验证（用户提供后执行；S2 fixture 已覆盖协议层正确性）
- `activityType` 过滤未使用（拉全类型后由 `Sport()` 映射）——量小时可接受，量大时可按运动类型分拉

## 票 15 扩展 · Garmin 中国站（garmin.cn）支持

### 调研结论（2026-10 实测）
| 项 | 国际站 | 中国站 |
|---|---|---|
| Personal API（票 15 原实现） | ✅ 可用 | ❌ **令牌交换 HTTP 526 / TOKEN_EXCHANGE_FAILED，大陆未开通** |
| 网页会话路径（running_page 做法） | 可用 | ✅ 可用 |

**关键**：中国站数据端点与 Personal API **完全同构**，只是 baseURL 与认证方式不同——
`connectapi.garmin.cn` + OAuth1 会话，路径仍是 `/activitylist-service/activities/search/activities`
与 `/download-service/files/activity/{id}`（ZIP→FIT）。所以现有 FIT 管线零改动。

### 实现（用户方案 c：先跑通，再优化体验）
1. **core/garmin/oauth1.go**：RFC 5849 HMAC-SHA1 签名 + `POST /oauth-service/oauth/exchange/user/2.0`
   用 OAuth1 token/secret 换 24h OAuth2 token。**Go 端自带续期**——用户只需一次性粘贴 OAuth1 凭据，
   之后每次同步自动换 token，无需反复跑脚本。
   - OAuth consumer 常量取自 garth 公开托管的 `oauth_consumer.json`（非用户密文）
   - 签名正确性由测试侧**独立重算比对**验证（真值对拍，非自证）
2. **server**：`platform_connections` 加 `region`（global/cn）与 `credential_kind`（api_key/oauth1）；
   同步按两者分派（Personal API Key → personal.connectapi.garmin.com；OAuth1 → connectapi.garmin.cn）
3. **scripts/garmin-cn-token.py**：用户本机跑一次（`pip install garth` + 账号密码），输出
   `oauth1_token` / `oauth1_secret` 两行，粘贴进 UI 中国站
4. **UI**：国际站/中国站切换，各自输入方式（API Key / OAuth1 对）+ 脚本用法内嵌提示

### 决策
- **不实现 Go 版账号密码登录**（方案 a）作为首选：需要 SSO 表单 + MFA 交互 + 国行 TLS 例外，
  且 SSO 流程随 Garmin 改动易失效。当前 OAuth1 方案把"易变部分"交给社区维护的 garth，Go 端只做稳定的 OAuth1 签名。
- **国行需关闭 TLS 校验**：garmin.cn 证书链不规范（garth 官方示例同样 `ssl_verify=False`）——
  仅限该域，且用户自托管场景可接受；DEVLOG 记录为已知偏差。

### 实测
- 国行凭据加密：DB 中 `credential_enc` 79 字节密文、`region=cn kind=oauth1`、零明文泄漏
- **真实请求打到 connectapi.garmin.cn**：假 OAuth1 → 401 → 「OAuth1 凭据失效，请重新运行辅助脚本获取」+ 502
- S3 覆盖：保存 OAuth1（region=cn/kind=oauth1/无明文）、同步（OAuth1→OAuth2→cn 端点→入库）、凭据失效

### TLS 例外（已实现）
`NewClientForRegion("cn", key)` 为 garmin.cn 单独启用 `InsecureSkipVerify`（证书链不规范，
garth 官方示例同样 `ssl_verify=False`）；国际域与测试注入 URL 保持严格校验。3 个测试锁定该行为。

### 挂账
- 真实 garmin.cn 账号端到端（用户跑 `scripts/garmin-cn-token.py` 后验证）

## 缺陷修复 · 用户实机验证（2026-10-25）

用真实 Garmin 中国账号跑通全链路，暴露并修复 4 个真缺陷：

| # | 缺陷 | 根因 | 修复 |
|---|---|---|---|
| 1 | 点「保存」**无任何提示**，请求根本没发出 | UI 里 `if (!id) return` **静默退出**——而用户还没创建 Athlete 档案（数据按档案隔离） | 所有编辑器缺档案时明确报错「请先在上方「Athlete 档案」创建并选择一个档案」；主页顶部加醒目警告条 |
| 2 | 粘贴带前缀的凭证（`oauth1_token = xxx`）无法保存 | 值原样存库 → 换 token 时非法 | 服务端 `cleanCredentialValue`：剥掉 `key = `、`Bearer`、引号、空白；5 用例测试 |
| 3 | 配好区间后**历史活动不重算** → 趋势页永远空白 | 只有单活动 recompute 端点 | 新增 `POST /recompute-all`；心率/配速区间保存后自动调用 |
| 4 | 趋势页**整页白屏** | ECharts 6 下 `markArea`+`dataZoom`+双 Y 轴组合触发内部异常 `__ec_inner_57` | 降级为最小配置（三折线+柱）；同时加 **ErrorBoundary** 让单页崩溃显示错误而非白屏 |

### 教训
1. **UI 静默 return 是最坏的失败模式**——用户看到的是"点了没反应"。任何前置条件不满足都必须有可见反馈。
2. **"我 curl 过 API" ≠ 页面可用**：趋势页的 ECharts 配置从未在真实浏览器验证过（之前只测了 API 返回）。**UI 必须用真实浏览器验证**（本轮用 kimi-webbridge 逐帧复现）。
3. 空白页/空白图表本身就是缺陷，必须给空状态引导（趋势页已加"请先配置心率区间"提示）。

### 待恢复（图表增强，需逐个在 echarts 6 下验证）
- SB < −40 红色过劳区（markArea）
- dataZoom 缩放
- 双 Y 轴（日负荷柱 vs 负荷曲线）
- 计划/预期虚线叠加

## 缺陷修复 · 地图底图（2026-10-25）

**现象**：地图页只有 44 条轨迹线飘在灰底上，底图空白。

**两个独立根因**：
1. **OSM 瓦片国内不可达**：`tile.openstreetmap.org` 请求 10s 超时（HTTP 000）；CartoDB raster 同样超时。
   实测高德瓦片 `webrd0{1-4}.is.autonavi.com` **0.068s 秒开**（200）。
2. **Leaflet 瓦片层从未被创建**（我的 React bug）：provider effect 声明在 map 初始化 effect **之前**，
   首次渲染时 `mapObj.current` 还是 null 直接 return；map 建好后 provider 未变化 → 不会重跑 → tiles=0。
   修法：加 `mapReady` state，map 初始化后置 true，provider effect 依赖 `[provider, mapReady]`。

**实现**：
- `ui/src/coord.ts`：WGS-84 → GCJ-02 转换（公开算法），仅在国境内偏移，境外原样。
  验证：天安门误差 0m/-6m、上海偏移量级正确、东京不偏移。
- 底图源可切换（高德 / OSM），选择存 localStorage；轨迹按底图坐标系决定是否转换
  （高德需 GCJ-02，OSM 用原始 WGS-84）。
- 参考 running_page：它用 **Mapbox GL 矢量瓦片**，无 token 时降级 CartoDB GL style
  （实测 `basemaps.cartocdn.com/gl/.../style.json` 可达 0.5s，但 raster PNG 超时）——
  我们选 Leaflet + 高德 raster：体积小得多（无需 WebGL/800KB SDK），国内直连最快。

**实测**：18 张高德瓦片全部加载、44 条轨迹渲染、首屏自动 fitBounds。

**教训**：`tiles` 元素存在但 `tileLoaded=0` 是"瓦片请求全失败"的判据——排查时先看这两个数。

### 底图方案升级（用户要求：海外对齐 running_page）
改用 **MapLibre GL**（Mapbox GL v1 开源分支，免 token）做统一渲染器，一套引擎支持两种底图：

| 底图 | 类型 | 坐标系 | 实测 |
|---|---|---|---|
| 高德（默认，国内） | raster 瓦片（自定义 style） | GCJ-02（轨迹自动转换） | 18 张瓦片全 200 |
| CartoDB（海外） | **矢量瓦片**（running_page 无 token 时的同类方案） | WGS-84（原样） | style.json 200 + vector tile 200 |

**关键实测发现**：CartoDB 的**矢量瓦片在国内可达**（`tiles.basemaps.cartocdn.com/v/.../gl/...` 200），
而它的 **raster PNG 不可达**——这解释了为什么之前测 CartoDB "超时"却与 running_page 实际可用看似矛盾：
两者用的不是同一类端点。

**移除 Leaflet**（`leaflet` + `@types/leaflet`），统一用 maplibre-gl：
- 轨迹用 GeoJSON source + line layer（`line-color: ['get','color']` 按运动类型着色）
- 点击轨迹弹 popup 链活动详情（保留原交互）
- 底图选择存 localStorage（`openrunner.mapProvider`），切换时重建 map（两种 style 结构不同）
- 底图不可达时显示提示条，轨迹仍渲染

**验证**：高德 18 瓦片 200 + 截图蓝色轨迹像素 251 个（0.07%）→ 轨迹已画上；CartoDB style+vector tile 均 200。

### 底图定案：两个底图都用 raster（MapLibre 矢量瓦片之坑）

**最终方案**：maplibre-gl **5.24**（不是 6.x），两个底图均为 **raster** 瓦片：

| 底图 | 瓦片源 | 坐标 | 实测 |
|---|---|---|---|
| 高德（默认，国内） | `webrd0{1-4}.is.autonavi.com` raster | GCJ-02（轨迹自动转换） | 33 瓦片 200、loaded=true |
| CartoDB（海外） | `basemaps.cartocdn.com/dark_all/{z}/{x}/{y}.png` raster | WGS-84 原样 | 33 瓦片 200、loaded=true |

**排除的弯路（按时间顺序）**：

1. **maplibre-gl 6.15 矢量**：报 `Worker failed to load`。用 `?worker&url` 修掉报错后，
   瓦片仍是 **0 请求、无报错**。把官方 ESM worker 拷到 `public/maplibre/` 用原生路径加载 ——
   仍然 0 请求（worker 脚本 200 加载成功，但握手不通，`dispatcher.waitForInitComplete()` 永远 pending）。
2. **降级 maplibre-gl 5.24 矢量**：同样 0 请求。**改变量只有把 raster 换回矢量** →
   说明 maplibre 的矢量瓦片 worker 在本项目 Vite 生产构建下不可用（5/6 都中招）。
3. **mapbox-gl 3.33 矢量**：唯一能跑通矢量的（8 个 .mvt 全 200、loaded=true），
   但 **v3 强制要求 Mapbox access token**（`A valid Mapbox access token is required`），
   自托管个人项目不该绑 Mapbox 账号，弃用。
4. **CartoDB raster 直接可达**：`dark_all/{z}/{x}/{y}.png` 实测 200 / 2.5KB / 0.35s
   （更早那次"超时"是临时网络波动，误导致我一开始排除了 raster 海外源）。

**结论**：raster 瓦片在主线程直接上传 WebGL 纹理，不依赖 tile worker，两端行为一致。
矢量瓦片的视觉优势（可换主题、文字随缩放重排）在个人跑步轨迹场景不值得这个稳定性代价。

**补充教训**：`map.loaded()` / `isStyleLoaded()` 返回 true **不代表底图渲染成功** ——
矢量 source 在 0 个瓦片时也会 `_loaded = true`。判断底图是否真的出来，看
**网络面板里的瓦片请求数 + 截图里是否出现底图配色**，不要只信 API 返回值。

### 底图最终方案（mapbox-gl）与关键误判教训

**改用 mapbox-gl 3.33（running_page 同款）**，替换 maplibre-gl 6：
- maplibre-gl 6 在 Vite 生产构建下 `setWorkerUrl` 指向的 worker 仍失效（报 `Worker failed to load`），
  dist 里 worker 是自包含 IIFE 但内部 worker 消息通道不通 → 矢量瓦片永不加载。
- mapbox-gl 的 worker 内联在 dist（blob URL），Vite 下开箱可用。

**底图与坐标系**：
| 底图 | 类型 | 坐标 | 验证 |
|---|---|---|---|
| 高德（默认） | raster | GCJ-02（自动转换） | loaded=true，瓦片 200 |
| CartoDB（海外，running_page 同款） | 矢量 .mvt | WGS-84 原样 | **8 个 mvt 全部 200、loaded=true** |

### ⚠️ 最重要的排查教训：后台标签页会伪装成"地图坏了"
调试时 mapbox/maplibre 始终不请求瓦片（0 个请求），极简 style 也一样，但：
- Worker 创建 + 消息往返正常（blob worker 返回 42）
- WebGL2 可用
- `.mvt` URL 用 fetch 能拿到 161KB

真相：`document.hidden === true`、`requestAnimationFrame` **完全不触发**（`rafFired: false`）——
**自动化测试的标签页在后台，Chrome 暂停 rAF**。MapLibre/Mapbox 的瓦片请求由渲染循环驱动，
所以后台标签下永不请求；而 Leaflet 用 `<img>` 加载瓦片不受影响（这解释了此前 Leaflet+高德"能用"）。

`Page.bringToFront`（webbridge 用 focus-emulation）后：`hidden: false`、`rafFired: true`，
carto 立刻请求 8 个 mvt 并渲染成功。

**结论**：以后排查 WebGL/动画类前端问题，先查 `document.hidden` 与 `requestAnimationFrame` 是否触发，
否则会把环境限制误判成代码 bug。

## Round 10：全量同步、观测 MaxHR、单底图

### 1. Garmin 首次同步只拉 30 天（真 bug）
`runPlatformSync` 的起点是 `time.Now().AddDate(0, 0, -30)` —— 新用户首次同步只能看到最近
30 天，且游标一旦推到今天，**历史数据永远不会补**。实测：61 条活动恰好落在
2026-09-25 ~ 2026-10-25 的 30 天窗口内。

修复：
- 首次同步（无游标）与 `?full=1` 都从 `fullSyncStart = 2010-01-01` 拉全量。
- UI 加「全量同步历史」按钮（忽略游标，重拉全部历史）。
- 实测全量：imported 436 / skipped 61 / failed 0 → 共 497 条。

### 2. 观测 MaxHR（从数据反推，而不是让用户猜）
- `core.PeakHeartRate`：剔除 ≤0 缺测与 >230 传感器噪声后取峰值（单测覆盖三类边界）。
- `store.ObservedMaxHeartRate`：扫该 Athlete 全部活动的逐秒心率通道，返回峰值 +
  样本数 + 峰值来源活动；单条 stream JSON 损坏不影响整体统计。
- `GET /api/athletes/{id}/hr-observed` + 心率区间编辑器显示建议值与「用此值」按钮。
- **第一版结论错了，必须记住这个教训**：只看「单点最大值」得到 207，我甚至用
  「连续 4 个点 205→207→205」论证它是真实冲刺 —— 但那次活动是 **0.2km 的「other」**
  （力量训练类），采样间隔 4 秒、心率有效率仅 **29.9%**，207 是光电腕表在腕部肌肉
  受压时的典型伪影。单点峰值加「≥峰-2 的点数」根本区分不了伪影与真实冲刺。
- 修正版 `core.ObserveHeartRate`：**数据质量（有效率 ≥80%）+ 持续 30 秒滚动均值**
  双重判据，低质量活动整条剔除。结果：采信 281 次、剔除 216 次，
  **持续峰值 181.4 / 峰值 186**（186 来自 11.3km 跑步，有效率 99.6%）。
  即用户原来填的 180 本来就很准，207 是我引入的错误值。
- 三种算法交叉验证（单点峰值@有效率≥80% = 186、30 秒窗口 = 181.4、60 秒窗口 = 180.9）
  结果收敛，可信。MaxHR 最终定 186，TRIMP 全部重算。
- **通用教训**：从可穿戴数据反推生理参数时，「最大值」类统计必须配数据质量门槛，
  尤其是光电心率。先看样本有效率与峰值持续时长，再谈结论。

### 3. 重复心率区间导致改了 MaxHR 不生效（P1）
库里存在两条日期完全重叠的 hr_zone（Max 183 / Max 180），取用规则 `valid_from DESC, id DESC`
只挑 id 大的那条 → 用户把 MaxHR 改成 207 后 `recompute-all` 返回 **computed: 0**（值没变）。
修复：`UpsertHrZone` 在 id 为空时先按 `athlete_id + valid_from + valid_to` 查重，
存在则更新同一行（另有 API 级测试覆盖）。清理历史重复数据后重算 → computed 497。

### 4. 删掉海外底图，只留高德
CartoDB 无论矢量还是 raster 在国内网络都不稳定（矢量瓦片 worker 在 Vite 产物下静默失效，
raster 时通时不通）。个人自托管跑步仪表盘的定位本就在国内，去掉这个随时会挂的选项：
`coord.ts` 只保留 `amapStyle`，地图页移除底图选择器，实测 33 个高德瓦片全 200。

### 5. 地图改成「列表 + 单条聚焦」，活动可跳转到地图
原实现把所有轨迹 fitBounds 到一张图：323 条 GPS 轨迹叠在一起，上海缩成一小块，
线糊成一团 —— 完全看不出「哪天在哪儿跑的」。

改为列表驱动：
- 左侧活动列表（可搜索日期/距离、按跑步/骑行/全部筛选，带配速），默认聚焦最新一条。
- 选中轨迹亮橙加粗，其余压淡到 15% 透明度作背景（能看出「别的日子也在这附近跑」）。
- URL `?activity=ID` 可直接定位（Home 列表与活动详情页都有「地图 ↗」入口）。
- 点地图上的轨迹也能反选列表项。

**踩到的时序坑**：`fitBounds` 初始不生效（zoom 卡在 11），但手动点列表项却正常。
原因是 `map.once('load', fn)` —— style 数据到达与 `isStyleLoaded()` 转真之间有窗口，
若注册时 `load` 事件已经触发过，回调**永远不会执行**。改成 `whenStyleReady` 短轮询
（最多 5 秒）后正常。以后凡是要「等地图能画了再做」，别用 `once('load')`。

### 6. 区分 hasStream 与 hasGps（否则地图链接是死链）
活动列表给 497 条全加了「地图 ↗」，但只有 323 条真的有 GPS —— 其余 174 条点进去
是空白（心率/配速流也算 Stream，但画不出轨迹）。
新增 `activities.has_gps` 列：导入时按「至少 2 个有效坐标」判定
（`core.IsValidCoordinate` 同时抽取复用，`DownsampleTrack` 改为调它），
一次性回填存量数据（497 条 → 323 条有 GPS，与轨迹接口一致），
UI 改用 `hasGps` 决定是否显示地图入口。

### 7. 地图弹层白底白字 + 又一个 `isStyleLoaded()` 坑

**弹层不可见**：MapLibre 自带 `.maplibregl-popup-content { background: #fff }`，但没有
设定文字色（继承）。深色主题下就成「白底 + 近白色文字」，完全看不见。
修法有两层坑：
- 首次覆盖无效 —— maplibre-gl.css 由组件内 `import`，打包顺序在 index.css **之后**，
  同特异性规则被它盖掉。改用 `.maplibregl-map .maplibregl-popup-content` 提权才生效。
- 弹层三角 `.maplibregl-popup-tip` 也要跟着设 border-*-color，否则深色卡片下挂白三角。

**更值得记的**：`isStyleLoaded()` 并不只表示「style 已解析」——
**任何 source 正在加载瓦片时它也返回 false**。本项目高德 raster 一直在拉瓦片，
于是 `whenStyleReady()` 轮询 6 秒全为假，直接放弃 → 初始选中既不 `fitBounds`
也不弹 popup（手动点列表项却正常，因为那时 style 恰好空闲，是**偶然**成功，
连续刷新三次能复现失败）。

最终把 waiting 逻辑换成「直接试着做，失败就重试」：
- `fitBounds` / `popup.addTo` **不需要等 style**，直接调用；
- 只有 `addSource`/`addLayer`/`setPaintProperty` 需要图层存在 → `whenReady(fn)`
  在 fn 返回 false 时每 100ms 重试（fn 写成幂等：source 已存在就只 setData）。
改完连续三次加载均稳定（zoom 14.28 / 高亮正确 / 弹层深色）。

### 8. 心率区间「保存没用」= 表单从不回填，保存把真实值覆盖成默认值

用户反馈"心率区间保存没有用"。实际比"没用"更糟：**保存把正确数据改坏了**。

根因：`HrZoneEditor` 的 `form` 用硬编码默认值初始化（`maxHr: 190, restHr: 55, ltHr: 170`），
而 `refresh()` 只更新下方展示用的 `zones` 列表，**从不把已有区间回填到表单**。
于是页面呈现成：「列表写着 Max <占位> / 表单里是 190」——用户以为自己在微调真实参数，
一点保存就把库里观测得到的 186 覆盖成默认值 190（实测库里就是这样被改成 190 的）。

修复：
- `zones` 变化时把**当前生效区间**（`validFrom <= 今天 <= validTo`，否则取最新一条）
  回填到表单；不回填 `id`，保持「同日期区间=更新、换区间=新增」的语义。
- 保存成功后给 `✓ 已保存，历史活动指标正在后台重算` 的可见反馈（3 秒消失）——
  之前保存完全静默，用户无法判断是否生效。

验证：改 MaxHR → 保存 → DB 立刻更新 → `recompute-all` 自动触发
（TRIMP 总和 29707.0 → 29340.6，改 RestHR 55→56 后重算可见）。

**通用教训**：编辑表单必须从「已经持久化的值」初始化，不能用常量兜底；
否则「保存」会变成一次静默的数据回退。

## Round 11：补齐 running_page 的主页信息密度

对照 running_page 源码（`src/themes/dashboard`）核对功能，先补最标志性的三件：

### 1. `core.FastestForDistance` — 真正的「个人最佳」
running_page 的 `PersonalBest` 是「选出总距离落在 4.8~5.5km 的活动，取它的总时长」——
只能发现「整条活动恰好是 5K」的情况，10K 跑里的前 5K 快段被浪费。
这里改为在**活动内部**用双指针滑动窗口求「覆盖目标距离的最短时间」（O(n)，
累计距离单调不减所以可滑动）。实测产出：1K 4:11 / 5K 22:39 / 10K 51:50 / 半马 1:55:19。

### 2. `GET /api/athletes/{id}/dashboard` — 首页一次拿全
逐日摘要（热力图数据源）+ 全部/年/月/周合计（距离/次数/时长/爬升）+ 运动类型分布。
月/周只在看当前年时计算（看历史年份时给 0，避免语义模糊）。
计划活动不计入统计。

### 3. 前端 `DashboardPanel`
- 统计卡片（年/月/周 各 4 项）
- **训练日历热力图**：53 周 × 7 天，按当日距离分 5 档着色，月份/星期标签，
  hover 显示「日期 · 距离 · 次数」，年份可切换。
- **个人最佳卡**：5 个距离的时间 + 配速 + 跳转原活动。
个人最佳扫描全部速度流较慢，独立加载且不阻塞活动列表。

### 与 running_page 仍存的差距（按价值排序，未做）
1. 中国地图省份热力（`ChinaMap`）
2. 头像/昵称资料卡（`ProfileCard`）
3. 月历组件（`CalendarWidget`）
4. 暗/亮主题切换、中英双语、公制/英制
5. 分享卡片图片导出（`exportCard`）
6. 多轨迹聚类（`clusterTracks.worker`）
7. 多平台同步：running_page 支持约 20 个（Garmin/Strava/Nike/COROS/iGPSPORT/
   Joyrun/Komoot/Onelap/Intervals.icu/KEEP…），openrunner 目前只有 Garmin

## Round 12：补齐 running_page 第 1~7 项

按 running_page 源码逐项实现（`src/themes/dashboard` + `src/components/*`）。

### 1. 省份热力地图（ChinaMap）
- `GET /api/athletes/{id}/track-centers` 返回每条 GPS 活动的**代表点**（取轨迹中间
  的有效坐标，起点常在楼宇间隙）。只回一个点而非整条轨迹 —— 323 条完整坐标约 10 万点，
  首页没必要。
- 前端复用 running_page 的 `china-provinces.json`（35 个省级要素，动态 import 成独立 chunk），
  equirectangular 投影铺到 SVG，活动点用**射线法 + 外接矩形粗筛**做点在多边形内判断。
- 实测：323 条活动归属 **6 个省级行政区**（上海 310/1949km、江苏 5、浙江 3、河北 2、湖北 2、天津 1）。

### 2. 个人资料卡（ProfileCard）
- Athlete 新增 `avatar_url` / `bio` 字段（可选，留空用首字母头像）。
- 更新接口用**指针字段**接收：请求体没传这两个键时保持原值（旧客户端不会清空头像）。
- 卡片展示累计距离/次数/时间/本年活跃天数/GPS 轨迹数。

### 3. 月历（CalendarWidget）
按月铺 7 列网格，有活动的日子按距离分档着色并显示公里数，可翻月、点某天看当天摘要。

### 4. 暗/亮主题切换
Tailwind v4 的 `@theme` 变量输出在 `:root`；用 `html[data-theme='light']`（特异性更高）
覆盖同名变量，所有 `bg-card` / `text-foreground` 工具类自动跟随。主题写 localStorage，
首屏跟随系统偏好。

### 5. 公制/英制 + 中英双语
- `units.tsx`：UnitProvider + `formatDistance/formatPace/formatElevation/formatSpeed`。
  换算实测正确：2266km → **1408 mi**，4:11/km → **6:44/mi**，4362m → **14311 ft**。
- `i18n.tsx`：LocaleProvider + 字典，已覆盖**导航 + 首页概览全部文案**。
- **挂账**：设置页/活动详情/趋势/地图页的文案仍是中文（仅首屏双语）。
  完整迁移需逐文件改几百处，风险与收益不匹配，留作后续。

### 6. 分享卡片（exportCard）
构造 1080×1080 SVG（统计四项 + 全年热力图）→ Image → canvas → PNG 下载。
不引入 html2canvas 等依赖，导出结果不受页面样式影响。实测生成 `openrunner-2026.png`。

### 7. 轨迹聚类
- 网格聚类（按当前缩放自适应格子大小：z≥14 用 0.002°，z<10 用 0.15°），
  圆点半径随簇内轨迹数插值。
- **不用 worker**：running_page 用 `clusterTracks.worker.ts` 是因为要对完整轨迹点聚类
  （几十万点）；这里只对**每条轨迹一个代表点**聚类（323 个点），同步计算几毫秒。
- 实测 175 条跑步聚成 **56 个簇**；聚合模式下单条轨迹线淡化到 5%，点击簇缩放到该区域。

### 仍未做（与 running_page 的剩余差距）
- 多平台同步：running_page 约 20 个源，openrunner 只有 Garmin（票 16）
- 设置/详情/趋势等页面的 i18n 全量迁移

## Round 13：地图页签换成「轨迹墙」（对齐 running_page 的 TracksPage）

导航第 3 项从「地图」改为「轨迹墙」。核心不是地图，而是 running_page 那面**轨迹缩略图墙**：

- 每条路线一个 **80×80 SVG 缩略图**：把该活动的坐标等比归一化到画布内
  （running_page 用平台预计算的 `summary_polyline`，我们存的是完整坐标流，
  所以前端归一化）。
- **同路线聚类**：起点终点都在 0.005°（约 550m）内且距离落在同一 2km 桶 → 合并，
  角标显示 `×N`（与 running_page 的 `clusterTracks.worker` 同一判据，
  但 323 条的量级同步算即可，不引入 worker）。
  实测 **323 次活动聚成 61 条路线**，最高一条跑了 **×64** 次。
- **配色**：跑步 ≥20km 红 `#ef4444`、其余跑步橙 `#f97316`、非跑步青 `#22d3ee`（同 running_page）。
- 左栏 sticky：统计卡（活动数/距离/时间/均配速）+ 选中详情卡 + 小地图（maplibre + 高德）。
- 右栏：年份 pills + 距离档（10/20/40km+）+ 排序（日期/距离）+ 缩略图网格。
- 点击缩略图 → 左栏详情 + 小地图 flyTo；再点取消。
- `/map?activity=ID` 仍可定位（首页/详情页的「地图 ↗」链接继续有效）。

**保留但未挂载**：原 `MapStats.tsx` 的 `MapView`（单条聚焦 + 聚合分布）不再挂在路由上。
它的聚合视图与轨迹墙的缩略图墙互补，先保留并在文件头注明；确认不用可整体删除。

## Round 14：只收录跑步与骑行

产品定位是跑步/骑行训练分析：游泳、力量训练、球类既没有可比的速度或功率语义，
也会污染 PMC、个人最佳、配速统计。因此从**所有导入路径**过滤掉其余类型。

### 三层防线（防止规则在 handler 间漂移）
1. `core.IsCollectedSport`（只 run/ride）—— 单一事实来源，带单测。
2. **store 层守卫**：`CreateActivityWithStream` 开头校验，返回 `ErrSportNotCollected`。
   这是关键：只加在 handler 里会漏（实测单文件导入接口就没走 `importOne`）。
3. 各调用点把该错误转成用户能看懂的结果：
   - 单文件导入 → **422**「只收录跑步与骑行（该文件为 swim）」
   - 批量导入 / 云同步 → `skipped` + 同一条原因
   - 云同步还会在**下载 FIT 前**按 `garmin.Activity.Sport()` 预过滤，省带宽。

### 存量数据清理
- 删除 293 条非跑步/骑行活动（468 km）及其 stream / metrics。
- 清理后：**204 条**（run 203 / 1782 km，ride 1 / 16.3 km），176 天有活动。
- 首页概览从 2266 km·497 次 → **1798 km·204 次**。

### 测试
`TestOnlyRunAndRideCollected`：单文件导入游泳返回 422 且不落库；批量导入返回
`skipped` 并说明原因。

## Round 15：「年度」页签改为「汇总」（对齐 running_page 的 SummaryPage）

导航第 4 项从「年度」改为「汇总」，页面按 running_page 的周期卡片模型重写。

### 结构
- 顶部：运动类型 / 年份 筛选
- 周期切换：**年 / 月 / 周 / 日 / Life**
- 每个周期一张卡片：8 项指标（活动次数、运动时间、平均配速、平均心率、
  最远距离、最快配速、平均距离、累计爬升）+ 按周期粒度分桶的柱状图
  （年→12 月、月→逐日、周→周一至周日、日→逐小时、Life→按年份）
- 日模式下列出当天活动，可点进详情；每 12 个周期「加载更多」

### 与 running_page 的差异
柱状图**自绘 SVG**，不用 recharts：项目里 ECharts 6 在最小配置外会崩
（见 Round 9），一张 12~31 根柱子的图不值得再引入第三个图表库。

### 顺手修掉两个数据问题
1. **`activities.avg_hr` 从票 03 起就没被填充过**（ingest 从未解析它），
   导致汇总页「平均心率」全是 —。改为在 `store.CreateActivityWithStream` 里
   用心率通道补均值（所有导入路径统一），并一次性回填存量 204 条（均值 142 bpm）。
2. **「最快配速」被骑行污染**：骑行的 min/km 能低到 3:5x，混入后显示 3:50/km。
   改为只统计 `sport='run'` 的活动（配速概念只对跑步有意义）。
3. 周柱状图顺序：中文「一二三…」不能交给 `localeCompare`（会排成「二六日三四五一」），
   改为固定按周一至周日输出。

### 汇总柱状图的悬浮提示
原生 `<title>` 有 1 秒延迟且样式不可控，改为自绘浮层：hover 柱子显示该时段的
「完整日期 · 距离 · 次数 · 时长 · 配速」（配速只在该桶里有跑步活动时显示，
避免骑行把 min/km 拉成 3:5x）。空桶不弹提示。
锚点用柱子的水平中心而不是鼠标位置，避免浮层随鼠标抖动。

## Round 16：配置项迁到独立设置页，入口为右上角齿轮

首页原本把「档案管理 + 导入 + 平台连接 + 三类区间 + 手工录入」全堆在活动列表前面，
一进首页先看到一堆表单，真正想看的「跑了多少、哪条轨迹」要往下翻。

### 拆分
- **首页（/）只留看数据**：概览（热力图/资料卡/个人最佳/足迹地图/月历）+ 活动列表（含筛选）。
- **设置（/settings，新路由）**：Athlete 档案（切换/新建/性别/头像/简介/导出 JSON）、
  导入 FIT/GPX/TCX、云平台连接、心率区间、阈值配速、CS/D'、手工录入计划活动。
- **入口**：导航右上角齿轮图标（`aria-label="设置"`），紧邻语言/单位/主题切换。
- 首页保留两个轻量指引：无档案时的警告条（链接到设置）、活动列表上方一行
  「需要导入/配置？都在设置 ⚙」。

### 状态同步
档案切换发生在设置页（写 localStorage + 通知 App 刷新 athleteList）；
首页 `useEffect` 依赖 `athleteList`，路由切回时自动重新拉取该档案的活动，
不需要额外的跨页通知机制。

## Round 17：跑量目标卡（对齐 running_page 的 StatsCards）

### 数据模型
Athlete 新增 `yearly_goal_km` / `monthly_goal_km` / `weekly_goal_km`（0 = 未设置）。
更新接口用指针字段接收，负数钳到 0；只在请求体出现时才改（旧客户端不会清空）。

### 设置页
Athlete 档案行里加「年 / 月 / 周」三个数字输入 + 「保存资料与目标」按钮
（与头像、简介一起提交）。

### 首页 `GoalCards`
四张卡，与 running_page 的 StatsCards 对齐：
1. **年度目标**：已达成/目标 + 进度条 + 次数/时长 + **vs 去年同期**（同期算法：
   去年同期第 1 天到今天相同的 day-of-year）
2. **月度目标**：+ **vs 上月同期**（上月 1 号到上月同一天）
3. **周目标**：+ **vs 上周同期**（上周一到上周同一天）
4. **连续记录**：连续天数（🔥）/ 连续周数 + 本周 7 天圆点（hover 显示当天距离）
   + 史上最长（天 / 周）

全部在前端用活动列表现算（与 running_page 一致，它也是拿全量 activities 算）。
**计划活动（`isPlanned`）被排除** —— 还没跑的不能算进目标完成度。

### 又清理了一批测试残留
验证导入功能时用 curl 上传的 3 个测试 FIT 残留在了真实库里（507-509，今天日期）。
已删。教训：集成验证要在临时数据目录或测试 DB 上做，别对着 `~/.openrunner` 灌数据。

## Round 18：设置页保存反馈 + 目标标注 + **云同步去重失效（P0）**

### 用户反馈的两点（已修）
1. **「保存资料与目标」没有任何反馈**：加 `savingProfile` / `saveMsg` 状态，
   成功显示「✓ 已保存」（3 秒消失），失败显示红字错误；按钮在保存中变「保存中…」并禁用。
2. **年/月/周输入看不出是什么**：重排成两个带标题的块 ——
   「个人资料」（头像 URL / 简介）和「跑量目标」（说明：首页目标卡的进度条用这些值），
   每个输入带完整标签「年度目标 / 月度目标 / 周目标」+ 右侧 `km` 单位 +
   脚注「填 0 表示不设该目标」。

### P0：Garmin 云同步去重失效，数据会无限膨胀
排查「活动数从 204 变 207」时发现 `sync_runs` 在反复 `imported 3`：

```
09:01:43  imported 3
08:35:32  skipped 3
08:07:10  imported 3
07:59:32  imported 3
```

根因：**同一活动每次下载的 FIT 字节并不相同**，`content_hash`（sha256 文件内容）
每次都变 → 去重失效 → 同一活动被反复插入。删除后下次同步又补回来（这正是我
误判为「测试脏数据」并删掉用户今天早上 3 次真实跑步的原因）。

修复：新增 `store.FindActivityByStartAndSport`，同步循环里先用
**「开始时间 + 运动类型」**业务键查重（对同一活动稳定），命中则 `skipped`。
验证：同一次同步从 `imported 3` 变为 **`imported 0 / skipped 3`**，活动数稳定不再增长。

> 教训：判断「脏数据」不能只看「今天的时间戳 + import_fit 来源」——
> 云同步来的活动也是这个组合。要查 `sync_runs` 与 `content_hash` 是否变化。

### 目标联动推算
改任一目标时按比例自动填另外两项：`年 = 月×12 = 周×52`。
只填一个就够，不用心算；实测 年 2600 → 月 217 / 周 50，周 60 → 年 3120 / 月 260。
表格下方写明规则，避免「为什么另外两个也变了」的困惑。

## Round 19：列表分页 / 详情页步频步幅 / 单位全站统一

### 1. 活动列表分页（每页 100）
207 条全量渲染明显掉帧。加 `PAGE_SIZE=100` + 分页条（首页/上一页/页码/下一页/末页，
页码多于 7 页时中间折叠成 …）。筛选条件变化自动回到第 1 页；计数显示
「命中 / 总数 条 · 第 N/M 页」。

### 2. 详情页步频 / 步幅 / 总步数 / 最大步频
- `core.SummarizeStream`：**FIT 的 cadence 对跑步是单脚值**（实测均值 90），
  步频要 ×2 才是 Garmin Connect 那种 steps/min（180 spm）。单测覆盖了换算与缺测点。
- 步幅用 **总距离 ÷ 总步数**，不是「逐点 速度/步频 再平均」—— 后者会被停顿点拉偏，
  且与 Garmin 的口径一致。实测活动 #1：181 spm / 0.88 m / 5725 步。
- 详情接口新增 `stats` 字段（从 Stream 现算，不入库；无 cadence 时省略）。

### 3. 配速/距离/爬升单位全站统一
此前只有概览、汇总、轨迹墙用了 `useUnit`，**活动列表、详情页、月历格子**还是硬编码
km/m。现在全部走 `formatDistance / formatElevation / formatPace / distanceValue`：

| 页面 | 公制 | 英制 |
|---|---|---|
| 列表行 | 7.63 km / 3 m | 4.74 mi / 3 ft |
| 详情 | 5.06 km / 6:57/km / 0.88 m | 3.14 mi / 11:11/mi / 2.90 ft |
| 月历格子 | 7 | 4 |

（`MapStats.tsx` 里还有两处硬编码，但那个文件的路由已改到 TracksWall/SummaryPage，
不再挂载，属于死代码。）

## Round 20：训练日历对齐 running_page 的活动热力图

### 后端
`/dashboard` 的 `daily` 从「只返回选定年份」改为**返回全部年份**，并新增
`runMeters` / `rideMeters`（按运动分列距离）—— 热力图要按类型配色，
「全部」模式还要多年并排。

### 前端（ContributionHeatmap 重写）
1. **年份筛选**：从下拉改成 **年份 pills**，并新增「全部年份」—— 选中后
   **多年并排**（最多 10 年，每年一块网格 + 各自的「N 天 · 距离 · 次数」）。
2. **按运动类型配色**（对齐 running_page 的 TYPE_PALETTES）：
   - 跑步：`#fed7aa → #fb923c → #f97316 → #ea580c`
   - 骑行：`#bfdbfe → #60a5fa → #3b82f6 → #2563eb`
   强度按「当日该类型距离 ÷ 全期该类型单日最大」分 4 档。
   实测分布 23 / 106 / 31 / 16（跑步）+ 1（骑行那条 16.3km）。
3. **图例**：少→多色阶 + 「跑步 / 骑行」色块。
4. 选具体年份时通过 `onYearChange` 同步父级，驱动上方的年/月/周合计。

### 年份很多时的处理（修正）
初版写的是 `years.slice(0, 10)` —— **硬截断 10 年**，更早的年份会静默消失，
这是错的（截断没有任何提示，用户无从发现）。

修正为：**「全部」模式渲染所有年份**。本页是纵向堆叠（`space-y-4`），没有横向宽度
限制，不需要 running_page 那种 10 年一窗口的翻页。年份 pills 放在
`overflow-x-auto` 容器里横向滚动，不会因年份多而挤爆换行。

实测（临时库造 2018–2026 共 9 年）：pills 10 个（全部 + 9 年）、
「全部」渲染 9 块网格共 3287 格、区域高 1976px，页面滚动浏览正常。
默认选中最新年份（避免一进来就渲染十几年）。

### 月历点击某天的明细（回应用户疑问）
用户发现月历里 2026-09-12 显示 28.56 km，比预期多 —— 因为那天有**两条**活动
（06:05 跑步 12.26 km + 15:26 骑行 16.30 km），月历格子显示的是**当日全部活动合计**。

改进：
- hover 提示拆到分类型：`2026-09-12 · 跑步 12.3 km · 骑行 16.3 km · 共 28.56 km · 2 次`
- 点击某天后展开明细卡：合计 + 分类型小计 + **当天每条活动**（时间/类型/距离，
  可点进详情，按时间升序）。
- 明细数据来自 Home 已有的活动列表（`DashboardPanel` 透传 `activities`），不新增请求。

### 月历格子加单位后缀
格子里的距离后面加 `K`（公制）/ `M`（英制，mi 的简写）。同时修掉一个取整坑：
不足 0.5 km 的日子原本 `toFixed(0)` 会显示成误导性的 **`0K`**（实测 9/5 是 0.4km）——
改为「整数不显示小数、非整数且 <10 保留一位」：`14K / 7K / 8.7K / 0.4K`。

### 训练负荷指标加悬浮说明
详情页的 TRIMP / TRIMP(100) / TRIMP Zonal / rTSS 四个指标各加了一个 ⓘ 图标，
hover 弹出解释（纯 CSS `group-hover`，无 JS 状态）。说明文字严格按代码口径写：
- TRIMP：Banister，`时长 × HRr × 0.64 × e^(ksex·HRr)`，ksex 男 1.92 / 女 1.67。
- TRIMP(100)：归一化到「阈值心率跑 1 小时 = 100 分」。
- TRIMP Zonal：Edwards 五区，分界 64/77/84/90/95% MaxHR，系数 0.84/1.65/2.57/4.01/5.91。
- rTSS：`100 × 时长 × (NGP ÷ 阈值配速)² / 1h`，阈值配速跑 1 小时 = 100 分。

细节：`dl` 原本有 `overflow-hidden`（为了圆角裁剪子项背景），会把浮层裁掉；
去掉后浮层可溢出显示，视觉上圆角几乎无差别。

## Round 21：指标通俗化 + **NP 恒为 0 的 bug**

### P1：NP 被错误地绑在 CP 配置上
详情页的 NP / 功率负荷一直是空的，但实测 **206 条活动有功率流**。原因：

```go
if hasPower {
    if pz, err := CsZoneForDate(db, id, "power", date); err == nil {  // 没有 power 域 CP
        np := ...; m.Np = np                                       // 整段被跳过
```

NP（标准化功率）只依赖功率流本身（30 秒滑动平均的四次方均值），**不需要 CP**；
只有 PowerLoad / PowerTiZ（IF = NP/CP、按 %CP 分区的时长）才需要。
已拆分：有功率流就写 NP；CP 存在时才算 PowerLoad / PowerTiZ。
重算后 206 条有 NP（均值 290 W，最大 457 W）。

### 指标说明改成通俗版（含例子）
tooltip 从公式改写为「一句话含义 + 举例 + 你该怎么读」，并在 UI 上区分三种空值：
- 无功率数据 → `—（无功率数据）`
- 有 NP 但没配 CP → `—（需配置功率 CP）`
- 正常 → 数值

### 教训
「把两个独立的计算绑在同一个 if 里」是这类 bug 的典型来源 ——
NP 不依赖 CP，只是因为两者都属「功率链」就被放在同一个条件分支里，
结果配置缺失时把能算的 NP 也一起吞了。判断条件应当精确到依赖项。

## Round 22：趋势页说明 + 赛季说明 + **PMC 图被未来 365 天压扁**

### 1. 「怎么看这张图」说明卡
趋势页顶部加默认展开的折叠面板：三条线的含义（LTS 42 天/STS 7 天/SB=差值）、
SB 读数对照表（>+10 适合比赛 / −10~+10 正常训练 / −30~−10 疲劳累积 / <−30 过劳）、
五种典型曲线形态，以及「三条线是相对值，只跟自己历史比」的提醒。

### 2. 赛季功能说明
折叠面板解释为什么需要它：PMC 是递归计算（`今天体能 = 昨天体能 × 衰减 + 今天负荷`），
必须有起点；从 0 起算会让前 42 天曲线偏低（看起来像新手）。
列出三种需要填的场景（换设备/断档/分赛季），并明确告知「你的数据连续，不需要配」。

### 3. 修：图被延伸到未来 365 天压扁
后端按 GoldenCheetah 口径把序列算到「今天 + 365 天」（GC 用来预演未来计划）。
但没配计划活动时这段全是 0 —— 实测 633 个点里只有前 267 天是真实数据，
曲线被挤在图的左侧 42%，右边一片贴地。前端改为**截断到今天**再渲染
（保留 planned/expected 序列，将来有未来计划时再放开）。

### 训练日历补全到 running_page 的展示内容
对照 running_page `ContributionHeatmap` 的渲染部分，补齐了四项：
1. **点击某天 → 当天活动面板**：列出 `时间 · 类型 · 距离`，每条可点进详情，带关闭按钮。
   格子从 `<span>` 改成 `<button>`（aria-label 描述当天内容），选中格加 accent 描边。
2. **单年统计行**：`N 次 · 距离 · 时长`（原来只有「N 天有活动」）。
3. **「全部」模式的年标签**：每年网格上方 `2026 · 207 次 · 1812 km`。
4. 图例保留「少→多 + 跑步/骑行」两行（running_page 按模式切换两种图例，
   这里合并展示，信息量等价）。

实测：点 9/12 → 面板显示 `06:05 · 跑步 · 12.3 km` / `15:26 · 骑行 · 16.3 km`；
底部 `2026 年汇总 | 207 次 | 1812 km | 182h`。

### 修：训练日历卡片底部一大片空白
现象：热力图下方的框线到卡片底边有 ~286px 空白。

原因不在热力图本身，而是外层 `grid` 的默认 `align-items: stretch` ——
右栏（资料卡 + 个人最佳）比热力图高得多（596px vs 310px），
grid 把热力图**拉伸**到与右栏等高，多出来的部分就是空白。

修复：给概览区的三个 grid 加 `items-start`（卡片按自身内容定高）。
实测热力图卡片 596px → **329px**，底部空白 286px → 19px（仅剩 padding）。
同类问题在「资料卡/个人最佳」列与「足迹地图/月历」那行也一并处理了。

## Round 24：多距离成绩预测（5K / 10K / 半马 / 全马）—— 先调研再实现

### 调研结果（权威算法与软件）

| 方案 | 公式 / 原理 | 评价 |
|---|---|---|
| **Riegel 1977** | `t₂ = t₁·(d₂/d₁)^1.06` | 最经典；约 80% 准确；**固定指数对长距离偏乐观**；仅保证 3.5–230 分钟 |
| **Cameron 1998** | Riegel + 与距离相关的修正 | 长距离略准（有说法称 1–3%），但增益只见于计算器网站、无同行评审对比 → **不采用** |
| **个人化幂律** | `log t = α·log d + log c` 对本人成绩回归 | **最可靠**：业余跑者 α 常 1.08–1.15（比 1.06 掉速快）→ **采用** |
| Garmin Race Predictor | VO₂max → 各距离等价 | 理论值，假设完美条件 |
| Strava Performance Prediction | ML，100+ 特征，需 24 周 ≥5 次跑步 | 无法在自托管小数据量上复现 |

参考：Vickers & Vertosick 2016（休闲跑者 N=2303，年龄/性别/BMI/训练量影响）、
Blythe & Király 2016 PLOS ONE（低秩 log-time 模型）。

### 实现
1. `core.FitRiegelIndex`：用本人多距离成绩做 log-log 回归得个人 α；
   同距离只取最快一条（同距离多条对 α 无信息量）；钳到 [1.02, 1.20]
   （超出说明数据异常，例如把骑行当跑步录）。
2. `core.ProjectRaceTimes`：`t_d = t_10k · (d/10km)^α`。
3. 基准 = **Banister 模型给出的「今天」等价 10K 表现** —— 所以预测回答的是
   「如果今天比赛，各距离能跑多少」，会随训练负荷/疲劳每天变化。

### 可信度分级（不是无脑外推）
- 目标距离 ≤ 本人最长成绩 → **可信**（跑过，不算外推）
- ≤ 1.5 倍最长成绩 → 可信（略超，可参考）
- 更远 / 相对 10K 超 3 倍 → **不可信**，并说明原因
- 全马额外提示「需要足够的专项长距离训练，否则真实成绩通常比预测慢 5–15%」
  （Cameron 的提醒）

实测（用 5K/10K/半马三条成绩）：个人 α=1.136，R²=0.9999 →
5K 22:52 / 10K 50:14 / 半马 1:57:15 可信，全马 4:17:37 带警告。

## Round 25：比赛成绩排序的两个 bug

用户反馈「设置里的比赛成绩排序有问题」「预测的图排序也不对」。查下来是两个独立缺陷：

### ① 设置页默认日期用 UTC（真 bug）
```tsx
useState(() => new Date().toISOString().slice(0, 10))   // ← UTC 日期
```
东八区凌晨 0–8 点，`toISOString()` 还停在**昨天** → 录入的成绩日期差一天 → 列表顺序看起来就是乱的。
改为本地日期拼接。

### ② 预测图数据倒序（真 bug）
后端 `ListRaceResults` 按 `date DESC` 返回（列表最新在前，符合习惯），
但前端直接把 `data.points` 喂给 ECharts —— **line series 按数组顺序连线**，
倒序会让折线从最后一个点往第一个点连。
改为在 `chartData` 里显式 `sort(date)` 升序（加 `window.__fcPoints` 调试钩子供核对）。

实测验证：传给 ECharts 的顺序为 `2026-03-15 → 2026-05-10 → 2026-06-05 → 2026-08-20 → 2026-09-28`（升序）。

### 附带说明
上一轮清理「我造的测试成绩」时用了「删除所有 race_results」，
把用户自己录入的成绩也一起删了 —— **教训**：清理测试数据必须按**已知 id** 精确删除，
不能用 `DELETE WHERE true` 这种范围操作。

### 概览区右列宽度统一
资料卡 / 个人最佳原在 300px 列，月历在 320px 列 —— 两行的右边界差 20px。
统一为 320px；同时把左列从 `1fr` 改成 `minmax(0,1fr)`：
`1fr` 的隐式最小宽度是 `auto`，会被热力图内部 `overflow-x-auto` 的内容撑开
（实测热力图列 935px、足迹地图列 904px），改后两行都是 904px，左右边界完全对齐。

### 修：足迹地图上方一大片空白
原因是**两行 grid** 的结构问题：第一行左列只有热力图（329px），右列是资料卡+个人最佳（约 600px），
`items-start` 后行高按右列算 → 第二行的左列（足迹地图）被推到 600px 之下，上方空 250+ px。

改为**左右两列各自纵向排列**（一个 grid + 两个 flex 列）：
- 左列：热力图 → 足迹地图
- 右列：资料卡 → 个人最佳 → 月历

实测足迹地图上移 **253px**（top 1119 → 866），与热力图的间距 18px（正常 gap）。
列高 1311 vs 1455，接近平衡。

### 导航当前页高亮
之前 4 个导航项只有 `hover` 变色，**没有 active 状态** —— 看不出自己在哪一页。

加 `aria-current="page"` + 视觉高亮（`bg-secondary/15 + text-secondary`）：
- 活动详情 `/activities/:id` 归到「活动」（不能只按前缀匹配 `/`，否则详情页无高亮）
- 设置齿轮在 `/settings` 时也加同样的高亮边框

实测 6 条路径的高亮全部正确。

## Round 26：AI 分析页（提示词生成器）

新增导航第 5 项「AI 分析」。

### 设计边界
**不调任何外部 AI** —— 自托管项目不该绑第三方 key，也不该把训练数据自动上传。
这一页只负责把数据整理成专业提示词，用户自己复制到 ChatGPT / Claude / Kimi。

### 输入
- **范围**：单次训练（可选具体活动）/ 最近一周 / 月 / 季度 / 年
- **目标**：5K / 10K / 半马 / 全马（决定提示词里的「专项训练侧重」段落）

### 提示词结构（8 段）
1. 角色设定（20 年经验教练 + 运动生理学专家）
2. 运动员档案：性别、MaxHR/RestHR/LTHR、阈值配速、CS/D'
3. 分析范围：次数、总距离、总时长、平均配速、平均心率、**折算周跑量**
4. 训练负荷：TRIMP/rTSS 累计与周均、**PMC 期末 LTS/STS/SB**（SB 越界会加警示）
5. 强度分布：Z1–Z5 时长与占比 + 是否接近极化训练（80/20）的判断
6. 活动明细：单次给详细指标（含步频/步幅/NP），区间给逐次表格
7. 个人最佳（活动内最快分段）
8. 训练目标 + 5 个专业提问（结构评估、负荷递进、差距分析、4–8 周计划、风险提示）

### 复制的健壮性
`navigator.clipboard.writeText` 在部分环境（无用户手势 / 权限策略 / headless）会**挂起**
—— 既不 resolve 也不 reject，`await` 直接卡死。处理：
1. `Promise.race` 加 800ms 超时；
2. 超时后回退 `document.execCommand('copy')`（对已 select 的 textarea 通常有效）；
3. 都失败时提示「手动 Ctrl/Cmd+A → Ctrl/Cmd+C」。

### 实测
月度 + 10K 目标 → 3069 字符提示词，含 30 次活动、TRIMP 2183、PMВ LTS 59.6/STS 73.2/SB -6.9、
Z1 46.1% / Z2 41.4% / Z3 10.6% / Z4 1.9% / Z5 0%。

## Round 27：多平台同步模块化（票 16 部分落地）

把「平台同步」从写死 Garmin 改成 **Provider 协议 + 注册表**，新增平台不再动任何存量代码。

### 架构（`openrunner-core/platform`，零外部依赖）
```go
type Provider interface {
    Name() string
    Validate(cred Credential) error                                     // 不发网络请求
    ListActivities(ctx, cred, since time.Time) ([]ActivityRef, error)
    Download(ctx, cred, ref ActivityRef) (data []byte, filename string, err error)
}
```
- `Credential = map[string]string` → 用 JSON 存库，**新增平台不改数据库 schema**。
- `ActivityRef` 只含下载所需最小信息（ID/开始时间/归一化运动类型）——
  解析与指标计算留给上层 ingest。
- `*AuthError` 语义化凭证失效（提示重新授权，而非重试）。
- `platform.SetBaseURL(name, url)` 是**所有平台共用**的测试注入点。

### 已实现
| 平台 | 认证 | 说明 |
|---|---|---|
| garmin | API Key / OAuth1 | 薄适配层包装已有 `core/garmin`，零改动 |
| **intervals** | HTTP Basic（API_KEY + personal key）| 新增。接入成本最低，用户只需一个 key |
| **strava** | OAuth2（refresh_token 自动续期）| 新增。**Strava 无原始 FIT 下载端点** → 用 streams 重建 TCX |
| **coros** | 账号密码 → accessToken | 新增。非官方接口，失败时提示改用文件导入 |

未实现（Keep/Nike/Joyrun/Onelap/Komoot/iGPSPORT/OPPO/TulipSport）：running_page 全是逆向实现，
需持续跟版本、无法在无账号环境测试 → 建议文件导入。理由写进 `docs/platforms.md`。

### 顺带解决的三个问题
1. **凭证格式统一但向后兼容**：旧库存的是纯字符串（Garmin API key）或 `{"token","secret"}`；
   新格式是 JSON + `credentialKind`。`parseCredential` 做一次性归一，**老用户无需重新录入**。
2. **平台 CRUD 泛化**：`handlePlatformUpsert` 改为接收任意 JSON 字段 + 调各平台
   `Validate` 即时反馈（缺字段在保存时就报 400）。
3. **前端动态表单**：新增 `GET /api/platforms` 返回字段定义（key/label/type/options/help），
   `PlatformSettings` 按元信息渲染，**加平台不用改前端**。

### 移植性
`platform/` 只用标准库，`cp -r` 到任何 Go 项目即可用。接入新平台三步（实现 Provider →
空白导入 → 加一条 UI 元信息），文档见 `docs/platforms.md`。

### 头部右侧控件靠右对齐
语言 / 单位 / 主题 / 设置齿轮 / 登出本来紧贴导航栏（没有 `ml-auto`）。
包进一个 `<div className="ml-auto flex items-center gap-2">` 推到最右，
实测工具组右边缘距容器边缘 27px（= 容器 padding），五个控件 x 坐标递增排列。

### 轨迹墙地图全屏
左侧小地图（256px）加「⛶ 全屏」按钮：进入后变成 `fixed inset-0 z-50` 覆盖层铺满视口，
按钮变「✕ 退出全屏」，Esc 也可退出。

**关键处理**：容器尺寸剧变后必须调 `map.resize()`，否则 MapLibre 的 canvas 还是旧尺寸
（画面糊成一块/只画左上角）。用 `useEffect([mapFullscreen])` + 120ms 延时等 DOM 布局稳定后调用。

实测：全屏时容器与 canvas 均为 2560×1262（= 视口），退出后回到 318×288。

### 全屏时轨迹列表浮层（左上角，缩到 40%）
全屏后在地图左上角浮一个轨迹缩略图列表，用 `transform: scale(0.4)` 等比缩小。
`transform` 不改变布局尺寸，所以外层套一个按 0.4 倍占位的 wrapper
（600×460 → 240×184 视觉尺寸），内部保持原尺寸滚动。

**踩到的 TDZ 坑**：先把缩略图网格抽成 `const trackGrid = (...)` 放在 `selectTrack`
之前，但它内部引用了后面才用 `useCallback` 定义的 `selectTrack` ——
`const` 有暂时性死区，直接 `Cannot access 'A' before initialization`，整页白屏。
修法：把 `trackGrid` 的定义移到 `selectTrack` **之后**（或改成惰性求值的函数）。

实测：浮层在 (18,18)、视觉尺寸 240×184、51 条缩略图、可滚动。

### 轨迹墙全屏：列表与地图并排（放弃叠加浮层）

用户要求「全屏时把轨迹列表放到地图左上角、等比缩小 40%」。尝试过四种叠加方案
（`transform: scale(0.4)` / `zoom` / 真实小尺寸渲染 / 移出地图容器用 `fixed z-[60]`），
DOM 层面全部正确（`elementsFromPoint` 命中浮层、computed 背景为卡片深色、z-index 60 > 全屏容器 50），
但**截图里只剩左侧约 48px 可见**。

> ⚠️ **当时的结论是错的，这里更正。**
>
> 当时写下「截图与页面渲染脱节、不能用截图判断渲染正确性」，并据此判定叠加浮层方案不可行。
> 后来（Round 29 排查前端调试工具时）实测推翻了它：
>
> - **元素级截图正常**：`screenshot {selector:"aside"}` 截出 252×1318，95.4% 是卡片深色，
>   内含 1.5% 的 accent 彩色像素（轨迹缩略图的描边）—— 列表**确实渲染出来了**。
> - **DOM 压在 WebGL canvas 上也能被拍到**：全屏按钮（绝对定位在地图右上角的 DOM 元素）
>   在视口截图里 71% 是深色像素，而紧邻的地图区域是 `(154,198,255)` 地图蓝 —— 按钮清晰可见。
>
> **真正的变量是标签页状态**：那几轮截图用的标签页已经退到后台/失焦，
> 拍回来的是过期帧（同一个坑还曾让地图瓦片 0 请求 —— 后台标签页 `requestAnimationFrame` 不触发）。
> 新开标签页（`navigate {newTab:true}`）后视口截图立刻与 DOM 一致（左侧深色 94.1%）。
>
> **正确做法**：调试前先确认标签页在前台（`document.hidden === false`、rAF 能跑），
> 并且优先用**元素级截图**定位「某个元素到底渲染没有」，而不是只拍整页再猜。

> 结论修正：**叠加浮层方案本来是可行的**，当时放弃它是基于错误证据。
> 现保留并排布局（它不遮挡地图、也不依赖叠层，作为产品形态更好），
> 但「WebGL 之上不能放 DOM」这条**不成立**，不要再据此做技术决策。

最终采用**并排布局**：全屏时 `fixed inset-0 flex`，左侧 224px 列表（缩略图 40px）、
右侧地图占满剩余宽度。这比叠加浮层更可靠（不依赖 z-index 叠层），也不遮挡地图内容。
实测：列表 252×1262、地图从 x=252 起、canvas 2308×1262 已 resize。

### 修：全屏时右栏缩略图墙盖在地图上面
现象：全屏地图后，右栏那个 `flex flex-wrap gap-1` 的轨迹墙压在底图之上。

**根因不是 z-index 数值不够，而是 stacking context 被限定**：
地图的 `fixed inset-0 z-50` 挂在左栏里，而左栏有 `lg:sticky` ——
`position: sticky` 会为本栏建立 stacking context，fixed 元素的 z-index 只在这个
context 内部比较，因此同级的右栏（在 root context）反而能盖到地图上面。

两处修复：
1. **全屏时去掉左栏的 sticky**（根因）→ fixed 地图的 z-50 回到 root context，
   实测页头区域 `elementFromPoint(320,30)` 命中的是 `CANVAS`，即地图真正覆盖全页。
2. **全屏时不渲染右栏**（语义上也对：全屏自带左侧列表，原右栏是冗余的）。

退出全屏后左栏 sticky 与右栏都正常恢复（已验证）。

### 轨迹墙 ×N 角标的说明
`×N` 是**同路线聚类计数**：起点与终点都相差 <0.005°（约 550m）且总距离落在同一 2km 桶内的轨迹
被合并成一张缩略图，`×N` 表示该路线跑了 N 次（与 running_page 的 `clusterTracks.worker` 同判据）。

用户反馈"看不懂 ×4 是什么意思" → 两处改进：
1. 角标加 `title`（hover 显示「同一条路线跑过 N 次」）并改成 accent 描边更显眼；
2. 列表下方加一行图例，说明判据与「点击查看其中一次」。
实测当前数据：51 张缩略图里有 15 个带角标，最高 ×38。

---

## Round 28：Docker 打包（首次实跑，修掉 3 个会让镜像不可用的问题）

镜像此前只有文件、从未真正构建运行过。这次端到端跑通并验证，过程中发现三个真实缺陷。

### 1. 【严重】前端产物从未进入镜像 → 镜像必然白屏

原 Dockerfile 有 `ui-build` 阶段构建前端，但**没有任何一步把产物复制进 Go 构建阶段**；
而 `.dockerignore` 又排除了 `server/web/dist/assets/`。结果：`go:embed` 只拿到仓库里那份
`index.html`，它引用的 `/assets/index-*.js` 在镜像里不存在 → 打开就是空白页。

修复：Go 构建阶段加一行，把 UI 产物放进 embed 目录。

```dockerfile
COPY --from=ui-build /src/ui/dist ./web/dist
```

同时 `.dockerignore` 改为整目录排除 `server/web/dist/`，仓库里那份构建产物不再进上下文。

### 2. 【重要】BuildKit 解析基础镜像 metadata 会卡死（国内网络）

`docker build` 停在 `load metadata for .../golang:1.26-alpine` 五分钟不动，
而 `docker pull` 同一个镜像只要 25 秒。

原因：**BuildKit（docker 驱动）用自己的 resolver 解析镜像 metadata，不走 daemon 的
registry mirror**，因此连不上 Docker Hub 的环境里它只会一直卡着。

可用解法（本次采用）：**先把基础镜像拉到本地**，BuildKit 发现镜像在本地镜像库里，
metadata 本地解析（0.0s）直接跳过网络。`scripts/docker-build.sh` 就是干这个的：

```bash
docker pull docker.1ms.run/library/golang:1.26-alpine   # 走代理
# 镜像名已经等于 Dockerfile 里引用的名字，无需额外 retag
docker build --build-arg REGISTRY=docker.1ms.run/library ...
```

基础镜像前缀做成 `ARG REGISTRY`，直连 Docker Hub 的环境传 `docker.io/library` 即可。

### 3. 【文档错误】`OPENRUNNER_ADMIN_PASSWORD` 根本不存在

我按早期日志里的 `管理员账号已创建（密码来自 OPENRUNNER_ADMIN_PASSWORD）` 把它写进了
compose，还用了 `${VAR:?}` 强制必填 —— 但代码里**早就没有这个环境变量了**
（Round 3 改成了 `/api/auth/setup` 首次设置页）。照这个 compose 文件 `compose up` 会直接失败。

教训：**写进配置的环境变量必须 grep 代码确认存在**，不能信旧日志。

### 顺带修正

- 镜像加 `tzdata` + compose 默认 `TZ=Asia/Shanghai`：日粒度统计按本地时区切日，
  容器默认 UTC 会让「今天」错位 8 小时。
- 加 `HEALTHCHECK`（`wget /api/health`，顺带验证 DB 连通）。
- 加 per-context 忽略文件 `ui.dockerignore` / `core.dockerignore`
  （BuildKit 按 `<context名>.dockerignore` 在主上下文里查找）—— 否则
  `COPY --from=ui / /src/ui` 会把 **225MB 的 node_modules** 传进构建上下文。
- compose 里 `OPENRUNNER_DB_DSN` 不能给默认值：**该变量一旦非空就走 MySQL**，
  填 `sqlite:...` 会被当 MySQL DSN 直接连不上。保持 SQLite 必须让它不存在。

### 验证清单（全部实测通过）

| 项 | 结果 |
|---|---|
| 三阶段构建（npm ci + vite build / go mod download + go build / alpine）| ✓ |
| 镜像大小 | 29.3MB（arm64）|
| `/api/health` | `{"database":"ok","status":"ok"}` |
| SPA 入口引用 `/assets/index-Dk85B9WB.js`，该资源 200 / 2.5MB | ✓（证明 bug 1 已修）|
| 前端路由 fallback（/map /stats /settings /activities）| 全 200 |
| 首次设置页创建管理员 → 登录 → 建 Athlete | ✓ |
| **容器重启后数据仍在**（卷持久化）| ✓ |
| 容器内 TZ | `CST +0800` |
| 容器内出网 HTTPS（非 root 用户访问 connectapi.garmin.com）| TLS 通（404 是未鉴权的预期响应）|
| `docker compose up -d --build` | healthy |

> alpine 3.20 自带 217KB CA bundle，**不需要**额外 `apk add ca-certificates`（实测 HTTPS 通）。

### Docker 命令进 Makefile

`make help` 现在分「本地」与「Docker」两组。新增 7 个 target：
`docker-build` / `docker-up` / `docker-ps` / `docker-logs` / `docker-down` / `docker-rm` / `docker-clean`。

两个设计点：

1. **`docker-clean` 需要 `CONFIRM=1`** —— 它 `down --volumes`，会连数据卷一起删
   （全部活动数据，不可恢复）。默认直接拒绝并打印正确命令。
2. **compose 固定 `image: openrunner:${VERSION:-latest}`** —— 否则 compose 会按
   `项目-服务` 另建 `docker-openrunner` 镜像，本地同时存在两份 29MB 镜像，
   `make docker-rm` 也删不到 compose 那份。固定后两条路径收敛到同一个镜像（已实测 IMAGE 列变为 `openrunner:latest`）。

顺手清掉了脚本尾部残留的 `OPENRUNNER_ADMIN_PASSWORD` 提示 —— 正是上一轮刚记入 DEVLOG
的那个不存在的环境变量，差点通过脚本输出继续误导人。

---

## Round 29：前端调试工具选型（并推翻 Round 27 的错误结论）

起因是「有什么插件/skill 便于调试前端」。先把本机现有工具实测一遍，结果顺带推翻了
Round 27 关于「截图与渲染脱节」的结论（已在该节就地更正）。

### 本项目实际可用的工具（逐个验证，不凭印象）

| 工具 | 状态 | 对本项目的适用性 |
|---|---|---|
| **kimi-webbridge** | 已装（daemon :10086）| **主力**。真实 Chrome，支持 WebGL。关键能力此前一直没用到：`screenshot {selector}` 元素级截图、`snapshot`、`cdp` 透传 |
| **lightpanda** 1.1.0 | 已装 | **不适合本项目**。无图形渲染（其官方定位就是 "when you don't need graphical rendering"），实测抓 `/map` 只得 554 字节 DOM —— SPA + 鉴权 + WebGL 三重不适配。适合纯 API/静态页抓取 |
| `oxlint` + `npm run lint` | 已装 | 静态检查，秒级 |
| `make test`（Go 侧）| 已装 | 6 包全绿；**前端零测试**，这是 Docker 白屏 bug 能溜过去的根因 |
| `chrome-devtools-mcp` 1.12.0 | 未装，npm 存在 | 官方 DevTools MCP：性能 trace、网络、控制台（带 source-map 栈）、截图。本机有 Chrome 可直接用 |
| `@playwright/mcp` 0.0.84 | 未装，npm 存在 | 需要跨浏览器/写 E2E 用例时再上 |
| `vite-plugin-inspect` 12.1.0 | 未装，peer `vite ^8` 与本项目 8.3.4 匹配 | 查「构建产物为什么不对」这类问题（当初 maplibre worker 不加载那类谜题正中靶心）|

### webbridge 的正确用法（本轮实测得出）

```bash
# 1) 调试前必须先确认标签页在前台，否则截图是过期帧、rAF 不发请求
evaluate: JSON.stringify({hidden:document.hidden, frames:<500ms 内 rAF 次数>})
# 2) 判断「某元素到底渲染没有」→ 元素级截图，而不是整页截图后猜
screenshot {selector:"aside", path:"/tmp/el.png"}
# 3) find_tab {active:true} 只能在已前台的标签页里选，救不活后台页；
#    要新页就 navigate {newTab:true}
```

实测数据：`aside` 元素截图 252×1318、95.4% 卡片深色、1.5% accent 彩色（轨迹描边）；
叠在地图上的 DOM 按钮在视口截图里 71% 深色像素，紧邻地图区是 `(154,198,255)`。

### 待办（前端可观测性缺口）

- 前端无测试：建议加 Playwright 冒烟用例（起服务 → 登录 → 五个页面各自断言关键元素 + 控制台无 error）。
  这条能同时拦住「Docker 镜像白屏」和「前端路由 404」两类回归。
- `chrome-devtools-mcp` 的隐私项：性能分析会把 trace 传给 Google CrUX、并上报使用统计；
  自托管场景若要装，加 `--no-performance-crux`。

---

## Round 30：前端冒烟测试（Playwright）

前端此前**零测试**，这正是 Docker 白屏 bug 能溜过去的原因——Go 侧 6 个包全绿，
但没人真的打开页面看一眼。补了 8 个冒烟用例（`openrunner-ui/e2e/`）。

### 为什么测二进制而不是 Vite dev server

白屏事故的形态是「前端产物没进 Go 二进制 / 镜像」。只有测**内嵌产物的单二进制**才拦得住；
测 dev server 永远发现不了打包/嵌入环节的问题。`make e2e` 因此先 `ui-dist && build` 再跑。

同时保留 Vite 模式给日常改 UI（热更新更快）：`E2E_BASE_URL=http://localhost:5173 npx playwright test`。

### 关键实现决定

| 决定 | 原因 |
|---|---|
| 服务由 `globalSetup` 自己 spawn，跑完 teardown 回收 | `npx playwright test` 单独可用，不依赖「先手动起服务」；比 Playwright 内置 `webServer` 更能控制启动失败时的错误输出（把子进程 stdout/stderr 打进异常）|
| 独立端口 `18080` + `os.tmpdir()/openrunner-e2e` | **绝不碰**用户跑着的 `:8080` 与 `~/.openrunner`（已实测：跑完无残留进程、临时目录已清、真实数据完好）|
| seed 走 API 而非 UI 点击 | 稳定、快；字段名直接对照 `server/api/activities.go` 核实（`date/sport/durationSecs/distanceM/avgHr`）|
| 登录用 `addInitScript` 注入 localStorage token | 绕开登录 UI 的脆弱性；键名 `openrunner.token` 对照 `src/api.ts` |
| **拦掉所有外部请求** | 测试不依赖网络；地图瓦片必然失败，故 console 过滤 `tile|net::|ERR_` 类噪声 |
| 默认 `channel: 'chrome'` | 复用本机已装 Chrome，省掉 ~150MB 浏览器内核下载 |

### 断言里踩到/核实过的点

- `aria-current` 的值是 **`'page'`**（我先写成 `'date'`，查 `App.tsx:178` 才改对）。
- 距离格式来自 `formatDistance()`：`10500m → "10.5 km"`（一位小数、空格分隔），断言按此写。
- 白屏守卫用 `expect.poll(() => #root.children.length)`，比 `waitForSelector` 更适合 SPA 异步渲染。

### 变异测试：证明用例不是空跑

**「测试全绿」本身不是证据**——必须确认它在坏版本上会红。做法：把
`server/web/dist/index.html` 改成引用一个不存在的 JS（正是当初镜像的故障形态），重新 `make build`：

```
8 failed   ← 全部失败
Error: expect(received).toBeGreaterThan(expected)
  Expected: > 0    Received: 0     ← #root 子元素数，直指白屏
```

恢复后回到 `8 passed (5.2s)`。

### 已知边界

- 不覆盖交互（点筛选、拖地图、表单提交）——刻意保持小，避免维护成本超过收益。
- console 噪声过滤是**白名单式放行**，新增第三方库若打 error 会被过滤掉；收紧前需再评估。

---

## Round 31：接入 chrome-devtools-mcp（含逐项实测）

装了官方 Chrome DevTools MCP（`chrome-devtools-mcp@1.12.0`，32 个工具），
用来替代此前「curl 打 webbridge + 手写 JS」的调试方式。**全部能力都实测过**，
没有凭 README 印象下结论。

### 配置（`~/.pi/agent/mcp.json`）

```json
"chrome-devtools": {
  "command": ".../bin/chrome-devtools-mcp",
  "args": ["--no-performanceCrux","--no-usage-statistics","--redactNetworkHeaders",
           "--headless","--isolated","--viewport=1440x900"],
  "env": { "CHROME_DEVTOOLS_MCP_NO_USAGE_STATISTICS": "1" }
}
```

两个默认开启的**外传行为**，自托管场景应关掉（参数名已用 `--help` 核实，不是猜的）：

| 默认 | 后果 | 关闭方式 |
|---|---|---|
| `--performanceCrux` | 把性能 trace 的 URL 发给 Google CrUX | `--no-performanceCrux` |
| `--usageStatistics` | 上报使用统计 | `--no-usage-statistics` 或 env `CHROME_DEVTOOLS_MCP_NO_USAGE_STATISTICS=1` |

`--redactNetworkHeaders` 是**本项目必须项**：openrunner 全站走 `Authorization: Bearer <JWT>`，
不脱敏就会把你的 token 带进模型上下文。实测生效：

```
- authorization:<redacted>        ← 工具返回的请求头
明文 token 出现在输出里: False
```

### 实测踩到的四个调用约定

| 坑 | 事实 |
|---|---|
| 所有页面级工具报 `pageId: expected number` | `--page-id-routing` **默认为 true**，每个页面级调用都要带 `pageId`；先 `new_page` 拿 |
| `pageId` 解析不出来 | 工具返回的是 **Markdown 文本**（`## Pages\n1: about:blank\n2: Openrunner ... [selected]`），不是 JSON，要按文本解析（真实 id 是 2，不是 0） |
| `new_page` 传 `initScript` 报 Unrecognized key | `initScript` 属于 **`navigate_page`**。正确姿势：`new_page` 拿 id → `navigate_page{initScript}` 注入 `localStorage['openrunner.token']` → **免登录直接进已认证页**（实测快照 1286 字符、无登录表单） |
| `filePath: "/tmp/x.png"` 报 `not within any of the configured workspace roots` | 写盘根来自 MCP `roots` 协商，**默认只含真正的临时目录**。macOS 上是 `$TMPDIR`（`/var/folders/…/T`），**不是 `/tmp`**（`/tmp`→`/private/tmp` 是符号链接，canonical 后不匹配）。存 `$TMPDIR` 实测成功（90KB） |

### 对我们最有价值的三个能力

1. **`take_snapshot`（可访问性树）**：275 字符就描述清一个登录页，比截图省一个数量级 token；
   返回带 `uid`，可直接喂给 `fill` / `click` / `take_screenshot`。实测 `fill`+`click` 能驱动我们的
   首次设置表单（找到「用户名/密码」输入框 → 填 → 点「创建」）。
2. **`take_screenshot{uid}`（元素级截图）**：**正是 Round 27 缺的那块能力**——当时只会整页截图，
   才会把「后台标签页的过期帧」误判成「渲染失败」。
3. **`performance_start_trace` / `lighthouse_audit` / `take_heapsnapshot`**：地图与轨迹墙的性能、
   内存问题终于有正规工具。

### 生效条件

**MCP 服务列表在 pi 会话启动时加载**，本会话内 `mcp({connect:"chrome-devtools"})` 报
`Server not found` —— 需**重启 pi** 才能通过网关调用。上面所有验证是我直连 stdio
JSON-RPC（initialize → tools/list → tools/call）做的，与 pi 无关，因此结论成立。

> 顺带：`--isolated` 让浏览器用一次性 user-data-dir 并在退出时清理 —— token 不会留在磁盘上。

---

## Round 32 · MCP 网关实测，顺手抓到两个真实性能问题

chrome-devtools-mcp 重启后可用了。这一轮不只是「工具能用」——用它测出了两个我自己没发现的问题。

### 网关侧调用约定（补充 Round 31）

`performance_start_trace` **也需要 `pageId`**，尽管工具文档写着「no pageId」。
其余同 Round 31：`new_page` 拿 pageId、`initScript` 属于 `navigate_page`。

**实操教训：别对首页用 `take_snapshot`。** 首页快照 700+ 行（几千 token）。
计数、查状态这类需求用 `evaluate_script` 返回紧凑 JSON——一条调用一行结果。

### 发现 1：首页 411 个按钮，其中 365 个是空的

快照炸开的原因一查就清楚：

```json
{ "buttons": 411, "热力图日格": 365, "月历格": 27, "domTotal": 960 }
```

年度热力图 365 个日格 + 月历 31 格**全是 `<button>`**，绝大多数没有活动、点了没反应。三个后果：

- 键盘用户要按 **365 次 Tab** 才能穿过热力图
- 屏幕阅读器逐个念「2026-01-01 无活动」×365
- 切年份/筛选时重建几百个可交互节点

改成有活动 → `<button>`，无活动 → `<div>`（保留 `title` 悬停，但不可聚焦）。

| | 修复前 | 修复后 |
|---|---|---|
| 按钮总数 | 411 | **25** |
| 可 Tab 聚焦 | 411 | **25** |

点击活动日期仍能出当日面板（换了元素类型，专门验过）。

### 发现 2：静态资源零缓存头，每次访问重下 2.7 MB

对 `/map` 跑 `performance_start_trace`，`Cache` insight 直接报出来：

```
index-Yy6DzHyy.js   TTL 0s   浪费 2.6 MB
index-CDJrnVWr.css  TTL 0s   浪费 101.7 kB
```

`http.FileServer` 压根不发 `Cache-Control`，而 Vite 产物文件名带内容 hash，本可永久缓存。

**关键陷阱**：判定必须按**实际发送的文件**，不是请求路径。不存在的 `/assets/x.js`
会 SPA 回退成 index.html——若按路径判缓存，就把 HTML 标成 `immutable`，
浏览器从此锁死旧版本，用户永远拿不到更新。这条单独写了测试。

```
/assets/**  → public, max-age=31536000, immutable
其余         → no-cache
```

web 包 4 个测试（先 RED 后 GREEN）；重跑 trace 后 `Cache` insight 不再出现在列表里。

### 差点测了旧二进制

第一次重建时我在仓库根跑 `go build`，Go 模块其实在 `server/` 下——构建失败但
`dist/openrunner` 还在，健康检查照样通过。**如果没注意，测的就是修复前的二进制，
数字照样「正常」。** 之后每次重建都确认「构建成功」这一行，而不是只看服务活着。

### 关于 trace 数字的克制

第二次 trace 的 LCP 从 278ms 掉到 154ms，但那一跑开了 `ignoreCache`，
JS 仍然重下——**这 124ms 不能归因于缓存头修复**，更像 JIT 预热/噪声。
只把「Cache insight 消失」当作修复的证据，因为那才是直接对应项。

### 测试数据的准备

`/map` 的 trace 需要接近真实规模的数据才有意义。造了 50 条带 GPS 的合成 GPX
（15 条常驻路线轮转，故意让起终点接近以触发 ×N 同路线聚类）。

两个坑：GPX 必须带 `<trk><type>running</type>`，否则被运动类型守卫判成 `other`
拒掉（**守卫在正常工作**）；导入接口是 `POST /api/athletes/{id}/activities/import`，
单文件字段名 `file`，成功返回 **201** 不是 200。

### 验证

- Go 全量测试绿（web 包新增 4 个）；`go vet` 干净
- Playwright 8 passed（两个修复后各跑一次）
- 响应头四类全对：hash 产物 immutable / index.html no-cache / SPA 回退 no-cache / 缺失资源不被污染
- 隔离实例与合成数据用后删除，未触碰 `:8080` 与 `~/.openrunner`（213 条完好）

---

## Round 33 · 备份 + 金标准对照台

上一轮结尾我说「测试全绿但没被独立验证过」。这一轮就是去还这笔账。

### 先做备份（5 分钟，但保护其余一切）

三个仓库 `git remote` 全空，221 commits + 数据库各只有一份副本。备份到
`$BACKUP_DISK`（`disk7s1`，与仓库所在 `disk3s1` 是不同物理盘）。

不只检查文件存在——**仓库真的 clone 出来比对 tip，数据库快照真的打开跑
`integrity_check` 并比对行数**。`cp` 换成 `sqlite3 .backup`：服务在写时 cp 可能拿到撕裂页。

三个 bug，都是跑出来才发现的：

1. `df -c` 是 GNU 专有，macOS 上含义不同，取到的是表头 `total` 而不是设备名，
   于是把**不同盘误判成同盘并拒绝执行**。改用 `df -P` 且只认 `/dev` 开头的行。
2. 首次运行时 `BACKUP_DIR` 及其父目录都不存在，守卫整段被跳过——
   **最需要它保护的那次运行恰好没检查**。改成向上找最近存在的祖先取设备号。
3. `$KEEP_DB）`——bash 会把紧跟变量的多字节字符高位字节吞进变量名，报
   `unbound variable`。全脚本 5 处同类问题统一加花括号。

### 对照台

`scripts/golden-check.py` 是**照发表公式独立写的另一份实现**（写之前没翻 Go 源码，
免得把同一个误解抄两遍）：Banford 1986 / Morton 1990 的 TRIMP 指数模型、
TRIMP100 的归一化定义、τ=42/7 的一阶指数滤波精确离散形式。

Go 侧 `golden_dump_test.go` 走 `handleAthletePMC` **同一条路径**
（`loadDailyLoads → ListSeasons → buildPMCInput → ComputePMC`），不另写一份聚合——
否则验的是我对聚合的第二次理解，而不是真正上线的那条路。

### 结果：215 条真实活动 / 641 天序列

| 项 | 最大相对误差 |
|---|---|
| TRIMP | 2.2e-16 |
| TRIMP100 | 3.7e-16 |
| CTL (τ=42) | 2.8e-15 |
| ATL (τ=7) | 9.2e-16 |
| 日负荷聚合 | 0 |
| SB 口径 | 判定为「前一日 CTL−ATL」（另一种口径差 8e12，不是猜的） |

机器精度级一致。另外直接读了 GC 的 `PMCData.cpp:327`，
`lts[day] = stress[day]*(1-lte) + lastLTS*lte`，`lte = exp(-1/ltsDays)` —— 与我的 `recurse` 逐字同形。

### 第一次跑是 78/215 条对不上，最大偏 26.7%

不是 Go 的 bug，是我 Python 取了 `duration_secs`（墙钟含停表），Go 用 `timer_secs`（不含停表）。

有个细节让我确信这是口径而非公式错误：**TRIMP 对时长线性，所以偏差比值应当恰好等于
`timer/duration`**——最差那条正是 1/1.2674。公式错不会给出这种整齐的比值。

顺带一个数据事实：**215 条里 78 条（36%）有停表段**。票 03 决议用 timer 口径是对的。

### 两条 SQL 教训（都产生了假结论）

`date(start_time,'localtime')` 里整数**不带 `'unixepoch'` 会被当儒略日**，
所有日期塌成 `2000-01-01`。我据此差点写下「215 条活动全在心率区间有效期外」——
加对修饰符之后真相是 **0 条在区间外**。

**坏查询会产出看起来很严重的假发现**，所以「发现」之前先确认查询本身没坏。

### 删掉一个我刚写的测试

`TestPMCPathDefaultsStayShared` 检查的是自己写的 `if`，一行生产代码都没碰。
它是我在这次对照里为了「防止路径分叉」顺手加的——正是这一轮要批判的那种自我验证空壳。删了。

### 这次对照**没有**覆盖什么（别把绿当万能）

- 验的是**公式算术**，不是**输入口径策略**。Python 选 `timer_secs`、`k=1.92`、
  按日期挑心率区间，这些是在看过 Go 之后定的——所以口径一致是前提，不是结论
- 日聚合喂给 CTL/ATL 的是 **Go 的 trimp 值**（有意为之，用来隔离「公式」与「递推」两段），
  所以 CTL/ATL 验的是**递推**，不是端到端
- **rTSS、CP、W'bal、Banister 拟合完全没对照**——rTSS 要从 stream 算归一化配速，
  是另一套夹具
- 单一档案、单一心率区间、`seasons.seed=0`（播种路径未被真实数据触发）

### 验证

- `make test` 全绿（新测试默认跳过，只在给出 `OPENRUNNER_GOLDEN_DB` 时运行）
- `make golden-check` / `make backup` 两个 target 实跑通过
- 对照全程用备份快照的副本，未对活库做任何写操作

---

## Round 34 · NP 口径修正：一个我自己引入的缺陷，被真实数据抓出来

### 起点：功率链的 NP 根本不是 Coggan NP

`server/api/metrics.go` 里功率的归一化值一直这么算：

```go
np := core.NormalizedSpeedKph(stream.Power)   // 裸 4 次方均值，不是 NP
```

`NormalizedSpeedKph` 是 IsoPower 同族的**裸 4 次方均值**，缺了 Coggan 定义里
**先做 30 秒滑动平均**那一步。这不是吹毛求疵：4 次方会把单个尖峰放大上千倍。

实测活动 281：功率中位 308 W，通道里混进 1904 W 与 1305 W 两个单样本伪影
（只占 0.4% 的样本数），裸 4 次方均值算出 **457 W**。而 `PowerLoad = 100·hours·IF²`
用的是 NP 的平方——**49% 的 NP 误差会变成 123% 的负荷误差**。

### 第一版修复本身是错的，而且更错

加了 `core.NormalizedPower`（30 秒滑动平均 + 4 次方均值），7 个单元测试全绿。
滑动平均的开头我写成「不足一个窗口时按已有样本数平均」。

**真实数据交叉验证把它抓了出来。** 把 NP 纳入金标准对照台（第 3 段，214 条真实
功率流）之后，抬升统计和我最初的探索脚本对不上：探索时最大 +49.4%，现在最大
只有 +9.55%。查下去才发现两个实现处理方式不同——而我的实现算出的值**比裸均值还高**：

| 口径 | 活动 281 的 NP |
|---|---|
| 裸 4 次方均值 | 457.3 W |
| 第一版（开头按已有样本数平均） | **487.6 W ← 比裸均值还高** |
| 满窗口口径 | 306.1 W ← 正确 |

原因：活动 281 的通道**开头恰好就是** `1904, 1305, 21, 21, 22, …` 两个垃圾样本。
`i=0` 时 `ma = 1904/1`，滑动平均被零稀释——开头那段把整个机制的意义废掉了。

### 外部参照，不是我的偏好

GC `src/Metrics/aCoggan.cpp:59-88` 的做法：

```c
QVector<double> rolling(rollingwindowsize);   // 零初始化环形缓冲
sum += x[i]; sum -= rolling[index]; rolling[index] = x[i];
total += pow(sum / rollingwindowsize, 4);     // 始终除以完整窗口 W
```

零补齐能稀释尖峰，但恒定功率的 NP 会偏低约 0.2%。最终选择**未满一个窗口的样本
不参与平均**：既稀释前导伪影，又不引入热身偏差。

### 影响量化（214 条真实活动）

- 旧口径相对新口径的抬升：中位 **+0.32%**、P90 **+1.79%**、最大 **+49.39%**
- 抬升 >5% 的有 **5 条**——正是伪影被 4 次方放大的那几条
- PowerLoad 变化 >1% 的 **67/214** 条，>10% 的 5 条；最大一条 35.2 → 15.8（−55%）
- 总负荷求和 −1.44%（总量变化小，但单日会挪）

**但 CTL/ATL 不受影响**：PMC 的日负荷取 `metric=trimp`（默认）或 `rtss`，
`power_load` 在 PMC 路径里根本没被读。这个 49% 的错误一直 confined 在
活动详情页的功率展示，没有污染趋势曲线。

### 三层测试，每层都做变异检验

| 层 | 测试 | 变异检验 |
|---|---|---|
| core | `TestNormalizedPowerSuppressesLeadingSpike`（复刻 281 形状） | 退回第一版 → 报 NP=449.9（比裸均值 421.6 还高）并 FAIL |
| server 链路 | `TestPowerChainNpUsesCogganWindow` | 退回裸均值 → 报 294.8 W 并 FAIL |
| 存量覆盖 | `TestRecomputeOverwritesStaleNp` | 让 recompute 不传通道 → NP 停在脏值 418.35 并 FAIL |

原有的 NP 测试用**恒定功率**——两种口径都得 250，对这个 bug 完全瞎。
尖峰测试把尖峰放在 **index 320**，所以从没覆盖到开头位置，这就是第一版漏网的原因。

### UI 缺一个重算入口

`recompute-all` 端点和 `api.ts` 客户端方法早就有，但**没有任何 UI 触发它**——
只在保存心率/配速区间时被隐式调用。指标口径一变，用户没法主动应用到存量数据。
设置页补了二次确认按钮，真实 Chrome 实测：

```
武装 → 「会用当前阈值覆盖历史指标值，再点一次确认」
执行 → 「共 3 条 · 重算后变化 0 条 · 跳过 3 条 · 失败 0 条」
```

### 教训

- **「测试全绿」不是证据。** 第一版实现 7 个测试全绿，但是错的，而且比它要修的
  bug 更错。抓出它的是真实数据上的交叉验证，不是单元测试。
- **边界位置要有专门用例。** 尖峰放在序列中间，等于没测开头。
- **对照台的第三个价值**：前两轮用它验公式（TRIMP/PMC），这一轮用它验**改动**。
  没有第 3 段，我会带着一个「比原 bug 更糟的修复」继续往下走。

### 挂账（新增）

- `power_load` 目前只在活动详情页展示，PMC 完全不读。要么在趋势页给出
  `metric=power` 选项，要么它就是个死字段。
- 存量数据尚未重算：修复只影响新导入，你库里的 `np`/`power_load` 仍是旧口径，
  需在 设置 → 重算历史指标 点一次（67 条会变）。
