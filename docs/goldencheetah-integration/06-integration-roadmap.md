# 06 · 集成路线图：依赖替换、移植顺序与测试策略

## 1. 依赖替换映射表

| GC 依赖 | 用途 | openrunner 替代方案 |
|---|---|---|
| Qt Core（QHash/QVector/QDate/QString） | 容器/日期 | 目标语言原生（TS: Map/Date；Python: dict/datetime） |
| Qt 信号槽（invalidate/refresh） | 惰性失效 | 纯函数 + 上层 memo（推荐）；或 Rx/observable |
| QThread（WPrimeIntegrator、Estimator、MeanMaxComputer） | 并行 | 4 个算法都是 O(n)，先串行；需要时 Web Worker / multiprocessing |
| Qwt（QwtSyntheticPointData/QwtSpline/QwtPlotCurve） | 绘图+样条 | 图表用目标项目自己的方案；样条用线性插值替代（误差可忽略） |
| **lmfit lmcurve** | LM 非线性拟合（Banister、PD 模型） | Python: `scipy.optimize.least_squares`；TS: `ml-levenberg-marquardt` / `numeric.js`；或自写 GN 迭代（2-3 参数问题足够） |
| appsettings / Zones / HrZones | 运动员配置 | Athlete profile 表（01 文档 §5 契约） |
| rideDB.json / .cpx | 度量/mean-max 缓存 | 活动表冗余列（日度量）+ 按需重算 |
| RideFile 格式解析器（143 个） | 导入 | **不需要**；openrunner 若要导入 FIT 用 fit-parser 类库即可 |

## 2. 移植顺序（依赖驱动）

```
Phase 0  数据契约层（01 文档 §6）                        ~0.5 天
  ActivitySeries / Activity / Athlete 接口 + 测试数据装载器
  ─────────────────────────────────────────────
Phase 1  日度量核心（02 文档）                           ~2 天
  average/np/xPower/if/tss/bikeScore/ri/trimp/tiss → 每活动快照
  产出: DailyMetric 表 → 已可支持 Phase 2/3
Phase 2  PMC（03 文档 Part A）                           ~1 天
  指数加权递推 + 赛季播种 + planned/expected 三序列 + RAG 阈值
  产出: 趋势页主图（产品核心价值点）
Phase 3  W'bal（04 文档 §3）                             ~1.5 天
  1s 重采样 + 积分式 W'bal + τ 公式 + min/expended 汇总
  依赖: 逐秒功率 + CP/W' 配置（无 CP 时的降级策略要有: 用 FTP 估 CP）
Phase 4  CP 拟合（04 文档 §1-2）                         ~2-3 天
  mean-max 聚合（01 文档 §4）→ CP2 线性回归 → LM 拟合（可选 CP3）
  产出: CP/W' 自动估计 → 回填 Phase 3 的参数源
Phase 5  Banister（03 文档 Part B）                      ~2-3 天
  power_index + 测试标记数据模型 + 窗口划分 + LM 拟合
  可选: 无测试数据时用"每日最优均值"替代测试点（GC Estimator 路线）
Phase 6  Aerolab（05 文档）                              ~1-2 天（可选）
  estimateCdACrr + VE 曲线；仅骑行 + 有海拔数据时开放
```

总量估算：核心分析能力（Phase 0–3）一周内可用；完整对齐 GC（+4/5/6）约 2–3 周。

## 3. 各阶段的验收测试

### 3.1 金标准对照（最重要）

1. 在 GC 里选 3–5 个代表性活动（平稳骑行/间歇/含空洞的长骑）导出 CSV；
2. openrunner 用同一 CSV 计算全部指标；
3. 比对阈值：日度量 <0.5%（GC rideDB 以 float 存储，本身有精度损失）；W'bal 序列逐点 <1J（样条 vs 线性插值差异集中在此，允许放宽或同样实现样条）。

### 3.2 解析恒等式（无 GC 也应过）

