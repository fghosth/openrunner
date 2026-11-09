# 依据公开论文重新实现 GoldenCheetah 的分析指标

> **2026-11 修订**：本 ADR 最初的措辞（「实现者不阅读 GC 源码」）与仓库里的实际文档不符，
> 现按事实改写。改写不是削弱立场，而是把一个站不住的强声明换成一个站得住的。

openrunner 的分析指标（PMC/Banister、CP/W′ 与 W′bal、训练负荷/TRIMP、单课分析）
**以 GoldenCheetah（GPL v2）的指标口径为对照目标**，但实现依据是**已公开发表的论文与标准定义**：
Skiba 2012（W′bal 分段模型）、Coggan（NP/IF/TSS 的公开定义）、Morton 三参数模型、
Morton/Banister TRIMP、Karvonen 心率储备、Riegel 外推、Daniels VDOT 等。
逐条出处见 [`docs/ALGORITHMS.md`](../ALGORITHMS.md)。

## 事实是什么

开发过程中**读过** GoldenCheetah 的源码与文档，用途限于三类：

1. 确认某个指标「业界实现取的口径」是什么（例如 TRIMP 用 `timer_secs` 还是总时长、
   低于 64% MaxHR 是否计入区间）；
2. 定位行为差异（例如 GC 的 `GC_WBALFORM=int` 是省 CPU 的积分近似，
   而 Skiba 原文是分段模型——我们按原文实现，见 `docs/agents/golden-checks.md` 的偏离表）；
3. 构造对照测试的期望口径。

**没有任何一行代码由 GC 的 C++ 翻译而来。** `core/` 里的每个公式都有一份
按论文/定义独立写出的 Python 参照（`scripts/goldenlib.py`）与之对照，
参照的 docstring 注明出自论文哪一句、以及有意偏离在哪里。

## Considered Options

- **直接移植 GC C++ 代码** —— 省力且行为 100% 一致，但 GPL v2 传染，否决。
- **严格双盲净室（一人读 GC、一人只读规格实现）** —— 立场最硬，但个人项目做不到，
  而且一旦文档里出现「GC 是怎么写的」这类描述就自相矛盾。否决，改为如实描述。

## Consequences

- **法律边界在「代码」不在「想法」**：版权不覆盖算法与数学公式，GPL 传染的是代码复制。
  读过 GPL 源码后自己重写实现，与翻译其代码，法律性质不同。
- openrunner 自身采用 **MIT** 许可证。
- 所有公式常数（NP 的 30 s 窗口、TRIMP 的 0.64/1.92/1.67、Skiba τ 公式、CS/D′ 模型等）
  必须标注论文出处；标不出出处的，就说明它其实是「某个实现的口径」而非论文定义，
  那种要单独记进偏离表。
- 与 GC 的数值一致性靠**金标准对照**保证，而不是靠代码同源：
  参照实现单一来源、参照自身有解析真值自测、dump 必须调用生产函数（见 `docs/agents/golden-checks.md`）。
- 文档里出现 GC 的内部标识名（如 `GC_WBALFORM`）是**有意的**：它记录的是
  「我们知道对方怎么做、并且有意选择不同做法」，这比假装没看过更经得起追问。
