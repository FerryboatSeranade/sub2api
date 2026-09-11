# Sub2API Codex OAuth 接口与抓包观察参考

## 1. 范围与结论

- 分析日期：2026-09-11；本地仓库：`/root/data/docker_data/sub2api-dev`。
- 分支：`dev/secondary-development-20260525`；HEAD：`9d6ca581b`。另包含工作区未提交的串行模型测试功能。
- 本文描述本地源码，不等于当前运行镜像的版本或配置；未调用真实账号接口、抓取生产流量或修改服务。
- `chatgpt.com/backend-api/...` 是源码使用的上游接口，不应当作 OpenAI 对第三方承诺稳定的公开 API 契约。
- 仅用于自有或获授权环境的协议诊断。请求一致性、身份字段一致性不等于规避风控；源码无法证明上游实际使用哪些风控信号。

核心区别：用户访问 Sub2API 时提交的是 Sub2API API Key；Sub2API 选择账号后，访问上游用的是该账号的 OAuth access token。两者不是同一个凭证。

```text
授权：管理员浏览器 -> Sub2API 生成授权会话
      浏览器 -> auth.openai.com 登录授权 -> 回调 code/state
      管理员浏览器 -> Sub2API 提交 code/state/session_id
      Sub2API -> auth.openai.com 换取令牌 -> 补全账号信息

调用：客户端 + Sub2API Key -> 入站鉴权/额度/调度 -> 选中账号
      -> 获取或刷新该账号 access token -> 请求转换
      -> chatgpt.com Codex HTTP/SSE 或 WebSocket
      -> 事件解析/错误处理/用量记录 -> 客户端
```

## 2. 管理端接口

以下路径前缀是 `/api/v1/admin/openai`，属于管理权限接口，不是供普通推理 Key 使用的入口。请求 JSON；返回遵循本项目管理 API 包装格式。

| 方法与相对路径 | 关键输入 | 作用与副作用 |
| --- | --- | --- |
| `POST /generate-auth-url` | 可选 `proxy_id`、`redirect_uri` | 生成授权 URL 和 `session_id`，在本进程保存 PKCE 会话 |
| `POST /exchange-code` | 必需 `session_id`、`code`、`state`；可选 `redirect_uri`、`proxy_id` | 校验会话与 state，换取令牌并补全信息；成功后删除会话；不是创建本地账号接口 |
| `POST /refresh-token` | `refresh_token` 或别名 `rt`；可选 `client_id`、`proxy_id` | 刷新传入凭证并返回令牌信息；不能假定它已更新某个本地账号 |
| `POST /accounts/:id/refresh` | 账号 ID | 从本地账号读凭证、刷新、合并保留其他凭证配置并写回账号 |
| `POST /create-from-oauth` | 授权会话字段及 `name`、`concurrency`、`priority`、`group_ids` 等 | 兑换授权码并创建本地账号；不要先兑换同一授权码再调用此入口 |
| `POST /create-from-codex-pat` | PAT 创建参数 | 独立的 Personal Access Token 接入方式，不是授权码/refresh token 流程 |
| `GET /accounts/:id/quota` | 账号 ID | 实时查询上游额度；还会通知自动兑换额度后台逻辑，不能简单认为绝无副作用 |
| `POST /accounts/:id/quota/refresh` | 账号 ID | 查询并更新本地额度快照，同样通知自动兑换逻辑；关注 `cache_persisted`，查询成功不等于缓存写入成功 |
| `POST /accounts/:id/reset-quota` | 账号 ID 和处理器定义的参数 | 兑换上游重置额度权益，会改变真实账号额度，不应用于无副作用抓包测试 |

其他相关入口位于 `/api/v1/admin/accounts`：

