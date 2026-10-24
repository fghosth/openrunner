# 05 · Aerolab：虚拟海拔与 CdA/Crr 估计

> 源文件：`Charts/Aerolab.{h,cpp}`（902 行，**物理计算与 UI 在同一文件**）、`Charts/AerolabWindow.{h,cpp}`（745 行，滑条/参数面板）
> 依赖：QwtPlot（仅展示）、Core/Units.h 常量。无 lmfit。

## 1. 物理模型

骑行功率平衡（沿行进方向）：

```
η·P = m_total·g·(Crr + s)·v  +  ½·ρ·CdA·(v+v_wind)²·v  +  m_total·a·v

η  传动效率（默认 1.0，曲柄功率计可设 0.97 等）
P  轮上/曲柄功率 W
m  人+车总重 kg（默认 85.0）
g  9.80665（KG_FORCE_PER_METER，Core/Units.h:38）
Crr 滚阻系数（默认 0.005）
CdA 气动阻力面积 m²（默认 0.500）★ 本模块的主角
ρ  空气密度 kg/m³（默认 1.236，可调）
v  速度 m/s；v_wind = headwind（未记录时 = v，即视为无风修正误差）
a  加速度 m/s²（由速度数值微分）
```

## 2. 虚拟海拔 Virtual Elevation（Aerolab.cpp:340 主循环）

把上式反解为"等效坡度"，再对距离积分还原成一条海拔曲线：

```
F = η·P / v
a = (v² − v_last²) / (2·Δt·v)                 # :379；v≈0 时退化为 (v−v_last)/Δt
s = F/(m·g) − Crr − CdA·ρ·v²/(2·m·g) − a/g    # slope() :605（小角度近似，headwind 并入 v 项时同样处理）
ΔE = s · v · Δt                                # 该秒的"虚拟海拔变化"
VE(t) = ∫ ΔE                                   # :388 累加
```

**使用方式**：VE 曲线与 GPS 实测海拔叠加显示；用户拖 CdA/Crr 滑条（精度 1e-4 / 1e-6，:627-643），当 VE 与实测海拔贴合 → 当前 CdA 即该路段的真实估计。这是"场测风洞"：一次绕圈骑行即可测不同姿势/装备的 CdA。

辅助机制：
- `eoffset` 曲线垂直偏移 + `adjustEoffset()` 自动对齐（GPS 海拔漂移可达 ±10m）；
- `constantAlt` 模式：室内台车无海拔时强制 alt=0/恒定；
- `bydist` X 轴按距离或时间；
- 单位：内部公制，展示层换算英制（FEET_PER_METER）。

## 3. 自动估计：estimateCdACrr（Aerolab.cpp:771，作者 Alejandro Martinez 2012）

不用手调滑条，用**分段能量守恒 + 最小二乘正规方程**直接解出 CdA 和 Crr：

### 3.1 分段策略

以**海拔非零的数据点**为段边界（典型：往返山的往返段），段内累积三个量：

```
X1[seg] = Σ ½·ρ·v_wind²·d          # 气动损失系数  → Aero-Loss = X1·CdA
X2[seg] = Σ m_total·g·d            # 滚阻损失系数  → RR-Loss   = X2·Crr
Egain[seg] = Σ η·P·Δt              # 段内输入能量
           + m_total·(g·(alt_start − alt_end) + ½·(v_start² − v_end²))
             # 段末一次性补上势能+动能变化（:839）
```

能量守恒要求：`X1·CdA + X2·Crr = Egain`。

### 3.2 正规方程求解（:856-876）

n 段数据构成超定方程组 `X·[CdA;Crr] = Egain`，左乘 Xᵀ 化为 2×2：

```
A = XᵀX,  B = Xᵀ·Egain
A11·CdA + A12·Crr = B1
A21·CdA + A22·Crr = B2

det = A11·A22 − A12·A21
CdA = (A22·B1 − A12·B2) / det      # 合法域 [0.001, 1.0]
Crr = (A11·B2 − A21·B1) / det      # 合法域 [0.0001, 0.1]
```

失败条件（返回错误信息，:882-888）：海拔/功率缺失、分段 <2、det≈0（段间线性相关，如全程同坡度）、解越界。

## 4. 数据要求与坑

| 要求 | 说明 |
|---|---|
| 功率计 | 必须；曲柄功率计需把 η 设 <1 |
| 海拔 | GPS 海拔噪声大，GC 图表上有平滑开关（smooth）；barometric 更佳 |
| headwind | 有记录最好；没有则 v_wind=v（隐含"无风假设"，误差直接进入 CdA） |
| 速度 | kph→m/s 除以 3.6（:774 vfactor） |
| 体重 | m_total = 骑手 + 车（用户在窗口设置） |

已知局限（集成时要接受或改进）：
1. 无风假设是最大误差源——侧风在模型里不存在；
2. 减速时负功率未特殊处理（power = max(0, watts)，:812：滑行时气动消耗被低估 → CdA 偏小）；
3. slope() 是小角度近似（sin θ ≈ tan θ），陡坡误差增大；
4. VE 主循环里 `distanceArray` 直接读 p->km 而非 v·Δt 累加（:373），两者不一致时曲线横轴会漂。

## 5. 集成清单

| 项 | GC 位置 | 移植 |
|---|---|---|
| slope + VE 积分 | Aerolab.cpp:340-395, :605-622 | 核心，~40 行 |
| estimateCdACrr | :771-902 | 核心，~80 行（2×2 线性求解手写即可） |
| 常数组 | Units.h:38 + 构造默认值 :208-213 | 直接采用，做成配置 |
| 平滑/偏移/单位 | 图表层 | 目标项目自己的图表 |

### MVP 建议

```
1. 先做 estimateCdACrr（一次算出 CdA/Crr，用户零操作）；
2. VE 叠加曲线作为可视化验证 + 手动微调滑条；
3. 输出"装备对比"场景：同一绕圈路线多次活动的 CdA 排序。
```

### 适用范围

需要功率+速度+海拔数据，本质是**自行车**工具；跑步场景（openrunner 若含跑步）此模型不适用（跑姿/垂直振荡未建模）——建议做成骑行专属高级功能。
