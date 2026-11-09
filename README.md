# Openrunner

自托管的个人跑步训练分析仪表盘：running_page 的重构 + GoldenCheetah 分析模块的
clean-room 复现（PMC 训练负荷、CS/D' 临界速度模型、TRIMP/rTSS 训练压力指标）。

## 功能

五个页面，围绕「看数据 → 找问题 → 拿建议」的闭环：

| 页面 | 内容 |
|---|---|
| **活动**（`/`） | 年度/月/周跑量目标卡（含同期对比）、年度训练日历热力图（按运动类型配色、点击看当天活动）、个人最佳、省份足迹图、月历、活动列表（年份 + 距离筛选、100 条/页分页） |
| **趋势**（`/trends`） | PMC 体能/疲劳/状态曲线（LTS/STS/SB）+ 日负荷、Banister 表现预测（含 5K/10K/半马/全马成绩预测）、赛季起点配置 |
| **轨迹墙**（`/map`） | running_page 式轨迹缩略图墙：每条路线一张归一化小图，同路线聚类显示 ×N；选中轨迹在左侧小地图定位，支持**全屏**（全屏时列表与地图并排）|
| **汇总**（`/stats`） | 按年/月/周/日/Life 分组的周期卡片，每组 8 项指标 + 分布柱图 |
| **AI 分析**（`/ai-analysis`） | 选范围（单次/周/月/季度/年）+ 目标（5K/10K/半马/全马）→ 生成专业提示词，复制到任意 AI 获取训练建议 |

设置页（右上角齿轮）：Athlete 档案与头像、跑量目标、导入 FIT/GPX/TCX、**云平台同步**、心率区间、阈值配速、临界速度 CS/D'、手工录入、比赛成绩。

**术语说明**：页面里带虚线下划线的词（PMC、CTL/ATL/TSB、TSS/rTSS、NP、IF、VI、TRIMP、CP/W′/W′bal、CS/D′、VDOT、Riegel/α、Tanda、Pa:Hr、EF、Drift、LTHR、%HRR、80/20、S10–S13…）
可以悬停或点击，弹出中英双语的简略说明。术语集中在 `openrunner-ui/src/glossary.ts`（一份数据源，
口径以 [`docs/ALGORITHMS.md`](docs/ALGORITHMS.md) 为准），加一条术语就能全站生效。

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

### 云平台同步

同步接入是**可插拔**的：`openrunner-core/platform` 定义了 `Provider` 协议（校验凭证 /
拉活动列表 / 下载原始文件），零外部依赖、可整目录移植到其他 Go 项目。

| 平台 | 认证 | 备注 |
|---|---|---|
| **Garmin** | Personal API Key（国际站）/ OAuth1（中国站）| |
| **Intervals.icu** | 个人 API Key（HTTP Basic）| 接入成本最低，无需注册应用 |
| **Strava** | OAuth2（refresh_token 自动续期）| Strava 无原始 FIT 下载 → 用 streams 重建 TCX |
| **COROS** | 账号密码 | 非官方接口，失败时建议改用文件导入 |

未实现的平台（Keep / Nike / Joyrun / Onelap / Komoot / iGPSPORT / OPPO / TulipSport）
在 running_page 里全是逆向实现，需要持续跟版本、无法在无账号环境测试 —— 建议直接用文件导入。
新增平台只需三步（实现 Provider → 空白导入 → 加一条 UI 元信息），详见 [`docs/platforms.md`](docs/platforms.md)。

### 数据约定

- **只收录跑步与骑行**：游泳/力量/球类在导入时被过滤（会污染 PMC、个人最佳、配速统计）。
- 单位与语言：公制/英制一键切换，中文/英文界面。
- 单条活动详情含：配速/心率/步频/步幅/总步数、区间分布、D'bal 曲线、各负荷指标（带悬浮说明）。
- 轨迹墙的 `×N` 表示同一条路线跑了 N 次（起点终点相差 <550m 且距离差 <2km 时聚类合并）。
- **AI 分析页不调用任何外部 AI** —— 只把训练数据整理成提示词，由用户自行复制到对话式 AI。

## 仓库结构（ADR-0002）