- `POST /:id/test`：真实连接/模型测试，会产生上游请求并可能消耗额度。
- `POST /:id/test-models`：本地新增串行测试；`GET/PUT /:id/test-models/preset` 读取/编辑模型列表。是否上线需另查镜像。
- `POST /:id/refresh`、`POST /:id/apply-oauth-credentials`：通用账号刷新或应用凭证，不能仅靠路径名称判断与 OpenAI 专用刷新完全相同。
- `POST /:id/set-privacy`：隐私设置操作，有上游写入副作用。
- `GET /:id/models`、`POST /:id/models/sync-upstream`：模型配置/同步相关。列表里出现模型不等于该账号已实际调用成功。

源码：[管理路由](/root/data/docker_data/sub2api-dev/backend/internal/server/routes/admin.go:447)、[OAuth 处理器](/root/data/docker_data/sub2api-dev/backend/internal/handler/admin/openai_oauth_handler.go:104)、[串行测试处理器](/root/data/docker_data/sub2api-dev/backend/internal/handler/admin/account_serial_test_handler.go)。

## 3. 授权码与刷新协议

### 3.1 浏览器授权

上游：`GET https://auth.openai.com/oauth/authorize`。

授权 URL 包括 `response_type=code`、`client_id`、`redirect_uri`、`scope`、`state`、`code_challenge`、`code_challenge_method=S256`。OpenAI 分支还设置 `id_token_add_organizations`、`codex_cli_simplified_flow`。

- 默认回调：`http://localhost:1455/auth/callback`。它是浏览器所在设备的 loopback 地址，不代表远端 Sub2API 容器监听此端口。
- scope：`openid profile email offline_access`。
- `client_id` 来自源码注册客户端常量；它不是客户端密钥，也不代表自研客户端获得了复用注册信息的授权。
- PKCE：随机 verifier，SHA-256 后用无 padding 的 Base64URL 生成 challenge。
- `session_id` 是本地授权会话索引；`state` 用来关联并校验授权回调；二者不能替代对方。
- 会话存于进程内存，TTL 为 30 分钟，定期清理。进程重启会丢失；多副本若不命中原进程也会找不到会话。

浏览器登录过程中的页面、Cookie、验证码等并非本项目完整实现的协议，不能从这份源码还原服务端全部登录行为。

### 3.2 换取令牌

上游：`POST https://auth.openai.com/oauth/token`。

重要：实际发送的是 `application/x-www-form-urlencoded`，不是 JSON；不要因为类型定义中有 JSON 标签就误判线上编码。

```text
grant_type=authorization_code
client_id=<registered-client-id>
code=<redacted>
redirect_uri=<same-flow-redirect-uri>
code_verifier=<redacted>
```

服务层先查会话，用 constant-time comparison 校验 state；再按会话/输入确定代理、回调和 client ID，调用上游。成功兑换后删除会话，再补全信息。

响应包含 `access_token`、`refresh_token`、`id_token`、`expires_in` 等。服务层计算 `expires_at`，并可能添加 email、account/user/org ID、plan、订阅到期时间、privacy mode 等字段。

**ID Token 注意事项：** 当前 `ParseIDToken` 只解码 payload 并校验过期时间，允许 120 秒时钟偏差，不验证 JWT 签名。不能将“能解析出 email/account ID”当成密码学意义上的身份验证通过。源码注释中的 JWKS 地址不代表当前代码已实际请求或使用 JWKS。

### 3.3 刷新令牌

仍然请求上述 `/oauth/token`，表单字段为：

```text
grant_type=refresh_token
refresh_token=<redacted>
client_id=<registered-client-id>
scope=openid profile email
```

