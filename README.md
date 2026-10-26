# Openrunner

自托管的个人跑步训练分析仪表盘：running_page 的重构 + GoldenCheetah 分析模块的
clean-room 复现（PMC 训练负荷、CS/D' 临界速度模型、TRIMP/rTSS 训练压力指标）。

## 功能

五个页面，围绕「看数据 → 找问题 → 拿建议」的闭环：

| 页面 | 内容 |
|---|---|
| **活动**（`/`） | 年度/月/周跑量目标卡（含同期对比）、年度训练日历热力图（按运动类型配色、点击看当天活动）、个人最佳、省份足迹图、月历、活动列表（年份 + 距离筛选、100 条/页分页） |
| **趋势**（`/trends`） | PMC 体能/疲劳/状态曲线（LTS/STS/SB）+ 日负荷、Banister 表现预测（含 5K/10K/半马/全马成绩预测）、赛季起点配置 |
| **轨迹墙**（`/map`） | running_page 式轨迹缩略图墙：每条路线一张归一化小图，同路线聚类显示 ×N，可切聚合分布视图 |
| **汇总**（`/stats`） | 按年/月/周/日/Life 分组的周期卡片，每组 8 项指标 + 分布柱图 |
| **AI 分析**（`/ai-analysis`） | 选范围（单次/周/月/季度/年）+ 目标（5K/10K/半马/全马）→ 生成专业提示词，复制到任意 AI 获取训练建议 |

设置页（右上角齿轮）：Athlete 档案与头像、跑量目标、导入 FIT/GPX/TCX、Garmin 云同步（国际站 API Key / 中国站 OAuth1）、心率区间、阈值配速、临界速度 CS/D'、手工录入、比赛成绩。

### 分析模型（clean-room 复现 GoldenCheetah，仅依据已发表论文）

| 模型 | 说明 |
|---|---|
| TRIMP / TRIMP(100) / TRIMP Zonal | Banister 训练冲量、归一化、Edwards 五区法 |
| NP / rTSS / 功率负荷 | 标准化功率（四次方均值）、跑步训练压力分、功率版负荷 |
| PMC（LTS 42d / STS 7d / SB / RR） | 体能-疲劳-状态，含赛季播种与计划/预期叠加 |
| CS / D' / D'bal | 临界速度模型（每活动最多贡献 1 个拟合点，D' 合理性校验） |
| CP / W' / W'bal / CP3 | 功率临界模型 + 三参数版（纯 Go Levenberg-Marquardt） |
| 个人最佳 | 活动**内部**最快分段（累计距离流，非整条活动匹配） |
| Banister 表现预测 + Riegel 投影 | 训练负荷 → 当前表现 → 5K/10K/半马/全马预测（个人化 α） |

### 数据约定

- **只收录跑步与骑行**：游泳/力量/球类在导入时被过滤（会污染 PMC、个人最佳、配速统计）。
- 单位与语言：公制/英制一键切换，中文/英文界面。
- 单条活动详情含：配速/心率/步频/步幅/总步数、区间分布、D'bal 曲线、各负荷指标（带悬浮说明）。

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

打开页面 → 首次设置管理员账号密码 → 创建 Athlete 档案（设置页）→ 导入或同步活动 → 配置心率区间 / 阈值配速 / CS-D' → 首页看概览、趋势页看负荷、AI 分析页拿建议。

## 文档

- 词汇表 `CONTEXT.md`；架构决策 `docs/adr/`；算法规格 `docs/goldencheetah-integration/`
- 工程日志与踩坑记录：`docs/DEVLOG.md`
- 当前迭代规格与票：`.scratch/openrunner-v1/`（v1 十六张票）、`.scratch/gc-completion/`（GC 能力补全）

## 许可证

MIT