| 仓库 | 内容 |
|---|---|
| `openrunner`（本仓库） | Go server：HTTP API、JWT 鉴权、GORM（SQLite 默认 / MySQL 兼容）、单二进制打包 |
| `openrunner-core`（同级独立仓库） | 纯 Go 领域库：分析算法 + 平台同步客户端（零 Web/DB 依赖） |
| `openrunner-ui`（同级独立目录） | React 19 + Vite + TS + Tailwind + ECharts 前端 |

## 快速开始

### 前置条件

| 需要 | 版本 | 说明 |
|---|---|---|
| Go | 1.26+ | 编译后端 |
| Node.js | 20+ | 编译前端（仅首次或 UI 改动后需要）|
| 仓库布局 | 三个目录同级 | `openrunner`、`openrunner-core`、`openrunner-ui` |

`openrunner` 通过 `replace` 引用 `../openrunner-core`，通过 `make ui-dist` 从 `../openrunner-ui`
取前端产物，所以三者必须放在同一父目录下。

### 运行（本地）

```bash
make dev        # 首次运行：构建前端 + 后端，然后启动（推荐）
```

只改后端时更快：

```bash
make build      # 只编译 Go 二进制 -> dist/openrunner
make run        # 启动（要求之前已 make ui-dist 过）
```

只改前端时：

```bash
make ui-dist    # 重新构建前端并复制进 server/web/dist
make build && pkill -f dist/openrunner && make run
```

启动后打开 <http://localhost:8080>（完整首次使用流程见文末）。

`make help` 列出全部命令。缺前端产物时 `make run` 会直接提示而不是跑出空白页。

#### 命令速查

| 命令 | 作用 |
|---|---|
| `make dev` | 构建前端 + 后端并启动（首次运行推荐）|
| `make ui-dist` | 只构建前端 → 复制进 `server/web/dist`（Go 的 embed 目录）|
| `make build` | 只编译 Go 单二进制 → `dist/openrunner` |
| `make run` | 启动（依赖 `check-ui`，缺前端产物会先报错而不是白屏）|
| `make test` | `openrunner-core` + `server` 全部测试与 `go vet` |
| `make e2e` | 前端冒烟测试（Playwright，见下）|
| `make vet` | 只跑 `go vet` |
| `make check-ui` | 自检嵌入的前端产物是否存在 |
| `make clean` | 删除 `dist/` 与 `data/` |
| `make docker-build` | 构建镜像（先经代理预拉基础镜像）|
| `make docker-up` / `docker-down` | compose 起 / 停（停服保留数据卷）|
| `make docker-ps` / `docker-logs` | 容器状态与健康检查 / 跟踪日志 |
| `make docker-rm` | 删除本地镜像（不动数据）|
| `make docker-clean CONFIRM=1` | 停止并**删除数据卷**（不可恢复，需显式确认）|

数据目录：`make run` / `make dev` 用 `./data`；直接运行二进制默认 `~/.openrunner`（`--data` 可覆盖）。

### 运行（Docker）

Makefile 已封装，推荐：

```bash
make docker-build   # 构建镜像（先经代理预拉基础镜像，见下）
make docker-up      # compose 起服务（含 TZ / 同步间隔默认值）
make docker-ps      # 容器状态 + 健康检查
make docker-logs    # 跟踪日志
make docker-down    # 停止（保留数据卷）
make docker-rm      # 删除本地镜像（不动数据）
make docker-clean CONFIRM=1   # 停止并删除数据卷（不可恢复，需显式确认）
```

变量可覆盖：`make docker-build REGISTRY=docker.io/library TAG=openrunner:v0.1.1`。
`make docker-up` 与 `make docker-build` 共用 `openrunner:latest` 这一个镜像。

等价的裸命令：

```bash
./scripts/docker-build.sh        # 或直接 docker build（需自己保证基础镜像已在本地）
docker run --rm -p 8080:8080 -v openrunner-data:/data -e TZ=Asia/Shanghai openrunner:latest
```

镜像约 29MB（alpine + 静态单二进制，前端已 embed）。数据在卷 `openrunner-data` 的 `/data`。

**首次启动**：打开 <http://localhost:8080> 会进入「首次设置」页创建管理员账号。
> 没有 `OPENRUNNER_ADMIN_PASSWORD` 这个环境变量 —— 管理员只能通过首次设置页创建。

**必须设 `TZ`**（如 `Asia/Shanghai`）：日粒度统计按本地时区切日，容器默认 UTC 会让「今天」错位 8 小时。镜像内已装 `tzdata`。

