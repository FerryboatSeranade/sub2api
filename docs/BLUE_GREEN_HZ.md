# hz 蓝绿发布

固定入口 `code.gogoais.com → 远端 NPM → hz:8081 → 本机 Nginx`。
蓝环境 `127.0.0.1:28081`，绿环境 `127.0.0.1:28082`。PostgreSQL、Redis 共用原有服务；数据库和 Redis 不随发布重启。

## 发布

在 `/root/data/docker_data/sub2api-dev` 执行：

```bash
# 先构建不可变版本镜像，并审查数据库迁移是否向后兼容。
docker build -t sub2api-staging:RELEASE .
python3 deploy/bluegreen.py status
# 目标必须是非活动且已停止的槽位；以下假设当前为绿。
python3 deploy/bluegreen.py prepare blue sub2api-staging:RELEASE
# 直接访问 127.0.0.1:28081，完成健康、管理认证、策略保存和真实流式请求检查。
python3 deploy/bluegreen.py switch blue
python3 deploy/bluegreen.py drain
# 若尚有旧连接/探针，稍后重复 drain。成功后旧容器停止但保留。
```

`prepare` 固定镜像 ID，健康检查失败不会切流。`switch` 检查配置、reload，并验证固定入口响应头 `X-Sub2api-Slot`；失败自动恢复旧配置。`drain` 不强杀已有 upstream TCP 连接，连接清空后再等待 195 秒让定时探针完成；然后停止旧容器并交接探针调度所有权。长连接超过 Nginx 的 3600 秒退出宽限仍可能中断，不能承诺无限长会话零中断。

旧版初始容器为 `sub2api-staging`，保留其原镜像。首次 `bootstrap` 把旧容器端口改为 28081 并让 Nginx 接管 8081，因此有一次短暂中断；日后切换无需重建入口。

## 回滚

```bash
python3 deploy/bluegreen.py switch green  # 指定仍保留的上一版本槽位
python3 deploy/bluegreen.py drain
```

切回前可先 `docker start` 旧槽位并直接做健康检查。若已经用 `prepare` 覆盖上一版本，需使用保留的旧镜像重新准备。回滚不会撤销数据库迁移。本次功能没有 schema migration，仅新增 `accounts.extra` 内的策略/最新结果摘要，旧版本忽略这些字段；旧版不具备降智自动排除逻辑。

## 持久化与互斥

- 私有部署状态在 `deploy/.bluegreen/`，不能提交 Git；含两环境 Compose、应用数据副本、入口配置、不可变镜像 ID。
- 环境文件继续引用 `deploy/.hz-private/sub2api.env`，保持 JWT/数据库/Redis 凭证一致。新配置发布时须同时考虑两个槽位。
- 定时检测扫描通过 `runtime/scheduler-owner` 指定活动槽位。切换排空期间暂停新扫描，恢复后补执行到期计划。
- 手动和定时检测均持有同一个 PostgreSQL advisory lock（按账号），锁内重读到期计划；连接退出自动释放，跨实例重复执行被阻止。锁覆盖测试、结果持久化和 next_run 更新。
- 历史旧版不认识 owner 标记，首次发布应避开其定时检测高峰，排空后将其停止。旧版回滚的扫描行为同理。
- 其他后台任务继续使用项目原有机制；蓝绿同时存活时间保持短，不作为永久双活方案。
- 不要再直接 `docker compose up -d sub2api` 更新线上；使用蓝绿脚本。原 Compose 留用于数据库、Redis、relay 和历史回滚容器。

## 账号列表功能

`/admin/accounts` 新增降智状态列，取该账号所有糖果检测计划中的最新结果（最大结果 ID），显示检测时间及错误说明。数字 21 是用户指定的单题判定，不代表综合能力评测。

“降智自动排除”为每账号独立开关，默认关闭。开启后，最近结果为 degraded 时调度拒绝该账号；not_degraded、inconclusive、未检测不会因该策略被排除。新的探针结果同步账号调度缓存。手动暂停、限流、到期及其他健康条件仍独立生效。

“强制恢复”复用现有恢复接口，清除本地可恢复异常和限流缓存，不重置上游配额，不打开手动暂停，不取消降智排除。操作需在页面确认，失败保留错误反馈。
