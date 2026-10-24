# 04 · 临界功率 CP / W' 模型与 W'bal

> 源文件：`Metrics/PDModel.{h,cpp}`（1438 行）、`Metrics/WPrime.{h,cpp}`（1498 行）、`Metrics/CPSolver.{h,cpp}`（370 行）、`Metrics/Zones.{h,cpp}`（参数存储）、`Metrics/Estimator.{h,cpp}`（估计持久化）
> 第三方依赖：contrib/lmfit（LM 拟合）、QwtSpline（1s 重采样，可用任意样条/线性插值替代）

## 1. 模型族总览（PDModel.h）

所有模型继承 `PDModel : QObject, QwtSyntheticPointData`——**模型即数据源**，`y(t)` 直接供 QwtPlotCurve 绘制 0–18000s（PDMODEL_MAXT）的理论曲线。集成时只需保留 `y(t)` + 参数提取。

| 模型 | code | 公式 | 可产出 |
|---|---|---|---|
| CP2Model 经典 2 参数 | cp2 | `P(t) = CP + W'/t` | CP, W' |
| CP3Model Morton 3 参数 | cp3 | `P(t) = CP + W'/(t+k)` | CP, W', Pmax |
| WSModel Ward-Smith | WS | 半经验模型 | CP, W', FTP, Pmax |
| MultiModel Veloclinic | Velo | 双分量指数 `w1·(1−exp(−t/tau1)) + ...`，2 个 variant | CP, W', FTP, Pmax |
| ExtendedModel 扩展 8 参数 | ext | 见下 | CP, W', FTP, Pmax |

ExtendedModel 的 8 参数式（PDModel.h `ExtendedModel::f`，时间 x 以分钟计）：

```
rt = paa·(1.20 − 0.20·exp(−x))·exp(paadec·x)                      # 短时磷酸原项
   + CP·(1−exp(taudel·x))·(1−exp(cpdel·x))·(1+cpdec·exp(cpdecdel/x))·(tau/x)   # 中间项
   + CP·(1−exp(taudel·x))·(1−exp(cpdel·x))·(1+cpdec·exp(cpdecdel/x))           # 长时平台项
```

## 2. 三种拟合方式（fittype）

```
Envelope          直接用 mean-max 包络线（不拟合，画参考）
LinearRegression  2 参数模型: P = CP + W'/t  →  对 (1/t, P) 最小二乘
                  slope = W', intercept = CP（PDModel.cpp:157 附近）
LeastSquares      lmcurve LM 非线性拟合，各模型实现 nparms()/f()/setParms()
```

### 2.1 拟合点选择 deriveCPParameters（PDModel.cpp:140）

```
输入: mean-max 数组（赛季聚合，来自 RideFileCache::meanMaxPowerFor，见 01 文档 §4）
      + 四个生理区间边界 sanI1..laeI2（无氧/有氧阈值等，用于选点）
过程: 在 mean-max 数组上挑出"最佳拟合点"集合（跳过低质量短时长点）
输出: tdata[]（时长）与对应功率点 → 交给三种拟合方式
```

### 2.2 CP2 模型的参数回收

LM 拟合返回 `[CP, W']` 后（PDModel.h `CP2Model::setParms`）：

```
cp  = parms[0]
tau = parms[1] / (cp · 60)     # GC 内部存 tau（分钟级时间常数）而非裸 W'
W'  = tau · cp · 60            # 读出时反推
```

### 2.3 估计值持久化

`PDEstimate { from, to, model, WPrime, CP, FTP, PMax, EI, wpk, sport, parameters[] }`
由 Estimator（QThread，15s 延迟防抖）全库计算，供"CP 历史趋势"和 Banister 使用；`saveParameters/loadParameters` 让曲线能用**已存的参数**直接重绘（不必重新拟合）。

## 3. W'bal：W' 的实时消耗-补充模型（WPrime.cpp）

### 3.1 输入准备（setRide，:109）

```
1. 逐点展开为 1s 序列，空洞填 0（:152），时间 >25h 直接放弃
2. QwtSplineCubic 重采样平滑（0.5s 步长）→ smoothed / distance 两条曲线
3. 参数: CP、W' ← 功率区间(按活动日期) ;  metadata "CP"/"W'"/"Tau" 可按次覆盖
4. powerValues[i] = max(0, P[i] − CP)        # 超临界功率部分
```

### 3.2 公式一：积分式（Skiba 2012，`GC_WBALFORM=int`，默认）

论文：Skiba et al., *"Modeling the Expenditure and Reconstitution of Work Capacity above Critical Power"*, MSSE 2012。

```
τ (Tau) = 546 · exp(−0.01 · (CP − P̄below)) + 316      # P̄below = CP 以下功率均值
          （CP 以下无数据时用 CP 本身代入；积分式下 τ 恒由数据算，metadata Tau 被忽略 :217）

I(t)     += exp(t/τ) · (P(t) − CP)+                   # 只对超 CP 部分累加
W'reexp(t) = exp(−t/τ) · I(t)                         # 指数衰减的"已消耗总量"
W'bal(t)   = W' − W'reexp(t)
```

