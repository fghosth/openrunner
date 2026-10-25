# 02 · 训练压力指标：TSS/NP/IF、xPower/BikeScore、TISS、TRIMP

> 源文件：`Metrics/Coggan.cpp`（396 行）、`Metrics/BikeScore.cpp`（502 行）、`Metrics/TRIMPPoints.cpp`（413 行）、`Metrics/aBikeScore.cpp`、`Metrics/aCoggan.cpp`（跑步版）
> 依赖：01 文档的度量引擎；无第三方数值库。

## 0. 科学来源

| 指标 | 论文/标准 |
|---|---|
| NP/IF/TSS | Coggan 训练压力体系（TrainingPeaks 公开定义） |
| xPower/RI/BikeScore | Skiba, *"Analysis of Power Output and Training Stress in Cyclists"*（GC 仓库 `doc/user/bikescore.pdf` 有全文；BikeScore.cpp:30 注明权重取自 Skiba 提供的电子表格） |
| TISS | Skiba 提供的双指数剂量-反应曲线（GC 内实现） |
| TRIMP | Morton/Banister 训练冲量 + Green et al 系数（TRIMPPoints.cpp:32 注明） |
| TRIMP Zonal（Edwards 系） | 区界 64/77/84/90/95% MHR、系数 0.84/1.65/2.57/4.01/5.91 —— **转引自 GC TRIMPZonalPoints.cpp，原始论文溯源挂账 DEVLOG** |
| Skiba 跑步配速分区 | %CV 6 区 80/87/95/105/119% —— 转引自 GC PaceZones.cpp（"based upon the Skiba run pace zones"） |

## 1. Coggan 链：NP → IF → TSS

### NP（IsoPower，symbol `coggan_np`，Coggan.cpp:60）

```
rollingwindowsize = 30 / recIntSecs        # 30 秒窗口
对每个采样点：30s 滚动均值 → 4 次方累加
NP = (Σ (滚动均值)^4 / count) ^ 0.25       # 4 次方均值开 4 次方根
```

- 采样间隔 ≥30s 时窗口=1，退化为 `(Σ P^4/n)^0.25`（无平滑）。
- 与 mean-max 预处理里的 IsoPower 是**同一公式**（RideFileCache.cpp:1341），保证 NP 曲线和 NP 度量一致。

### IF（`coggan_if`，Coggan.cpp:152）

```
IF = NP / FTP     # FTP 取骑行 metadata "FTP" 或功率区间设置
                  # 若全局设置 useCPforFTP，则用 CP 代替 FTP
```

### TSS（`coggan_tss`，内部名 BikeStress，Coggan.cpp:218）

```
normWork  = NP × duration_secs
rawTSS    = normWork × IF
TSS       = rawTSS / (FTP × 3600) × 100        # 等价 100·hours·IF²
```

条件：仅自行车（`isRun/isSwim` 置 NIL）；无功率数据不适用。
同类：`coggan_tssperhour`、跑步版 `aCoggan.cpp`（rTSS，用速度/配速区间）。

## 2. Skiba 链：xPower → RI → BikeScore

### xPower（`skiba_xpower`，BikeScore.cpp:39）

**25 秒指数加权移动平均**（这是与 NP 的 30s 矩形窗的本质区别）：

```
sampsPerWindow = 25 / Δt
attenuation  = sampsPerWindow / (sampsPerWindow + Δt)   # 每个采样点的衰减
sampleWeight = Δt / (sampsPerWindow + Δt)               # 新采样点权重

# 逐点迭代（含数据空洞时按时间衰减，BikeScore.cpp:87 的 while 循环）
weighted   = weighted * attenuation + sampleWeight * P
xPower     = (Σ weighted^4 / count) ^ 0.25
```

注意两个实现细节：
- **空洞处理**（:87-93）：若当前点与上点间隔 >1 个采样，先把 weighted 连续衰减（每 Δt 乘一次 attenuation）补齐空档，否则 EWMA 会虚高。
- 权重公式在 RideFileCache.cpp:1373 被原样复刻用于 mean-max 预处理。

### RI（`skiba_relative_intensity`，BikeScore.cpp:152）

```
RI = xPower / CP        # CP 取 metadata "CP" 或区间设置；与 IF 用 FTP 对应
```

### BikeScore（`skiba_bike_score`，BikeScore.cpp:387）

```
BikeScore = 100 · (xPower × secs × RI) / (CP × 3600)    # = 100·hours·RI²
```

### Response Index（`skiba_response_index`，:435）

`xPower / 平均心率` —— 类似 EF（效率因子），用于追踪有氧效率趋势。

## 3. TISS：有氧/无氧双通道（BikeScore.cpp:240-435）

对每个采样点套用**双指数剂量-反应函数**，按秒累加：

```
aTISS = Σ Δt · 0.663788683661645 · exp(−7.5095428451195 · exp(−0.86118031563782 · P/CP))
anTISS= Σ Δt · 0.238923886004611 · exp(−61.849          · exp(−1.73549567522521 · P/CP))
TISS Aerobicity (dTISS) = aTISS / (aTISS + anTISS) × 100%
```