**国内网络**：连不上 Docker Hub 时，用 `REGISTRY=docker.1ms.run/library` 换基础镜像代理、
`GOPROXY=https://goproxy.cn,direct` 换 Go 模块代理（`scripts/docker-build.sh` 已默认如此）。
直连 Docker Hub 的环境用 `REGISTRY=docker.io/library`。

## 配置与部署

### 前端冒烟测试

```bash
make e2e
```

用 Playwright 打开**生产形态**的产物（Go 单二进制内嵌前端），断言五张页面都渲染出来、
导航完整、无 JS 错误、无 5xx、静态资源不 404。8 个用例约 5–9 秒。

- **隔离**：自起自停一个实例，独立端口 `18080` + 临时数据目录，不碰你在 `:8080` 跑着的服务，也不读写 `~/.openrunner`。
- **默认复用本机 Chrome**（免下载浏览器内核）；要用 Playwright 自带 Chromium：`E2E_CHANNEL=bundled npx playwright test`。
- **只改 UI 时走 Vite 热更新**：另开 `make run` + `npm run dev`，然后
  `E2E_BASE_URL=http://localhost:5173 npx playwright test`。
- **失败留 trace**：`npx playwright show-trace test-results/*/trace.zip` 可逐步倒带 DOM / 网络 / 控制台。

> 有效性经过变异验证：把 `index.html` 改成引用一个不存在的 JS 复现白屏后，8 个用例全部失败
> （报 `#root` 子元素数为 0）。不是空跑。

### 配置项

**启动参数**（二进制）：

| 参数 | 默认 | 说明 |
|---|---|---|
| `--addr` | `:8080` | 监听地址 |
| `--data` | `~/.openrunner` | 数据目录（SQLite 库、头像等；`make run` 传的是 `./data`）|

**环境变量**（全部，已对照代码核实）：

| 变量 | 默认 | 说明 |
|---|---|---|
| `OPENRUNNER_DB_DSN` | 未设 | **一旦非空即改用 MySQL**；保持 SQLite 必须让它不存在，不能填 `sqlite:` 前缀的值 |
| `OPENRUNNER_SYNC_INTERVAL` | `6h` | 云同步尝试间隔，Go duration 格式（`30m` / `24h`）|
| `TZ` | 容器内 `Asia/Shanghai` | 日粒度统计按本地时区切日；容器不设则按 UTC，「今天」错位 8 小时 |

> `OPENRUNNER_TEST_MYSQL_DSN` 仅用于 MySQL 集成测试，部署时无需关心。
> **没有** `OPENRUNNER_ADMIN_PASSWORD` —— 管理员只能通过首次设置页创建。

### 部署要点

- **备份**：SQLite 模式下备份数据目录里的 `openrunner.db` 即可（Docker 是卷 `openrunner-data` 的 `/data`）。
  停服后单文件复制；`make docker-clean` 会连卷一起删，慎用。
- **局域网访问**：直接 `http://<服务器IP>:8080`，无需额外配置（地图与字体都用系统栈/在线瓦片，无本地资源限制）。
- **公网暴露**：务必置于反向代理之后并启用 HTTPS（Caddy / nginx / Traefik 皆可），
  本服务自身只监听 HTTP，不带 TLS。
- **升级**：`make docker-build && make docker-down && make docker-up`（数据卷不动）。
  0.1 之前的 schema 变动见下「升级说明」。


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

### 首次使用流程

打开页面 → 首次设置管理员账号密码 → 创建 Athlete 档案（设置页）→ 导入或同步活动 → 配置心率区间 / 阈值配速 / CS-D' → 首页看概览、趋势页看负荷、AI 分析页拿建议。

## 文档

- 词汇表 `CONTEXT.md`；架构决策 `docs/adr/`；算法规格 `docs/goldencheetah-integration/`；平台接入 `docs/platforms.md`
- 算法与术语手册（指标含义、公式、论文出处）：`docs/ALGORITHMS.md`
- 工程日志与踩坑记录：`docs/DEVLOG.md`
- 金标准对照纪律（怎么证明公式对）：`docs/agents/golden-checks.md`
- 当前迭代规格与票：`.scratch/openrunner-v1/`（v1 十六张票）、`.scratch/gc-completion/`（GC 能力补全）

## 许可证

MIT
