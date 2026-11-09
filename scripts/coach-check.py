#!/usr/bin/env python3
"""coach-check：Go 周校验引擎 vs running-coach 的 check_week.py 金标准对照。

同一批用例（check_week.py 的 simple 输入形状）分别喂两边，按规则分组比对
判定级别序列。用例 = 合成用例（覆盖每条规则的通过/触发）+ 可选真实用例
（--db 备份库：近 8 周实际 + 下周计划行）。

用法：
  ./scripts/coach-check.py                     # 仅合成用例
  ./scripts/coach-check.py --db /tmp/golden.db # 加真实数据用例
环境变量：
  CHECK_WEEK_PY  check_week.py 路径（默认 ~/.pi/agent/skills/running-coach/scripts/）
"""
import argparse
import json
import os
import re
import shutil
import subprocess
import sqlite3
import sys
import tempfile
from datetime import date, timedelta

HOME = os.path.expanduser("~")
CHECK_WEEK = os.environ.get(
    "CHECK_WEEK_PY",
    os.path.join(HOME, ".pi/agent/skills/running-coach/scripts/check_week.py"))
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
CORE = os.environ.get("CORE_DIR", os.path.join(ROOT, "core"))

BASE = date(2026, 1, 5)  # 周一


def wd(w, d):
    return (BASE + timedelta(days=w * 7 + d)).isoformat()


def sess(w, d, mins, km, typ=None):
    s = {"date": wd(w, d), "minutes": mins, "km": km}
    if typ:
        s["type"] = typ
    return s


# ---------------------------------------------------------------- 合成用例

