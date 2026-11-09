"""参照实现的自测 —— 钉住 goldenlib 自己。

    python3 scripts/test_golden_refs.py      # 或 make ref-test

## 为什么需要这个文件

金标准对照的推理链是：「Go 算 X，参照算 X，两者相等 → Go 对」。
这条链有个隐含前提：**参照本身是对的**。而参照没有任何东西在检查它——
它既不是 Go 的实现，也不在 Go 的测试里，出错了没人会知道（W'bal 那次就是：
参照照抄了一个错误的近似公式，Go 照抄参照，单元测试的「独立真值」又照抄同一个公式，
于是三处同错、对照台全绿）。

所以参照必须用**解析真值**或**数学恒等式**自证，而不是用「Go 也是这么算的」。
本文件的每一条断言都只依赖论文/定义里的数学性质，不 import Go 的任何东西。

断言分两类：
  * 解析解：能写出闭式答案的（指数衰减、线性耗竭、阈值 1 小时 = 100 分、真值回收）；
  * 恒等式：幂平均不等式、钳位边界、单调性、零值守卫、约定不变性。
"""

from __future__ import annotations

import math
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import goldenlib as ref  # noqa: E402


class TestHeartRate(unittest.TestCase):
    def test_hr_reserve_ratio_definition(self):
        # %HRR = (HR − HRrest)/(HRmax − HRrest)：端点是 0 和 1
        self.assertEqual(ref.hr_reserve_ratio(48, 48, 186), 0.0)
        self.assertEqual(ref.hr_reserve_ratio(186, 48, 186), 1.0)
        self.assertAlmostEqual(ref.hr_reserve_ratio(117, 48, 186), 0.5)

    def test_hr_reserve_ratio_guards(self):
        # 分母非正 → 0（无定义不产生负荷）
        self.assertEqual(ref.hr_reserve_ratio(150, 100, 100), 0.0)
        self.assertEqual(ref.hr_reserve_ratio(150, 120, 100), 0.0)

    def test_trimp_closed_form(self):
        # 直接代入论文式：分钟 × %HRR × 0.64 × e^(k·%HRR)
        r = 0.75
        got = ref.trimp(3600, 48 + r * (186 - 48), 48, 186, ref.K_MALE)
        want = 60 * r * 0.64 * math.exp(ref.K_MALE * r)
        self.assertAlmostEqual(got, want, places=12)

    def test_trimp_monotone_in_hr(self):
        # 同长度，心率越高负荷越大（指数模型的基本性质）
        vals = [ref.trimp(3600, hr, 48, 186, ref.K_MALE) for hr in (120, 140, 160, 180)]
        self.assertEqual(vals, sorted(vals))
        self.assertTrue(len(set(vals)) == 4)

    def test_trimp_superlinear(self):
        # 指数项的后果：心率翻倍（%HRR 加倍）负荷 > 2 倍
        a = ref.trimp(3600, 48 + 0.3 * 138, 48, 186, ref.K_MALE)
        b = ref.trimp(3600, 48 + 0.6 * 138, 48, 186, ref.K_MALE)
        self.assertGreater(b / a, 2.0)

    def test_trimp_guards(self):
        self.assertEqual(ref.trimp(0, 150, 48, 186, ref.K_MALE), 0.0)
        self.assertEqual(ref.trimp(3600, 40, 48, 186, ref.K_MALE), 0.0)  # 低于静息

    def test_trimp100_one_hour_at_threshold_is_100(self):
        # 定义：阈值心率 1 小时 = 100 分。这条是整个归一化的锚点。
        t = ref.trimp(3600, 165, 48, 186, ref.K_MALE)
        self.assertAlmostEqual(ref.trimp100(t, 48, 186, 165, ref.K_MALE), 100.0, places=9)

    def test_trimp100_scales_linearly(self):
        t1 = ref.trimp(1800, 165, 48, 186, ref.K_MALE)
        self.assertAlmostEqual(ref.trimp100(t1, 48, 186, 165, ref.K_MALE), 50.0, places=9)
        t2 = ref.trimp(7200, 165, 48, 186, ref.K_MALE)
        self.assertAlmostEqual(ref.trimp100(t2, 48, 186, 165, ref.K_MALE), 200.0, places=9)

    def test_trimp100_guards(self):
        self.assertEqual(ref.trimp100(0, 48, 186, 165, ref.K_MALE), 0.0)
        self.assertEqual(ref.trimp100(10, 186, 186, 165, ref.K_MALE), 0.0)  # 分母无定义


