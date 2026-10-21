# 账号转入 vm2api

账号管理 → OpenAI OAuth 账号行的 **vm2api** → 确认并同步。

- 始终加入 `vm2api-hz`；可勾选继承原 OpenAI 分组。已有目标分组保留。
- 每个上游账号身份关联一个 Docker Codex 容器、一把只能使用该容器的 Key、一个 Sub2API API Key 类型账号。三层并发均为 2。
- 容器已存在时按上游 account ID 识别并更新凭证，保留代理、指纹、模型与风控设置；新容器使用部署指定代理及官方默认设置，不应用会修改全局设置的预设。
- 使用专属 Key 完成真实流式 API 测试，收到文本和 response.completed 后才切换。原 OAuth 账号保持记录和凭证，但状态变为 inactive、schedulable=false。
- Sub2API 引用的共享 Key 会移除已转入容器；移除最后一个容器时禁用 Key 及对应共享入口，不写入可能表示无限制的空范围。非 Sub2API 消费者的 Key 不修改。池绑定的共享 Key 需要先解除池绑定。
- 后台任务使用 PostgreSQL session advisory lock，蓝绿两实例不能同时创建资源。进度保存在原账号 extra.vm2api_bridge。页面可关闭，重新打开继续看进度。
- 失败不执行新旧账号数据库切换。已创建容器/Key 保留用于重试；已修改共享 Key 范围会恢复。任务被杀死后重试先恢复未完成的范围变更，再按稳定身份核对资源。
- 切换事务同时写 scheduler_outbox，缓存按现有机制刷新；已经开始的旧请求不强行中断。
- 不自动重新启用原账号。如需回退，先停止新 vm2api 账号的调度，再手动启用原 OAuth 账号；不要同时开启两个入口。

## 部署配置

仅服务器环境变量，浏览器无法指定管理 URL 或读取 Master Key：

```
VM2API_MANAGEMENT_URL=http://private-management-endpoint
VM2API_MANAGEMENT_KEY=<master key>
VM2API_UPSTREAM_URL=http://private-api-endpoint
VM2API_GROUP_NAME=vm2api-hz
VM2API_PROXY_ID=px-local
VM2API_TEST_MODEL=gpt-6.1-sol
```

Master Key 只保存在私有环境文件，不能提交 Git。管理端点只对受信任的应用网络开放。未配置时 UI 显示未配置并禁用提交。该功能无 schema migration，回滚旧镜像会忽略 extra 新字段；已完成的账号切换不随镜像回滚撤销。

管理 API：GET `/api/v1/admin/accounts/:id/vm2api` 查询；POST 同一路径提交 `{ "confirm": true, "inherit_groups": true }`。异步返回 running，轮询直到 completed/failed；任务中断超过 16 分钟显示 interrupted，可重试。

蓝绿排空额外检查 vm2api 任务 advisory lock；任务仍在运行时保留旧容器，避免后台任务被提前终止。

## 计划与列表标识

已完成关联的原账号按钮呈绿色并带勾选标识；衍生账号使用淡蓝背景（支持暗色模式）。
账号切换事务将原账号全部 scheduled_test_plans 转移到衍生账号，保留计划 ID、历史记录、启用状态和执行时间配置；重复同步不会复制计划。原账号不再持有这些计划。正在执行测试时不切换，提示稍后重试；排队中的旧账号计划会被重新校验并拒绝执行。
降智定时/手动计划执行器允许跨账号同时执行 10 个测试，单账号互斥及业务三层并发 2 保持不变。