def synthetic_cases():
    cases = {}

    def hist_weeks(specs):
        # specs: [(mins_per_session, n_sessions), ...] 每周
        h = []
        for w, (m, n) in enumerate(specs):
            for d in range(n):
                h.append(sess(w, d * 2, m, m / 6))
        return h

    flat = hist_weeks([(100, 3)] * 4)
    cases["volume_ok"] = {"week_role": "build", "history": flat,
                          "plan": [sess(4, 0, 100, 16, "easy"), sess(4, 2, 100, 16, "easy"),
                                   sess(4, 4, 100, 16, "long")]}
    cases["volume_warn"] = {"week_role": "build", "history": flat,
                            "plan": [sess(4, 0, 116, 19, "easy"), sess(4, 2, 116, 19, "easy"),
                                     sess(4, 4, 116, 19, "long")]}
    cases["volume_fail"] = {"week_role": "build", "history": flat,
                            "plan": [sess(4, 0, 130, 21, "easy"), sess(4, 2, 130, 21, "easy"),
                                     sess(4, 4, 130, 21, "long")]}
    cases["volume_cautious"] = dict(cases["volume_warn"], cautious=True)
    cases["volume_deload_notlight"] = {"week_role": "deload", "history": flat,
                                       "plan": [sess(4, 0, 100, 16, "easy"), sess(4, 2, 100, 16, "easy")]}
    cases["volume_deload_ok"] = {"week_role": "deload", "history": flat,
                                 "plan": [sess(4, 0, 60, 10, "easy"), sess(4, 2, 60, 10, "easy")]}
    cases["volume_lightweek"] = {
        "week_role": "build",
        "history": hist_weeks([(100, 2)] * 3) + [sess(3, 0, 50, 8), sess(3, 2, 50, 8)],
        "plan": [sess(4, 0, 105, 17, "easy"), sess(4, 2, 105, 17, "long")]}
    cases["no_history"] = {"week_role": "build",
                           "plan": [sess(0, 0, 40, 7, "easy")]}
    cases["spike_fail"] = {
        "week_role": "build",
        "history": [sess(3, 0, 50, 10), sess(3, 2, 30, 6)],
        "plan": [sess(4, 0, 40, 8, "easy"), sess(4, 4, 75, 15, "long")]}
    cases["spike_ok"] = {
        "week_role": "build",
        "history": [sess(3, 0, 50, 10), sess(3, 2, 30, 6)],
        "plan": [sess(4, 0, 40, 8, "easy"), sess(4, 4, 55, 10.8, "long")]}
    cases["spike_race_exempt"] = {
        "week_role": "race",
        "history": [sess(3, 0, 50, 10)],
        "plan": [sess(4, 6, 90, 21, "race")]}
    cases["spike_review"] = {  # 已完成课里藏一个超标（回顾 WARN）
        "week_role": "build",
        "history": [sess(2, 0, 40, 8), sess(3, 0, 70, 14), sess(3, 2, 30, 6)],
        "plan": [sess(4, 0, 40, 8, "easy"), sess(4, 4, 45, 9, "long")]}
    cases["spike_minutes_only"] = {  # 无距离 → 分钟口径
        "week_role": "build",
        "history": [sess(3, 0, 50, 0), sess(3, 2, 30, 0)],
        "plan": [sess(4, 4, 75, 0, "long")]}
    cases["history_weeks_fallback"] = {
        "week_role": "build",
        "history_weeks": [{"week_start": wd(w, 0), "minutes": 180, "km": 30, "long_min": 60}
                          for w in range(4)],
        "plan": [sess(4, 4, 90, 15, "long"), sess(4, 0, 60, 10, "easy"),
                 sess(4, 2, 60, 10, "easy")]}
    cases["deload_rhythm"] = {
        "week_role": "build",
        "history": [sess(0, 0, 120, 20), sess(1, 0, 125, 20), sess(2, 0, 130, 21)],
        "plan": [sess(3, 0, 135, 22, "easy")]}
    cases["long_share_warn"] = {
        "week_role": "build",
        "history": [sess(3, 0, 60, 10)],
        "plan": [sess(4, 0, 30, 5, "easy"), sess(4, 4, 70, 12, "long")]}
    cases["hard_backtoback"] = {
        "week_role": "build",
        "history": [sess(3, 0, 60, 10)],
        "plan": [sess(4, 1, 50, 9, "threshold"), sess(4, 2, 50, 9, "intervals"),
                 sess(4, 4, 40, 7, "easy")]}
    cases["hard_toomany"] = {
        "week_role": "build",
        "history": [sess(3, 0, 60, 10)],
        "plan": [sess(4, 0, 50, 9, "threshold"), sess(4, 2, 50, 9, "intervals"),
                 sess(4, 4, 50, 9, "hills"), sess(4, 5, 40, 7, "easy")]}
    cases["long_next_to_hard"] = {
        "week_role": "build",
        "history": [sess(3, 0, 60, 10)],
        "plan": [sess(4, 3, 50, 9, "threshold"), sess(4, 4, 80, 14, "long"),
                 sess(4, 0, 40, 7, "easy")]}
    cases["strength_before_hard"] = {
        "week_role": "build",
        "history": [sess(3, 0, 60, 10)],
        "plan": [sess(4, 2, 30, 0, "strength"), sess(4, 3, 50, 9, "threshold"),
                 sess(4, 5, 40, 7, "easy")]}
    cases["intensity_warn"] = {
        "week_role": "build",
        "history": [sess(3, 0, 60, 10)],
        "plan": [sess(4, i, 60, 11, t) for i, t in enumerate(
            ["threshold", "intervals", "threshold", "intervals", "threshold", "easy", "easy"])]}
    cases["quality_minutes_explicit"] = {
        "week_role": "build",
        "history": [sess(3, 0, 60, 10)],
        "plan": [dict(sess(4, 0, 60, 11, "threshold"), quality_minutes=5),
                 sess(4, 2, 60, 10, "easy"), sess(4, 4, 60, 10, "long")]}
    cases["no_rest"] = {
        "week_role": "build",
        "history": [sess(3, 0, 60, 10)],
        "plan": [sess(4, i, 30, 5, "easy") for i in range(7)]}
    cases["empty_plan"] = {"week_role": "build", "history": flat, "plan": []}
    cases["peak_hard3_ok"] = {
        "week_role": "peak",
        "history": [sess(3, 0, 60, 10)],
        "plan": [sess(4, 0, 50, 9, "threshold"), sess(4, 2, 50, 9, "intervals"),
                 sess(4, 4, 50, 9, "hills"), sess(4, 6, 40, 7, "easy")]}
    return cases


