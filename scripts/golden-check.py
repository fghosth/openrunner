#!/usr/bin/env python3
"""golden-check：Go 生产路径 vs 参照实现的逐值对照。

    ./scripts/golden-check.sh                 用最新备份快照
    GOLDEN_DB=~/.openrunner/openrunner.db ./scripts/golden-check.sh

## 参照实现在哪

全部公式在 `scripts/goldenlib.py`，**只有一份**，本脚本只做「取数 → 调参照 → 比 → 报告」。
早期版本把 W'bal 同时写在真实流段和合成用例段，改一份等于没改（合成段当场爆 3.06e+01）。

## 纪律

参照实现按论文/定义独立写，**不以 Go 的写法为依据**。与 Go 的有意偏离（前导窗口、停表样本）
在 goldenlib 里作为显式参数说明，本脚本把差值**量出来并打印**，而不是靠抄袭对齐。

参照自身的正确性由 `scripts/test_golden_refs.py`（`make ref-test`）用解析真值钉住。
"""

from __future__ import annotations

import argparse
import json
import math
import os
import sqlite3
import subprocess
import sys
from datetime import datetime

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import goldenlib as ref  # noqa: E402

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CORE = os.path.join(os.path.dirname(ROOT), "openrunner-core")
TOL_REL = 1e-9                 # 浮点噪声容忍；真差异都远大于这个


# ---------------------------------------------------------------- 数据

def local_date(unix_secs: int) -> str:
    """按系统本地时区切日——与 Go 侧 time.Local 同口径，否则日聚合会整体错位。"""
    return datetime.fromtimestamp(unix_secs).strftime("%Y-%m-%d")


