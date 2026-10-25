# 03 · PMC 性能管理图与 Banister 刺激-响应模型

> 源文件：`Metrics/PMCData.{h,cpp}`（574 行）、`Metrics/Banister.{h,cpp}`（712 行）、`Metrics/Estimator.{h,cpp}`（power_index 估计）、`Charts/LTMPlot.cpp`（图表宿主 :3338 createPMCData）
> 第三方依赖：contrib/lmfit（仅 Banister 拟合需要）

## Part A · PMC（CTL/ATL/TSB 的 GC 实现）

### A1. 术语映射

| 通用术语 | GC 术语 | symbol/设置 |
|---|---|---|
| CTL（Chronic Training Load） | **LTS** Long Term Stress | `GC_LTS_DAYS` 默认 42 |
| ATL（Acute Training Load） | **STS** Short Term Stress | `GC_STS_DAYS` 默认 7 |
| TSB（Training Stress Balance） | **SB** Stress Balance | `GC_SB_TODAY` 决定画今天还是明天 |
| Ramp Rate | **RR** | STS 天数窗口的 LTS 增量滚动和 |

### A2. 核心算法（PMCData.cpp:322 `calculateMetrics`，全部数学就这 10 行）

```
lte = exp(−1/LTS_DAYS)         # 42 天慢性负荷时间常数
ste = exp(−1/STS_DAYS)         # 7 天急性负荷时间常数

# 一阶 IIR 低通滤波（等价于无限冲激响应的指数加权）
LTS[d] = stress[d]·(1−lte) + LTS[d−1]·lte
STS[d] = stress[d]·(1−ste) + STS[d−1]·ste

SB[d]  = LTS[d] − STS[d]       # 正=新鲜，负=疲劳（画今天或明天由设置决定）
RR[d]  = Σ_{k=d−STS_DAYS+1..d} (LTS[k] − LTS[k−1])   # 滚动窗内 LTS 日增量之和
```

> 与 TrainingPeaks 的差别：TP 用 `CTL_today = CTL_yesterday + (TSS − CTL_yesterday)/42`，GC 用 `exp(−1/42)` 连续指数——两者在稳态下数值几乎一致，但 GC 版允许**非整数天数**且天然支持中途播种。

### A3. 数据装配（PMCData.cpp:115 `refresh`）

```
STEP 1 日期区间：
  start = min(最早活动−1天, 最早播种赛季起点)
  end   = max(最晚活动+365天, 今天+365天)      # 多留一年供 SB 衰减展示

STEP 2 三条平行日序列（同尺寸数组，逐日相加同日多次活动）：
  stress_   实际活动的度量值（RideItem.planned == false）
  planned_  计划活动（planned == true）
  expected_ 预测序列：
            · 过去的日期  → 只算实际
            · 今天        → 有实际用实际，否则用计划 (:305)
            · 未来日期    → 只算计划
  来源：metricName 精确匹配走 getForSymbol 快路径；
        DataFilter 表达式（如 "1/(1+wbal)"）走表达式求值慢路径

STEP 3 赛季播种（:246）：
  Season.seed > 0 时在赛季起点写入 −seed（负数是"已播种"标记），
  递推遇到负值直接取反作为初值（:347）——使 LTS 起点不必从 0 爬升

STEP 4 对三条序列各跑一遍 A2 的递推 → lts_/sts_/sb_/rr_ 与 planned_*/expected_*
```

### A4. RAG 配色与图表宿主

- `PMCData::ltsColor/stsColor/sbColor/rrColor`：静态函数按阈值返回红/黄/绿（GC 源码 sbColor 以 **SB < −40** 为红色过劳区——openrunner UI 依此校准）。
- `LTMPlot::createPMCData`（LTMPlot.cpp:3338）：每个 PMC 曲线一个 `PMCData` 实例（ athlete 级全局共享 `athletePMC` + 图表局部筛选 `localPMC`），按日期逐点查 `pmcData->lts/sts/sb/rr/planned*/expected*`。
- 计划 vs 实际在图上区分显示，用于赛季规划对比。

### A5. 集成要点

```
输入:  DailyMetric[]  { date, value }  +  Season[] { start, seed? }  + 参数 {ltsDays, stsDays}
输出:  { lts[], sts[], sb[], rr[], planned*, expected* }  按 date index 对齐
```

- 纯函数实现约 60 行；GC 里的复杂度全部在：失效信号网（可删）、表达式引擎（可砍）、计划/预期三序列（建议保留，产品价值高）。
- **SS TODAY 特判**（:305）：今天的 expected = 实际>0 ? 实际 : 计划 —— 做"今日训练建议"功能时的关键语义。
- 注意 SB 展示位（今天 or 明天）是文化习惯问题，GC 默认画在明天（`sb[day+1]`），配置可切。

---

## Part B · Banister 模型（PMC 的"科学加强版"）