- 刷新可能返回更新后的 refresh token；不能把刷新当作可随意重放的只读操作。
- 自动取令牌路径包含临近到期提前刷新、缓存和刷新锁；OpenAI 缓存键按本地账号 ID 组织为 `openai:account:<id>`。
- 自动刷新协调逻辑不意味着所有手动刷新入口都走同一把锁。研究并发时应区分 token provider、后台刷新与管理接口调用路径。
- 两套独立数据库/Redis 环境若导入相同上游凭证，不会因为各自有本地锁就自动实现跨环境互斥。
- `refresh_token_reused` 被代码视为需要重新授权的硬失败之一，不适合无限重试。
- 授权码兑换显式代理查询失败会返回错误；账号刷新服务存在代理查询失败后继续使用空代理 URL 的路径。实际是否直连还取决于客户端/插件配置，不能直接断定生产发生了出口变化。

源码：[OAuth 常量与会话](/root/data/docker_data/sub2api-dev/backend/internal/pkg/openai/oauth.go:19)、[令牌解析](/root/data/docker_data/sub2api-dev/backend/internal/pkg/openai/oauth.go:354)、[兑换与补全服务](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_oauth_service.go:133)、[上游表单请求](/root/data/docker_data/sub2api-dev/backend/internal/repository/openai_oauth_service.go:27)、[令牌提供器](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_token_provider.go)、[刷新协调](/root/data/docker_data/sub2api-dev/backend/internal/service/oauth_refresh_api.go)、[缓存键](/root/data/docker_data/sub2api-dev/backend/internal/service/token_cache_key.go)。

## 4. 授权/刷新附带请求和额度接口

以下主机为 `https://chatgpt.com`，一般使用账号 access token 的 Bearer 鉴权，不是 Sub2API Key。

| 方法与路径 | 作用 | 抓包注意 |
| --- | --- | --- |
| `GET /backend-api/accounts/check/v4-2023-04-27` | 获取账号、工作区、权益等信息 | 授权/刷新补全可能触发；工作区权益与个人订阅不能混用 |
| `GET /backend-api/subscriptions` | 获取订阅信息；请求带 `account_id` 查询参数 | 条件触发，不是每次授权固定出现 |
| `PATCH /backend-api/settings/account_user_setting` | 尝试关闭训练数据共享 | 查询参数 `feature=training_allowed`、`value=false`；是上游写操作，补全令牌时也可能触发 |
| `GET /backend-api/wham/usage` | 读取上游使用量/限额 | 不是本地 Key 的 5h/日/周账本 |
| `GET /backend-api/wham/rate-limit-reset-credits` | 查询额度重置权益 | 不等同于兑换 |
| `POST /backend-api/wham/rate-limit-reset-credits/consume` | 消耗权益兑换重置 | 真实状态变更；不要用于重复采样或重放 |

`enrichTokenInfo` 对补全/隐私操作采用尽力处理，缺少某个附带响应不一定代表令牌兑换失败。账号信息与隐私请求还各自有超时限制。

打开额度页后出现额外请求，需要结合自动兑换设置和后台任务时间线分析，不能只看 UI 发出的那一个 GET。

源码：[信息与隐私请求](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_privacy_service.go:18)、[补全逻辑](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_oauth_service.go:260)、[额度服务](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_quota_service.go:27)、[额度处理器](/root/data/docker_data/sub2api-dev/backend/internal/handler/admin/openai_oauth_handler.go:475)。

## 5. 推理入站接口与上游接口

### 5.1 客户端访问 Sub2API

| 入站接口 | 协议/语义 |
| --- | --- |
| `POST /v1/responses`、`POST /responses` | Responses 请求；按调用方式返回 JSON 或事件流 |
| `GET /v1/responses`、`GET /responses` | WebSocket 入口，不是按 ID 查询历史响应 |
| `POST /v1/responses/*subpath`、根路径对应别名 | 有子路径校验，不是任意路径/任意目标透传；例如 compact 要走对应转换分支 |
| `POST /v1/chat/completions` | Chat Completions 兼容入口，可能转换成 Responses 上游请求 |
| `POST /v1/messages` | Messages 兼容入口，按平台分派，可走 OpenAI 桥接 |
| `POST /v1/messages/count_tokens` | 计数入口；不要误判成一次 Responses 推理 |
| `GET /v1/models`、`GET /models` | 模型列表；Codex `client_version` 场景可走模型 manifest 路径 |
| `GET /v1/usage` | Sub2API 使用量接口，不是上游 wham 路径的同名透传 |