class TestPMC(unittest.TestCase):
    def test_ema_closed_form_constant_load(self):
        # 常数负荷 s：y[n] = s·(1 − a^(n+1))，a = e^(−1/τ)
        s, tau, n = 100.0, 42.0, 200
        out = ref.ema([s] * n, tau, {})
        a = math.exp(-1.0 / tau)
        self.assertAlmostEqual(out[-1], s * (1 - a ** n), places=9)

    def test_ema_converges_to_load(self):
        # 跑得够久 → 收敛到负荷本身（时间常数决定收敛速度，不是值）
        out = ref.ema([80.0] * 5000, ref.TAU_SHORT, {})
        self.assertAlmostEqual(out[-1], 80.0, places=6)

    def test_ema_zero_load_decays(self):
        # 负荷归零后按 e^(−1/τ) 衰减：第 k 天 = y0·a^k
        out = ref.ema([100.0] * 1 + [0.0] * 99, ref.TAU_LONG, {})
        a = math.exp(-1.0 / ref.TAU_LONG)
        # 第 0 天 = 100，第 k 天 = 100·a^k
        self.assertAlmostEqual(out[50], out[0] * a ** 50, places=9)

    def test_ema_seed_resets(self):
        # 播种日直接给初值，且之后从该值继续递推
        out = ref.ema([0.0] * 10, ref.TAU_SHORT, {3: 200.0})
        self.assertEqual(out[3], 200.0)
        self.assertAlmostEqual(out[2], 0.0)
        self.assertGreater(out[4], 0.0)
        self.assertLess(out[4], 200.0)

    def test_ema_step_response_bounds_ctrl(self):
        # 一阶低通的阶跃响应恒 ≤ 输入（不会过冲）
        out = ref.ema([50.0] * 50, ref.TAU_LONG, {})
        self.assertTrue(all(v <= 50.0 + 1e-12 for v in out))


class TestPowerMeans(unittest.TestCase):
    def test_fourth_mean_constant(self):
        self.assertAlmostEqual(ref.fourth_mean([12.0] * 100), 12.0, places=12)

    def test_fourth_mean_power_mean_inequality(self):
        # 4 次幂平均 ≥ 算术平均（等号仅当全相等）——幂平均不等式的必然结果
        xs = [10.0, 12.0, 14.0, 8.0]
        self.assertGreater(ref.fourth_mean(xs), sum(xs) / len(xs))

    def test_fourth_mean_ignores_non_positive(self):
        self.assertEqual(ref.fourth_mean([0, 0, -5]), 0.0)
        self.assertAlmostEqual(ref.fourth_mean([10, 0, -3, 10]), 10.0, places=12)

    def test_conventions_agree_when_series_long(self):
        # 前导窗口的稀释量只贡献 29 个窗口：序列越长越无关（真实活动都是几千个样本）。
        xs = [200.0] * 3000
        skip = ref.coggan_np(xs, 1, "skip")
        pad = ref.coggan_np(xs, 1, "zeropad")
        self.assertAlmostEqual(skip, 200.0, places=9)
        self.assertLess(abs(skip - pad) / skip, 0.005)

    def test_short_series_conventions_diverge(self):
        # 序列只比窗口长一点时，零补齐会把均值显著压低——所以这个约定必须写进文档，
        # 不能靠「反正差不多」蒙过去（100 个样本上两者差 6.5%）。
        xs = [200.0] * 100
        skip = ref.coggan_np(xs, 1, "skip")
        pad = ref.coggan_np(xs, 1, "zeropad")
        self.assertAlmostEqual(skip, 200.0, places=9)
        self.assertGreater((skip - pad) / skip, 0.05)

    def test_leading_window_difference_bounded_on_long_series(self):
        # 长序列上 skip 恒 ≥ zeropad（零补齐只会把均值压低），且差异 <0.5%
        # —— 这就是 golden-check 里 TOL_NP_CONVENTION 的数学依据。
        xs = [600.0] * 3000
        skip = ref.coggan_np(xs, 1, "skip")
        pad = ref.coggan_np(xs, 1, "zeropad")
        self.assertGreater(skip, pad)
        self.assertLess((skip - pad) / skip, 0.005)

    def test_window_dilutes_single_spike(self):
        # 30 秒窗口把单样本尖峰稀释到 1/30 的权重：4 次方放大被压住
        xs = [300.0] * 600
        xs[320] = 1904.0
        np_ = ref.coggan_np(xs, 1, "skip")
        self.assertLess(np_, 330.0)
        # 裸 4 次方均值会被同一尖峰抬得远高于此
        self.assertGreater(ref.fourth_mean(xs), np_ * 1.1)

    def test_window_scales_with_interval(self):
        # 30 秒是「按秒」定义的：2 s 采样时窗口是 15 个样本，结果应基本不变
        base = 300.0
        one = ref.coggan_np([base] * 600 + [base * 1.5] * 600, 1, "skip")
        two = ref.coggan_np(([base] * 300 + [base * 1.5] * 300) * 2, 2, "skip")
        self.assertLess(abs(one - two) / one, 0.02)

    def test_short_series_falls_back_to_bare(self):
        # 一个窗口都凑不满 → 退化为裸 4 次方均值（定义上无可平均）
        xs = [100.0, 200.0, 300.0, 400.0]
        self.assertAlmostEqual(ref.coggan_np(xs, 1, "skip"), ref.fourth_mean(xs), places=12)

    def test_bad_convention_rejected(self):
        with self.assertRaises(ValueError):
            ref.coggan_np([100.0] * 40, 1, "nonsense")


