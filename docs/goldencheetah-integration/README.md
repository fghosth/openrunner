# GoldenCheetah 模块集成研究文档

> 对照目标：GoldenCheetah 上游仓库（master @ b4a20bd9c）
> 目标：将 4 个核心分析模块集成到 openrunner 项目
> 文档基于对源码的逐行分析，所有行号均为当前 commit 下真实位置。

## 文档目录

| 文档 | 内容 | 集成优先级 |
|---|---|---|
| [00-overview.md](00-overview.md) | 架构总览、许可证警示、集成策略选择 | 必读 |
| [01-metrics-engine.md](01-metrics-engine.md) | 度量引擎：RideMetric 工厂、依赖求解、mean-max 计算（所有模块的地基） | ★★★ |
| [02-training-stress-metrics.md](02-training-stress-metrics.md) | TSS/NP/IF、xPower/BikeScore、TISS、TRIMP 家族 | ★★★ |
| [03-pmc-banister.md](03-pmc-banister.md) | PMC（CTL/ATL/TSB）与 Banister 刺激-响应模型 | ★★☆ |
| [04-cp-wprime.md](04-cp-wprime.md) | CP/W' 功率-时间模型族、W'bal、模拟退火 τ 求解 | ★★☆ |
| [05-aerolab.md](05-aerolab.md) | 虚拟海拔与 CdA/Crr 最小二乘估计 | ★☆☆ |
| [06-integration-roadmap.md](06-integration-roadmap.md) | 依赖替换映射表、移植顺序、测试策略 | 必读 |

## 四个模块的关系图

```
                ┌──────────────────────────────────────────┐
                │            逐秒采样数据 RideFilePoint      │
                │  secs/hr/kph/watts/alt/headwind/km ...    │
                └──────┬───────────────┬───────────────────┘
                       │               │
        ┌──────────────▼──────┐  ┌─────▼──────────────────┐
        │ 02 度量引擎 (01文档) │  │ mean-max 最佳功率曲线    │
        │  RideMetricFactory  │  │  (RideFileCache, 01文档)│
        └──┬───────┬─────────┬┘  └───────┬────────────────┘
           │       │         │           │
   TSS/BikeScore  TRIMP   W'bal 序列   CP/W'/Pmax 模型拟合
   (02文档)      (02文档)  (04文档)     (04文档)
           │       │         │           │
           └───┬───┴────┬────┘           │
               ▼        ▼                ▼
        ┌─────────────────────┐   ┌──────────────┐
        │ 03 PMC: LTS/STS/SB  │   │ Banister     │
        │  指数加权递归滤波     │◄──┤ power_index  │
        └──────────┬──────────┘   └──────────────┘
                   ▼
             训练负荷趋势图 / 疲劳监控

        Aerolab (05文档) 独立：只用逐秒 watts/kph/alt/headwind
```

## 快速结论（TL;DR）

1. **公式都很简单，复杂度在工程**：四个模块的数学核心加起来不到 200 行伪代码；GC 的复杂度在于度量工厂、缓存、信号失效和多线程。
2. **移植最小路径**：如果目标项目已有"逐秒活动数据 + 日汇总"两层结构，四个模块可全部重写为纯函数，无需引入 Qt。
3. **许可证是硬约束**：GPL v2（见 00 文档），直接翻译 GC 代码 → 衍生作品必须 GPL v2；只复现**已发表论文**中的公开算法则可自由选择许可证。
4. **建议顺序**：02 度量引擎的地基（NP/xPower/TRIMP）→ 03 PMC → 04 W'bal/CP → 05 Aerolab（可选，需要海拔+风速数据质量高）。

## 落地状态（截至当前版本）

| 模块 | 文档 | 状态 | 说明 |
|---|---|---|---|
| 度量引擎地基 | 01 | ✅ | 未搬 GC 的度量工厂/惰性失效，用「写入时预计算 + recompute-all」替代 |
| 训练负荷（TSS/NP/TRIMP 家族） | 02 | ✅ 大部分 | TRIMP / TRIMP(100) / TRIMP Zonal / NP / rTSS / 功率负荷 全部落地；xPower / BikeScore / TISS 原版未移植（BikeScore 用 `Rtss(NP,CP)` 近似，TISS 为骑行专用） |
| PMC | 03 Part A | ✅ | LTS/STS/SB/RR + 赛季播种 + 计划/预期叠加 |
| Banister | 03 Part B | ✅ | 固定 τ₁=45/τ₂=15 使模型对 p₀/k₁/k₂ 线性化（纯 Go 最小二乘），需比赛成绩作为观测点 |
| CP/W'/W'bal | 04 | ✅ 核心 | CS/D' 拟合、D'bal、CP/W'、W'bal、CP3（纯 Go LM）；模拟退火 CPSolver 用 Skiba 2012 闭式公式替代 |
| Aerolab | 05 | ❌ 不做 | 需高质量海拔+风速与恒定功率输出，本质是骑行 TT/铁三的空气动力学工具；跑步 15 km/h 以下空气阻力占比 <5%，拟合噪声盖过信号 |

实现细节与踩坑记录见 `docs/DEVLOG.md`；票据见 `.scratch/gc-completion/`。