def real_cases(db):
    """备份库：近 8 周实际跑步 + 下周计划行（若有）。"""
    con = sqlite3.connect(db)
    con.row_factory = sqlite3.Row
    rows = con.execute(
        "SELECT start_time, timer_secs, duration_secs, distance_m, is_planned "
        "FROM activities WHERE sport='run' ORDER BY start_time").fetchall()
    con.close()
    hist, plan = [], []
    today = date.today()
    for r in rows:
        d = date.fromtimestamp(r["start_time"])
        mins = (r["timer_secs"] or r["duration_secs"] or 0) / 60
        item = {"date": d.isoformat(), "minutes": round(mins), "km": round((r["distance_m"] or 0) / 1000, 2)}
        if r["is_planned"] and d >= today:
            item["type"] = "easy"
            plan.append(item)
        elif not r["is_planned"] and (today - d).days <= 56:
            hist.append(item)
    if not hist:
        return {}
    cases = {}
    if plan:
        cases["real_nextweek"] = {"week_role": "build", "history": hist, "plan": plan}
    # 用最近一周的实际当「计划」再校验一遍（模拟复盘）
    last_monday = today - timedelta(days=today.weekday())
    prev_week = [h for h in hist
                 if 7 <= (last_monday - date.fromisoformat(h["date"])).days <= 13]
    if prev_week:
        for h in prev_week:
            h["type"] = "long" if h["minutes"] == max(x["minutes"] for x in prev_week) else "easy"
        cases["real_prevweek_asplan"] = {"week_role": "build", "history": hist, "plan": prev_week}
    return cases


# ---------------------------------------------------------------- 两侧执行

RULE_PATTERNS = [
    (re.compile(r"^volume |^no history given|^deload week |^taper week |^race week |^return week "), "volume"),
    (re.compile(r"3 weeks of rising load"), "deload-rhythm"),
    (re.compile(r"^no run is more than 10|race is .*30-day longest|longer than the longest run|"
                r"above the recent longest|^done .*longer than the 30-day longest"), "spike"),
    (re.compile(r"^long run is "), "long-share"),
    (re.compile(r"hard days \(max|back to back|next to a hard day"), "hard-days"),
    (re.compile(r"^strength on "), "strength"),
    (re.compile(r"of time at X/Z3"), "intensity"),
    (re.compile(r"day\(s\) without running"), "rest"),
]


def run_python(cases_dir, names):
    out = {}
    for name in names:
        proc = subprocess.run([sys.executable, CHECK_WEEK, os.path.join(cases_dir, name + ".json")],
                              capture_output=True, text=True)
        verdicts = []
        for line in proc.stdout.splitlines():
            m = re.match(r"^(OK|WARN|FAIL)\s+(.*)$", line.strip())
            if not m:
                continue
            level, msg = m.groups()
            rule = None
            for pat, rid in RULE_PATTERNS:
                if pat.search(msg):
                    rule = rid
                    break
            if rule:
                verdicts.append({"level": level, "rule": rule, "detail": msg})
        out[name] = verdicts
    return out


def run_go(cases_dir, _names):
    out = os.path.join(cases_dir, "..", "go_verdicts.json")
    env = dict(os.environ, OPENRUNNER_COACH_CASES=cases_dir, OPENRUNNER_COACH_OUT=out)
    proc = subprocess.run(["go", "test", "./coach/", "-run", "TestCoachDumpWeekCheck", "-count=1"],
                          cwd=CORE, env=env, capture_output=True, text=True)
    if proc.returncode != 0:
        print(proc.stdout[-2000:], proc.stderr[-800:])
        sys.exit("Go dump 失败")
    data = json.load(open(out))
    return {k: [{"level": v["level"], "rule": v["rule"], "detail": v.get("detail", "")}
                for v in vs] for k, vs in
            ((f[:-5], vs) for f, vs in data.items())}


