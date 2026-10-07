# Contributor and Agent Instructions

[English](#english) · [简体中文](#简体中文)

## English

- Keep replies concise. Report actual changes, verification results, and remaining work.
- Define behavior and acceptance criteria before implementing a feature. Prove the cause before fixing a defect.
- Keep one authority for each fact. Do not add speculative fallbacks, compatibility paths, or duplicated state.
- This repository owns the standalone Go relay, its tunnel protocol, admin UI, and Docker deployment. Desktop business logic and canonical session data belong to pudding-core.
- Pudding browser pages and assets belong to the desktop installation. Forward every `/d/{desktopID}/*` request through its authenticated tunnel; never bundle or separately deploy the Pudding UI in Relay. Only Relay admin is hosted here.
- Preserve REST and session-scoped SSE at the business boundary. WebSocket carries the reverse tunnel; it does not replace business APIs.
- Keep the daemon startup token on the desktop. Do not log credentials, pairing codes, cookies, request bodies, or conversation content.
- Relay registration authentication, tunnel routing, and admin are implemented here. Phone pairing and route authorization belong to the desktop gateway; do not bypass them or expose a general proxy.
- Keep README.md and README.zh-CN.md equivalent. Maintain English and Simplified Chinese for user-facing documentation and future admin UI; keep protocol identifiers stable.
- Run make check, make build, and git diff --check for Go changes. For deployment changes, also validate Compose and build and smoke-test the container.
- Preserve unrelated work and remove temporary files. Do not claim unrun tests or unfinished features are complete.

## 简体中文

- 回答简短，说明实际改动、验证结果及未完成事项。
- 新功能先明确行为和验收条件；修复问题先证明根因。
- 每个业务事实只有一个权威来源，不添加无证据的 fallback、兼容路径或重复状态。
- 本仓负责独立 Go 中继、隧道协议、admin 和 Docker 部署；桌面业务和 canonical session 数据属于 pudding-core。
- Pudding 浏览器页面与资源属于桌面安装包。所有 `/d/{desktopID}/*` 请求经已鉴权隧道透传，不在 Relay 打包或独立部署 Pudding 界面；本仓只提供 Relay 自身的管理页。
- 业务边界保留 REST 与 session-scoped SSE；WebSocket 仅承载反向隧道，不替代业务 API。
- daemon 启动 token 留在电脑，不记录凭据、配对码、cookie、请求正文或会话内容。
- 本仓已实现 relay 登记鉴权、隧道路由及 admin。手机配对与路由授权属于桌面网关，不得绕过它们或开放通用代理。
- README.md 与 README.zh-CN.md 保持内容一致。用户文档和后续 admin 界面支持英文及简体中文，协议标识保持稳定。
- Go 改动运行 make check、make build 和 git diff --check；部署改动同时验证 Compose、构建镜像并执行容器冒烟。
- 保留无关改动，删除临时文件，不把未执行的检查或未实现的功能写成已完成。
