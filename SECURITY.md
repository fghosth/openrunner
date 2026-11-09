# 安全策略

openrunner 是**自托管**的个人训练数据服务，不对外提供多租户。安全边界因此很窄，
但下面几类问题仍然欢迎报告。

## 报告方式

优先开 [GitHub issue](https://github.com/fghosth/openrunner/issues)；
涉及可利用漏洞（鉴权绕过、SQL 注入、路径穿越）请先走 GitHub 的
**Report a vulnerability** 私聊通道，别直接公开贴细节。

## 凭据处理（本项目特有）

平台同步需要保存 Garmin / Strava / Intervals.icu / COROS 的凭据：

- 落库前用 **AES-256-GCM** 加密，密钥从 `secrets` 表的 `jwt_secret` 派生；
  明文不会写进日志、API 响应或导出文件。
- **轮换才是修复**：凭据一旦进过聊天记录、截图或 git 历史，就按已泄露处理。
  各平台入口与步骤见 [`docs/platforms.md`](docs/platforms.md) 的「凭据轮换」小节。
- 换掉 `secrets.jwt_secret` 会让**所有**已存凭据解不开（这是「轮换密钥即全量作废」的
  预期行为，不是 bug），需要重新录入。
- 明文凭据不应出现在仓库任何位置，包括测试夹具——早期夹具里用过真实 OAuth1 片段，
  已换成假值并在发布前从历史中抹除。

## 部署时的最低要求

| 项 | 要求 |
|---|---|
| `OPENRUNNER_JWT_SECRET` | 生产必须显式设置（不设则每次启动随机，重启即全员掉线） |
| 管理员密码 | 首次启动的「首次设置」页创建；没有 `OPENRUNNER_ADMIN_PASSWORD` 这个环境变量 |
| 暴露方式 | 建议放反向代理后面并启用 TLS；本服务自身不做 TLS |
| 数据库备份 | `scripts/backup.sh` 会做快照并**验证可恢复**，目标盘必须与仓库不同盘 |

## 不在范围内

- 多用户权限模型：一个实例 = 一个信任域，Athlete 只是数据分组，不是安全边界。
- 前端资源完整性（SRI）：静态资源由同一二进制内嵌，不引第三方 CDN。
