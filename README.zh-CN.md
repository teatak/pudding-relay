# pudding-relay

[English](README.md) · 简体中文

独立、可自行部署的 Go 中继，让手机浏览器在局域网外访问 Pudding 桌面端。采用 Apache-2.0 许可证，无 Cloudflare 运行时依赖。

服务已实现受鉴权的反向隧道、有界 REST/SSE 转发、持久化桌面凭据摘要，以及轻量中英双语管理页。手机访问需要兼容的 Pudding 桌面网关和共享手机 Web 构建。Docker 发行镜像包含匹配的浏览器资源，用户无需手动复制。Go 服务及其开发镜像仍可独立构建，无需访问私有桌面仓库。

## 接入流程

1. 按下方一键安装部署 HTTP 后端，无需填写域名或准备证书。安装器生成管理员密钥并启动含浏览器资源的镜像；公网 HTTPS 由已有反向代理处理。
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
mkdir -p pudding-relay
cd pudding-relay
curl -fsSL https://raw.githubusercontent.com/teatak/pudding-relay/main/install.sh | sh
```

安装不询问域名或 HTTPS 地址。Relay 提供 HTTP 后端，外部 HTTPS 由现有反向代理处理。安装成功显示本机 HTTP 管理地址、绑定地址和管理员密钥文件位置，密钥内容不会输出到日志。镜像包含共享浏览器界面，支持 Linux amd64／arm64。

非交互安装可以直接传入参数：

```sh
curl -fsSL https://raw.githubusercontent.com/teatak/pudding-relay/main/install.sh \
  | env INSTALL_DIR=/opt/pudding-relay PORT=9623 sh
```

默认安装到当前目录，不额外创建 `pudding-relay/` 子目录。会生成 `.env`、`compose.yaml`、`makefile` 和 `secrets/admin-secret`。目录需对当前用户可写；`/opt` 等系统目录需相应权限。登记数据使用 Compose 命名卷 `relay_data`，不会放进镜像。重复运行保留密钥、配置和登记数据；显式传入的参数更新对应配置。`.env` 按数据读取，不作为 shell 脚本执行。

| 参数 | 默认值／含义 |
| --- | --- |
| `INSTALL_DIR` | `$PWD`；以后重复安装使用同一目录 |
| `IMAGE` | `teatak/pudding-relay:latest`；可改用固定 tag／digest |
| `PORT` | 宿主机 HTTP 端口，默认 `9623` |
| `BIND_ADDRESS` | 默认 `0.0.0.0`；可从宿主机 IP 访问，亦可指定具体宿主机 IP |
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
make run
```

默认监听 `127.0.0.1:9623`，可直接在可信本机环境使用 HTTP 管理页。公网 HTTPS/WSS 由反向代理处理，Relay 不保存固定外部域名。每个管理 API 都要求管理员 Bearer 密钥。管理接口不使用 Cookie 鉴权、不开放 CORS，也不从 Host、Origin 或转发头推断管理员身份。普通反向代理配置即可支持非标准外部 HTTPS 端口，无需可信代理网段或自定义管理路径。

| 参数 | 含义 |
| --- | --- |
| `--listen` | HTTP 监听，默认 `127.0.0.1:9623` |
| `--data-file` | 摘要登记文件，默认 `data/registrations.json` |
| `--assets-dir` | 共享手机构建目录；环境变量 `PUDDING_RELAY_ASSETS_DIR` |
| `PUDDING_RELAY_ADMIN_SECRET_FILE` | 必需的管理员密钥文件，去除首尾空白后至少 32 字节 |

`make build` 输出 `bin/pudding-relay` 并嵌入 Git 提交，版本来自 `VERSION` 文件，`COMMIT` 可覆盖提交元数据。`--version` 无需服务配置即可显示版本。SIGINT/SIGTERM 关闭隧道、唤醒活动流并关闭 HTTP。登记文件原子替换；POSIX 系统文件权限为 `0600`，新建目录为 `0700`。Windows 使用数据目录的 ACL 权限。安全备份该文件；遗失凭据需撤销后重建。

## Docker 与公网 HTTPS

需要 Docker Engine 和 Compose。按上方生成 `secrets/admin-secret`。Docker bind-mounted secret 保留宿主文件权限；让非 root 容器可读文件，同时保持宿主目录私有：

```sh
chmod 700 secrets
chmod 444 secrets/admin-secret
```

启动源码 HTTP 后端：

```sh
docker compose up --build --detach --wait
curl --fail http://127.0.0.1:9623/healthz
docker compose down
```

此源码 Compose 构建 `server` 开发 target，仅映射宿主机 loopback，将管理员密钥挂载为文件，在 `relay_data` 卷持久化摘要。镜像使用非 root 用户、只读文件系统并移除 capabilities。用户安装采用上方含浏览器资源的 Docker Hub 发行镜像。设置 `PUDDING_RELAY_PORT=18080` 可更改宿主机端口。除非有意删除全部登记，不使用 `down --volumes`。

