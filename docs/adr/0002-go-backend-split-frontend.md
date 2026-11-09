# Go backend, SQLite-default storage, split frontend

> **已被 ADR-0004 部分修订**：本文「领域库为独立 git 仓库」那一条已作废——
> `core/` 现在是本仓库内的独立 go module（模块边界不变，仓库边界取消）。
> 前后端分离、GORM/SQLite 默认、单二进制交付这些决定仍然有效。

openrunner 采用前后端分离：后端 Golang（API/鉴权/DB 编排，位于本仓库 `server/`），数据层 GORM，默认 SQLite、兼容 MySQL；前端独立目录 `openrunner-ui`（React 19 + Vite + TS + Tailwind + ECharts，UI 设计由 ui-ux-pro-max skill 辅助）；领域库为独立 git 仓库 `openrunner-core`（纯 Go module：分析算法 + 平台同步客户端，不依赖 Web 框架与 DB），由 server 引用。

## Considered Options

- TS 全栈（分析代码前后端共享）—— 被否：团队 Go 偏好，且单二进制交付需要后端原生编译。
- Python（scipy 拟合便利）—— 被否：多一层运行时，破坏单二进制部署。
- core 与 server 同仓子目录 —— 被否：用户选择独立仓库以强化模块边界。

## Consequences

- 单二进制交付（Go embed 前端静态资源）+ Docker compose 双部署形态。
- 拟合类算法（LM）在 Go 侧自实现或引入轻量库，不依赖 scipy。
- GORM 方言层兜底 SQLite/MySQL 双支持，复杂聚合查询用 raw SQL 补充。