此外有 `/backend-api/codex` 兼容路由组，包含 Responses、Models、Alpha Search、Realtime Calls。**即使路径像 ChatGPT 内部接口，它仍受 Sub2API 入站 API Key 中间件保护。**

源码：[网关路由与子路径校验](/root/data/docker_data/sub2api-dev/backend/internal/server/routes/gateway.go:161)。

### 5.2 Sub2API 访问上游

- 普通 OAuth Responses：`POST https://chatgpt.com/backend-api/codex/responses`。
- OAuth Responses WebSocket：`wss://chatgpt.com/backend-api/codex/responses`；源码由对应 HTTP URL 转换 scheme。
- API Key 账号默认上游：`https://api.openai.com/v1/responses`，属于另一认证分支，不能和 ChatGPT OAuth 混为一谈。
- 模型 manifest：`GET https://chatgpt.com/backend-api/codex/models`，涉及 `client_version`、ETag、`If-None-Match`、304 和缓存。

HTTP 请求体通常为 JSON。SSE 响应应按事件边界解码，不能假定一个 TCP 包、一次 Read 或一个 HTTP chunk 就是一个完整 JSON。

WebSocket 有握手和后续消息两层：观察是否成功升级，再观察 `response.create` 及上游事件。一个持久连接可承载多个 turn，连接数不等于推理请求数。

### 5.3 转发不是字节级原样透传

OAuth 转换逻辑会处理模型名、instructions、input、tools 和不支持字段。普通 OAuth 路径会设置 `store=false`、`stream=true`；compact 分支删除 store/stream 字段。另有 passthrough、Chat Completions、Messages 桥接和插件分支，需要根据实际调用路径分析。

因此：

- 客户端 `stream=false` 不代表上游不是 SSE，Sub2API 可以汇总上游事件再返回非流式结果。
- 客户端模型名、本地映射后的模型名、最终请求模型名、上游返回模型名需要分别记录。
- `High` 等推理强度字段不等于计费倍率，也不等于账号权限判定。
- HTTP 200/WS 101 只说明连接或请求阶段通过，不说明推理完成。
- 需要识别 `response.completed`、兼容的 `response.done`、`response.failed`、`response.incomplete` 及 error 事件；它们并非都代表成功。
- 流中途断开且没有成功终止事件，不能因为已收到文本就记为成功。
- 同一个入站请求可能产生多个上游尝试；重试和切换账号要单独关联，不能把它们算成重复用户请求。

源码：[上游地址](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_gateway_service.go:31)、[构建上游请求](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_gateway_forward.go:1336)、[OAuth 请求转换](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_codex_transform.go:171)、[WS URL 与头部](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_ws_forwarder_payload.go:29)、[WS 转发](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_ws_forwarder.go)、[模型 manifest](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_codex_models_service.go:1575)。

## 6. 可选扩展路径

这些不是一次普通文本推理必然涉及的请求，不应为了“抓全接口”主动全部触发。

| 功能 | 源码中的目标 | 备注 |
| --- | --- | --- |
| Alpha Search | `https://chatgpt.com/backend-api/codex/alpha/search` | 也有使用 Responses 的搜索工具路径，不能只按入口推断目标 |
| Live | `POST https://chatgpt.com/backend-api/codex/realtime/calls?intent=quicksilver&architecture=avas` | JSON 请求，接受 SDP；另有基于 call ID 的 WS sideband，不是普通文本 SSE |
| 图片文件地址解析 | `GET https://chatgpt.com/backend-api/files/{fileID}/download` | 返回下载地址后再取文件，下载 URL 也可能包含敏感授权信息 |
| 会话附件地址解析 | `GET https://chatgpt.com/backend-api/conversation/{conversationID}/attachment/{attachmentID}/download` | 与普通 Responses 请求分开计时；不要向任意文件主机附加账号 Bearer |
| Codex PAT 校验 | `GET https://auth.openai.com/api/accounts/v1/user-auth-credential/whoami` | PAT 分支，不使用传统 refresh token 流程 |
| Agent Identity | `POST https://auth.openai.com/api/accounts/v1/agent/{runtimeID}/task/register` | 条件启用的任务身份机制，不是每个 OAuth 账号都必经 |