**工程实现**（对性能至关重要，文件头注释 :31-48）：
- 不对每个 t 回看历史，而是**每秒向前积分衰减**（WPrimeIntegrator::run，:645）——CPU 省 4 倍；
- `WPrimeIntegrator` 是 QThread，分段并行（4h 骑行 250ms vs 串行 4000ms）；
- 输出 `output[t]` 即消耗量，W'bal = W' − output[t]。

### 3.3 公式二：微分式（Froncioni/Clarke，`GC_WBALFORM=diff`）

```
P(t) < CP :  W'bal += (CP − P) · (W' − W'bal) / W'    # 按"剩余比例"比例再充
P(t) ≥ CP :  W'bal += (CP − P)                         # 负增量（线性扣减）
```

- 恢复速率依赖**当前剩余量**（空了恢复快、满了恢复慢），行为与积分式不同；
- τ 此时可用 metadata Tau 或 Skiba 公式（:217 的 `if (!TAU || integral)` 逻辑）。

### 3.4 派生量

```
Matches:      连续超 CP 且 W' 消耗 >100J 的段落（25s 平滑后识别，:65/:322）
W'bal Zones:  W1..W4 = W' 剩余 0-25/25-50/50-75/75-100%（低/中/重/严重疲劳）
派生度量 ~20 个: min_wprime / wprime_expended / wbal 区间时间(TiZ)/区间做功/CPExp...
summarize():  生成 HTML 区间表（zoneName/zoneLo/zoneHi :84-87）
```

W'bal 序列还输出到 RideFileCache（.cpx v23+ 含 W'bal TiZ/分布）和 AllPlot 图表叠加。

## 4. CPSolver：模拟退火求 τ（CPSolver.cpp）

**目的**：积分式的 τ 公式（546·exp(...)）是群体平均；CPSolver 用**个人力竭数据**反推个人化 τ（和 CP/W' 校准）。

```
输入: 多次"力竭骑行"（reference points: 用户标记的力竭时刻 secs）
      → 每次取 0..力竭秒 的功率 1s 数组 power1s() (:58)

目标函数 cost(parms) (:74):
  对每次力竭骑行跑积分式 W'bal 到力竭点
  cost = mean( (W'bal_end − 500)² ) / 1000
  # 500J 是经验力竭阈值: Vickers等, PubMed 24509723 —— W' 不可能真耗到 0

搜索: 模拟退火 (:124 neighbour)
  参数: CP, W', TAU 三元组
  约束: constraints {cpf..cpto, wf..wto, tf..tto}（用户在 ExhaustionDialog 设置）
  步长: 从 ±150% 线性收敛到 ±1%（k/kmax 因子）(:136)
  接受准则: 标准 Metropolis（温度随 k 下降）
```

UI：`Charts/ExhaustionDialog.cpp` 选择力竭活动/标记点，后台跑退火，结果写回区间设置（个人 τ）。

## 5. 参数存储（Zones.h）

```
Zones: 按日期区间列表存 { CP, Wprime, ... }（:144 getCP / :150 getWprime）
HrZones: { MaxHr, RestHr, LT } 
覆盖链: ride metadata "CP"/"W'"/"Tau"  >  zone(日期)  >  默认 250W/20000J（:178-179）
```

集成建议：openrunner 用一张 `athlete_power_profile(date_range, cp, wprime, ftp)` 表 + `activity.overrides` 即可等价实现。

## 6. 集成清单

| 项 | GC 位置 | 移植 |
|---|---|---|
| 1s 重采样+填洞 | WPrime.cpp:131-176 | 必做；样条可换线性插值（精度差异极小） |
| 积分式 W'bal + 前向积分 | :228-249, :645-654 | 核心；O(n) 无需并行 |
| 微分式 W'bal | :261-289 | 可选（配置开关） |
| τ 公式 | :217-224 | 纯公式 |
| Matches 识别 | :322+ | 可选 |
| mean-max 聚合数组 | 01 文档 §4 | CP 拟合的前置 |
| 2 参数 LM 拟合 | PDModel.cpp:299 | scipy.least_squares / JS 库 |
| 模拟退火 τ | CPSolver.cpp | 可选进阶（个人化 τ） |
| Estimator 持久化 | Estimator.cpp | 可简化为按需计算+缓存表 |

### 6.1 最小可用版本（MVP）

```
1. 活动页 W'bal 曲线: CP/W' 手动设置 + 积分式 + τ 公式     ← 150 行内可完成
2. 度量: wprime_expended / min_wprime 两个汇总值
3. 进阶: CP2 拟合（需要 mean-max）→ W'bal 曲线自动用拟合 CP/W'
```

### 6.2 数值注意

- τ 公式里 `(CP − P̄below)` 若 P̄below > CP（纯间歇课无低于 CP 数据时不太可能，但分段区间可能）→ 指数为正、τ 巨大 → 需要钳制（GC 未显式钳制，靠 TAU=int() 截断兜底）。
- 微分式在 W'bal→0 附近恢复项 →0，永远不会真正归零（与积分式一致）；长骑行中数值漂移 GC 用 `minY<−30000 → 0` 兜底。
- 积分式中 `exp(t/τ)` 在 t≫τ 时溢出风险：GC 用 double（~1e308），t=5h、τ=300s 时 exp(60)≈1e26 安全；但**移植到 float 会溢出**，务必用 double 并按前向积分实现（先 exp(+t/τ) 累加再乘 exp(−t/τ)，而非先乘后加）。
