# openrunner-core

openrunner 的纯 Go 领域库：领域类型（Athlete / Activity / Stream / Zone / Season）、
分析引擎（TRIMP 家族、rTSS、跑步功率链、CS/D'、W'bal、PMC、Estimated Metric）与
平台同步客户端。

零 Web 框架、零数据库依赖（见 openrunner 仓库 ADR-0002）。所有分析算法仅依据
已公开发表的论文实现（clean-room，见 ADR-0001），公式常数须标注论文出处。

## 开发

```
go test ./...
go vet ./...
```

词汇表与架构决策见 openrunner 主仓库的 `CONTEXT.md` 与 `docs/adr/`。