| 用例 | 期望 |
|---|---|
| 恒功率 P=CP 骑 1h | NP=xPower=P, IF=RI=1, TSS=BikeScore=100 |
| 恒功率 P 骑 t 秒 | W'bal ≡ W'（无超 CP 消耗），TRIMP 随 %HRR 单调 |
| 全程功率 0 | TSS=BikeScore=0, W'bal≡W', mean-max 全 0 |
| CP 以下恒功率骑到力竭 | W'bal 按微分式恢复公式单调回升 |
| 30s on/off 间歇 | NP > AP 且 xPower > NP（平滑窗差异） |
| PMC 播种 | 赛季起点 seed=s 后首日 LTS=STS=s |
| Banister 无训练日递推 | g/h 严格按 exp(−1/t) 衰减，perf → p0 |

### 3.3 性能基准

- 4h@1s 活动：W'bal + 全日度量 < 500ms（GC 注释称 250ms，现代硬件应轻松达标）；
- 1000 活动 × 3 年日度量：PMC 全量重算 < 100ms（10 万日 × 10 行递推）。

## 4. 数据模型落库建议

```sql
-- Phase 1 产出
CREATE TABLE activity_metrics (
  activity_id, date, sport, is_planned,
  tss REAL, bike_score REAL, trimp REAL, trimp100 REAL,
  np REAL, xpower REAL, if_ REAL, ri REAL, atiss REAL, antiss REAL,
  PRIMARY KEY(activity_id));

-- Phase 2 配置
CREATE TABLE seasons (id, name, start_date, end_date, seed REAL);

-- Phase 4/5 产出
CREATE TABLE pd_estimates (
  id, athlete_id, from_date, to_date, model TEXT,   -- cp2/cp3/...
  cp REAL, wprime REAL, ftp REAL, pmax REAL, parameters JSON);
CREATE TABLE performances (                         -- Banister 测试点
  id, athlete_id, date, power_index REAL, source TEXT); -- test|estimate

-- W'bal 序列（可选缓存，不存则按需重算）
CREATE TABLE wbal_cache (activity_id, t INTEGER, wbal REAL);
```

## 5. 风险清单

| 风险 | 缓解 |
|---|---|
| 许可证污染（GPL v2） | 全程按论文复现（00 文档路径 B）；实现者不读 GC 源码；本文档仅记录算法规格与行为核对点 |
| 拟合发散（Banister/CP） | 参数加界（k1,k2∈[0,1]，p0>0）；RMSE 门槛；测试点 <5 时禁用拟合只画 PMC |
| 无功率数据用户（跑步为主） | TRIMP/PaceZone 链路独立可用（aCoggan.cpp 跑步版参考）；功率功能按数据可用性渐进启用 |
| CP/W' 未配置 | W'bal 默认 CP=250W/W'=20000J 会误导 → 无配置时隐藏该功能并引导设置 |
| GPS 海拔噪声（Aerolab） | 进入估算前平滑 + 要求环路（起点终点海拔差 <5m） |
| 时区/DST | 日度量聚合按**活动本地日期**（GC 用 dateTime.date()），跨午夜活动归开始日 |

## 6. 与 GC 保持行为一致的细节速查

1. NP/xPower 的窗口数 `30/dt`、`25/dt` 在 dt>窗长时退化为无平滑；
2. xPower 空洞衰减循环（02 文档 §2）——漏掉则间歇课 xPower 虚高 1-3%；
3. PMC SB 的 day+1 偏移与 GC_SB_TODAY 开关；
4. W'bal 积分式里 τ 由数据计算、忽略 metadata Tau（:217 条件 `!TAU || integral`）；
5. TRIMP 的 ksex=1.92/1.67 与 0.64 前置系数（TRIMPPoints.cpp:32）；
6. powerIndex 的参考运动员三元组 (261, 15500, 1100)（Banister.cpp:57）；
7. CPSolver 的力竭阈值 500J（CPSolver.cpp:116）；
8. mean-max 自适应步长表（01 文档 §4.1）——步长不同 CP 拟合点会不同。