class TestRtss(unittest.TestCase):
    def test_one_hour_at_threshold_is_100(self):
        # TSS 家族的定义锚点：阈值强度 1 小时 = 100 分
        self.assertAlmostEqual(ref.rtss(3600, 12.0, 12.0), 100.0, places=12)

    def test_quadratic_in_intensity(self):
        # IF 加倍 → 4 倍（IF² 的直接后果）
        self.assertAlmostEqual(ref.rtss(3600, 24.0, 12.0), 400.0, places=9)

    def test_linear_in_duration(self):
        self.assertAlmostEqual(ref.rtss(1800, 12.0, 12.0), 50.0, places=12)

    def test_guards(self):
        self.assertEqual(ref.rtss(0, 12.0, 12.0), 0.0)
        self.assertEqual(ref.rtss(3600, 0.0, 12.0), 0.0)
        self.assertEqual(ref.rtss(3600, 12.0, 0.0), 0.0)


class TestWBal(unittest.TestCase):
    """W'bal 的断言全部来自 Skiba 2012 分段模型的解析解。"""

    CP, WP, TAU = 250.0, 20000.0, 316.0

    def test_sustained_surge_depletes_linearly(self):
        # 恒定 ΔP 时储备线性下降，(W'/ΔP) 秒耗尽——这是分段模型的定义性行为。
        # 「前向积分 + 并发衰减」在这里只会降到 W'−τ·ΔP，永远到不了 0。
        dp = 20.0
        power = [self.CP + dp] * 1300
        bal = ref.wbal(power, self.CP, self.WP, self.TAU, 1)
        # 第 i 个样本处理完之后，累计扣掉 (i+1)·ΔP
        deplete_at = int(self.WP / dp)  # 第 1000 个样本处理完刚好归零
        self.assertAlmostEqual(bal[499], self.WP - dp * 500, places=6)
        self.assertEqual(bal[deplete_at - 2], dp)
        self.assertEqual(bal[deplete_at - 1], 0.0)
        self.assertEqual(bal[-1], 0.0)

    def test_recovery_is_exponential(self):
        # 恢复段解析解：W'bal = W' − (W' − W'bal0)·e^(−Δt/τ)
        power = [self.CP + 100.0] * 100 + [self.CP - 50.0] * 100
        bal = ref.wbal(power, self.CP, self.WP, self.TAU, 1)
        deficit = 100.0 * 100  # 100 s × 100 W
        self.assertAlmostEqual(bal[99], self.WP - deficit, places=6)
        want = self.WP - deficit * math.exp(-100 / self.TAU)
        self.assertAlmostEqual(bal[199], want, places=6)

    def test_clamped_bounds(self):
        power = [self.CP + 500.0] * 2000 + [self.CP - 100.0] * 5000
        bal = ref.wbal(power, self.CP, self.WP, self.TAU, 1)
        self.assertTrue(all(0.0 <= v <= self.WP + 1e-9 for v in bal))

    def test_below_cp_only_never_drops(self):
        bal = ref.wbal([self.CP - 1.0] * 1000, self.CP, self.WP, self.TAU, 1)
        self.assertTrue(all(v == self.WP for v in bal))

    def test_missing_samples_do_not_recover(self):
        # p ≤ 0 是「没记录」，不是「在滑行」——不能白送 W'
        power = [self.CP + 100.0] * 100 + [0.0] * 500
        bal = ref.wbal(power, self.CP, self.WP, self.TAU, 1)
        self.assertEqual(bal[-1], bal[99])

    def test_interval_scales_consumption(self):
        # 采样间隔 2 s 时每秒扣的功翻倍 → 同样时长消耗相同总功
        p1 = [self.CP + 50.0] * 600
        p2 = [self.CP + 50.0] * 300
        b1 = ref.wbal(p1, self.CP, self.WP, self.TAU, 1)
        b2 = ref.wbal(p2, self.CP, self.WP, self.TAU, 2)
        self.assertAlmostEqual(b1[-1], b2[-1], places=6)

    def test_tau_formula_endpoints(self):
        # τ = 546·e^(−0.01·(CP−P̄below)) + 316：P̄below=CP 时 = 862，差 100 W 时 ≈ 516.9
        self.assertAlmostEqual(ref.tau_from_data(250, 250), 862.0, places=9)
        self.assertAlmostEqual(ref.tau_from_data(250, 150), 546 * math.exp(-1.0) + 316, places=9)


