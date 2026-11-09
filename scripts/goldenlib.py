"""goldenlib — 金标准对照用的参照实现（唯一一份）。

## 为什么要有这个文件

金标准对照的**唯一价值**是「另一个人按论文独立算出同一个数」。一旦参照实现是照抄 Go
代码写的，两边就共享同一个错误，对照永远报「全部一致」。

这个坑真踩过：W'bal 的参照照抄了 GoldenCheetah 的 `GC_WBALFORM=int`（省 CPU 的近似，
超临界段也带回充项），Go 实现照抄参照，单元测试的「独立真值」又照抄同一个公式——
三处同错，持续小幅超 CP 时储备永不耗竭（实测 1000s 仍剩 13936J，应为 0），
而对照台一直显示「最大绝对差 0.000e+00 ✓」。

另外同一个公式在 golden-check.py 里出现过**两份**（真实流段 + 合成用例段），
改了一份等于没改：合成段立刻爆出 3.06e+01 的差。

## 纪律（详见 docs/agents/golden-checks.md）

1. 每个公式**只在这里出现一次**，两个对照脚本都从这里 import。
2. 每一份实现都必须能指出「这一行来自论文/定义的哪一句」，不许以「Go 是怎么写的」为依据。
3. 与 Go 的有意偏离（前导窗口、停表样本等）写在 docstring 里，并在对照脚本里用
   **显式的容差 + 实测差值**表达，不许靠抄袭对齐。
4. 本模块自带测试：`scripts/test_golden_refs.py`（`make ref-test`），用解析真值与
   数学恒等式钉住每个函数，不依赖 Go 的任何输出。
"""

from __future__ import annotations

import math

# ---------------------------------------------------------------- 常数

GENDER_COEF = 0.64  # Banister/Green 式里与性别无关的系数
K_MALE = 1.92
K_FEMALE = 1.67
TAU_LONG, TAU_SHORT = 42.0, 7.0  # CTL / ATL 时间常数（天，Coggan 惯例）
NP_WINDOW_SECS = 30  # Coggan 的标准化功率滑动平均窗口（秒）
TAU_BASE, TAU_SLOPE = 316.0, 546.0  # Skiba τ 公式：546·e^(−0.01·Δ) + 316


# ---------------------------------------------------------------- 心率 / TRIMP

def hr_reserve_ratio(hr_avg: float, hr_rest: float, hr_max: float) -> float:
    """心率储备比 %HRR = (HR − HRrest) / (HRmax − HRrest)（Karvonen 1957）。"""
    if hr_max <= hr_rest:
        return 0.0
    return (hr_avg - hr_rest) / (hr_max - hr_rest)


def trimp(duration_secs: float, hr_avg: float, hr_rest: float, hr_max: float, k: float) -> float:
    """TRIMP = 分钟 × %HRR × 0.64 × e^(k·%HRR)（Banister 1975 的指数模型，
    Green 等给出的 0.64/1.92/1.67 系数）。"""
    minutes = duration_secs / 60.0
    if minutes <= 0:
        return 0.0
    r = hr_reserve_ratio(hr_avg, hr_rest, hr_max)
    if r <= 0:
        return 0.0
    return minutes * r * GENDER_COEF * math.exp(k * r)


def trimp100(trimp_value: float, hr_rest: float, hr_max: float, hr_lt: float, k: float) -> float:
    """把 TRIMP 归一化成「阈值心率 1 小时 = 100 分」（与 TSS 同量纲）。"""
    if trimp_value == 0:
        return 0.0
    r_lt = hr_reserve_ratio(hr_lt, hr_rest, hr_max)
    if r_lt <= 0:
        return 0.0
    one_hour_at_lt = 60.0 * r_lt * GENDER_COEF * math.exp(k * r_lt)
    if one_hour_at_lt == 0:
        return 0.0
    return 100.0 * trimp_value / one_hour_at_lt


# ---------------------------------------------------------------- PMC

