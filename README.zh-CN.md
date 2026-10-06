# pudding-relay

[English](README.md) · 简体中文

独立、可自行部署的 Go 中继，让手机浏览器在局域网外访问 Pudding 桌面端。采用 Apache-2.0 许可证，无 Cloudflare 运行时依赖。

服务已实现受鉴权的反向隧道、有界 REST/SSE 转发、持久化桌面凭据摘要，以及轻量中英双语管理页。手机访问需要兼容的 Pudding 桌面网关和共享手机 Web 构建。Docker 发行镜像包含匹配的浏览器资源，用户无需手动复制。Go 服务及其开发镜像仍可独立构建，无需访问私有桌面仓库。

## 接入流程

1. 按下方一键安装部署 relay，准备可访问的外部 HTTPS 地址。安装器会生成管理员密钥并启动含浏览器资源的镜像。
2. 在 Pudding **设置 → 远程访问**复制桌面 ID。打开自己 relay 的 `/admin`，登记此 ID，复制仅显示一次的桌面接入凭据。
3. 在 Pudding 中继设置填写 relay HTTPS origin 和接入凭据，保存并等待“已连接”。
4. 在电脑点击“生成授权二维码”，手机扫码后点“连接”。无需输入设备名或再次在电脑批准；授权码五分钟有效，只能使用一次。

电脑显示已配对设备和配对时间。“取消配对”立即撤销浏览器访问，不取消已经接受的任务。relay 管理页撤销桌面接入凭据则会断开该电脑的整条隧道。

## 连接方式与边界

Pudding 支持两个可独立启用、同时使用的入口：

| 方式 | 路径 |
| --- | --- |
| 局域网直连 / LAN Direct | 手机 → 桌面 HTTP 网关 → loopback daemon；不经过本中继 |
| 中继连接 / Relay | 手机 → 公网 HTTPS 中继 → 经桌面主动建立的 WSS 隧道 → 桌面网关 → loopback daemon |

局域网直连由 Pudding 桌面端提供。两种入口复用手机 Web、桌面授权码、路由授权和业务处理。daemon 仅监听 loopback，启动 token 留在电脑。用户明确打开对应入口，首版不自动发现或切换。直连与中继分别建立浏览器登录，共用桌面授权模型。局域网在信任的本地网络使用免证书 HTTP；中继继续要求公网 HTTPS／WSS。局域网 IP 改变后需打开新地址并重新配对。

业务请求保留 REST，会话事件保留支持 `Last-Event-ID` 续传的 SSE。会话、任务、审批和文件留在电脑。relay 仅持久化桌面 ID、名称、创建时间及 SHA-256 凭据摘要，不持久化会话数据，不记录 token、cookie、请求正文或消息内容。HTTPS/WSS 保护每段连接，不代表跨中继端到端加密，用户需要信任部署者。仅运行一个 relay 实例，独占登记文件。

手机首版覆盖会话、流式结果、附件、取消、用户补答和 Pudding 审批；完整远程桌面控制、系统原生授权、语音与离线执行不在范围内。Pudding 须保持运行，入口须可达。直连不依赖中继可用性，模型和工具仍可能需要外网。

## 一键安装

