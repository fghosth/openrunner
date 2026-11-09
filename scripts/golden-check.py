#!/usr/bin/env python3
"""金标准对照：用「照发表公式独立写的一份」重算训练负荷，与 Go 实现逐项比。

为什么要有这个脚本
------------------
Go 侧的单元测试是恒等式 + 我从自己对参考实现的理解推出来的期望值。
同一个错误理解会同时产生实现和期望值，所以那套测试全绿也证明不了公式对。
这里用另一套语言、另一份出处独立写一遍，两边对上才算证据。

公式出处（写之前没有翻 Go 源码，避免把同一个误解抄第二遍）
----------------------------------------------------------
TRIMP 指数模型  Banford HC et al., Int J Sports Med 1986；Morton RH et al. 1990
                TRIMP = t_min · ΔHRr · 0.64 · e^(k·ΔHRr)，
                ΔHRr = (HRavg − HRrest)/(HRmax − HRrest)，k = 1.92 男 / 1.67 女
TRIMP100        归一化定义：在乳酸阈值心率上跑满 1 小时 = 100 分
CTL / ATL       一阶指数滤波，时间常数 τ（离散精确形式）：
                    x_d = x_{d−1} + α·(s_d − x_{d−1})，α = 1 − e^(−1/τ)
                τ_long = 42 天，τ_short = 7 天
TSB             前一日 CTL − 前一日 ATL（参考实现默认把 SB 画在次日）

用法
----
  OPENRUNNER_GOLDEN_DB=~/.openrunner/openrunner.db OPENRUNNER_GOLDEN_ATHLETE=1 \\
  OPENRUNNER_GOLDEN_OUT=/tmp/go_pmc.json go test ./api -run TestGoldenDumpPMC -count=1
  ./scripts/golden-check.py --db ~/.openrunner/openrunner.db --go /tmp/go_pmc.json
"""

from __future__ import annotations

import argparse
import json
import math
import os
import sqlite3
import sys
from collections import defaultdict
from datetime import datetime, timedelta

K_MALE, K_FEMALE = 1.92, 1.67          # Banford/Morton 发表值
GENDER_COEF = 0.64                      # 性别无关的那个系数
TAU_LONG, TAU_SHORT = 42.0, 7.0         # CTL / ATL 时间常数（天）
TOL_REL = 1e-9                          # 浮点噪声容忍；真差异都远大于这个


# ---------------------------------------------------------------- 公式

def hr_reserve_ratio(hr_avg: float, hr_rest: float, hr_max: float) -> float:
    """心率储备比 ΔHRr。分母非正时返回 0（无定义 → 不产生负荷）。"""
    if hr_max <= hr_rest:
        return 0.0
    return (hr_avg - hr_rest) / (hr_max - hr_rest)


def trimp(duration_secs: float, hr_avg: float, hr_rest: float, hr_max: float, k: float) -> float:
    """TRIMP 指数模型（Banford 1986 / Morton 1990）。"""
    minutes = duration_secs / 60.0
    if minutes <= 0:
        return 0.0
    r = hr_reserve_ratio(hr_avg, hr_rest, hr_max)
    if r <= 0:
        return 0.0
    return minutes * r * GENDER_COEF * math.exp(k * r)


def trimp100(trimp_value: float, hr_rest: float, hr_max: float, hr_lt: float, k: float) -> float:
    """把 TRIMP 归一化成「阈值心率 1 小时 = 100 分」。"""
    if trimp_value == 0:
        return 0.0
    r_lt = hr_reserve_ratio(hr_lt, hr_rest, hr_max)
    if r_lt <= 0:
        return 0.0
    one_hour_at_lt = 60.0 * r_lt * GENDER_COEF * math.exp(k * r_lt)
    if one_hour_at_lt == 0:
        return 0.0
    return 100.0 * trimp_value / one_hour_at_lt


def ema(stress: list[float], tau: float, seeds: dict[int, float]) -> list[float]:
    """一阶指数滤波（精确离散形式）。seeds[idx] 会用该值重置并跳过递推。"""
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


def fourth_mean(xs: list[float]) -> float:
    """裸 4 次方均值（IsoPower 同族，不做滑动平均）。"""
    xs = [x for x in xs if x > 0]
    if not xs:
        return 0.0
    return math.pow(sum(x ** 4 for x in xs) / len(xs), 0.25)


