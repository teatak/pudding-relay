# pudding-relay

[English](README.md) · 简体中文

可自行部署的 Pudding 中继，让手机浏览器在局域网外访问 Pudding 桌面端。

**当前状态：仓库初始化。** 可运行服务目前仅提供健康检查和构建版本接口。桌面隧道、鉴权、配对、手机 Web 界面及 admin 管理均未实现；运行本仓库暂时不能远程访问 Pudding。

## 规划架构

手机通过 HTTPS 访问公网 relay。Pudding 桌面端主动建立到 relay 的 WSS 隧道，将已授权请求转发至本机 loopback daemon。

- 业务请求保留 REST，会话事件保留支持事件 ID 续传的 SSE。
- 会话、任务、审批与文件的事实源在电脑；relay 不持久化这些内容。
- daemon 启动 token 只留在电脑。
- 使用独立 Go 服务与 Docker 部署，不依赖 Cloudflare 运行时。
- 首版采用单实例 relay，提供轻量 admin 界面。
- 用户文档及后续 admin 界面支持英文和简体中文。

手机首版计划覆盖会话、流式结果、附件、取消任务、用户补答和 Pudding 审批。完整远程桌面控制、系统原生授权、语音和离线执行不在首版范围；电脑必须保持联网且 Pudding 正在运行。

HTTPS／WSS 保护每段网络连接，不代表跨 relay 的端到端加密，用户需要信任 relay 部署者。开放业务流量前必须先实现鉴权和授权。

## 本地运行

需要 Go 1.26 或更高版本，以及 make。

```sh
git clone https://github.com/teatak/pudding-relay.git
cd pudding-relay
make run
```

默认监听 `127.0.0.1:8080`。

```sh
curl --fail http://127.0.0.1:8080/healthz
curl --fail http://127.0.0.1:8080/version
```

| 接口 | 当前响应 |
| --- | --- |
| `GET /healthz` | `{"status":"ok"}`；仅表示进程健康，不表示中继隧道已就绪 |
| `GET /version` | 构建版本和提交；直接 `go run` 时默认为 `dev` 和 `unknown` |

两个接口均支持 HEAD。未知路由返回 404；对这两个路由使用不支持的方法返回 405。

构建并运行二进制：

```sh
make build
./bin/pudding-relay --version
./bin/pudding-relay --listen=127.0.0.1:8080
```

`make build` 会嵌入 Git 提交；需要时可显式设置 `VERSION` 和 `COMMIT`。SIGINT 和 SIGTERM 会触发优雅退出。

## Docker 运行

需要 Docker Engine 和 Docker Compose。

```sh
docker compose up --build --detach --wait
curl --fail http://127.0.0.1:8080/healthz
docker compose logs
docker compose down
```

Compose 在本地构建镜像，本次初始化不发布镜像到公共仓库。容器使用非 root 用户、只读文件系统、移除额外 capabilities，并带健康检查。容器内监听 8080，Compose 仅映射到宿主机 loopback 地址。

若宿主端口被占用：

```sh
PUDDING_RELAY_PORT=18080 docker compose up --build --detach --wait
curl --fail http://127.0.0.1:18080/healthz
PUDDING_RELAY_PORT=18080 docker compose down
```

当前服务没有持久化数据或数据卷。未来公开部署需要 HTTPS、桌面接入鉴权、手机配对、有界流式转发及中继存储。当前 Compose 用于初始化阶段的运行验证，不代表公网中继已经交付。

## 开发验证

```sh
make fmt
make check
make build
git diff --check
```

`make check` 检查格式、运行 go vet，并执行启用 race detector 的测试。CI 执行这些检查，同时构建 Docker 服务并验证容器运行。

| 路径 | 用途 |
| --- | --- |
| `cmd/pudding-relay/` | 进程入口、参数及退出处理 |
| `internal/httpserver/` | HTTP handler 和接口契约测试 |
| `Dockerfile`、`compose.yaml` | 容器构建与本地部署 |
| `.github/workflows/ci.yml` | Go 检查和 Docker 冒烟验证 |
| `AGENTS.md` | 贡献与实现边界 |

后续里程碑是受鉴权的反向隧道、桌面接入与配对、共享手机 Web 入口及轻量 admin。协议与产品行为随实现维护文档。打包共享浏览器资源前需确定其分发方式；构建开源 relay 不应要求访问私有 desktop 仓库。

## 许可证

[Apache License 2.0](LICENSE)。