公开的 [Docker Hub 镜像](https://hub.docker.com/r/teatak/pudding-relay)已包含浏览器界面，支持 Linux amd64／arm64。

先安装 Docker Engine 和 Docker Compose v2，然后在服务器运行：

```sh
curl -fsSL https://raw.githubusercontent.com/teatak/pudding-relay/main/install.sh | sh
```

首次运行会询问公网 HTTPS 地址，例如 `https://relay.example.com`。Relay 自身提供 HTTP 后端，外部 HTTPS 由现有反向代理处理。安装成功会显示管理地址和管理员密钥文件位置，密钥内容不会输出到日志。镜像包含共享浏览器界面，支持 Linux amd64／arm64。

非交互安装可以直接传入参数：

```sh
curl -fsSL https://raw.githubusercontent.com/teatak/pudding-relay/main/install.sh \
  | env PUBLIC_URL=https://relay.example.com INSTALL_DIR=/opt/pudding-relay PORT=8080 sh
```

默认安装到当前目录下的 `pudding-relay/`，会生成 `.env`、`compose.yaml`、`makefile` 和 `secrets/admin-secret`。目录需对当前用户可写；`/opt` 等系统目录需相应权限。登记数据使用 Compose 命名卷 `relay_data`，不会放进镜像。重复运行保留密钥、配置和登记数据；显式传入的参数更新对应配置。`.env` 按数据读取，不作为 shell 脚本执行。

| 参数 | 默认值／含义 |
| --- | --- |
| `PUBLIC_URL` | 首次询问；外部 HTTPS origin，不含路径、查询或片段 |
| `INSTALL_DIR` | `$PWD/pudding-relay`；以后重复安装使用同一目录 |
| `IMAGE` | `teatak/pudding-relay:latest`；可改用固定 tag／digest |
| `PORT` | 宿主机 HTTP 端口，默认 `8080` |
| `BIND_ADDRESS` | 默认 `127.0.0.1`；仅本机访问，按需要改为可达的内网 IP |
| `NETWORK` | 可选，加入已存在的 Docker 网络；留空使用 Compose 自身网络 |

以下快捷命令需要 `make`；也可以在同目录直接使用对应的 `docker compose` 命令。

在安装目录执行：

```sh
make upgrade   # 拉取新镜像并等待服务就绪，保留数据
make restart   # 重启
make stop      # 停止服务，保留数据卷
make start     # 启动或应用 .env 修改
make logs      # 查看日志
make status    # 查看状态
```

安装器不配置域名、TLS 或系统 Docker 服务。外部代理须支持 WebSocket 和不缓冲的 SSE。停止时不要添加 `--volumes`，除非有意删除登记数据。桌面远程功能尚未正式发布，需使用包含此功能的兼容桌面构建。

Go 中继源码遵循 Apache-2.0；镜像中已编译的 Pudding 浏览器资源遵循其 Pudding Desktop 许可，相关许可和第三方声明一并附带，不公开桌面私有源码。

## 本地开发

需要 Go 1.26+ 和 make。将管理员密钥生成到私有文件，不放入命令行参数：

```sh
mkdir -p secrets
openssl rand -hex 32 > secrets/admin-secret
chmod 600 secrets/admin-secret
PUDDING_RELAY_ADMIN_SECRET_FILE="$PWD/secrets/admin-secret" \
PUDDING_RELAY_PUBLIC_URL=http://127.0.0.1:8080 \
go run ./cmd/pudding-relay --allow-insecure-loopback
```

HTTP 仅允许显式启用的 loopback 测试入口。公网地址必须为 HTTPS origin，不含路径、查询、fragment 或用户信息。默认监听 `127.0.0.1:8080`，由 TLS 反向代理处理公网 HTTPS/WSS。`--public-url` 或 `PUDDING_RELAY_PUBLIC_URL` 定义可信公网 origin，传入的 forwarded headers 无法改变它。

| 参数 | 含义 |
| --- | --- |
| `--listen` | HTTP 监听，默认 `127.0.0.1:8080` |
| `--public-url` | 公网 HTTPS origin；环境变量 `PUDDING_RELAY_PUBLIC_URL` |
| `--data-file` | 摘要登记文件，默认 `data/registrations.json` |
| `--assets-dir` | 共享手机构建目录；环境变量 `PUDDING_RELAY_ASSETS_DIR` |
| `--allow-insecure-loopback` | 显式允许 HTTP loopback 测试 |
| `PUDDING_RELAY_ADMIN_SECRET_FILE` | 必需的管理员密钥文件，去除首尾空白后至少 32 字节 |

`make build` 输出 `bin/pudding-relay` 并嵌入 Git 提交，可用 `VERSION`、`COMMIT` 覆盖构建信息。`--version` 无需服务配置即可显示版本。SIGINT/SIGTERM 关闭隧道、唤醒活动流并关闭 HTTP。登记文件原子替换；POSIX 系统文件权限为 `0600`，新建目录为 `0700`。Windows 使用数据目录的 ACL 权限。安全备份该文件；遗失凭据需撤销后重建。

## Docker 与公网 HTTPS

需要 Docker Engine 和 Compose。按上方生成 `secrets/admin-secret`。Docker bind-mounted secret 保留宿主文件权限；让非 root 容器可读文件，同时保持宿主目录私有：

```sh
chmod 700 secrets
chmod 444 secrets/admin-secret
```

设置实际公网 origin：

```sh
PUDDING_RELAY_PUBLIC_URL=https://relay.example.com docker compose up --build --detach --wait
curl --fail http://127.0.0.1:8080/healthz
PUDDING_RELAY_PUBLIC_URL=https://relay.example.com docker compose down
```

此源码 Compose 构建 `server` 开发 target，仅映射宿主机 loopback，将管理员密钥挂载为文件，在 `relay_data` 卷持久化摘要。镜像使用非 root 用户、只读文件系统并移除 capabilities。用户安装采用上方含浏览器资源的 Docker Hub 发行镜像。设置 `PUDDING_RELAY_PORT=18080` 可更改宿主机端口。除非有意删除全部登记，不使用 `down --volumes`。

在同一宿主机配置反向代理，例如 Caddy：

```caddyfile
relay.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

代理须支持 WebSocket upgrade 和不缓冲的 SSE，允许适合部署的附件大小，且不记录凭据、cookie、正文内容。不要将 relay 内部 HTTP 监听直接暴露到公网。真实手机需要受信任的 HTTPS。密钥文件不得提交到版本库；轮换时替换文件并重启 relay。桌面凭据通过 admin 独立撤销。

本功能尚未发布，请使用兼容的桌面构建。桌面包内含匹配版本的手机资源：macOS 路径为 `Pudding.app/Contents/Resources/app/web/dist/remote`，Windows 为 `<安装目录>/resources/app/web/dist/remote`。将目录内容复制到 `mobile-dist`，即可在无需私有源码的情况下部署；维护者也可从匹配的桌面源码构建 `web/dist/remote`。发行 target 会包含这些资源；本节仅供源码开发和自行构建。

安装手机资源时，只读挂载共享构建，并将 `PUDDING_RELAY_ASSETS_DIR` 设为容器路径，例如覆盖配置：

```yaml
services:
  relay:
    environment:
      PUDDING_RELAY_ASSETS_DIR: /assets
    volumes:
      - ./mobile-dist:/assets:ro
```

共享 HTML 使用 `<base href="__PUDDING_REMOTE_BASE__" />`；relay 将标记替换为 `/d/{desktopID}/`，CSP 允许同源 base。`/pair`、`/s/{sessionID}` 深链接返回该 HTML，使相对资源定位到桌面基路径。桌面离线时仍可加载资源，业务请求明确返回 `503`。缺少 `index.html` 时显示安装提示，不提供替代手机客户端。

## Admin 与接口

打开 `/admin`，选择 English 或简体中文，输入管理员密钥。密钥仅在页面内存中，不写浏览器存储。从 Pudding 远程访问设置复制已有桌面 ID，登记后将仅显示一次的凭据填写到桌面中继设置。不要另造中继桌面 ID。撤销会删除摘要、断开隧道并拒绝后续握手；撤销后重新登记相同 ID 会生成新凭据。

Admin API 要求 `Authorization: Bearer <管理员密钥>`；若传入 `Origin`，必须等于配置的公网 origin。响应均为 `Cache-Control: no-store`。

| 接口 | 契约 |
| --- | --- |
| `GET /healthz` | `{"status":"ok"}`，仅表示进程健康，不代表隧道就绪；支持 HEAD |
| `GET /version` | 构建 `version`、`commit`；支持 HEAD |
| `GET /admin/api/desktops` | `{"desktops":[{"desktopID":"…","label":"…","createdAt":"RFC3339","online":true}]}`，无凭据或摘要 |
| `POST /admin/api/desktops` | JSON `{"desktopID":"existing-core-id","label":"My desktop"}` → 201，含 `desktopID`、`label`、`createdAt`、一次性 `token`；重复 ID → 409 |
| `DELETE /admin/api/desktops/{desktopID}` | 204，持久化撤销并关闭活动隧道 |
| `GET /tunnel` | WebSocket v1，首帧鉴权 |
| `/d/{desktopID}/api/*`、`/d/{desktopID}/remote/*` | 通过已登记桌面网关转发 HTTP；离线 → 503 |
| `GET /d/{desktopID}/…` | 已安装手机资源；未知桌面 → 404 |

手机配对、登录及路由授权由桌面网关负责。relay 无法绕过网关，也不能代理任意目标。转发 Cookie、`Origin`、`Last-Event-ID`；删除 authorization、host、hop-by-hop、forwarded 和传入的 `X-Pudding-*` headers。relay 自行写入 `X-Pudding-Remote-Origin=<配置 origin>`、`X-Pudding-Remote-Mode=relay`。

## 隧道协议 v1

桌面向 `/tunnel` 主动建立 WSS，子协议为 `pudding-relay.v1`。五秒内发送文本 JSON `{"type":"hello","protocol":1,"desktopID":"…","token":"…"}`，成功后返回 `{"type":"hello","protocol":1,"desktopID":"…"}`。token 不放入 URL。每个桌面仅有一个活动隧道。relay 每 30 秒发送标准 WebSocket Ping，10 秒内未收到 Pong 则关闭隧道并唤醒活动 HTTP 流；原生浏览器与 Node WebSocket 自动回复 Pong。版本不匹配或凭据无效时关闭连接。

其余每帧均含字符串 `id`。转发的 `path` 保留 URL 编码，包括 canonical 补答请求 ID 中的 `%3A`；编码分隔符、双重编码、NUL 和目录穿越会被拒绝：

| 方向 | 帧 |
| --- | --- |
| Relay → 桌面 | `request` 含 `method`、网关相对 `path`（含 query）、`headers`；`request_data` 含 base64 `data`；`request_end` |
| 桌面 → Relay | `response` 含 `status`、`headers`；`response_data` 含 base64 `data`；`response_end` |
| 双向 | `cancel`；`ack` 含 `direction: "request"` 或 `"response"` |

每块解码数据最大 32,768 字节，JSON 帧最大 65,536 字节。发送方逐块等待 ACK 后才发送下一块或流结束。桌面消费请求块后 ACK；relay 写入并 flush HTTP 响应后 ACK。每桌面最多 64 个活动流，每方向最多一块未确认数据，不维护会话缓冲或重放存储。取消、撤销、断线和退出唤醒等待中的流。响应状态和 headers 在数据前仅发送一次。隧道承载 REST/SSE，不改变业务协议。

## 构建发行镜像

维护者提供已编译的共享浏览器目录和对应许可目录，二者不提交到本仓：

```sh
WEB_ASSETS_DIR=/path/to/pudding/web/dist/remote \
WEB_LEGAL_DIR=/path/to/pudding/dist/legal \
make docker-build
```

`make docker-publish` 使用同样的输入发布 Linux amd64／arm64 镜像；默认镜像为 `teatak/pudding-relay:latest`，可通过 `IMAGE`、`VERSION` 覆盖。需要当前 Docker 用户有目标仓库的推送权限。`scripts/build-image.sh` 校验 Pudding 的 base 标记和许可，将编译资源通过独立构建 context 放入镜像，不复制私有源码或 source map。

源码 Go 服务镜像可用 `docker build --target server .` 独立构建；完整发行 target 需要 `browser` context。CI 的资源 fixture 只用于安装及持久化验收，不作为发行资源发布。

## 验证

```sh
make fmt
make check
make build
make test-install
git diff --check
```

`make check` 检查格式、运行 vet 和 race detector 测试。覆盖摘要持久化和重启、文件权限、并发登记、管理员鉴权、撤销断线、协议和公网 origin 校验、有界帧和流、上传、SSE、可信 headers、取消、退出以及手机深链接。CI 还构建并冒烟测试容器。桌面和真实手机联调需要兼容网关及已安装手机构建。

## 许可证

[Apache License 2.0](LICENSE)。
