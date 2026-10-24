# Refactor running_page: product inheritance, code rewrite

openrunner 是 running_page 的重构而非全新产品：**产品级继承**其平台清单（Strava/Garmin 国行国际/NRC/Keep/咕咚/JoyRun/Coros/Suunto/Komoot/intervals.icu/igpsport/Oppo 等 + FIT/GPX/TCX 文件导入）、地图轨迹页与年度统计；**代码级重写**——同步层全部用 Go 重写（含流数据扩展摄取，summary-only 活动产出 Estimated Metric 并显式标注），UI 按新架构重建。SVG 年度海报可选保留；TUI 砍除。

## Considered Options

- 保留 running_page 的 Python 同步脚本生态 —— 免重写 13+ 平台的凭证细节，但进程内引入 Python，单二进制交付出局，否决。
- fork running_page 仓库直接改造 —— 其静态站点架构（无后端）与新架构冲突，演进成本高于重写，否决。

## Consequences

- 各平台同步客户端按需分批实现：首发文件导入跑通分析，云同步逐平台移植。
- 地图沿用 running_page 的免 token 方案（MapCN 瓦片）。
- 无流数据的云同步活动按 Estimated Metric 降级，保证 PMC 日负荷全量覆盖。