源码：[搜索](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_alpha_search.go)、[Live](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_live.go)、[图片](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_images.go)、[PAT](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_codex_pat_service.go:15)、[Agent Identity](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_agent_identity.go)。

## 7. 身份、代理、连接测试与实际请求的差异

应观察字段的来源和作用域，而不是把所有客户端的会话标识改成同一个值。

- 上游 Authorization 来源于选中账号的凭证，不应沿用入站用户的 Sub2API Key。
- account ID 表示上游账号/工作区选择；session、conversation、previous response 等字段代表不同层面的连续性，不是可任意互换的“设备 ID”。
- 源码包含按 API Key 和凭证命名空间隔离会话的处理，目的是避免跨用户、跨账号错误复用上下文。
- UA、originator 等身份头有统一处理，也有账号配置和插件介入路径。仅观察最终值与配置来源，不应据此推断“已经伪装成官方客户端”。
- 标准转发先尝试插件传输，未接管才走默认 HTTP upstream；连接测试有独立分支，特定情况下走 TLS fallback。测试成功不是生产请求所有协议都成功的证明。
- 代理配置存在不代表已经验证实际出口；应记录选中代理 ID、实际传输实现、连接错误阶段，不记录含用户名密码的代理 URL。

连接测试发给前端的 SSE 包括本地 `test_start`、`content`、`test_complete`、`error` 等事件，和上游 `response.*` 事件不是同一协议。本地新增串行测试采用直接模型探测，结果不能自动证明所有模型映射路径都等价。

源码：[传输分派](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_plugin_transport.go:11)、[连接测试](/root/data/docker_data/sub2api-dev/backend/internal/service/account_test_service.go:276)、[直接模型探测](/root/data/docker_data/sub2api-dev/backend/internal/service/account_model_probe.go)、[身份处理](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_codex_identity.go)、[会话作用域](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_codex_account_identity.go)、[可选指纹处理](/root/data/docker_data/sub2api-dev/backend/internal/service/openai_codex_fingerprint.go)。

## 8. 错误与用量应如何归因

至少区分四层：Sub2API 入站错误、调度/本地限制错误、上游 HTTP 错误、上游流内错误。

| 现象 | 可作出的判断 | 不能直接作出的判断 |
| --- | --- | --- |
| `API_KEY_REQUIRED` | 入站认证没有识别到所需 Key | 不能直接断定 ChatGPT access token 已失效 |
| 授权服务返回本地 502 | 可能是令牌请求初始化、网络或上游非成功响应被包装 | 不代表上游 HTTP 状态一定是 502 |
| 429 | 需要结合上游 body/code、重试信息、本地调度记录定位 | 不代表一定是 Key 周额度耗尽 |
| 503 / capacity / overloaded | 需要确定由本地、上游 HTTP 还是流内事件产生 | 不足以证明账号被风控或封禁 |
| 200 后断流 | 已有连接/部分输出，但终态需要另查 | 不能记为完整成功，也不能盲目重放有副作用的工具调用 |
| 403/503 HTML 响应 | 可能进入网关/挑战页面分类路径 | 仅状态码不能证明具体安全判定原因 |

OAuth 请求失败包装码包括 `OPENAI_OAUTH_CLIENT_INIT_FAILED`、`OPENAI_OAUTH_REQUEST_FAILED`、`OPENAI_OAUTH_TOKEN_EXCHANGE_FAILED`、`OPENAI_OAUTH_TOKEN_REFRESH_FAILED`；优先保留脱敏后的内层状态与错误码。