### B1. 模型定义（Banister.cpp:89 注释 + :463 compute）

```
输入（日粒度）:
  I[d]  = 训练冲量（任选度量: BikeScore/TRIMP/TSS，用户配置 symbol）
  R[d]  = 表现测试值（只来自标记为 test 的 interval 或 Estimator 的 power_index）

两条指数记忆曲线:
  g[d] = g[d−1]·exp(−1/t1) + I[d]        # PTE 成分，t1 默认 50 天
  h[d] = h[d−1]·exp(−1/t2) + I[d]        # NTE 成分，t2 默认 11 天

表现预测:
  PTE[d] = k1 · g[d]
  NTE[d] = k2 · h[d]
  perf[d]= p0 + PTE[d] − NTE[d]          # p0 = 初始表现基线
```

与 PMC 的本质区别：PMC 把负荷映射到"负荷"本身（自变量=因变量）；Banister 把负荷映射到**可测的表现**（CP/测试成绩），参数从数据里拟合出来，能回答"我现在什么状态、何时到峰值"。

### B2. 表现响应变量：power_index（Banister.cpp:570）

把任意时长的努力归一化为"相对参考运动员的百分比"：

```
参考运动员（来自 GC OpenData 统计，Banister.cpp:57）:
  typical_CP = 261 W, typical_WPrime = 15500 J, typical_Pmax = 1100 W

3 参数模型反解该时长下参考运动员的功率:
  P_ref(t) = W' / (t − W'/(CP − Pmax)) + CP

powerIndex = 100 · P_actual / P_ref(t)      # >100 优于常人，<100 低于
越界（P_ref<0 或 >2500W）返回 0
```

对应度量 `power_index`（单次努力均值）与 `peak_power_index`（从 3 分钟起逐时长取最佳，:654）。

### B3. 测试点从哪来（Banister.cpp:273 收集循环）

1. 活动 interval 标记为 `istest()` → 取当天最佳 `perf_symbol` 值；
2. 否则查 `rideCache->estimator` 的当日最佳表现（Estimator 后台线程全库扫描所有 ride 的 power_index，`Performance` 结构含 `submaximal` 过滤标志）；
3. 同日多次取 max（two-a-days 处理，:296-302）。

### B4. 拟合窗口划分（:349-430）

```
按日扫描 data[]:
  开始: 首个 score>0 的日
  结束: 无训练间隔 > 42 天 (typical_SeasonBreak) 或窗口 > 700 天 (typical_SeasonSize)
  有效: 窗口内测试数 ≥ 2
  后处理: 测试数 <5 的窗口与相邻窗口合并（:409）；窗口之间强制连续（stopIndex 顺延）
```

### B5. LM 拟合（:517 `fit`）

- 自由参数 3 个：`[k1, k2, p0]`，初值 `[0.2, 0.2, 首个测试成绩]`；t1/t2 由用户固定（`setDecay()`）。
- 每次迭代 lmcurve 调 `banisterFit::f(t, parms)`（:440）：参数变了就**重算整窗递推**并返回当天的 perf。
- 目标：最小化 `Σ (perf[test日] − test值)²`。
- 全局 `QMutex calllmfit` 保护（lmfit C 库用全局变量传上下文）。
- 拟合质量：`RMSE(from, to)` 对窗口内测试点评估（:144）；`getPeakPerf()` 返回窗口内预测峰值日（配典型 CP 换算成瓦特，:170）。

### B6. UI 集成

- Trends（LTMPlot/LTMWindow）：叠加 PTE/NTE/perf/测试点 + 拟合窗口底色。
- CP 图（CPPlot.cpp）：Banister 预测的 CP 曲线（power_index × typical_CP/100）。
- DataFilter 暴露 `banister_nte/pte/perf` 符号，可在用户度量/图表里引用。

### B7. 集成清单

| 项 | 内容 | 移植难度 |
|---|---|---|
| powerIndex | 3 参数公式 + 越界防御 | 易（纯函数） |
| 日序列装配 | I[d] 从日度量、R[d] 从测试/估计 | 中（需要"测试标记"数据模型） |
| 递推 | g/h/perf 三数组 | 易 |
| LM 拟合 | lmcurve → scipy `least_squares`(带 bounds) / JS `fmin` 类库 | 中 |
| 窗口划分 | 42/700 天规则 + 合并 | 易 |
| Estimator | 后台全库扫描 power_index | 可简化为按需计算 |

### B8. 与 PMC 的组合建议

openrunner 可以：先用 PMC 做主产品功能（稳、好解释），把 Banister 做成"实验性表现预测"——需要用户认真标记测试（比如 CP 测试日），数据少时拟合会发散（GC 用 `status.outcome >= 0` 才打印 RMSE，坏拟合直接不管，集成时建议加拟合质量门槛如 RMSE/threshold > x 显示警告）。
