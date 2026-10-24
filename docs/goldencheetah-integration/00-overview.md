# 00 · GoldenCheetah 架构总览与集成策略

## 1. 项目基本盘

| 项 | 值 |
|---|---|
| 语言/框架 | C++ / Qt5（Widgets + Concurrent）+ Qwt 6 |
| 构建 | qmake（`build.pro` + `src/gcconfig.pri.in`），AppVeyor CI 三平台 |
| 规模 | `src/` 12 个模块、845 个 .cpp/.h、约 44MB 源码 |
| 第三方（bundled） | `contrib/`（含 **lmfit**：lmcurve Levenberg-Marquardt，本研究所用模块唯一硬依赖的数值库）、`qwt/`（绘图） |
| 许可证 | **GPL v2** |

## 2. 与本研究相关的模块地图

```
src/
├── Core/            # 运动员、上下文、数据缓存骨架
│   ├── RideItem.{h,cpp}        # 一次活动的内存表示 + 度量计算入口 (:663)
│   ├── RideCache.{h,cpp}       # 所有 RideItem 的列表 + .json 持久化
│   ├── RideCacheModel / RideDB # 度量缓存 (rideDB.json)
│   ├── Athlete.{h,cpp}         # 运动员聚合根：zones/hrZones/rideCache/seasons
│   ├── Context.{h,cpp}         # 运行时上下文：所有 invalidate 信号的中枢
│   ├── Specification.{h,cpp}   # 时间区间/筛选器（度量计算的切片参数）
│   ├── Seasons / Measures / Zones 相关
│   └── Units.h                 # 常量：KG_FORCE_PER_METER=9.80665 等
├── FileIO/
│   ├── RideFile.{h,cpp}        # 逐秒数据容器：RideFilePoint 结构体
│   ├── RideFileCache.{h,cpp}   # mean-max 预计算 + .cpx 二进制缓存 + MeanMaxComputer
│   └── 143 个格式解析器（FIT/CSV/GPX...，集成时不需要）
├── Metrics/         # ★ 本研究核心
│   ├── RideMetric.{h,cpp}      # 度量基类 + 工厂 + computeMetrics 依赖求解
│   ├── Coggan.cpp              # NP/IF/TSS（内部名 BikeStress）
│   ├── BikeScore.cpp           # xPower/RI/BikeScore + TISS
│   ├── TRIMPPoints.cpp         # TRIMP 家族
│   ├── PMCData.{h,cpp}         # PMC 数据层（LTS/STS/SB/RR）
│   ├── Banister.{h,cpp}        # Banister 模型 + lmcurve 拟合
│   ├── PDModel.{h,cpp}         # 功率-时间模型族（CP2/CP3/WS/Velo/Extended）
│   ├── CPSolver.{h,cpp}        # W'bal τ 模拟退火求解器
│   ├── WPrime.{h,cpp}          # W'bal 两种公式 + 并行积分器 + ~20 个度量
│   ├── Estimator.{h,cpp}       # 后台线程：power_index 表现估计（供 Banister）
│   └── Zones.{h,cpp} / HrZones # 功率/心率区间存储（CP、W'、FTP、MaxHR、RestHR）
├── Charts/          # 展示层（本研究只读不搬）
│   ├── LTMPlot/LTMTool/LTMWindow # PMC 趋势图的宿主（createPMCData :3338）
│   ├── CriticalPowerWindow.cpp   # CP 曲线图（2096 行）
│   ├── Aerolab.{h,cpp}           # ★ 物理计算在这里（不是纯 UI）
│   └── CPPlot.cpp                # PD 模型曲线绘制
└── contrib/lmfit/   # lmcurve：LM 非线性拟合（Banister + PDModel 使用）
```

## 3. 贯穿全局的四个工程机制

### 3.1 数据三层结构（集成时必须复刻的骨架）

```
Layer 1  RideFile          逐秒采样数组（secs/hr/kph/watts/alt/headwind/km/nm/cad...）
         recIntSecs()      采样间隔（通常 1s，可 0.1~10s，算法都要除以它）
Layer 2  RideItem          一次活动 = RideFile + 元数据 + 度量结果数组 metrics[]
                           metrics[index] 按工厂注册顺序存 double，全活动只算一次
Layer 3  RideCache         全部活动 + rideDB.json 磁盘缓存（含每个活动的度量快照）
```

PMC 等趋势计算读 Layer 2/3 的日度量；W'bal、xPower 读 Layer 1 的逐秒序列；CP 拟合读 Layer 1 预计算出的 mean-max 数组。

### 3.2 度量工厂 + 依赖求解（见 01 文档）

所有指标是 `RideMetric` 子类，静态注册进单例工厂；`RideMetric::computeMetrics()` 用"工作表 + 依赖回插"的方式拓扑排序计算。这是 TSS→PMC 数据流的发动机。

### 3.3 惰性失效三段式

```
connect(context, SIGNAL(rideAdded/rideDeleted/itemChanged...), SLOT(invalidate()))
invalidate() { isstale = true; }
refresh()    { if (!isstale) return; ...重算...; isstale=false; }
```
PMCData、Banister、Estimator 全是这个模式。集成建议：改为纯函数式重算 + 上层做 memo，可省掉信号网。

### 3.4 全局互斥保护的 lmfit

lmfit 是 C 库，通过**全局静态变量**传递拟合上下文，所以 GC 里所有 `lmcurve()` 调用都包 `QMutex calllmfit`（Banister.cpp:530、PDModel.cpp:299）。如果目标项目一次只跑一个拟合可以去掉锁，多线程则需每线程独立实例。

## 4. ⚠️ 许可证约束（集成前必须决策）

GoldenCheetah 是 **GPL v2**。两条路径：

| 路径 | 做法 | 后果 |
|---|---|---|
| A. 代码移植 | 参照/翻译 GC 源码（哪怕改语言） | 目标项目整个变成 GPL v2 衍生作品 |
| B. 算法复现 | **只依据已发表的论文与公开标准**重新实现 | 无传染；公式本身不受版权保护 |

关键事实：
- 路径 B 完全可行——本研究的所有算法都有论文出处（见各模块"科学来源"小节），GC 代码里也逐条注明了。需要来自论文的**常数**（xPower 25s 窗口、TRIMP 0.64/1.92/1.67、Skiba τ 公式 546·exp(−0.01·ΔCP)+316、W'bal 力竭阈值 500J 等）均出自论文而非 GC 独有。
- 清洁室（clean-room）纪律：实现者只读论文/规格，不读 GC 源码；本系列文档按"算法规格"而非"代码翻译"撰写，行号引用仅用于**核对行为**。
- 如目标项目本身开源且愿意 GPL v2，路径 A 最省力、行为 100% 一致。

## 5. 集成策略推荐

**推荐：路径 B + 按模块渐进**（理由见 06 文档）：

1. 先定义两个数据接口（见 01 文档 §6 的数据契约）：
   - `ActivitySeries`：逐秒序列（time, hr, speed, power, altitude, headwind, distance, cadence...）
   - `DailyMetric`：日期 → {tss, bikeScore, trimp, ...} 的日汇总表
2. 依次实现：NP/xPower/TRIMP 纯函数 → PMC 滤波 → W'bal 序列 → CP 拟合（lmfit 可换 scipy.optimize.least_squares / JS 的 ml-levenberg-marquardt 等）→ Banister → Aerolab。
3. 用 GC 导出的真实活动数据做金标准回归测试（GC 可导出 CSV/JSON，逐指标比对）。