用量方面，需要分开记录上游 usage、本地价格计算、本地账号/用户/Key 倍率、限额和 extraQuota。上游 token 数不等于本地扣款金额；缓存 token 也不能按界面几列数字简单相加推断总输入，需根据具体协议字段口径避免重复计数。本文没有重新审计计费模块，不能用本文推断某模型官方长上下文定价。

## 9. 自研观察工具的最小记录结构

建议下列字段作为你自研工具的设计，而非声称项目已完整输出这些字段：

```json
{
  "trace_id": "local-correlation-id",
  "attempt": 1,
  "phase": "inference",
  "account_ref": "pseudonymous-account",
  "key_ref": "pseudonymous-key",
  "direction": "sub2api_to_upstream",
  "transport": "http_sse",
  "method": "POST",
  "host": "chatgpt.com",
  "path": "/backend-api/codex/responses",
  "model_requested": "client-model",
  "model_forwarded": "mapped-model",
  "http_status": 200,
  "upstream_request_id": "redacted-example",
  "terminal_event": "response.completed",
  "elapsed_ms": 1200,
  "error_code": null
}
```

另可增加：开始时间、首字节/首事件延迟、响应 Content-Type、连接是否复用、WS turn 序号、刷新是否发生、代理 ID、取消方、流结束原因、usage 原始数值及字段名。请求 ID 应保留所属层级，Cloudflare ID、本地 request ID、上游 request ID 和 response ID 不能混成一个字段。

推荐观察点：

1. 入站鉴权之后：记录 Key/账号的本地匿名引用，不记录原始认证头。
2. 调度和模型转换之后、最终传输发送之前：记录真实目标、模型和传输路径。
3. 上游响应头返回时：记录状态、Content-Type 和诊断 ID。
4. SSE/WS 解码之后：记录事件类型、usage、终态和脱敏错误；不逐 token 写完整内容。
5. 本地响应/账单落库之后：关联最终成功状态与费用，而非只保留首次上游响应。

注意：在插件分派之前观察到的请求不一定就是插件最终发出的请求，探针位置必须注明。

## 10. 抓包边界与安全验证顺序

- 被动抓取 HTTPS/WSS 网络包一般只能观察连接、握手和加密流量元数据，不能直接读取 OAuth JSON、SSE 内容或认证头；HTTP/2 复用也会让 TCP 连接与逻辑请求不是一一对应。
- 入站 TLS 终止处只能看到客户端到 Sub2API 的内容，不能据此还原 Sub2API 到上游转换后的请求。
- 优先在自有测试进程中加入脱敏的应用层观测，保留 TLS 校验；不要为了抓包关闭生产证书校验、扩大管理接口暴露面或导出真实令牌。
- 先用一个获授权测试账号、一个无敏感内容的短请求建立基线，再分别测试 HTTP 流式、非流式与 WS；不自动压测、不并发刷新凭证。
- 授权码、verifier、access/refresh/ID token、Cookie、完整 Authorization、代理密码和签名下载 URL 均不得明文写入报告。账号 ID、email、会话 ID 应用稳定匿名值关联。
- HAR、请求 URL 和错误 body 同样可能泄漏授权码或凭证；“只记录 headers”或“只记录错误”不自动等于脱敏。
- 不重放授权码、refresh token、额度兑换请求；真实连接测试也会产生费用。
- 完成测试后清理临时诊断文件，限制可读人员和保留时间；若令牌已外泄，按凭证泄漏处理，而不是仅删除日志。

仍需运行环境核验的事项：实际镜像版本、所选账号类型、插件是否接管、代理实际出口、WS/透传开关、自动额度兑换、后台刷新任务及重试参数。本文没有把这些配置假定为已启用，也没有执行任何线上变更。
