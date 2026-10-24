# Openrunner

自托管的个人跑步训练分析仪表盘：running_page 的重构 + GoldenCheetah 分析模块的
clean-room 复现（PMC 训练负荷、CS/D' 临界速度模型、TRIMP/rTSS 训练压力指标）。

## 仓库结构（ADR-0002）

| 仓库 | 内容 |
|---|---|
| `openrunner`（本仓库） | Go server：HTTP API、JWT 鉴权、GORM（SQLite 默认 / MySQL 兼容）、单二进制打包 |
| `openrunner-core`（同级独立仓库） | 纯 Go 领域库：分析算法 + 平台同步客户端（零 Web/DB 依赖） |
| `openrunner-ui`（同级独立目录） | React 19 + Vite + TS + Tailwind + ECharts 前端 |

## 快速开始

```
make ui-dist   # 构建 UI 并复制进 server 嵌入目录（需要同级存在 openrunner-ui）
make build     # 产出单二进制 dist/openrunner
make run       # 启动，默认 :8080，数据在 ~/.openrunner
```

Docker：

```
docker compose -f docker/docker-compose.yml up --build
```

打开 http://localhost:8080 —— 占位页会调用 `/api/health` 显示后端状态。

数据目录：`make run` 为 `./data`；直接运行二进制时默认 `~/.openrunner`（`--data` 可覆盖）。

## 文档

- 词汇表 `CONTEXT.md`；架构决策 `docs/adr/`；算法规格 `docs/goldencheetah-integration/`
- 当前迭代规格与票：`.scratch/openrunner-v1/`

## 许可证

MIT