def import_ref():
    """直接 import skill 的 paces.py / zones.py 拿函数（不解析文本输出）。"""
    import importlib.util
    scripts_dir = os.path.dirname(CHECK_WEEK)
    sys.path.insert(0, scripts_dir)

    def load(name):
        spec = importlib.util.spec_from_file_location(name, os.path.join(scripts_dir, name + ".py"))
        mod = importlib.util.module_from_spec(spec)
        sys.modules[name] = mod
        spec.loader.exec_module(mod)
        return mod

    paces = load("paces")
    zones = load("zones")
    return paces, zones


def paces_golden():
    """票 04/05：配速表 / VDOT / 比赛→阈值 / 全马预测，数值对照。"""
    paces, zonesmod = import_ref()
    ok = True

    thresholds = [(280.0, 165.0), (250.0, 172.0), (320.0, 150.0)]
    daniels_races = [(10.0, 2400.0), (5.0, 1200.0), (21.0975, 5663.0), (10.0, 2928.0)]
    race_thr = [(10.0, 2928.0), (21.0975, 5663.0), (5.0, 1200.0), (15.0, 4500.0)]
    predict_cases = [
        {"base_km": 10, "base_secs": 2928, "weekly_km": 50, "weeks_observed": 8},
        {"base_km": 10, "base_secs": 2928, "weekly_km": 50, "race2_km": 21.0975,
         "race2_secs": 6300, "training_pace_secs": 345, "weeks_observed": 8},
        {"base_km": 5, "base_secs": 1200, "weekly_km": 30, "weeks_observed": 2},
        {"base_km": 42.195, "base_secs": 14000, "weekly_km": 50, "weeks_observed": 8},
        {"base_km": 10, "base_secs": 2928},
    ]
    cases = {
        "zone_cases": [{"threshold_s": t, "lthr": l} for t, l in thresholds],
        "daniels_cases": [{"km": k, "secs": s} for k, s in daniels_races],
        "race_thr_cases": [{"km": k, "secs": s} for k, s in race_thr],
        "predict_cases": predict_cases,
    }
    tmp_in = "/tmp/coach_paces_cases.json"
    tmp_out = "/tmp/coach_go_paces.json"
    json.dump(cases, open(tmp_in, "w"))

    env = dict(os.environ, OPENRUNNER_COACH_PACES_IN=tmp_in, OPENRUNNER_COACH_PACES_OUT=tmp_out)
    proc = subprocess.run(["go", "test", "./coach/", "-run", "TestCoachDumpPaces", "-count=1"],
                          cwd=CORE, env=env, capture_output=True, text=True)
    if proc.returncode != 0:
        print(proc.stdout[-1500:], proc.stderr[-500:])
        return False
    go = json.load(open(tmp_out))

    def close(a, b, tol, what):
        nonlocal ok
        if abs(a - b) > tol:
            ok = False
            print(f"      ✗ {what}: go={a:.2f} py={b:.2f}")

    print("\n[配速表] SpeedZoneTable vs zones.from_thresholds(8020)")
    for (thr, lthr), grows in zip(thresholds, go["zone_cases"]):
        pref = zonesmod.from_thresholds(thr, lthr, method="8020")["rows"]
        for g, p in zip(grows, pref):
            close(g["paceSlow"], p["pace_slow"], 0.5, f"thr={thr:.0f} {p['name']} pace_slow")
            if p.get("pace_fast") is not None:
                close(g["paceFast"], p["pace_fast"], 0.5, f"thr={thr:.0f} {p['name']} pace_fast")
            if p.get("hr_hi") is not None:
                close(g.get("hrHi", 0), p["hr_hi"], 1, f"thr={thr:.0f} {p['name']} hr_hi")
    print("      ✓ 逐区比对完成" if ok else "      （见上）")

    print("[VDOT] Daniels vs zones.daniels")
    for (km, secs), g in zip(daniels_races, go["daniels_cases"]):
        vd, rows = zonesmod.daniels(km, secs, "en")
        close(g["vdot"], vd, 0.05, f"daniels vdot {km}km")
        for gr, pr in zip(g["rows"], rows):
            close(gr["paceSlow"], pr["pace_slow"], 0.5, f"daniels {km}km {pr['name']} slow")
            close(gr["paceFast"], pr["pace_fast"], 0.5, f"daniels {km}km {pr['name']} fast")

    print("[比赛→阈值] ThresholdFromRace vs paces.RACE_TO_THRESHOLD")
    for (km, secs), g in zip(race_thr, go["race_thr"]):
        nearest = min(paces.RACE_TO_THRESHOLD, key=lambda d: abs(d - km))
        want = (secs / km) / paces.RACE_TO_THRESHOLD[nearest]
        close(g, want, 0.5, f"race_thr {km}km")

    print("[全马预测] PredictMarathon vs paces vickers/tanda/riegel")
    for case, g in zip(predict_cases, go["predict"]):
        bk, bs = case["base_km"], case["base_secs"]
        close(g["riegel"], bs * (42.195 / bk) ** 1.06, 2, "riegel")
        wk = case.get("weekly_km", 0)
        if wk:
            miles = wk / 1.609344
            if bk < 42:
                close(g.get("vickers1", 0), paces.vickers_model1(bk * 1000, bs, miles), 2, "vickers1")
            if case.get("race2_km"):
                (d1, t1), (d2, t2) = sorted([(bk * 1000, bs), (case["race2_km"] * 1000, case["race2_secs"])])
                close(g.get("vickers2", 0), paces.vickers_model2(d1, t1, d2, t2, miles), 2, "vickers2")
            if case.get("training_pace_secs"):
                close(g.get("tanda", 0), paces.tanda(wk, case["training_pace_secs"]), 2, "tanda")
        avail = [v for k, v in g.items() if k in ("riegel", "vickers1", "vickers2", "tanda") and v]
        close(g["recommended"], max(avail), 1e-6, "recommended=最慢值")
    return ok