将反向代理指向 Relay HTTP 后端即可，无需填写可信代理网段或自定义 `/admin/api/` location。桌面与浏览器的配对仍验证已配置公网地址的精确 Origin。

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

打开 `/admin`，选择 English 或简体中文，输入管理员密钥。页面仅在内存中保留密钥，退出或关闭页面后清除。标准登录表单支持浏览器密码管理器；是否保存由你选择，并取决于浏览器设置。从 Pudding 远程访问设置复制已有桌面 ID，登记后将仅显示一次的凭据填写到桌面中继设置。不要另造中继桌面 ID。撤销会删除摘要、断开隧道并拒绝后续握手；撤销后重新登记相同 ID 会生成新凭据。

每次 Admin API 操作都要求 `Authorization: Bearer <管理员密钥>`；Cookie 不能用于管理员鉴权，不开放跨域预检。Host、Origin 或代理协议改写不会拒绝已正确认证的管理请求。更换代理域名无需重启或修改 Relay；Pudding 桌面端仍需更新连接地址，浏览器在新域名重新配对。响应均为 `Cache-Control: no-store`。

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

手机配对、登录及路由授权由桌面网关负责。relay 无法绕过网关，也不能代理任意目标。转发 Cookie、`Origin`、`Last-Event-ID`；删除 authorization、host、hop-by-hop、forwarded 和传入的 `X-Pudding-*` headers。relay 仅写入 `X-Pudding-Remote-Mode=relay`。浏览器 Origin 保留，由桌面网关按其已配置的 Relay 地址验证配对和业务权限。

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

`VERSION` 是版本号的唯一来源，初始版本为 `0.1.0`。`make docker-publish` 使用同样的输入发布 Linux amd64／arm64 镜像，同时生成 `latest` 和固定版本标签；可通过 `IMAGE` 指定镜像目标。需要当前 Docker 用户有目标仓库的推送权限。`scripts/build-image.sh` 校验 Pudding 的 base 标记和许可，将编译资源通过独立构建 context 放入镜像，不复制私有源码或 source map。

源码 Go 服务镜像可用 `docker build --target server .` 独立构建；完整发行 target 需要 `browser` context。CI 的资源 fixture 只用于安装及持久化验收，不作为发行资源发布。

## 版本与发版

`make build`、`make run` 和 Docker 构建均读取根目录 `VERSION`，`--version` 与 `/version` 显示相同正式版本。

```sh
make version-patch   # 0.1.0 -> 0.1.1，仅更新 VERSION
make version-minor   # 0.1.0 -> 0.2.0
make version-major   # 0.1.0 -> 1.0.0
make release-current # 发布当前尚未发版的 VERSION
make release         # 等同 release-patch
make release-patch
make release-minor
make release-major
```

发版前需要在 main 提交源码，并提供上方浏览器资源／许可目录及 Docker Hub 推送权限。脚本同步远端与 Git tags、执行测试、按需提交版本更新、推送双架构镜像，然后原子推送 main 与 `vX.Y.Z` Git tag。首次 patch 发版使用当前准备好的版本；当前版本已有 tag 时递增 patch。已发版版本拒绝重复发布，镜像失败时不创建 Git tag；修复后重试保留已准备的版本。

固定版本安装：

```sh
curl -fsSL https://raw.githubusercontent.com/teatak/pudding-relay/main/install.sh \
  | env IMAGE=teatak/pudding-relay:0.1.4 sh
```

`latest` 跟随新发行版；固定标签保持该版本，`make upgrade` 沿用安装时选择的镜像。

## 验证

```sh
make fmt
make check
make build
make test-install
git diff --check
```

`make check` 检查格式、运行 vet 和 race detector 测试。覆盖摘要持久化和重启、文件权限、并发登记、管理员鉴权、撤销断线、协议校验与关闭管理接口 CORS、有界帧和流、上传、SSE、可信 headers、取消、退出以及手机深链接。CI 还构建并冒烟测试容器。桌面和真实手机联调需要兼容网关及已安装手机构建。

## 许可证

[Apache License 2.0](LICENSE)。

## 从 0.1.0 升级

重新运行安装命令，让安装器移除已废弃的 `PUBLIC_URL` 和旧 Compose 环境项，并保留密钥与登记数据；只拉取镜像不会更新旧安装模板。从 0.1.3 起，安装器同时移除已废弃的 `TRUSTED_PROXIES`。镜像升级后，可删除此前为非标准 HTTPS 端口添加的自定义管理路径反代配置。`--public-url`、`--allow-insecure-loopback` 和 `--trusted-proxies` 已删除，不保留旧参数路径。

默认服务端口从 `0.1.2` 起统一为 `9623`（Go、容器、健康检查和全新安装）。已有安装的宿主机 `PORT` 会保留；重新运行安装器将 Compose 容器 target 更新为 `9623`。若已有安装使用旧版固定镜像标签，重新运行时显式传入 `IMAGE=teatak/pudding-relay:0.1.4`。