class TestFits(unittest.TestCase):
    def test_cs_ref_recovers_truth(self):
        # 从 D(t) = D' + CS·t 的合成点精确回收参数
        cs, dp = 3.4, 150.0
        pts = [(float(t), dp + cs * t) for t in (120, 180, 240, 300, 420, 600)]
        got_cs, got_dp = ref.cs_ref(pts)
        self.assertAlmostEqual(got_cs, cs, places=9)
        self.assertAlmostEqual(got_dp, dp, places=6)

    def test_cs_ref_degenerate(self):
        # 时长全相同 → 分母 0，返回 0 而不是抛异常/NaN
        self.assertEqual(ref.cs_ref([(100.0, 500.0), (100.0, 600.0)]), (0.0, 0.0))

    def test_solve3_known_system(self):
        a = [[2.0, 1.0, -1.0], [-3.0, -1.0, 2.0], [-2.0, 1.0, 2.0]]
        b = [8.0, -11.0, -3.0]
        x = ref.solve3(a, b)
        want = [2.0, 3.0, -1.0]
        for got, w in zip(x, want):
            self.assertAlmostEqual(got, w, places=9)

    def test_solve3_singular(self):
        a = [[1.0, 2.0, 3.0], [2.0, 4.0, 6.0], [1.0, 1.0, 1.0]]
        self.assertIsNone(ref.solve3(a, [1.0, 2.0, 3.0]))

    def test_banister_recovers_known_coefficients(self):
        # 用已知 (p0,k1,k2) 生成无噪表现值，最小二乘必须原样回收
        p0, k1, k2 = 5.0, 0.08, 0.05
        loads = {f"2026-03-{d:02d}": 40.0 + d for d in range(1, 29)}
        races = []
        for day in ("2026-03-10", "2026-03-17", "2026-03-24", "2026-03-28"):
            a = sum(w * math.exp(-((int(day[-2:]) - int(d[-2:]))) / 45.0)
                    for d, w in loads.items() if int(d[-2:]) < int(day[-2:]))
            b = sum(w * math.exp(-((int(day[-2:]) - int(d[-2:]))) / 15.0)
                    for d, w in loads.items() if int(d[-2:]) < int(day[-2:]))
            races.append({"date": day, "perf": p0 + k1 * a - k2 * b})
        got = ref.banister_lstsq(loads, races, 45.0, 15.0)
        self.assertIsNotNone(got)
        self.assertAlmostEqual(got[0], p0, places=6)
        self.assertAlmostEqual(got[1], k1, places=9)
        self.assertAlmostEqual(got[2], k2, places=9)

    def test_banister_needs_three_races(self):
        # 3 个未知数 → 少于 3 个观测必然不可辨识
        loads = {"2026-03-01": 50.0}
        self.assertIsNone(ref.banister_lstsq(loads, [{"date": "2026-03-02", "perf": 5.0},
                                                    {"date": "2026-03-03", "perf": 5.1}], 45.0, 15.0))


class TestZoneLookup(unittest.TestCase):
    def test_zone_for_picks_latest_start(self):
        # 区间重叠时取生效起点最晚者（否则用户改阈值会被旧记录盖掉）
        zones = [
            {"id": 1, "valid_from": "2026-01-01", "valid_to": "2026-12-31", "v": "old"},
            {"id": 2, "valid_from": "2026-06-01", "valid_to": "2026-12-31", "v": "new"},
        ]
        self.assertEqual(ref.zone_for("2026-07-01", zones)["v"], "new")
        self.assertEqual(ref.zone_for("2026-03-01", zones)["v"], "old")
        self.assertIsNone(ref.zone_for("2027-01-01", zones))


if __name__ == "__main__":
    unittest.main(verbosity=2)