- 形状：低强度时有氧项缓慢累积；接近/超过 CP 时无氧项急剧上升（b=−61.8 非常陡）。
- 常数是曲线拟合结果（非论文公开值，GC 实现）——集成时直接采用并在配置里允许调整。
- GC 还把这些值写回每个采样点（RideFilePoint.atiss/antiss）供图表用。

## 4. TRIMP 家族（TRIMPPoints.cpp）

### TRIMP Points（`trimp_points`，:39）

Morton/Banister 公式 + Green 系数：

```
%HRR  = (AvgHR − RestHR) / (MaxHR − RestHR)
Ksex  = 1.92 (男) / 1.67 (女)                 # GC_SEX 设置
TRIMP = (secs/60) · %HRR · 0.64 · exp(Ksex · %HRR)
```

- **基于平均心率，非逐秒积分**——实现最简单的心率负荷指标。
- RestHR/MaxHR 来自心率区间（按日期生效），`Rest HR` 可被单活动 metadata 覆盖（:72）。
- 前置条件：`hrZoneRange >= 0`，否则 NIL。AvgHR < RestHR → 0 分。
- 时长优先 `time_riding`，缺则用 `workout_time`（:85）。

### TRIMP(100)（`trimp_100_points`，:108）

归一化：1 小时阈值心率骑行 = 100 分

```
TRIMP100 = 100 · TRIMP / [60 · (LTHR−RHR)/(MHR−RHR) · 0.64 · exp(Ksex·(LTHR−RHR)/(MHR−RHR))]
```

用途：不同运动员/不同 MHR-RHR 区间下可横向比较。

### TRIMP Zonal（`trimp_zonal_points`，:173）

按 5 个心率区间的**时间占比 × 区间系数**加权（系数注释见 :167-171，两套可选体系）：

```
EDwards 风格: 0.84 / 1.65 / 2.57 / 4.01 / 5.91   （64-76% / 77-83% / 84-89% / 90-94% / 95-100% MHR）
简化版本:     1 / 1.1 / 1.2 / 2.2 / 4.5          （50-60 / 60-70 / 70-80 / 80-90 / 90-100%）
```

逐秒判定区间并累计——这个版本是逐秒积分，对心率波动更敏感。

### 相关衍生

- `HrTimeInZone.cpp`：心率区间时间分布；
- `TRIMPPoints.cpp` 还包含 `WattsTRIMP`-风格功率分区间变体（同文件后半部分）。

## 5. 依赖清单（集成时的耦合面）

| GC 依赖 | 性质 | 集成处理 |
|---|---|---|
| `RideItem/RideFile`（逐秒数组 + recIntSecs + metadata tags） | 数据 | 换成 ActivitySeries + overrides（01 文档 §6） |
| `Zones/HrZones`（CP/FTP/MaxHR/RestHR/LTHR 按日期） | 配置 | Athlete 配置接口 |
| `appsettings`（GC_SEX、useCPforFTP） | 配置 | 用户偏好 |
| `deps` 哈希（NP→IF→TSS 链） | 引擎 | 显式函数调用链即可，无需工厂 |
| 单位换算 | UI | 建议 openrunner 内部统一公制，展示层再换算 |

**无 Qt 之外依赖、无 lmfit 依赖** —— 本模块是最适合第一个移植的（纯计算、可独立单测）。

## 6. 建议的开放实现（参考规格，非 GC 代码翻译）

```ts
// 30s 矩形窗 4 次方均值
function normalizedPower(p: number[], dt: number): number {
  const w = Math.max(1, Math.round(30 / dt));
  let sum = 0, total = 0, count = 0;
  const ring = new Array(w).fill(0);
  for (let i = 0; i < p.length; i++) {
    sum += p[i] - ring[i % w];
    ring[i % w] = p[i];
    total += (sum / w) ** 4; count++;
  }
  return count ? total ** 0.25 : 0;   // 注意: 窗口未满时 GC 仍按 w 平均（保守低估）
}

// 25s EWMA 4 次方均值（含空洞衰减）
function xPower(p: number[], t: number[], dt: number): number {
  const spw = 25 / dt, att = spw / (spw + dt), wgt = dt / (spw + dt);
  let weighted = 0, last = t[0], total = 0, count = 0;
  for (let i = 0; i < p.length; i++) {
    while (weighted > 0.1 && t[i] > last + dt + 0.1) {  // 空洞: 持续衰减
      weighted *= att; last += dt; total += weighted ** 4; count++;
    }
    weighted = weighted * att + wgt * p[i]; last = t[i];
    total += weighted ** 4; count++;
  }
  return count ? total ** 0.25 : 0;
}
```

## 7. 测试建议

1. 构造恒功率 1h @ CP：NP≈P、IF=1、TSS=100、BikeScore=100、RI=1 —— 全链路的恒等式测试。
2. 构造 30s on/off 间歇：NP 明显 > AP，xPower > NP（EWMA 对间歇更敏感）——验证两套平滑的差异。
3. TRIMP：AvgHR=RestHR → 0；AvgHR=LTHR 且时长 60min → TRIMP100=100。
4. 用 GC 导出的同一活动 CSV 对照，容差 <0.5%（GC 内部 float 精度损失所致）。