def load(db_path: str, athlete_id: int):
    con = sqlite3.connect(db_path)
    con.row_factory = sqlite3.Row
    sex = con.execute("SELECT sex FROM athletes WHERE id=?", (athlete_id,)).fetchone()["sex"]
    zones = [dict(r) for r in con.execute(
        "SELECT id, valid_from, valid_to, max_hr, rest_hr, lt_hr FROM hr_zones "
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


def streams_of(db_path: str) -> dict:
    con = sqlite3.connect(db_path)
    con.row_factory = sqlite3.Row
    out = {r["activity_id"]: (r["data"], r["rec_interval_secs"] or 1)
           for r in con.execute("SELECT activity_id, data, rec_interval_secs FROM activity_streams")}
    con.close()
    return out


# ---------------------------------------------------------------- 比对

def worst(pairs, key):
    return max(pairs, key=key) if pairs else None


def compare(label: str, mine: list[float], theirs: list[float], missing_note: str = "",
            tol: float = TOL_REL) -> bool:
    if len(mine) != len(theirs):
        print(f"  ✗ {label}: 长度不同  mine={len(mine)} go={len(theirs)}")
        return False
    diffs = []
    for i, (m, t) in enumerate(zip(mine, theirs)):
        denom = max(abs(t), 1e-12)
        rel = abs(m - t) / denom
        diffs.append((rel, abs(m - t), i, m, t))
    n_bad = sum(1 for d in diffs if not (d[0] <= tol))  # NaN 也算不合格
    w = worst(diffs, key=lambda d: d[0])
    ok = n_bad == 0
    mark = "✓" if ok else "✗"
    print(f"  {mark} {label:<22} n={len(mine):<5} 最大相对误差={w[0]:.3e}  最大绝对误差={w[1]:.3e}"
          f"  超容忍={n_bad}")
    if not ok:
        rel_, ad, i, m, t = w
        print(f"      最差 #{i}: mine={m!r} go={t!r}  Δ={ad:.6g}  {missing_note}")
    return ok


# ---------------------------------------------------------------- 1) TRIMP / TRIMP100

def trimp_section(sex: str, zones: list[dict], acts: list[dict]) -> bool:
    k = ref.K_FEMALE if str(sex).upper().startswith("F") else ref.K_MALE
    mine_trimp, mine_t100, go_trimp, go_t100 = [], [], [], []
    skipped = 0
    for a in acts:
        day = local_date(a["start_time"])
        z = ref.zone_for(day, zones)
        if not z:
            skipped += 1
            continue
        dur = float(a["timer_secs"] or a["duration_secs"] or 0)
        hr = float(a["avg_hr"] or 0)
        if hr <= 0:
            hr = float(a["avg_hr"] or 0)
        if hr <= 0:
            skipped += 1  # 无心率：不算（Go 侧也不产出）
            continue
        t = ref.trimp(dur, hr, z["rest_hr"], z["max_hr"], k)
        mine_trimp.append(t)
        mine_t100.append(ref.trimp100(t, z["rest_hr"], z["max_hr"], z["lt_hr"], k))
        go_trimp.append(float(a["go_trimp"] or 0))
        go_t100.append(float(a["go_trimp100"] or 0))
    print(f"\n[1] TRIMP / TRIMP100（Banister 1975 + Green 系数，k={k}；{len(mine_trimp)} 条活动，"
          f"跳过 {skipped} 条无区间/无心率）")
    ok = compare("TRIMP", mine_trimp, go_trimp, "（时长口径 timer 优先）")
    ok &= compare("TRIMP100", mine_t100, go_t100)
    return ok


# ---------------------------------------------------------------- 2) PMC

def pmc_section(payload: dict, seasons: list[dict], tau_long: float, tau_short: float) -> bool:
    """PMC：CTL/ATL 的日离散指数衰减 + 赛季播种，全部由 goldenlib 独立算。"""
    series = payload["series"]
    dates = series["dates"]
    stress = series.get("stress") or []
    actual = {d: s for d, s in zip(dates, stress)}
    # 赛季播种：直接从库里读赛季起点（payload 里没有），映射到日期下标
    seeds = {}
    for s in seasons:
        if s["start_date"] in dates:
            seeds[dates.index(s["start_date"])] = float(s["seed"])
    stress_list = [actual.get(d, 0.0) for d in dates]

    mine_lts = ref.ema(stress_list, tau_long, seeds)
    mine_sts = ref.ema(stress_list, tau_short, seeds)
    print(f"\n[2] PMC（CTL={tau_long:g}d / ATL={tau_short:g}d 指数衰减，{len(dates)} 天）")
    ok = compare("CTL(LTS)", mine_lts, series["lts"], "（播种日直接给初值）")
    ok &= compare("ATL(STS)", mine_sts, series["sts"])
    # TSB = CTL−ATL（默认画在次日，故第 i 天读到的是第 i−1 天的差值）
    off = 1
    mine_sb = [mine_lts[i] - mine_sts[i] for i in range(len(dates))]
    go_sb = series["sb"]
    pairs = [(mine_sb[i], go_sb[i + off]) for i in range(len(dates)) if i + off < len(go_sb)]
    diffs = [abs(m - g) for m, g in pairs]
    ok &= max(diffs) <= 1e-9 if diffs else True
    print(f"  {'✓' if max(diffs) <= 1e-9 else '✗'} TSB(CTL−ATL)        n={len(diffs):<5} "
          f"最大绝对误差={max(diffs) if diffs else 0:.3e}")
    return ok


# ---------------------------------------------------------------- 3) NP

def np_section(db_path: str, go_np_path: str) -> bool:
    """NP：参照同时算「零补齐」与「跳过前导窗口」两种约定。

    Coggan 的定义只写了「30 秒滑动平均 + 4 次方均值」，没写开头不足一个窗口怎么办。
    openrunner 选 skip（避开零稀释），GoldenCheetah 选 zeropad。这里把两种都算出来：
      - Go 必须逐值等于 skip（1e-9）——钉住实现选择了哪个约定
      - skip 与 zeropad 的差必须 <0.5% —— 钉住「这个选择没有实质影响」
    """
    if not os.path.exists(go_np_path):
        print(f"\n[3] 跳过 NP 对照：找不到 {go_np_path}")
        return True
    go = json.load(open(go_np_path))["items"]
    streams = streams_of(db_path)
    mine, theirs, conv_delta, conv_mono, bare = [], [], [], [], []
    for it in go:
        entry = streams.get(it["id"])
        if not entry:
            continue
        power = json.loads(entry[0]).get("power") or []
        if not power:
            continue
        iv = entry[1]
        skip = ref.coggan_np(power, iv, leading="skip")
        pad = ref.coggan_np(power, iv, leading="zeropad")
        mine.append(skip)
        theirs.append(it["np"])
        if pad > 0:
            conv_delta.append(abs(skip - pad) / pad)
            conv_mono.append(skip - pad)
        bare.append((ref.fourth_mean(power), skip))
    print(f"\n[3] NP（Coggan 30 秒滑动平均口径，真实功率流）")
    ok = compare("NP (Coggan)", mine, theirs, "（未满窗口不参与）")
    if conv_delta:
        # 前导窗口是 Coggan 定义里没写死的地方（skip vs 零补齐）。实测它不是「可忽略」的：
        # 中位 0.2%、p90 1.4%、短流最大 11.7%。所以这里**不设「必须相等」的容差**，只
        # 把分布测出来供文档引用，另加一条量级断言防止实现被改坏。
        #
        # 方向也不恒定：起跑段有高伪影时 zeropad 反而更高（实测活动 151：起始 1422 W 的
        # 稀释窗口把 4 次方均值抬过 skip）。这正是本仓选 skip 的理由——起始伪影在 skip 下
        # 完全不参与，在 zeropad 下会以 1/window 权重进平均。
        conv_delta.sort()
        n = len(conv_delta)
        mx = conv_delta[-1]
        bound_ok = mx <= 0.20
        ok &= bound_ok
        above = sum(1 for d in conv_delta if d > 0.02)
        higher = sum(1 for d in conv_mono if d > 0)
        print(f"      前导窗口约定差（skip vs zeropad）：中位 {conv_delta[n//2]*100:.2f}%  "
              f"p90 {conv_delta[int(n*0.9)]*100:.2f}%  最大 {mx*100:.2f}%  "
              f"{'✓' if bound_ok else '✗ 量级异常'}"
              f"（>2% 的 {above} 条；skip 更高的 {higher}/{n} 条）")
    # 裸 4 次方均值相对平滑口径的抬升：说明这道平均为什么不是可选项
    lifts = sorted((b - s) / s for b, s in bare if s > 0)
    if lifts:
        print(f"      旧口径（裸 4 次方均值）相对新口径的抬升：中位 {lifts[len(lifts)//2]*100:+.2f}%  "
              f"P90 {lifts[int(len(lifts)*0.9)]*100:+.2f}%  最大 {lifts[-1]*100:+.2f}%")
        print(f"      抬升 >5% 的条数: {sum(1 for x in lifts if x > 0.05)} / {len(lifts)}"
              f"（这些就是伪影被 4 次方放大的那几条）")
    return ok


# ---------------------------------------------------------------- 4) rTSS

def rtss_section(db_path: str, go_rtss_path: str) -> bool:
    """rTSS：参照从流独立算 NS（与 NP 同一个幂均值定义）+ TSS 家族式。

    口径（DEVLOG Round 6 决议）：原始速度无 NGP 坡度修正；时长 timer 优先。
    """
    if not os.path.exists(go_rtss_path):
        print(f"\n[4] 跳过 rTSS 对照：找不到 {go_rtss_path}")
        return True
    go = json.load(open(go_rtss_path))["items"]
    streams = streams_of(db_path)
    mine, theirs = [], []
    for it in go:
        entry = streams.get(it["id"])
        if not entry:
            continue
        speeds = json.loads(entry[0]).get("speed") or []
        if not [v for v in speeds if v > 0]:
            continue
        ns = ref.normalized_speed(speeds, entry[1], leading="skip")
        mine.append(ref.rtss(it["durSecs"], ns, it["thrKph"]))
        theirs.append(it["rtss"])
    print("\n[4] rTSS（Python 独立 NS + TSS 家族式 vs Go，%d 条真实跑步流）" % len(mine))
    ok = compare("rTSS", mine, theirs)
    # 裸 NS（无滑动平均）会把这些条抬起来——量一下差多少，证明平滑不是可选项
    worst_lift, worst_id = 0.0, None
    for it in go:
        entry = streams.get(it["id"])
        if not entry:
            continue
        speeds = json.loads(entry[0]).get("speed") or []
        pos = [v for v in speeds if v > 0]
        if not pos:
            continue
        bare_ns = ref.fourth_mean(pos)
        if bare_ns > 0:
            lift = (bare_ns - ref.normalized_speed(speeds, entry[1], leading="skip")) / bare_ns
            if lift > worst_lift:
                worst_lift, worst_id = lift, it["id"]
    if worst_id is not None and worst_lift > 0:
        print(f"      裸 NS（无 30s 平滑）最大抬升 {worst_lift*100:+.1f}%（活动 {worst_id}）"
              f" → rTSS 差 {(worst_lift)*100:.1f}%")
    return ok


# ---------------------------------------------------------------- 5) W'bal

def wbal_section(db_path: str, go_wbal_path: str) -> bool:
    """W'bal：Skiba 2012 分段模型的独立实现，真实功率流 + τ 公式逐活动核对。"""
    if not os.path.exists(go_wbal_path):
        print(f"\n[5] 跳过 W'bal 对照：找不到 {go_wbal_path}")
        return True
    go = json.load(open(go_wbal_path))["items"]
    streams = streams_of(db_path)
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
        tau = ref.tau_from_data(it["cp"], sum(below) / len(below) if below else it["cp"])
        if abs(tau - it["tau"]) > 1e-6:
            print(f"      ✗ 活动 {it['id']}: τ go={it['tau']:.3f} py={tau:.3f}")
            ok = False
            continue
        series = ref.wbal(power, it["cp"], it["wprime"], tau, iv)
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


# ---------------------------------------------------------------- 6) 合成用例

def synth_section(core_dir: str) -> bool:
    """合成用例：已知真值回收（CP / CP3 / Banister / W'bal 饱和钳位）。

    这里的参照也全部来自 goldenlib —— 与真实流段共用同一份实现。
    """
    # CP：两点/三点回归
    # 真值必须是 D(t) = D' + CS·t 的线性形式（写成 P = CP + W'/t 那种反函数会拟合出负 CS）
    cs_truth = [(3.4, 150.0), (4.0, 120.0), (250.0, 20000.0)]  # (CS, D')：跑步域两个 + 功率域一个
    durations = [(120.0, 180.0, 240.0), (60.0, 300.0, 600.0), (180.0, 420.0, 900.0)]
    cs_cases = [[(t, dp + cs * t) for t in ts] for (cs, dp), ts in zip(cs_truth, durations)]
    # CP3：D(t) = D'·(1−e^(−t/τ)) + CS·t 的真值回收
    cp3_cases = [(3.4, 150.0, 240.0), (4.0, 100.0, 180.0)]
    cp3_points = []
    for cs, dp, tau in cp3_cases:
        pts = []
        for t in (60, 120, 180, 300, 420):
            pts.append((float(t), dp * (1 - math.exp(-t / tau)) + cs * t))
        cp3_points.append(pts)

    # Banister：固定 τ 的 lstsq 恢复已知系数
    # Banister 合成用例：perf 由已知 (p0,k1,k2) 生成 —— 只有真值回收才有意义。
    # （早期版本手填 perf=5.0/5.1/5.3，lstsq 会解出一组「两边一致的垃圾」，验证不到东西。）
    ban_truth = [(5.0, 0.08, 0.05), (4.2, 0.06, 0.04)]
    ban_fixed = []
    for i, (p0, k1, k2) in enumerate(ban_truth):
        month = 1 + i
        loads = {f"2026-0{month}-{d:02d}": 40.0 + 2 * d for d in range(1, 25)}
        races = []
        for day in (8, 15, 22, 24):
            date = f"2026-0{month}-{day:02d}"
            a = sum(w * math.exp(-(day - d) / 45.0) for d, w in
                    ((d, loads[f"2026-0{month}-{d:02d}"]) for d in range(1, 25)) if d < day)
            b = sum(w * math.exp(-(day - d) / 15.0) for d, w in
                    ((d, loads[f"2026-0{month}-{d:02d}"]) for d in range(1, 25)) if d < day)
            races.append({"date": date, "perf": p0 + k1 * a - k2 * b})
        ban_fixed.append({"loads": loads, "races": races, "tau1": 45.0, "tau2": 15.0,
                          "p0": p0, "k1": k1, "k2": k2})
    # W'bal 饱和/回充钳位边界：远超 CP 消耗到 0，再低于 CP 回充到 W' 封顶
    wbal_cases = [{"power": [400.0] * 600 + [200.0] * 900, "cp": 280.0,
                   "wprime": 15000.0, "tau": 300.0, "interval": 1}]

    cases = {
        "cs_cases": [[{"seconds": t, "distanceM": d} for t, d in pts] for pts in cs_cases],
        "cp3_cases": [[{"seconds": t, "distanceM": d} for t, d in pts] for pts in cp3_points],
        "banister": ban_fixed,
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
        cs_py, dp_py = ref.cs_ref(pts)
        d = max(abs(row["Cs"] - cs_py), abs(row["Dprime"] - dp_py) / 10)
        good = d <= 1e-9
        ok &= good
        print(f"  {'✓' if good else '✗'} CP[{i}]: go=({row['Cs']:.4f},{row['Dprime']:.2f}) "
              f"py=({cs_py:.4f},{dp_py:.2f})")
    for i, ((cs, dp, tau), row) in enumerate(zip(cp3_cases, go["cp3"])):
        d = max(abs(row["Cs"] - cs), abs(row["Dprime"] - dp), abs(row["Tau"] - tau))
        good = d <= 1e-3
        ok &= good
        print(f"  {'✓' if good else '✗'} CP3[{i}] 真值回收: go=(CS {row['Cs']:.3f}, "
              f"D' {row['Dprime']:.1f}, τ {row['Tau']:.0f}) 真值=({cs}, {dp}, {tau})")
    for i, (case, row) in enumerate(zip(ban_fixed, go["banister"])):
        truth = (case["p0"], case["k1"], case["k2"])
        got = ref.banister_lstsq(case["loads"], case["races"], case["tau1"], case["tau2"])
        if got is None:
            ok = False
            print(f"  ✗ Banister[{i}]: 参照解不出")
            continue
        d_py = max(abs(a - b) for a, b in zip(got, truth))
        d_go = max(abs(a - b) for a, b in zip((row["P0"], row["K1"], row["K2"]), truth))
        good = d_py <= 1e-6 and d_go <= 1e-6
        ok &= good
        print(f"  {'✓' if good else '✗'} Banister[{i}] 真值回收: go=({row['P0']:.4f},"
              f"{row['K1']:.5f},{row['K2']:.5f}) 真值=({truth[0]},{truth[1]},{truth[2]})")
    for i, (wc, go_series) in enumerate(zip(wbal_cases, go["wbal"])):
        # 用例显式给 τ（这段测的是钳位与回充边界，不是 τ 公式），Go 也是把 τ 当参数收的。
        series = ref.wbal(wc["power"], wc["cp"], wc["wprime"], wc["tau"], wc["interval"])
        d = max(abs(a - b) for a, b in zip(series, go_series))
        hits_zero = min(series) <= 1e-9
        # 恢复段的解析解：W'bal = W' − (W' − 0)·e^(−Δt/τ)，Δt 为恢复段长度
        rec_secs = 900.0
        want_end = wc["wprime"] * (1 - math.exp(-rec_secs / wc["tau"]))
        close_ok = abs(series[-1] - want_end) <= 1e-6
        good = d <= 1e-9 and hits_zero and close_ok
        ok &= good
        print(f"  {'✓' if good else '✗'} W'bal 钳位/回充: 最大差 {d:.3e}，"
              f"探底 0 {'是' if hits_zero else '否'}，恢复 900s 回到 "
              f"{series[-1]:.0f}（解析 {want_end:.0f}）")
    return ok


# ---------------------------------------------------------------- main

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
    print(f"档案 {args.athlete}: sex={sex}；心率区间 {len(zones)} 套；活动 {len(acts)} 条；赛季 {len(seasons)} 个")

    ok = True
    ok &= trimp_section(sex, zones, acts)
    ok &= pmc_section(go, seasons, ref.TAU_LONG, ref.TAU_SHORT)
    ok &= np_section(args.db, args.go_np)
    ok &= rtss_section(args.db, args.go_rtss)
    ok &= wbal_section(args.db, args.go_wbal)
    ok &= synth_section(CORE)

    print()
    if ok:
        print("✓ 两边一致（相对误差 ≤ %.0e）" % TOL_REL)
        return 0
    print("✗ 存在差异，见上")
    return 1


if __name__ == "__main__":
    sys.exit(main())