def coggan_np(power: list[float], interval_secs: int, window_secs: int = 30) -> float:
    """Coggan 标准化功率：先 30 秒滑动平均，再取 4 次方均值。

    与 fourth_mean 的唯一差别就是那道滑动平均——它把单个尖峰稀释到 1/窗口，
    否则 4 次方会把一个伪影样本放大上千倍权重。
    """
    if interval_secs <= 0:
        return 0.0
    pos = [p for p in power if p > 0]
    if not pos:
        return 0.0
    # 窗口按秒换算成样本数。这里刻意照 Go 的整数舍入写法（而不是 Python 的
    # round 银行家舍入）：换算属于实现细节，两边不一致只会产出假差异。
    window = max(1, (window_secs + interval_secs // 2) // interval_secs)
    if len(pos) < window:
        return fourth_mean(pos)
    total = 0.0
    total4 = 0.0
    count = 0
    for i, v in enumerate(pos):
        total += v
        if i >= window:
            total -= pos[i - window]
        if i < window - 1:
            continue  # 未满一个窗口不参与：开头按已有样本数平均会让尖峰零稀释
        ma = total / window
        total4 += ma ** 4
        count += 1
    if count == 0:
        return 0.0
    return math.pow(total4 / count, 0.25)

# ---------------------------------------------------------------- 数据

def local_date(unix_secs: int) -> str:
    """按系统本地时区切日——与 Go 侧 time.Local 同口径，否则日聚合会整体错位。"""
    return datetime.fromtimestamp(unix_secs).strftime("%Y-%m-%d")


def zone_for(day: str, zones: list[dict]) -> dict | None:
    for z in zones:
        if z["valid_from"] <= day <= z["valid_to"]:
            return z
    return None


def load(db_path: str, athlete_id: int):
    con = sqlite3.connect(db_path)
    con.row_factory = sqlite3.Row
    sex = con.execute("SELECT sex FROM athletes WHERE id=?", (athlete_id,)).fetchone()["sex"]
    zones = [dict(r) for r in con.execute(
        "SELECT valid_from, valid_to, max_hr, rest_hr, lt_hr FROM hr_zones "
        "WHERE athlete_id=? ORDER BY valid_from", (athlete_id,))]
    acts = [dict(r) for r in con.execute(
        "SELECT a.id, a.start_time, a.duration_secs, a.timer_secs, a.is_planned, "
        "       m.trimp AS go_trimp, m.trimp100 AS go_trimp100, a.avg_hr "
        "FROM activities a JOIN activity_metrics m ON m.activity_id = a.id "
        "WHERE a.athlete_id=?", (athlete_id,))]
    seasons = [dict(r) for r in con.execute(
        "SELECT start_date, seed FROM seasons WHERE athlete_id=?", (athlete_id,))]
    con.close()
    return sex, zones, acts, seasons


# ---------------------------------------------------------------- 比对

def worst(pairs, key):
    return max(pairs, key=key) if pairs else None


def compare(label: str, mine: list[float], theirs: list[float], missing_note: str = "") -> bool:
    if len(mine) != len(theirs):
        print(f"  ✗ {label}: 长度不同  mine={len(mine)} go={len(theirs)}")
        return False
    diffs = []
    for i, (m, t) in enumerate(zip(mine, theirs)):
        denom = max(abs(t), 1e-12)
        rel = abs(m - t) / denom
        diffs.append((rel, abs(m - t), i, m, t))
    n_bad = sum(1 for d in diffs if not (d[0] <= TOL_REL))   # NaN 也算不合格
    w = worst(diffs, key=lambda d: d[0])
    ok = n_bad == 0
    mark = "✓" if ok else "✗"
    print(f"  {mark} {label:<22} n={len(mine):<5} 最大相对误差={w[0]:.3e}  最大绝对误差={w[1]:.3e}"
          f"  超容忍={n_bad}")
    if not ok:
        rel, ad, i, m, t = w
        print(f"      最差 #{i}: mine={m!r} go={t!r}  Δ={ad:.6g}  {missing_note}")
    return ok



# ---------------------------------------------------------------- 票 03 扩展段

def rtss_section(db_path: str, athlete_id: int, go_rtss_path: str) -> bool:
    """[4] rTSS：Python 从流独立算 NS（正值样本 4 次方均值）与 TSS 家族式。
    口径（DEVLOG Round 6 决议）：原始速度无 NGP 坡度修正；时长 timer 优先。"""
    if not os.path.exists(go_rtss_path):
        print(f"\n[4] 跳过 rTSS 对照：找不到 {go_rtss_path}")
        return True
    go = json.load(open(go_rtss_path))["items"]
    con = sqlite3.connect(db_path)
    con.row_factory = sqlite3.Row
    streams = {r["activity_id"]: (r["data"], r["rec_interval_secs"] or 1)
               for r in con.execute("SELECT activity_id, data, rec_interval_secs FROM activity_streams")}
    con.close()
    mine, theirs = [], []
    for it in go:
        entry = streams.get(it["id"])
        if not entry:
            continue
        speeds = json.loads(entry[0]).get("speed") or []
        pos = [v for v in speeds if v > 0]
        if not pos:
            continue
        ns = (sum(v ** 4 for v in pos) / len(pos)) ** 0.25
        tss = 100 * it["durSecs"] * ns * ns / (it["thrKph"] * it["thrKph"] * 3600)
        mine.append(tss)
        theirs.append(it["rtss"])
    print("\n[4] rTSS（Python 独立 NS + TSS 家族式 vs Go，%d 条真实跑步流）" % len(mine))
    return compare("rTSS", mine, theirs)


def wbal_section(db_path: str, athlete_id: int, go_wbal_path: str) -> bool:
    """[5] W'bal：Skiba 2012 分段模型独立实现，真实骑行功率流。"""
    if not os.path.exists(go_wbal_path):
        print(f"\n[5] 跳过 W'bal 对照：找不到 {go_wbal_path}")
        return True
    go = json.load(open(go_wbal_path))["items"]
    con = sqlite3.connect(db_path)
    con.row_factory = sqlite3.Row
    streams = {r["activity_id"]: (r["data"], r["rec_interval_secs"] or 1)
               for r in con.execute("SELECT activity_id, data, rec_interval_secs FROM activity_streams")}
    con.close()

    def tau_from_data(cp, avg_below):
        return 546 * math.exp(-0.01 * (cp - avg_below)) + 316  # 04 文档 §3.2

    def wbal(power, cp, wprime, tau, iv):
        """Skiba 2012 分段模型：超 CP 线性扣减、低于 CP 按 tau 指数回充。

        早期这里照抄了 GC 的 GC_WBALFORM=int「前向积分 + 并发衰减」，那是省 CPU 的
        近似：超临界段也带一个回充项，储备只能趋近 W' − tau·(P−CP)，持续小幅超 CP
        时永不耗竭（Go 侧同错，两边一起改）。"""
        out, bal = [], wprime
        for p in power:
            if p > cp:
                bal = max(bal - (p - cp) * iv, 0.0)
            elif p > 0:
                bal = wprime - (wprime - bal) * math.exp(-iv / tau)
                bal = min(bal, wprime)
            out.append(bal)  # p <= 0（停表/缺测）：储备不变
        return out

    worst_abs = 0.0
    n_pts = 0
    ok = True
    for it in go:
        entry = streams.get(it["id"])
        if not entry:
            continue
        power = json.loads(entry[0]).get("power") or []
        if not power:
            continue
        iv = entry[1]
        below = [p for p in power if 0 < p < it["cp"]]
        tau = tau_from_data(it["cp"], sum(below) / len(below) if below else it["cp"])
        if abs(tau - it["tau"]) > 1e-6:
            print(f"      ✗ 活动 {it['id']}: τ go={it['tau']:.3f} py={tau:.3f}")
            ok = False
            continue
        series = wbal(power, it["cp"], it["wprime"], tau, iv)
        for j, v in enumerate(it["series"]):
            k = j * 10
            if k < len(series):
                worst_abs = max(worst_abs, abs(series[k] - v))
                n_pts += 1
    print(f"\n[5] W'bal（Skiba 分段模型独立实现 vs Go，{len(go)} 条骑行流 / {n_pts} 采样点）")
    if n_pts:
        ok &= worst_abs <= 1e-9
        print(f"      最大绝对差 {worst_abs:.3e} {'✓' if worst_abs <= 1e-9 else '✗'}")
    return ok


def synth_section(core_dir: str) -> bool:
    """[6] 合成用例：CP 闭式最小二乘 / CP3 已知真值回收 / Banister 已知真值回收。"""
    import random
    import subprocess

    random.seed(42)
    from datetime import date as _d, timedelta as _td
    # --- CP 2 参数：已知 (CS=3.4, D'=140) 生成带噪点；Python 闭式独立解
    cs_cases = []
    for cs_true, dp_true in ((3.40, 140.0), (4.10, 95.0), (2.85, 210.0)):
        pts = []
        for t in (120, 180, 300, 420, 600, 900):
            d = dp_true + cs_true * t + random.uniform(-15, 15)
            pts.append({"seconds": t, "distanceM": d})
        cs_cases.append(pts)

    def cs_ref(pts):
        n = len(pts)
        mt = sum(p["seconds"] for p in pts) / n
        md = sum(p["distanceM"] for p in pts) / n
        num = sum((p["seconds"] - mt) * (p["distanceM"] - md) for p in pts)
        den = sum((p["seconds"] - mt) ** 2 for p in pts)
        cs = num / den
        return cs, md - cs * mt

    # --- CP3：已知 (CS=3.4, D'=150, τ=240) 生成无噪点，回收验证 LM 求解器
    cp3_cases = []
    for cs_t, dp_t, tau_t in ((3.40, 150.0, 240.0), (4.00, 100.0, 180.0)):
        pts = [{"seconds": t,
                "distanceM": dp_t * (1 - math.exp(-t / tau_t)) + cs_t * t}
               for t in (60, 120, 240, 420, 600, 900, 1200)]
        cp3_cases.append((pts, cs_t, dp_t, tau_t))

    # --- Banister：已知 P0/k1/k2/τ 生成成绩；固定 τ 用 Python lstsq 独立解，
    #     自由 τ 验证回收
    def banister_ground(loads, races, p0, k1, k2, t1, t2):
        def acc(day_idx, tau):
            return sum(w * math.exp(-(day_idx - i) / tau)
                       for i, w in sorted(loads.items()) if i < day_idx and w > 0)
        out = []
        for r in races:
            idx = r["day"]
            out.append(p0 + k1 * acc(idx, t1) - k2 * acc(idx, t2))
        return out

    ban_fixed, ban_free = [], []
    for (p0, k1, k2, t1, t2, tau_given) in ((5.0, 0.08, 0.05, 45, 15, (45, 15)),
                                            (4.2, 0.10, 0.07, 60, 20, (60, 20))):
        loads = {}
        for i in range(120):
            if random.random() < 0.5:
                loads[i] = random.uniform(30, 120)
        race_days = [40, 60, 80, 100, 115]
        perfs = banister_ground(loads, [{"day": d} for d in race_days], p0, k1, k2, t1, t2)
        base = _d(2026, 1, 1)
        loads_iso = {(base + _td(days=i)).isoformat(): w for i, w in loads.items()}
        races_iso = [{"date": (base + _td(days=d)).isoformat(), "perf": p}
                     for d, p in zip(race_days, perfs)]
        ban_fixed.append({"loads": loads_iso, "races": races_iso, "tau1": tau_given[0], "tau2": tau_given[1]})
        ban_free.append({"loads": loads_iso, "races": races_iso, "tau1": 0, "tau2": 0})

    def solve3(A, b):
        # 纯 Python 高斯消元（3x3）
        M = [row[:] + [b[i]] for i, row in enumerate(A)]
        for c in range(3):
            piv = max(range(c, 3), key=lambda r: abs(M[r][c]))
            M[c], M[piv] = M[piv], M[c]
            if abs(M[c][c]) < 1e-12:
                return None
            for r in range(3):
                if r != c:
                    f = M[r][c] / M[c][c]
                    for k in range(c, 4):
                        M[r][k] -= f * M[c][k]
        return [M[i][3] / M[i][i] for i in range(3)]

    def banister_lstsq(loads, races, t1, t2):
        base = _d(2026, 1, 1)
        day0 = min(_d.fromisoformat(k) for k in loads)
        def acc(day_idx, tau):
            return sum(w * math.exp(-(day_idx - (k_d - day0).days) / tau)
                       for k, w in loads.items()
                       if (k_d := _d.fromisoformat(k)) < day0 + _td(days=day_idx) and w > 0)
        rows = []
        for r in races:
            idx = (_d.fromisoformat(r["date"]) - day0).days
            a = acc(idx, t1)
            b = acc(idx, t2)
            rows.append((1.0, a, -b, r["perf"]))
        ata = [[sum(x[i] * x[j] for x in rows) for j in range(3)] for i in range(3)]
        aty = [sum(x[i] * x[3] for x in rows) for i in range(3)]
        return solve3(ata, aty)

    # --- W'bal 饱和/回充钳位边界：远超 CP 消耗到 0，再低于 CP 回充到 W' 封顶
    sat_power = [400.0] * 600 + [200.0] * 900
    wbal_cases = [{"power": sat_power, "cp": 280.0, "wprime": 134.8, "tau": 300.0, "interval": 1}]

    cases = {
        "cs_cases": cs_cases,
        "cp3_cases": [c[0] for c in cp3_cases],
        "banister": ban_fixed + ban_free,
        "wbal_cases": wbal_cases,
    }
    json.dump(cases, open("/tmp/gs_cases.json", "w"))
    env = dict(os.environ, OPENRUNNER_GS_IN="/tmp/gs_cases.json", OPENRUNNER_GS_OUT="/tmp/gs_go.json")
    proc = subprocess.run(["go", "test", ".", "-run", "TestGoldenDumpSynth", "-count=1"],
                          cwd=core_dir, env=env, capture_output=True, text=True)
    if proc.returncode != 0:
        print(proc.stdout[-1500:], proc.stderr[-400:])
        return False
    go = json.load(open("/tmp/gs_go.json"))

    ok = True
    print("\n[6] 合成用例（独立参照 / 已知真值回收）")
    for i, (pts, row) in enumerate(zip(cs_cases, go["cs"])):
        cs_py, dp_py = cs_ref(pts)
        d = max(abs(row["Cs"] - cs_py), abs(row["Dprime"] - dp_py) / 10)
        good = d <= 1e-9
        ok &= good
        print(f"  {'✓' if good else '✗'} CP[{i}]: go=({row['Cs']:.4f},{row['Dprime']:.2f}) py=({cs_py:.4f},{dp_py:.2f})")
    for i, ((_, cs_t, dp_t, tau_t), row) in enumerate(zip(cp3_cases, go["cp3"])):
        good = (abs(row["Cs"] - cs_t) / cs_t < 0.01 and abs(row["Dprime"] - dp_t) / dp_t < 0.05
                and abs(row["Tau"] - tau_t) / tau_t < 0.10)
        ok &= good
        print(f"  {'✓' if good else '✗'} CP3[{i}] 真值回收: go=(CS {row['Cs']:.3f}, D' {row['Dprime']:.1f}, τ {row['Tau']:.0f}) "
              f"真值=({cs_t}, {dp_t}, {tau_t})")
    n_fixed = len(ban_fixed)
    for i, bc in enumerate(ban_fixed):
        sol = banister_lstsq(bc["loads"], bc["races"], bc["tau1"], bc["tau2"])
        row = go["banister"][i]
        d = max(abs(row["P0"] - sol[0]), abs(row["K1"] - sol[1]), abs(row["K2"] - sol[2]))
        good = d <= 1e-6
        ok &= good
        print(f"  {'✓' if good else '✗'} Banister[{i}] 固定τ lstsq: go=({row['P0']:.4f},{row['K1']:.5f},{row['K2']:.5f}) "
              f"py=({sol[0]:.4f},{sol[1]:.5f},{sol[2]:.5f})")
    for i, bc in enumerate(ban_free):
        row = go["banister"][n_fixed + i]
        # 自由 τ 用默认 45/15——回收质量只查 R²（参数组不唯一，τ 不同解不同）
        good = row["R2"] > 0.999
        ok &= good
        print(f"  {'✓' if good else '✗'} Banister[{n_fixed + i}] 默认τ R²={row['R2']:.5f}（应 >0.999，无噪合成）")

    # W'bal 合成饱和段：独立实现逐点比（含 0 与 W' 两侧钳位）
    def tau_from_data(cp, avg_below):
        return 546 * math.exp(-0.01 * (cp - avg_below)) + 316

    def wbal_ref(power, cp, wprime, tau, iv):
        """Skiba 2012 分段模型（与 wbal_section 同一口径）：超 CP 线性扣减、
        低于 CP 按 tau 指数回充、p<=0 不动储备。"""
        out, bal = [], wprime
        for p in power:
            if p > cp:
                bal = max(bal - (p - cp) * iv, 0.0)
            elif p > 0:
                bal = min(wprime - (wprime - bal) * math.exp(-iv / tau), wprime)
            out.append(bal)
        return out

    for wc, go_series in zip(wbal_cases, go["wbal"]):
        # 合成用例显式给了 tau（用例测的是钳位与回充边界，不是 tau 公式）；
        # Go 的 WBal 也是把 tau 当参数收的，两边必须用同一个值。
        # （此前这里用 tau_from_data 覆盖了用例值，积分式下恰好不敏感，分段式下会露馅。）
        tau = wc["tau"]
        ref = wbal_ref(wc["power"], wc["cp"], wc["wprime"], tau, wc["interval"])
        d = max(abs(a - b) for a, b in zip(ref, go_series))
        hits_zero = min(ref) <= 1e-9
        good = d <= 1e-9 and hits_zero
        ok &= good
        print(f"  {'✓' if good else '✗'} W'bal 饱和钳位: 最大差 {d:.3e}，探底 0 {'是' if hits_zero else '否（用例失效）'}")
    return ok

def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", required=True)
    ap.add_argument("--go", required=True, help="TestGoldenDumpPMC 写出的 JSON")
    ap.add_argument("--go-np", default="/tmp/go_np.json",
                    help="TestGoldenDumpNP 写出的 JSON（不存在则跳过 NP 段）")
    ap.add_argument("--go-rtss", default="/tmp/go_rtss.json")
    ap.add_argument("--go-wbal", default="/tmp/go_wbal.json")
    ap.add_argument("--athlete", type=int, default=1)
    args = ap.parse_args()

    go = json.load(open(args.go))
    sex, zones, acts, seasons = load(args.db, args.athlete)
    k = K_FEMALE if str(sex).upper().startswith("F") else K_MALE
    print(f"档案 {args.athlete}: sex={sex} → k={k}；心率区间 {len(zones)} 套；活动 {len(acts)} 条")

    # ---- 1) 逐活动 TRIMP / TRIMP100 ----
    mine_trimp, mine_t100, go_trimp, go_t100 = [], [], [], []
    skipped = 0
    daily: dict[str, float] = defaultdict(float)
    for a in acts:
        day = local_date(a["start_time"])
        z = zone_for(day, zones)
        if z is None or not a["avg_hr"]:
            skipped += 1
            continue
        # 时长口径用 timer_secs（不含停表段），不是 duration_secs（含停表的墙钟时长）。
        # 第一次对照时这里取了 duration_secs：78/215 条对不上、最大偏 26.7%，
        # 但差异全来自口径而非公式——TRIMP 对时长线性，偏差比值恰等于 timer/duration。
        secs = a["timer_secs"] or a["duration_secs"]
        t = trimp(secs, a["avg_hr"], z["rest_hr"], z["max_hr"], k)
        t100 = trimp100(t, z["rest_hr"], z["max_hr"], z["lt_hr"], k)
        mine_trimp.append(t)
        mine_t100.append(t100)
        go_trimp.append(a["go_trimp"])
        go_t100.append(a["go_trimp100"])
        if not a["is_planned"]:
            daily[day] += a["go_trimp"]        # 日聚合用 Go 的 trimp，隔离「公式」与「递推」两段

    print("\n[1] 逐活动公式（Python 独立实现 vs Go 落库值）")
    ok = compare("TRIMP", mine_trimp, go_trimp)
    ok &= compare("TRIMP100", mine_t100, go_t100)
    if skipped:
        print(f"      （{skipped} 条无阈值或无平均心率，两边都算 0，未纳入）")

    # ---- 2) PMC 序列 ----
    print("\n[2] PMC 序列（日聚合 + 指数递推）")
    dates_go = go["series"]["dates"]
    start = datetime.strptime(dates_go[0], "%Y-%m-%d")
    stress = [daily.get((start + timedelta(days=i)).strftime("%Y-%m-%d"), 0.0)
              for i in range(len(dates_go))]
    ok &= compare("日负荷 stress", stress, go["series"]["stress"])

    seeds: dict[int, float] = {}
    for s in seasons:
        # seed=0 也是锚点（赛季首日从 0 起链，debt-sprint 票 03 修正后口径）
        if s["seed"] is not None:
            try:
                seeds[dates_go.index(s["start_date"])] = float(s["seed"])
            except ValueError:
                pass
    print(f"      播种点 {len(seeds)} 个（含 seed=0 锚点）")

    ctl = ema(stress, TAU_LONG, seeds)
    atl = ema(stress, TAU_SHORT, seeds)
    ok &= compare("CTL (τ=42)", ctl, go["series"]["lts"])
    ok &= compare("ATL (τ=7)", atl, go["series"]["sts"])

    # TSB 的两种口径都算一遍，看 Go 用的是哪一种，而不是猜
    sb_prev = [0.0] + [ctl[i - 1] - atl[i - 1] for i in range(1, len(ctl))]
    sb_same = [ctl[i] - atl[i] for i in range(len(ctl))]
    print("      SB 口径判定：")
    for name, cand in (("前一日 CTL−ATL", sb_prev), ("当日 CTL−ATL", sb_same)):
        rel = max((abs(m - t) / max(abs(t), 1e-12) for m, t in zip(cand, go["series"]["sb"])), default=0)
        print(f"        {'✓ 匹配' if rel <= TOL_REL else '  不匹配'}  {name}   最大相对误差={rel:.3e}")

    # ---- 3) NP（Coggan 口径）：真实功率流上的交叉验证 ----
    # 合成序列的单元测试覆盖不到停表段、伪影尖峰、非 1 秒采样这些真实数据形态，
    # 所以这一步要在真实通道上再对一遍。
    if os.path.exists(args.go_np):
        print("\n[3] NP（Coggan 30 秒滑动平均口径，真实功率流）")
        go_np = {it["id"]: it for it in json.load(open(args.go_np))["items"]}
        con = sqlite3.connect(args.db)
        con.row_factory = sqlite3.Row
        streams = {r["activity_id"]: (bytes(r["data"]), r["rec_interval_secs"])
                   for r in con.execute(
                       "SELECT activity_id, data, rec_interval_secs FROM activity_streams")}
        con.close()
        mine_np, go_np_list, drift = [], [], []
        for aid, it in sorted(go_np.items()):
            if aid not in streams:
                continue
            data, interval = streams[aid]
            pw = json.loads(data).get("power") or []
            if not pw:
                continue
            m = coggan_np(pw, interval or 1)
            if m <= 0:
                continue
            mine_np.append(m)
            go_np_list.append(it["np"])
            drift.append((fourth_mean(pw) - m) / m)
        ok &= compare("NP (Coggan)", mine_np, go_np_list)
        if drift:
            ds = sorted(drift)
            n = len(ds)
            print(f"      旧口径（裸 4 次方均值）相对新口径的抬升："
                  f"中位 {ds[n // 2] * 100:+.2f}%  P90 {ds[int(n * 0.9)] * 100:+.2f}%  "
                  f"最大 {ds[-1] * 100:+.2f}%")
            print(f"      抬升 >5% 的条数: {sum(1 for d in ds if d > 0.05)} / {n}"
                  f"（这些就是伪影被 4 次方放大的那几条）")
    else:
        print(f"\n[3] 跳过 NP 对照：找不到 {args.go_np}（先跑 TestGoldenDumpNP）")

    # ---- 票 03 扩展段 ----
    ok &= rtss_section(args.db, args.athlete, args.go_rtss)
    ok &= wbal_section(args.db, args.athlete, args.go_wbal)
    # core 仓库是 openrunner 的兄弟目录（scripts/../.. = ~/project）
    core_dir = os.environ.get("OPENRUNNER_CORE_DIR") or os.path.join(
        os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))), "openrunner-core")
    if os.path.isdir(core_dir):
        ok &= synth_section(core_dir)

    print("\n" + ("✓ 两边一致（相对误差 ≤ %g）" % TOL_REL if ok else "✗ 存在差异，见上"))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
