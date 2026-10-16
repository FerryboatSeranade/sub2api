# 糖果题账号检测

入口：管理后台 → 账号 → **降智检测总览**；单账号的 **定时测试** 面板可创建、修改、删除检测计划并查看历史。

- 测试类型 `candy_v1`，固定模型 `gpt-6.1-sol`、reasoning effort `high`。
- 使用账号自身的凭证、代理和测试传输；显式请求模型，绕过账号的模型别名映射和 OAuth 模型归一化。不自动降级到其他模型。第三方上游实际内部使用何种模型仍取决于该上游。
- 发送完整糖果题，未提供预期答案；`tools: []`、`tool_choice: none` 禁用工具。
- 提取可见回答中的数字，支持全角数字；独立数字字符串 `21` 通过，`121`、`210`、`21.5`、`-21` 不通过。任意推导位置出现 `21` 也通过，不仅检查最终结论。
- `not_degraded`：不降智（包含 21）；`degraded`：降智（未包含 21）；`inconclusive`：无法判定（请求失败、超时、空回答、不适用账号）。
- 这是用户指定的单题启发式，不是对模型整体能力的证明。答案用中文“二十一”而不含数字 `21`，按此规则仍不通过。
- 调用错误不会被当作降智。该类型不会自动恢复账号，也不会因探测的 401/429 变更账号的错误/限流状态。正常代理可能自行执行其内部调度策略。

## 运行与历史

复用现有定时测试 runner。新建计划选择“糖果题 · 降智检测”；手动运行禁用的计划不会启用它。总览可以重测单账号、并发 2 重测全部、停止尚未启动的排队任务、切换定时开关、搜索和导出 JSON。关闭总览也会停止浏览器端尚未启动的排队任务，已开始的调用继续持久化结果。

每个调用最多 180 秒；服务端限制糖果题最多 2 个并发，并对同一账号防止测试重入。当前部署为单实例，锁和并发限制是进程级的。运行多个实例前需加分布式 claim/lease。

计划沿用服务器的时区（hz：Asia/Shanghai）。建议每天一次，按账号错开分钟，最多保留 50 次；现有连接测试计划不改变。最大保留值为 500。结果保存请求模型、测试版本、原始回答、异常和耗时；修改计划不会把旧的连接测试结果重新解释为糖果题结果。

历史保留在 `scheduled_test_results`，数字与判定由版本化规则生成。失败调用可能仍消耗上游额度。手动运行将更新计划的下次执行时间。

## 管理 API

均使用现有管理员认证：

- `POST /api/v1/admin/scheduled-test-plans`：传 `account_id`、`test_kind: candy_v1`、`model_id: gpt-6.1-sol`、`cron_expression`、`enabled`、`max_results`。
- `POST /api/v1/admin/scheduled-test-plans/:id/run`：执行并持久化一次检测，返回结果及 `quality`。
- `GET /api/v1/admin/scheduled-test-plans/:id/results`：历史结果。
- `GET /api/v1/admin/scheduled-test-plans/reasoning-overview`：各糖果题计划的最近结果及账号展示名称，不返回凭证。

## 部署与回滚

迁移 `246_scheduled_reasoning_probe.sql` 仅新增带默认值的列，兼容旧应用。发布前保留原镜像和 Compose 配置，备份定时测试表。回滚应用镜像不需要删除新增列；暂停糖果题计划后再使用旧应用，避免旧版 runner 把它当连接测试运行。正式卸载检测功能应先禁用或删除 `test_kind = candy_v1` 的计划。

### hz 代理配置

`code.gogoais.com` 的 Nginx Proxy Manager → Settings → Custom Nginx Configuration：

```nginx
# Allow scheduled reasoning probes to finish (application deadline: 180s).
proxy_read_timeout 240s;
proxy_send_timeout 240s;
```

用于允许手动检测接口在模型思考期间等待完整响应。前端请求超时为 210 秒。hz 当前 Compose 镜像为 `sub2api-staging:hz-quality-20261017`。
