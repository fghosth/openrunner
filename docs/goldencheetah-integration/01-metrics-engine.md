# 01 · 度量引擎：RideMetric 工厂、依赖求解与 mean-max 计算

> 这是其余三个模块（PMC/CP/BikeScore）共同的地基。理解本篇后再看 02–05 会非常顺。

## 1. 逐秒数据模型（Layer 1）

### 1.1 RideFilePoint（FileIO/RideFile.h:503）

集成时需要支持的字段（GC 用 40+ 字段的扁平 struct，目标项目建议按需子集化）：

```
// 记录数据
secs   相对活动开始的秒数      hr    心率 bpm
kph    速度 km/h               watts 功率 W
nm     扭矩 N·m                cad   踏频 rpm
km     累计距离 km             alt   海拔 m
lon/lat 经纬度                headwind 风速(相对,km/h)
slope  坡度                    temp  温度
// 派生数据（GC 计算后缓存回点里）
xp, np, apower, atiss, antiss, gear ...
```

### 1.2 关键约定（所有算法都依赖）

- `recIntSecs()`：采样间隔。GC 支持 0.1–10s；**NP/xPower/TRIMP/W'bal 全部以它换算窗口大小**，不是硬编码 1s。
- 数据空洞（暂停/丢包）必须**填 0 补齐**成等间隔序列——WPrime.cpp:152 和 MeanMaxComputer（RideFileCache.cpp:1287）都做了这个，否则 xPower/mean-max 会严重失真。
- 时间倒退、单活动 >2 天（RideFileCache.cpp:1310）、>25 小时（WPrime.cpp:145）都直接放弃计算。

## 2. RideMetric 抽象（Metrics/RideMetric.h:52）

每个指标的骨架：

```cpp
class RideMetric {
    // 元数据
    virtual QString symbol()          // 稳定标识，如 "coggan_tss"、"skiba_bike_score"
    MetricType type_                  // Total|Average|Peak|Low|RunningTotal|StdDev
                                      //   → 决定跨天/跨区间聚合方式（PMC 聚合也用它）
    MetricValidity                    // Unreliable..High，科学可信度标注（:77）
    // 计算
    virtual void compute(RideItem *item, Specification spec,
                         const QHash<QString,RideMetric*> &deps) = 0;
    void setValue(double); void setCount(double);  // value + count(样本秒数)
    // 单位/聚合
    double value(bool useMetricUnits);             // 内置公英制换算
    virtual bool isRelevantForRide(ride)           // "P"=有功率; 跑步/游泳排除功率指标
};
```

**注册**（单例工厂，进程级静态初始化）：

```cpp
static bool added = RideMetricFactory::instance().addMetric(BikeScore());
```

工厂持有 `QHash<QString,RideMetric*> metrics` 与 `dependencyMap`（构造时从 `deps.append("coggan_np")` 这类调用收集，Coggan.cpp:389）。

## 3. 依赖求解：computeMetrics（Metrics/RideMetric.cpp:195）

GC 没有用真正的拓扑排序，而是**工作表循环 + 缺依赖回插**，等效于迭代拓扑排序：

```python
def compute_metrics(activity, wanted_metrics):
    done = {}                      # symbol -> 计算好的 metric 实例(带 value)
    todo = [m for m in wanted_metrics if is_builtin(m)] \
         + [m for m in wanted_metrics if is_user(m)]   # 用户自定义指标排最后
    while todo:
        sym = todo.pop(0)
        deps = factory.dependencies(sym)
        if any(d not in done for d in deps):
            todo += [d for d in deps if d not in done] # 依赖插队到队尾
            todo.append(sym)                            # 自己再排队等
            continue
        m = factory.new_metric(sym)     # ★ clone 新实例 —— 线程安全的关键
        m.compute(activity, spec, done)
        if sym in activity.overrides:   # 单次活动手动覆盖值
            m.override(activity.overrides[sym])
        done[sym] = m
    return done
```

集成要点：
- **clone-then-compute**：度量对象是注册时的原型，计算时 clone 出新实例，多活动并行计算互不干扰。纯函数式重写时这一条自动满足。
- 依赖在 `compute(item, spec, deps)` 内通过 `deps.value("coggan_np")` + `dynamic_cast` 取上游值（强类型断言）。
- 计算入口在 Core/RideItem.cpp:663：`RideMetric::computeMetrics(this, Specification(), factory.allMetrics())` —— 全量计算一次，结果存进 `RideItem.metrics[]` 数组并随 rideDB.json 持久化。
- `Specification` 是时间切片参数（区间/搜索过滤器），同一份逐秒数据可对任意区间重算（间隔分析用）。

## 4. mean-max（最佳功率/心率曲线）—— CP 模型与 CP 图表的数据源

