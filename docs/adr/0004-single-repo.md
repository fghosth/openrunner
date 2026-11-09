# 三仓合并为单仓（core/ 与 web/ 成为目录）

开发期是三个同级 git 仓库（`openrunner` / `openrunner-core` / `openrunner-ui`），
发布到 GitHub 时合并成**一个仓库**，目录布局 `server/` + `core/` + `web/`。
本 ADR 修订 ADR-0002 中「领域库为独立 git 仓库」那一条；前后端分离本身不变。

## Considered Options

- **三个仓库都发布** —— 最忠实 ADR-0002 的模块边界，但个人项目要维护三份 CI、
  三份 Docker 上下文、跨仓 PR 与版本对齐；`web/` 单独成仓没有任何收益。否决。
- **合并历史 vs 开新仓** —— 开新仓最省事，但 300 个提交是这个项目最值钱的部分
  （决策链、踩坑记录、变异检验的来路），DEVLOG 是摘要不是替代。选择用
  `git subtree add` 保留全部历史。
- **单 go module** —— `go test ./...` 一条命令覆盖全部，最省事；但 core
  「不依赖 Web 框架与 DB」这条边界就只剩口头约定。否决。

## Consequences

- **模块边界改由嵌套 go.mod 兜住**：`core/` 保留自己的 `go.mod`
  （`github.com/fghosth/openrunner/core`），依赖清单里没有 Web/DB 依赖，
  越界会立刻在 `core/go.mod` 上显形。根 `go.mod` 用
  `replace github.com/fghosth/openrunner/core => ./core` 指回本地。
- CI 从「两个 checkout + 跨仓占位」简化成单 checkout，job 拆成
  `reference / core / server / server-mysql / ui / e2e`。
- Docker 不再需要 `--build-context` 注入同级目录，构建上下文就是仓库根。
- 本地开发不再有「三个目录必须同级」的隐含要求，clone 下来就能 `make dev`。
- 历史里 `.scratch/`（个人训练数据的过程记录）与早期测试夹具中的真实凭据片段
  在首次 push 前用 `git filter-repo` 整体抹除——**没有 remote 的时候重写历史是免费的**，
  push 之后再洗就要 force-push，成本完全不同。
