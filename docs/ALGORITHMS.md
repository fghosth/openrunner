# openrunner 算法与术语手册

本文档汇总 openrunner 用到的全部跑步算法、指标与专有名词：每条给出**中文含义、公式/原理、文献依据（论文链接）、代码位置**。

约定：

- 本项目遵循 [ADR-0001 净室复现](adr/0001-clean-room-reimplementation.md)：算法只按**公开发表的论文/出版物**复现，不逆向任何商业产品源码。部分工程常数（如 PMC 的 42/7 天）转引自 GoldenCheetah 的公开默认值，其论文溯源见各条目标注。
- 证据等级沿用 running-coach 的 [sources.md](../../.pi/agent/skills/running-coach/references/sources.md) 标签：**strong**（荟萃/大队列/RCT）、**moderate**（小样本或共识）、**practice**（教练惯例，无直接试验）。
- S 编号（如 S1、S25）与 running-coach skill 的文献表同源。

---

## 目录

1. [训练负荷与压力链](#1-训练负荷与压力链)
2. [PMC 表现管理](#2-pmc-表现管理)
3. [阈值与功率/速度容量模型](#3-阈值与功率速度容量模型)
4. [心率体系](#4-心率体系)
5. [成绩预测与外推](#5-成绩预测与外推)
6. [训练配速与强度分布](#6-训练配速与强度分布)
7. [周计划校验规则](#7-周计划校验规则)
8. [单课分析](#8-单课分析)
9. [缩写速查表](#9-缩写速查表)

---

## 1. 训练负荷与压力链

### NP — 标准化功率（Normalized Power）

**含义**：整段运动「等效恒定功率」——用变功率跑出与恒定功率相同的生理代价。

**原理**：功率经 **30 秒滑动平均**（抹掉瞬时波动）后取 **4 次方均值再开 4 次方**：

```
NP = ( mean( (MA₃₀)⁴ ) )^(1/4)
```

4 次方来自功率→生理代价（摄氧/乳酸）的非线性关系。实现细节：开头不足 30 秒的残缺窗口直接跳过（零补齐会让恒定功率的 NP 偏低约 0.2%）；非正值（停表）先剔除。

**依据**：Coggan & Patterson & Medbø 的 NP 定义，见 Coggan 的权威阐述 [Training with Power](https://trainwithpi.com/)（无正式期刊论文，属 practice 级行业标准）。

**代码**：`openrunner-core/normalized_power.go`

### IF — 强度因子（Intensity Factor）／ VI — 变异指数（Variability Index）

```
IF = NP / 阈值（跑步用阈值速度代理，功率用 CP/FTP）     —— 0~1.2，课内平均强度
VI = NP / 平均功率                                       —— ≥1，越大越「颠簸」
```

IF 是 TSS/PowerLoad 的输入；VI 用于判断配速是否均匀（比赛配速纪律的量化）。

**依据**：同 NP（Coggan）。**代码**：`server/api/metrics.go`

### TSS / rTSS — 训练压力分数（Training Stress Score / running TSS）

**含义**：单课「压力分」，阈值强度 1 小时 = 100 分。

```
TSS  = 100 × 小时 × IF²
rTSS = 100 × 秒 × (NS/CV)² / 3600          NS = 配速流的 4 次方均值（IsoPower 同族）
```

rTSS 把「速度当功率用」：跑步没有功率计，用配速流的 4 次方均值 NS（IsoPower 同族，压抑低强度、放大高强度）代替 NP，CV 为阈值速度（km/h），IF 即 NS/CV。

**依据**：TSS 为 Coggan 提出（同 NP，practice 级）；rTSS 为跑步版 TSS（Skiba 思路，转引自 GoldenCheetah 公开实现注释，原文溯源挂账 DEVLOG，ADR-0001 纪律）。

**代码**：`openrunner-core/pace_metrics.go`（Rtss）

### TRIMP — 训练冲量（TRaining IMpulses）

**含义**：心率版单课负荷，Banister「刺激-反应」理论的量化。

```
TRIMP = 分钟 × %HRR × 0.64 × exp(Ksex × %HRR)
%HRR  = (平均心率 − 静息心率) / (最大心率 − 静息心率)
Ksex  = 男 1.92 / 女 1.67（Green 系数）
```

指数项体现「心率越高、单位时间的训练刺激超线性增长」。

**依据**：**Banister EW, Calvert TW, Savage MV. 1975**, *A model for training evaluation: a brief review of the impulse-response theory of physical training*. Sport and Fitness Canada, Technical Fascicle 9（DOI 前时代，无稳定链接，Google Scholar 可检）（moderate，理论模型）。现代验证见 **Busso T, et al. 1991**, *Adequacy of a systems structure in the modeling of training effects on performance*. J Appl Physiol 71(5):2044-51 — <https://doi.org/10.1152/jappl.1991.71.5.2044>。0.64/1.92/1.67 为 Green 等对 Banister 原式的修订系数（转引自 GoldenCheetah 公开实现注释）。

**代码**：`openrunner-core/hrmetrics.go`（TrimpPoints / Trimp100）

### TRIMP100 — 归一化 TRIMP

把 TRIMP 缩放到「阈值心率强度骑/跑 1 小时 = 100 分」，与 TSS 同量纲，便于混用 PMC。

**代码**：`openrunner-core/hrmetrics.go`

### Edwards TRIMP（分区训练冲量）

**含义**：Edwards 分区法——按 %MaxHR 把心率分 5 区，各区乘固定系数后按秒累加：

```
区界 (%MaxHR)：64 / 77 / 84 / 90 / 95
区系数：       0.84 / 1.65 / 2.57 / 4.01 / 5.91
```

**依据**：Edwards《Optimal Training》（1996，书籍，practice 级）。原始论文溯源挂账（ADR-0001 纪律，见 DEVLOG）。

**代码**：`openrunner-core/hrmetrics.go`（TrimpZonal）

### PowerLoad — 功率负荷（类 BikeScore）

```
PowerLoad = 100 × 小时 × (NP/CP)²
```

与 TSS 同构，但阈值用实测 CP（比 FTP 更客观）。BikeScore 本身是 Hunter Allen 的专利式算法（无论文），本项目用公开的 IF² 结构实现等价物，不抄 BikeScore 的权重细节。

**代码**：`server/api/metrics.go`、`store/activity_metrics.go`

### 计划负荷估算（isEstimated）

计划课没有心率/配速流，按课型（easy/tempo/threshold/intervals…）的**典型强度占比**估算负荷，PMC 虚线（planned 序列）用它。属 practice 级工程近似。

**代码**：`server/api/metrics.go`、`coach/week.go`（DEFAULT_QUALITY_SHARE 同源）

---

## 2. PMC 表现管理

### PMC — 表现管理图（Performance Management Chart）

三条曲线 + 派生指标，全部由「日负荷 → 指数衰减滚动」得到：

```
CTL[d] = load[d]·(1−e^(−1/42)) + CTL[d−1]·e^(−1/42)    —— 42 天时间常数
ATL[d] = load[d]·(1−e^(−1/7))  + ATL[d−1]·e^(−1/7)     —— 7 天时间常数
TSB[d] = CTL[d] − ATL[d]                                 —— 训练压力平衡（体能−疲劳）
```

- **CTL**（Chronic Training Load，长期训练负荷）：约 6 周窗口的体能基线。
- **ATL**（Acute Training Load，急性负荷）：约 1 周窗口的疲劳。
- **TSB**（Training Stress Balance）：正值=新鲜，负值=疲劳。比赛日目标区间通常 +15~−10（practice）。
- **RR**（Load Range）：ATL 窗口内 CTL 日增量的滚动和，图上表示负荷波动带。

**原理依据**：指数衰减双时间常数来自 **Banister 的 impulse-response（刺激-反应）模型**（同 TRIMP，1975；现代系统学验证见 Busso 1991，doi 同上）。42/7 天与「CTL/ATL/TSB」命名由 Coggan 在 TrainingPeaks/GoldenCheetah 推广，属 practice 级工程惯例。

**播种（seed）**：赛季起点直接给定 CTL/STS 初值，避免从 0 爬坡污染曲线（等价 GC 的负值标记法）。

**三序列**：actual（实际）/ planned（计划）/ expected（过去=实际、今日特判、未来=计划），计划与实际互不干扰。

**代码**：`openrunner-core/pmc.go`、`server/api/pmc.go`

### ACWR — 急慢性负荷比（本项目**有意不用**）

`ACWR = ATL/CTL` 在文献中流行，但 **Impellizzeri 2020/2021** 证明其数学构造有根本缺陷（比值把两个强相关的量相除，产生伪相关），且 **Frandsen 2025** 的 5200 人队列发现 ACWR 不预测损伤。本项目遵循该证据，用「单次 +10%」与「周增量」规则替代（见 §7）。

**依据**：Impellizzeri 2020, IJSPP 15(6):907-13 <https://doi.org/10.1123/ijspp.2019-0864>；Impellizzeri 2021, Sports Med 51(3):581-92 <https://doi.org/10.1007/s40279-020-01378-6>（strong）。

### Banister 刺激-响应拟合（PerformanceForecast 的生理线）

对日负荷序列拟合双指数模型：

```
perf(t) = p0 + k1·e^(−t/τ1) + k2·e^(−t/τ2)     τ1=45（体能，慢衰减）, τ2=15（疲劳，快衰减）
```

用最小二乘解 p0/k1/k2，以历史比赛名次/成绩为响应，预测未来某日的竞技状态曲线。τ 取固定值（拟合 4 参数在个人数据上不稳定）。

**依据**：Banister 1975 / Busso 1991（同上）；τ 常数为工程取值（GC 默认，practice）。

**代码**：`openrunner-core/banister.go`（FitBanister / Predict）

---

## 3. 阈值与功率/速度容量模型

### CP / CS — 临界功率 / 临界速度（Critical Power / Critical Speed）

**含义**：重功率（速度）-时间双曲线渐近线——理论上可无限维持的强度。

**原理**：做功 W 与力竭时间 t 线性：`W = W′ + CP·t`；跑步版用距离：`d = D′ + CS·t`。

**依据**：**Monod H, Scherrer J. 1965**, *The work capacity of a synergic muscular group*. Ergonomics 8(3):329-38 — <https://doi.org/10.1080/00140136508930810>（经典）；概念史与生理综述见 **Poole DC. 2009**, *Historical perspectives on the critical power concept*. Med Sci Sports Exerc 41(5):1073-84 — <https://doi.org/10.1249/01.mss.0000352951.49384.8e>（moderate）。

**实现**：
- `FitCriticalSpeed`：多场次最佳努力（2–20 分钟）线性回归，带质量门槛（拟合点跨度、R²）。
- `FitCP3`：3 分钟全力测试协议（功率版），**Burnley M, Doust JH, Vanhatalo A. 2007**, *Determination of critical power using a 3-min all-out cycling test*. Med Sci Sports Exerc 39(1):165-72 — <https://doi.org/10.1249/mss.0b013e31802dd3e6>；协议可靠性综述见 Broxterman 2019（moderate）。
- `BestEfforts`：从流数据提取各时长最佳配速（Season's Best 曲线）。

**代码**：`openrunner-core/critical_speed.go`

### W′ — 无氧做功容量（Anaerobic Work Capacity）／ D′ — 距离储备

CP 以上还能做的「有限总功」（J）/ 总距离（m），即双曲线的截距。W′≈15kJ 量级、D′≈200m 量级（个体差异大）。

**依据**：同 Monod & Scherrer；三参数模型见 **Morton RH. 1996**, *A 3-parameter critical power model*. Ergonomics 39(2):410-9 — <https://doi.org/10.1080/00140139608964484>。

### W′bal — W' 余额（功率版）

**含义**：间歇中「还剩多少 W′」，逐秒模拟 W′ 的消耗与恢复。

**原理**（Skiba 2012，分段模型）：超 CP 段按 `(P−CP)` 线性扣减储备；低于 CP 段按时间常数 τ 指数回充（**只在恢复段施加衰减**）：

```
P > CP:  W′bal -= (P − CP)·dt，钳到 [0, W′]
P ≤ CP:  W′bal  = W′ − (W′ − W′bal)·e^(−dt/τ)
τ = 546·e^(−0.01·(CP−P̄below)) + 316        （P̄below = 低于 CP 的功率均值；取不到时退化为 CP → τ=862）
```

**依据**：**Skiba PF, Chidnok W, Vanhatalo A, Jones AM. 2012**, *Modeling the expenditure and reconstitution of work capacity above critical power*. Med Sci Sports Exerc 44(8):1546-54 — <https://doi.org/10.1249/mss.0b013e3182517a80>（moderate，实验室验证；「store-and-pothole」是文中的模型比喻）。τ 的 316s 下界来自 Skiba 原文；P̄below 取不到时公式退化为 τ=862（546+316），实现见 `AvgPowerBelowCP`。

**代码**：`openrunner-core/wbal.go`（TauFromData / W'bal 前向积分）

### DBal — D′ 余额（跑步版）

W′bal 的速度版：以 CS 为渐近线、D′ 为储备，逐秒算「还剩多少超阈值距离额度」。供跑步间歇课分析。

**代码**：`openrunner-core/critical_speed.go`（DBal）

### 跑步配速分区（%CV 六区）

以阈值速度 CV 为基准的六区：上界 80 / 87 / 95 / 105 / 119 %CV（Z1..Z5，Z6 无上界）。转引自 GoldenCheetah 公开实现（其注明 "based upon the Skiba run pace zones"），原文溯源挂账 DEVLOG（ADR-0001 纪律）。

**代码**：`openrunner-core/pace_metrics.go`（PaceZoneIndex）

### FTP — 功能阈值功率（Functional Threshold Power）

60 分钟最大功率，≈CP 的实操代理（Allen & Coggan《Training and Racing with a Power Meter》，practice 级，无论文）。本项目功率链优先用实测 CP，FTP 仅作兼容概念出现。

---

## 4. 心率体系

### MaxHR / RestHR / LTHR

- **MaxHR** 最大心率：实测优先；未填时用「观测 MaxHR」——从历史活动流里找**持续 30 秒的峰值**（滤掉光电腕表孤立尖峰），并要求活动质量达标（时长、心率覆盖）。
- **LTHR** 乳酸阈值心率：30 分钟阈值测试或体感测定（Friel 协议，practice）。
- **RestHR** 静息心率：晨起实测。

**代码**：`server/store/hrpeak.go`（ObservedHr）

### %HRR — 心率储备百分比（Karvonen 法）

```
%HRR = (HR − HRrest) / (HRmax − HRrest)
```

TRIMP 与心率区间编辑器的基础量。

**依据**：**Karvonen MJ, Watanen E. 1957**, *Personal physical working capacity at heart-rate determination*. Br J Ind Med 14(3):200-4 — <https://oem.bmj.com/content/14/3/200>（经典，DOI 前时代）。

### 心率区间（HR zones）

两套并存：

1. **%HRR 区间**（区间编辑器，用户自定义生效日期）——Karvonen 传统。
2. **Edwards %MaxHR 区界**（64/77/84/90/95%）——用于分区 TRIMP（见 §1）。
3. **LTHR 区间**（Friel 式，跑步配速卡附带心率列）——**依据**：Friel《The Triathlete's Training Bible》/ TrainingPeaks 文档（practice，S49）。

**代码**：`server/store/hrzones.go`、`openrunner-core/hrmetrics.go`

### 心率漂移（Cardiac Drift / HR drift）

**含义**：稳态强度下，心率随时间缓慢上升的现象（脱水/热应激/疲劳）。**<5% 为佳**。

**原理**：取热身后稳态段前 15 分钟与末 15 分钟的平均心率，漂移 % = (末−前)/前。漂移大提示有氧耐久性不足或散热/补水问题。

**依据**：Uphill Athlete 心率漂移测试法（S50，practice）；学术综述见 **Coyle EF. 2001**, *Cardiovascular drift during prolonged exercise: new perspectives*. Exerc Sport Sci Rev 29(2):51-6 — <https://doi.org/10.1249/00003677-200104000-00009>（moderate）。

**代码**：`openrunner-core/coach/analysis.go`

---

## 5. 成绩预测与外推

### Riegel 幂律（成绩距离外推）

```
t₂ = t₁ × (d₂/d₁)^α        经典 α = 1.06
```

**依据**：**Riegel PS. 1981**, *Athletic records and human endurance*. American Scientist 69(3):285-90 — <https://www.jstor.org/stable/27851195>（经典，DOI 前时代；注意原始出处常被写作 1977，正式发表为 1981）。

**已知缺陷**：固定 α 对长距离**系统性偏乐观**（Vickers 2016 实测常快 10 分钟以上）。本项目两条链路：

1. **个人化幂律**：用跑者自己的多距离成绩做 log-log 回归拟合个人 α（业余跑者常在 1.08–1.15），比固定 1.06 靠谱。`openrunner-core/projection.go`
2. **Riegel 等价**：Banister 拟合里把不同距离比赛换算到参考距离再比。`banister.go`（RaceRiegelEquivalent）

Cameron 1998 修正（距离相关指数）增益仅 1–3%，**不采用**（projection.go 注释有决策记录）。

### Vickers & Vertosick 全马预测（S25）

**模型 1**（一场比赛 + 周跑量）：

```
t_M = 0.16018617 + 0.83076202·t_Riegel + 0.06423826·(周英里/10)
```

**模型 2**（两场比赛 + 周跑量，论文中最准形态）。

**依据**：**Vickers AJ, Vertosick EA. 2016**, *An empirical study of race times in recreational endurance runners*. BMC Sports Sci Med Rehabil 8:26 — <https://doi.org/10.1186/s13102-016-0052-y>（moderate，系数取自论文 Additional file 1）。同文还给出「90% 规则」「10 天规则」（本项目未实现，仅预测模型）。

### Tanda 回归（S26）

```
全马配速 = f(周均跑量 km, 训练平均配速)     —— 多元线性回归
```

样本验证范围约 2:47–3:36，超出范围外推需谨慎。

**依据**：**Tanda G. 2011**, *Prediction of marathon performance time on the basis of training indices*. J Hum Sport Exerc 6(3):511-20 — <https://doi.org/10.4100/jhse.2011.63.05>（moderate，小样本）。

### 预测纪律：最慢可信值

多个模型并存时，`recommended` 取**所有可信预测里最慢（最大秒数）**的一个——「Plan from the slowest credible estimate」。周跑量样本 <4 周时 Vickers/Tanda 标低可信。

**代码**：`openrunner-core/coach/predict.go`

### Daniels-Gilbert VDOT（S46）

**含义**：VDOT = 最大摄氧量近似值；由任一比赛成绩反推 VDOT，再按百分比生成五档训练配速：

| 档位 | 用途 | %VDOT |
|---|---|---|
| E 轻松跑 | 有氧基础 | 59–74 |
| M 马拉松配速 | 比赛节奏 | 75–84 |
| T 阈值跑 | 乳酸阈 | 86–88 |
| I 间歇 | VO₂max | 95–100 |
| R 重复跑 | 神经肌肉 | 105（单点） |

**依据**：**Daniels J, Gilbert J. 1979**, *Oxygen Power: Performance Tables for Distance Runners*；**Daniels, Daniels' Running Formula 3rd ed. 2014**（书籍，practice 级但有发表方程）。

**代码**：`openrunner-core/coach/paces.go`（Daniels）

---

## 6. 训练配速与强度分布

### 80/20 七区（S48）

**含义**：80/20 Endurance（Matt Fitzgerald）的强度分区——Z1、Z2、X、Z3、Y、Z4、Z5 七区，X/Y 是「灰色地带」边界区。比例与 TrainingPeaks 的 80/20 计算器同款。

**依据**：**Fitzgerald M. 80/20 Running**（2014，书籍，practice）；分区计算器 TrainingPeaks 文档。

**代码**：`openrunner-core/coach/paces.go`（DefaultSpeedZones）

### 强度分布（Intensity Distribution）

周校验里统计 **X/Z3+ 占比**（中高强度时间占比），对照三种流派：

- **极化（polarized）**：约 80% 低 + 20% 高
- **金字塔（pyramidal）**：中间略多
- **阈值（threshold）**：中间最多

**依据**（S10–S13）：
- Rosenblat 2025 网络荟萃：无哪种分布明显最优，金字塔适合大众跑者。Sports Med 55:655-73 — <https://doi.org/10.1007/s40279-024-02149-3>（strong）
- Silva Oliveira 2024 荟萃：极化主要利好短周期/训练水平高者。Sports Med 54(8):2071-95 — <https://doi.org/10.1007/s40279-024-02034-z>（moderate）
- Muniz-Pumares 2025（151,813 场马拉松）：各水平马拉松跑者实际都是金字塔。Sports Med 55(4):1023-35 — <https://doi.org/10.1007/s40279-024-02137-7>（moderate）
- Muñoz & Seiler 2014：大众跑者极化 vs 金字塔无显著差异。IJSPP 9(2):265-72 — <https://doi.org/10.1123/ijspp.2012-0350>（moderate）

### 课型（plan_type）

easy / recovery / long / tempo / threshold / intervals / hills / race 八种，写入日历时落库，强度分布判定按课型估算质量时间占比。

---

## 7. 周计划校验规则

week-check 引擎（`openrunner-core/coach/week.go`）的每条规则都有稳定 Rule ID 与证据标签：

### +10% 单次规则（spike，S1）

**任何一次跑步不超过「它之前 30 天内最长一次」的 +10%**（公里口径 ±0.3km 舍入余量，分钟口径 ±3min）。

**依据**：**Frandsen JSB, Hulme A, Parner ET, et al. 2025**, *How much running is too much? Identifying high-risk running sessions in a 5200-person cohort study*. Br J Sports Med 59(17):1203-10 — <https://doi.org/10.1136/bjsports-2024-109380>（moderate–strong，5,205 人、588,071 次跑步的队列；同文证明周增比与 ACWR 不预测损伤）。

### 周增量规则（volume，S3/S4）

- 周跑量相对近 4 周均值跳升 **>30%** → WARN（S3 Nielsen 2014：跳跃 >30% 与距离相关损伤相关。JOSPT 44(10):739-47 — <https://doi.org/10.2519/jospt.2014.5164>，moderate）
- 周增 **+20~60%** 短期风险升高（S4 Damsted 2019：半马备赛队列。JOSPT 49(4):230-8 — <https://doi.org/10.2519/jospt.2019.8541>，moderate）
- 注意：经典「10% 周增规则」本身证据薄弱（S2 Buist 2008 RCT 无效 — <https://doi.org/10.1177/0363546507307505>；S5 综述），本项目用 S1 的单次 +10% 替代它做硬约束。

### 减量节奏（deload-rhythm，S6 practice）

连续 3 周爬升且无轻周 → WARN。来源为力量运动减量惯例，跑步无直接试验。

### 长课占比（long-share，S6 practice）

最长课占周量比例上限（约 30–35%），超出 WARN。教练惯例。

### 硬课数量与间隔（strength-count / spacing，S10–S13 + practice）

质量课（tempo/threshold/intervals/hills）每周数量上限与最小间隔（硬课之间至少隔一天轻松）。

### 力量课位置（strength placement，S35）

大重量力量课后紧接高强度跑 → WARN。

**依据**：**Doma K, Deakin GB, Bentley DJ. 2017**, *Optimizing the Order of Concurrent Training*. Sports Med 47(11):2187-2200 — <https://doi.org/10.1007/s40279-017-0758-3>（moderate）。

### 休息日（rest days，practice）

每周至少 1 天完全休息（新手/有伤史收紧到 2 天）。

### 周角色（WeekRole）

base / build / peak / deload / taper / race / return 七种角色，规则阈值随角色调整（如 taper 期允许负荷骤降、race 周不判 spike）。减量的学术依据：Bosquet 2007 荟萃（Sports Med 39:1358-65 — <https://doi.org/10.1249/mss.0b013e31806010e0>，strong）。

### 判定输出

每条判定 = `Rule ID + Level(OK/WARN/FAIL) + 英文 detail（金标准逐字）+ Params 结构化数值`，前端按 zh/en 模板渲染——保证与金标准（running-coach skill 的 check_week.py）可逐字对照的同时支持本地化。

---

## 8. 单课分析

### Pa:Hr 解耦（decoupling，S21）

**含义**：后半程「每单位心率跑出的速度」相对前半程的下降 %——有氧耐力不足时，同样心率配速越来越慢。

```
解耦% = ( (速度/心率)_前半 − (速度/心率)_后半 ) / (速度/心率)_前半 × 100
<5% 良好；>5% 提示有氧底子不够或热/脱水干扰
```

**依据**：Friel 的 TrainingPeaks 文章（practice 级）：[Aerobic Endurance and Decoupling](https://www.trainingpeaks.com/learn/articles/aerobic-endurance-and-decoupling/)、[Efficiency Factor and Decoupling](https://www.trainingpeaks.com/learn/articles/efficiency-factor-and-decoupling/)。马拉松内的解耦预测价值见 S22。

### EF — 效率因子（Efficiency Factor，S21）

```
EF = 平均速度 / 平均心率     （同配速心率越低越好）
```

跨课比较的有氧效率指标，**必须同条件比**（配速、地形、热）。

### 耐久性（Durability，S22/S23）

**含义**：疲劳后维持「心率→速度」关系的能力。马拉松后半程内部负荷/外部负荷解耦越慢，成绩越好。

**依据**：**Smyth B, Maunder E, Meyler S, Hunter B, Muniz-Pumares D. 2022**, *Decoupling of internal and external workload during a marathon*. Sports Med 52:2283-95 — <https://doi.org/10.1007/s40279-022-01680-5>（moderate，82,303 名跑者）；概念综述 Maunder 2021, Sports Med 51(8):1619-28 — <https://doi.org/10.1007/s40279-021-01459-0>。

### 心率漂移窗口

稳态后前 15 分钟 vs 末 15 分钟（<5% 为佳，S50），见 §4。

**代码**：`openrunner-core/coach/analysis.go`（CoachActivityAnalysis 的 Pa:Hr / EF / drift 三块）

---

## 9. 缩写速查表

| 缩写 | 全称 | 中文 | 章节 |
|---|---|---|---|
| NP | Normalized Power | 标准化功率 | §1 |
| IF | Intensity Factor | 强度因子 | §1 |
| VI | Variability Index | 变异指数 | §1 |
| TSS / rTSS | (running) Training Stress Score | （跑步）训练压力分数 | §1 |
| TRIMP | TRaining IMpulses | 训练冲量 | §1 |
| PowerLoad | BikeScore-like load | 功率负荷 | §1 |
| PMC | Performance Management Chart | 表现管理图 | §2 |
| CTL | Chronic Training Load | 长期训练负荷（体能） | §2 |
| ATL | Acute Training Load | 急性训练负荷（疲劳） | §2 |
| TSB | Training Stress Balance | 训练压力平衡 = CTL−ATL | §2 |
| ACWR | Acute:Chronic Workload Ratio | 急慢性负荷比（**有意不用**） | §2 |
| CP / CS | Critical Power / Speed | 临界功率 / 临界速度 | §3 |
| W′ / D′ | Anaerobic Work Capacity / Distance Reserve | 无氧做功容量 / 距离储备 | §3 |
| W′bal / DBal | W' balance / D' balance | W'/D' 余额 | §3 |
| FTP | Functional Threshold Power | 功能阈值功率 | §3 |
| MaxHR / RestHR | — | 最大 / 静息心率 | §4 |
| LTHR | Lactate Threshold HR | 乳酸阈值心率 | §4 |
| %HRR | Heart Rate Reserve % | 心率储备百分比（Karvonen） | §4 |
| VDOT | VO₂max-derived training pace index | 丹尼尔斯训练指数 | §5 |
| 80/20 | Polarized distribution | 极化强度分布 | §6 |
| X/Z3+ | 灰色地带时间 | 中高强度时间 | §6 |
| Pa:Hr | Pace:Heart Rate decoupling | 配速-心率解耦 | §8 |
| EF | Efficiency Factor | 效率因子 | §8 |
| GAP | Grade Adjusted Pace | 坡度修正配速（**未实现**） | — |
| EPOC | Excess Post-exercise Oxygen Consumption | 运动后过量氧耗（**未实现**） | — |

---

## 附：金标准对照

所有算法均有 Python 金标准参照（running-coach skill 的 `check_week.py`/`paces.py` 或合成夹具），由 `openrunner/scripts/golden-check.py` 与 `coach-check.py` 在生产路径上逐值比对；测试纪律见 DEVLOG（变异检验 + 真实数据交叉验证，「测试全绿」不算证据）。