def analysis_golden(db):
    """票 06：真实长课双流 vs analyze_session.py 的解耦/EF（±0.5% / ±0.02）。"""
    import statistics
    spec_dir = os.path.dirname(CHECK_WEEK)
    sys.path.insert(0, spec_dir)
    import importlib.util
    aspec = importlib.util.spec_from_file_location("analyze_session", os.path.join(spec_dir, "analyze_session.py"))
    amod = importlib.util.module_from_spec(aspec)
    sys.modules["analyze_session"] = amod
    aspec.loader.exec_module(amod)

    con = sqlite3.connect(db)
    con.row_factory = sqlite3.Row
    rows = con.execute(
        "SELECT a.id, a.timer_secs, a.duration_secs, a.distance_m, s.data "
        "FROM activities a JOIN activity_streams s ON s.activity_id = a.id "
        "WHERE a.sport='run' AND COALESCE(a.timer_secs, a.duration_secs, 0) >= 3600 "
        "ORDER BY a.start_time DESC LIMIT 5").fetchall()
    con.close()
    if not rows:
        print("      （备份库无 ≥60 分钟双流活动，跳过）")
        return True

    cases, metas = [], []
    for r in rows:
        st = json.loads(r["data"])
        hr, spd = st.get("hr") or [], st.get("speed") or []
        iv = st.get("recIntervalSecs") or 1
        cases.append({"id": r["id"], "durationSecs": (r["timer_secs"] or r["duration_secs"]),
                      "intervalSecs": iv, "hr": hr, "speed": spd})
        metas.append((hr, spd, iv, r))

    tmp_in, tmp_out = "/tmp/coach_analysis_cases.json", "/tmp/coach_go_analysis.json"
    json.dump({"cases": cases}, open(tmp_in, "w"))
    env = dict(os.environ, OPENRUNNER_COACH_ANALYSIS_IN=tmp_in, OPENRUNNER_COACH_ANALYSIS_OUT=tmp_out)
    proc = subprocess.run(["go", "test", "./coach/", "-run", "TestCoachDumpAnalysis", "-count=1"],
                          cwd=CORE, env=env, capture_output=True, text=True)
    if proc.returncode != 0:
        print(proc.stdout[-1200:], proc.stderr[-400:])
        return False
    go = json.load(open(tmp_out))

    ok = True
    for g, (hr, spd, iv, r) in zip(go, metas):
        # 造 TP data_file 形态喂参照实现：NGP=原始速度换算（无梯度通道，口径差记录在案）
        series = []
        ngps, hrs = [], []
        for i, (h, s) in enumerate(zip(hr, spd)):
            if h > 0 and s >= 4.0:
                ngp = 60.0 / s  # min/km
                series.append({"time": i * iv, "HeartRate": h, "NGP": ngp})
                ngps.append(ngp)
                hrs.append(h)
        totals = [
            {"name": "Duration", "value": (r["timer_secs"] or r["duration_secs"])},
            {"name": "Distance", "value": (r["distance_m"] or 0) / 1000},
            {"name": "NGP", "value": 1000.0 / (statistics.mean([1000 / x for x in ngps])) if ngps else 0},
            {"name": "Avg heart rate", "value": statistics.mean(hrs) if hrs else 0},
        ]
        path = f"/tmp/tp_like_{r['id']}.json"
        json.dump({"totals": totals, "lapData": [], "data": series,
                   "startTimestamp": "2026-01-01T00:00:00"}, open(path, "w"))

        class A:
            threshold_pace = None
            lthr = None
            easy_cap = None
            rftp = None
        row = amod.summary_row(path, A())
        dec_py, ef_py = row.get("decoupling"), row.get("ef")
        dec_go, ef_go = g.get("decouplingPct"), g.get("efPerBpm")
        d_ok = dec_py is not None and abs(dec_go - dec_py) <= 0.5
        e_ok = ef_py is not None and abs(ef_go - ef_py) <= 0.02
        print(f"  {'✓' if d_ok and e_ok else '✗'} 活动 {r['id']:>4}  decoupling go={dec_go:.2f} py={dec_py}  ef go={ef_go:.3f} py={ef_py}")
        if not (d_ok and e_ok):
            ok = False
    return ok


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--db", help="备份库路径：追加真实数据用例")
    args = ap.parse_args()

    if not os.path.exists(CHECK_WEEK):
        sys.exit(f"找不到 check_week.py：{CHECK_WEEK}（装 running-coach skill 或设 CHECK_WEEK_PY）")

    cases = synthetic_cases()
    if args.db:
        cases.update(real_cases(args.db))

    tmp = tempfile.mkdtemp(prefix="coach-check-")
    cases_dir = os.path.join(tmp, "cases")
    os.makedirs(cases_dir)
    for name, c in cases.items():
        json.dump(c, open(os.path.join(cases_dir, name + ".json"), "w"))
    names = sorted(cases)

    py = run_python(cases_dir, names)
    go = run_go(cases_dir, names)

    ok = True
    print(f"对照 {len(names)} 个用例（check_week.py vs coach.CheckWeek）\n")
    for name in names:
        def by_rule(vs):
            d = {}
            for v in vs:
                d.setdefault(v["rule"], []).append(v["level"])
            return d
        p, g = by_rule(py[name]), by_rule(go.get(name, []))
        # 空计划等畸形输入：python 崩溃无判定行，Go 报 ERROR/input —— 都拒绝即一致
        if not p and any(v["rule"] == "input" for v in go.get(name, [])):
            print(f"  ✓ {name:28s} 两边都拒绝（畸形输入）")
            continue
        rules = sorted(set(p) | set(g))
        diffs = [(r, p.get(r), g.get(r)) for r in rules if p.get(r) != g.get(r)]
        status = "✓" if not diffs else "✗"
        print(f"  {status} {name:28s} 规则 {len(rules)} 条")
        for r, pl, gl in diffs:
            ok = False
            print(f"      ✗ {r}: python={pl} go={gl}")
            pd = [v["detail"] for v in py[name] if v["rule"] == r]
            gd = [v["detail"] for v in go.get(name, []) if v["rule"] == r]
            for x in pd:
                print(f"        py: {x[:110]}")
            for x in gd:
                print(f"        go: {x[:110]}")
    shutil.rmtree(tmp, ignore_errors=True)

    print("\n════ 票 04/05：配速表 / VDOT / 预测 ════")
    ok = paces_golden() and ok
    if args.db:
        print("\n════ 票 06：单课分析（真实长课双流）════")
        ok = analysis_golden(args.db) and ok

    print("\n" + ("✓ 全部一致" if ok else "✗ 存在差异"))
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
