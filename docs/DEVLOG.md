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