### 4.1 算法（FileIO/RideFileCache.cpp:1230 `MeanMaxComputer::run`）

输入：某个序列的逐秒数组；输出：`bests[sec] = 该时长内可维持的最大均值`。

```
1. 预处理（按序列类型）：
   - 补 0 填洞、时间归零（offset 对齐）
   - IsoPower: 30s 滚动均值 ^4       (:1341)
   - xPower : 25s 指数加权均值 ^4     (:1371，权重公式与 BikeScore.cpp 相同)
   - VAM    : 高度差 × 360/Δt，削 <0
   - wattsKg: 除以骑手体重
2. 前缀积分 integrate_series() → divided_max_mean() 求"积分序列上的最大均值"
   （Mark Rages 高精度 mean-max 算法，利用积分数组 O(n) 求任意窗口均值）
3. 自适应步长遍历时长 (:1436)：
   <2min 步长 1s；<10min 2s；<20min 5s；<1h 20s；<2h 2min；之后 5min
4. 尾部 0 截断、缺口线性回填，得最终 bests[] 数组
```

### 4.2 缓存与聚合

- 单活动结果存 **.cpx 二进制缓存**（版本号 25，RideFileCache.h:44），下次直接读。
- 跨活动聚合：`RideFileCache::meanMaxPowerFor(context, from, to, ...)`（:173）把日期区间内每天的 bests 逐秒取 max 合成"赛季最佳曲线"——**CP 模型拟合用的就是这个聚合数组**。
- 集成时：mean-max 计算本身是 O(n)（积分法），无需 .cpx 那么激进的缓存，日级缓存即可。

## 5. 区间/运动员配置数据（度量计算的"上下文"）

| 数据 | GC 位置 | 被谁消费 | 集成契约 |
|---|---|---|---|
| CP / W' / FTP | `Zones::getCP(range)/getWprime`（Metrics/Zones.h:144）按日期区间生效 | TSS/IF、BikeScore、W'bal | `athlete.zones_for(date) -> {cp, wprime, ftp?}` |
| 按次覆盖 | RideFile metadata tag `CP`/`FTP`/`W'`/`Tau`（WPrime.cpp:186） | 所有 | `activity.overrides` |
| MaxHR/RestHR/LTHR | `HrZones::getMaxHr/getRestHr/getLT` | TRIMP 家族 | `athlete.hr_zones_for(date)` |
| 性别 | 全局设置 GC_SEX | TRIMP 的 ksex | athlete profile 字段 |
| 体重 | ride metadata / athlete measures | wattsKg、Aerolab totalMass | athlete profile 字段 |
| 传动效率 eta | Aerolab 设置（默认 1.0） | Aerolab | 配置项 |
| 训练计划标记 | `RideItem::planned` + linked activity | PMC 三序列 | activity.is_planned |

## 6. 数据契约（目标项目需要实现的最小接口）

```ts
// Layer 1: 逐秒序列
interface ActivitySeries {
  recIntervalSecs: number;
  time: number[];            // 相对秒（GC 约定从 0 开始连续）
  power?: number[]; hr?: number[]; speedKph?: number[];
  altitude?: number[]; headwindKph?: number[]; distanceKm?: number[];
  cadence?: number[]; torqueNm?: number[];
}
// Layer 2: 活动级
interface Activity {
  date: string; sport: 'bike'|'run'|'swim'|'other';
  isPlanned?: boolean;
  series?: ActivitySeries;          // 原始数据（可延迟加载）
  metrics: Record<string, number>;  // 度量快照（持久化）
  overrides?: Record<string, number>; // CP/FTP/W'/Tau 手动覆盖
}
// Layer 3: 运动员
interface Athlete {
  powerZones(date): { cp: number; wprime: number } | null;
  hrZones(date): { maxHr: number; restHr: number; ltHr: number } | null;
  sex: 'M'|'F'; weightKg: number;
}
```

度量函数签名（所有模块统一）：

```ts
computeMetric(sym: string, act: Activity, athlete: Athlete,
              spec?: {startSec: number, stopSec: number},
              deps?: Record<string, number>): number
```

## 7. GC 实现里的坑（移植时必须保留的防御）

1. NaN/Inf 检查：PMCData.cpp:278 显式丢弃 nan/inf 度量值（旧缓存里出现过）。
2. 负值/非法值：W'bal minY < −30000 直接清零（WPrime.cpp:291）；powerIndex 越界返回 0。
3. 除零：`recIntSecs()==0` 的文件（BikeScore.cpp:63）、无 CP 配置、总数为 0 的分母全部短路。
4. 空 spec 直接 NIL：`spec.isEmpty(ride)` 是所有 compute 的第一行。
5. 度量值以 float 存 rideDB.json（节省空间），比较时注意精度。