def ema(stress: list[float], tau: float, seeds: dict[int, float]) -> list[float]:
    """一阶指数衰减：y[d] = y[d−1]·e^(−1/τ) + s[d]·(1 − e^(−1/τ))。

    直接由「连续时间 y' = (s − y)/τ 的日离散解」得出，不是照抄任何实现。
    seeds[idx] 表示那天直接给定初值（赛季播种）。
    """
    alpha = 1.0 - math.exp(-1.0 / tau)
    out: list[float] = []
    prev = 0.0
    for i, s in enumerate(stress):
        if i in seeds:
            prev = seeds[i]
            out.append(prev)
            continue
        prev = prev + alpha * (s - prev)
        out.append(prev)
    return out


# ---------------------------------------------------------------- 幂均值 / NP / rTSS

def fourth_mean(xs: list[float]) -> float:
    """裸 4 次方均值（IsoPower 同族，不做滑动平均）。只统计正值样本。"""
    xs = [x for x in xs if x > 0]
    if not xs:
        return 0.0
    return math.pow(sum(x ** 4 for x in xs) / len(xs), 0.25)


def fourth_power_normalized(values: list[float], interval_secs: int,
                            window_secs: int = NP_WINDOW_SECS,
                            leading: str = "zeropad") -> float:
    """Coggan 标准化口径：先 window_secs 秒滑动平均，再取 4 次方均值。

    leading 决定「序列开头不足一个窗口」怎么办——这是 Coggan 定义里没写死的地方，
    两种都有人用，本参数把差异显式化，而不是偷偷选一个：

      "zeropad"（默认，GoldenCheetah aCoggan.cpp 的口径）：缺的历史按 0 补齐。
                恒定功率序列的 NP 会偏低约 0.2%。
      "skip"    ：未满一个窗口的时刻不参与平均（openrunner 的选择，见
                NormalizedPower 注释：零补齐会把开头的伪影稀释得不够）。

    非正值样本（停表 / 缺测）先剔除再算——这是定义问题不是实现细节：
    把等停时间算进平均会把 NP 拖向 0。
    """
    if interval_secs <= 0:
        return 0.0
    pos = [v for v in values if v > 0]
    if not pos:
        return 0.0
    window = max(1, (window_secs + interval_secs // 2) // interval_secs)
    if leading not in ("zeropad", "skip"):
        raise ValueError(f"未知的 leading 约定: {leading}")

    total4 = 0.0
    count = 0
    for i in range(len(pos)):
        lo = max(0, i - window + 1)
        n = i - lo + 1
        s = sum(pos[lo:i + 1])
        if n < window:
            if leading == "skip":
                continue
            s = s / window  # 缺的部分按 0 补齐
        else:
            s = s / window
        total4 += s ** 4
        count += 1
    if count == 0:
        return fourth_mean(pos)  # 序列比一个窗口还短：定义上退化为裸 4 次方均值
    return math.pow(total4 / count, 0.25)


def coggan_np(power: list[float], interval_secs: int, leading: str = "zeropad") -> float:
    """标准化功率 NP（功率域）。"""
    return fourth_power_normalized(power, interval_secs, NP_WINDOW_SECS, leading)


def normalized_speed(speeds_kph: list[float], interval_secs: int, leading: str = "zeropad") -> float:
    """标准化速度 NS（速度域，rTSS 用）。与 NP 同一个幂均值定义，只是量纲不同。"""
    return fourth_power_normalized(speeds_kph, interval_secs, NP_WINDOW_SECS, leading)


def rtss(duration_secs: float, ns_kph: float, threshold_kph: float) -> float:
    """rTSS = 100 × 小时 × IF²，IF = NS / 阈值速度（TSS 家族式，跑步版）。"""
    if duration_secs <= 0 or ns_kph <= 0 or threshold_kph <= 0:
        return 0.0
    if_ = ns_kph / threshold_kph
    return 100.0 * (duration_secs / 3600.0) * if_ * if_


# ---------------------------------------------------------------- W'bal / D'bal

def tau_from_data(cp: float, avg_power_below_cp: float) -> float:
    """Skiba 2012 的恢复时间常数：τ = 546·e^(−0.01·(CP − P̄below)) + 316。"""
    return TAU_SLOPE * math.exp(-0.01 * (cp - avg_power_below_cp)) + TAU_BASE


def wbal(power: list[float], cp: float, wprime: float, tau: float, interval_secs: int) -> list[float]:
    """W'balance：Skiba 2012 的**分段**模型（"store and pothole"）。

      P > CP : W'bal −= (P − CP)·Δt，钳到 [0, W']        ← 超临界段线性扣减，无回充
      P ≤ CP : W'bal  = W' − (W' − W'bal)·e^(−Δt/τ)      ← 恢复段按 τ 指数回充

    **不是**「前向积分 + 并发衰减」（I += e^(t/τ)(P−CP)+，再乘 e^(−t/τ)）：那是
    GoldenCheetah 为省 CPU 引入的近似，等价于 dW'bal/dt = −(P−CP) + (W'−W'bal)/τ，
    超临界段也带回充项，储备只能趋近 W' − τ·(P−CP)，小幅持续超 CP 时永不耗竭。
    GC 自己的微分实现（WPrime.cpp minForCP）与 Skiba 原文都是分段式。

    p ≤ 0（停表 / 缺测）不改动储备：没有记录不等于在滑行恢复。
    """
    out: list[float] = []
    bal = wprime
    for p in power:
        if p > cp:
            bal = max(bal - (p - cp) * interval_secs, 0.0)
        elif p > 0:
            bal = min(wprime - (wprime - bal) * math.exp(-interval_secs / tau), wprime)
        out.append(bal)
    return out


# ---------------------------------------------------------------- 拟合（CS / Banister）

def cs_ref(points: list[tuple[float, float]]) -> tuple[float, float]:
    """2 参数临界速度模型 D(t) = D' + CS·t 的最小二乘（Monod & Scherrer 1965）。

    直接用正规方程的解析式写，不复用 Go 的累加写法。
    """
    n = len(points)
    st = sum(t for t, _ in points)
    sd = sum(d for _, d in points)
    stt = sum(t * t for t, _ in points)
    std = sum(t * d for t, d in points)
    denom = n * stt - st * st
    if denom == 0:
        return 0.0, 0.0
    cs = (n * std - st * sd) / denom
    dprime = (sd - cs * st) / n
    return cs, dprime


def solve3(a: list[list[float]], b: list[float]) -> list[float] | None:
    """3×3 线性方程组（带部分主元的高斯消元）。奇异返回 None。"""
    m = [list(a[i]) + [b[i]] for i in range(3)]
    for col in range(3):
        piv = max(range(col, 3), key=lambda r: abs(m[r][col]))
        if abs(m[piv][col]) < 1e-12:
            return None
        m[col], m[piv] = m[piv], m[col]
        for r in range(3):
            if r == col:
                continue
            f = m[r][col] / m[col][col]
            for c in range(col, 4):
                m[r][c] -= f * m[col][c]
    return [m[i][3] / m[i][i] for i in range(3)]


def banister_lstsq(loads: dict[str, float], races: list[dict], tau1: float, tau2: float):
    """Banister 刺激-响应模型的固定-τ 最小二乘：perf = p0 + k1·A − k2·B。

    A/B 是「该日之前各日负荷的指数加权和」，由模型定义直接得出，不是照抄实现。
    """
    def acc(day: str, tau: float) -> float:
        from datetime import date
        d0 = date.fromisoformat(day).toordinal()
        return sum(w * math.exp(-(d0 - date.fromisoformat(d).toordinal()) / tau)
                   for d, w in loads.items() if w > 0 and date.fromisoformat(d).toordinal() < d0)

    rows = []
    for r in races:
        rows.append((1.0, acc(r["date"], tau1), -acc(r["date"], tau2), r["perf"]))
    if len(rows) < 3:
        return None
    ata = [[sum(x[i] * x[j] for x in rows) for j in range(3)] for i in range(3)]
    aty = [sum(x[i] * x[3] for x in rows) for i in range(3)]
    theta = solve3(ata, aty)
    if theta is None:
        return None
    return theta[0], theta[1], theta[2]


# ---------------------------------------------------------------- 其它

def zone_for(day: str, zones: list[dict]) -> dict | None:
    """按生效日期取区间配置（重叠时取生效起点最晚的那条，与 store 的查询同规则）。"""
    hit = [z for z in zones if z["valid_from"] <= day <= z["valid_to"]]
    if not hit:
        return None
    return max(hit, key=lambda z: (z["valid_from"], z.get("id", 0)))
