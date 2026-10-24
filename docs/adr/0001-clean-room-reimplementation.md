# Clean-room reimplementation of GoldenCheetah analytics

GoldenCheetah (GPL v2) 的四个分析模块（PMC/Banister、CP/W' 与 W'bal、BikeScore/TRIMP/TISS、Aerolab）是本项目的算法来源。若直接移植其代码，openrunner 将整体传染为 GPL v2 衍生作品；我们决定**仅依据已公开发表的论文与标准重新实现**（Skiba 2012 W'bal、Coggan NP/IF/TSS 公开定义、Morton 3 参数模型、Morton/Banister TRIMP 等），算法规格文档存于 `docs/goldencheetah-integration/`（按算法规格撰写，不含代码翻译），实现者不阅读 GC 源码。

## Considered Options

- 直接移植 GC C++ 代码 —— 省力且行为 100% 一致，但许可证传染，否决。

## Consequences

- openrunner 自身采用 **MIT** 许可证。
- 所有公式常数（xPower 25s 窗口、TRIMP 0.64/1.92/1.67、Skiba τ 公式、力竭阈值 500J、CS/D' 模型等）须标注论文出处。
- 与 GC 的数值一致性靠回归测试（金标准活动对照）保证，而非代码同源。
