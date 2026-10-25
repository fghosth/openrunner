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

### 数据库选择

默认 SQLite（单文件、零配置，随数据目录）。切换到 MySQL：

```
OPENRUNNER_DB_DSN='user:pass@tcp(host:3306)/openrunner?parseTime=true&charset=utf8mb4' \
  ./dist/openrunner --addr :8080
```

Docker compose 同理，在 `docker/docker-compose.yml` 的 environment 里设置该变量（文件内有注释示例）。

### 升级说明（0.1 之前）

数据库 schema 在 0.1 之前仍可能变动，且**没有自动迁移**（`AutoMigrate` 只建新表/加列，不改列类型）。
从更早的开发快照升级时请删除旧库重建（SQLite：删 `openrunner.db`；MySQL：`DROP DATABASE` 后重建），
或手动执行对应 `ALTER`（例如 0.1 前 `activity_streams.data` 由 `BLOB` 改为 `LONGBLOB`）。

### 首启

打开页面 → 首次设置管理员账号密码 → 创建 Athlete 档案 → 导入 FIT/GPX/TCX（支持多选批量）→ 配置心率区间/阈值配速/CS-D' → 看趋势与地图。

## 文档

- 词汇表 `CONTEXT.md`；架构决策 `docs/adr/`；算法规格 `docs/goldencheetah-integration/`
- 当前迭代规格与票：`.scratch/openrunner-v1/`

## 许可证

MIT
