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
- 定时检测扫描通过 `runtime/scheduler-owner` 指定活动槽位。现代槽位切换后立即把扫描所有权交给新槽位，账号 advisory lock 防止与旧槽位在途探针重复；仅 legacy 切换排空期间暂停新扫描。
- 手动和定时检测均持有同一个 PostgreSQL advisory lock（按账号），锁内重读到期计划；连接退出自动释放，跨实例重复执行被阻止。锁覆盖测试、结果持久化和 next_run 更新。
- 历史旧版不认识 owner 标记，首次发布应避开其定时检测高峰，排空后将其停止。旧版回滚的扫描行为同理。
- 其他后台任务继续使用项目原有机制；蓝绿同时存活时间保持短，不作为永久双活方案。
- 不要再直接 `docker compose up -d sub2api` 更新线上；使用蓝绿脚本。原 Compose 留用于数据库、Redis、relay 和历史回滚容器。

## 账号列表功能

`/admin/accounts` 新增降智状态列，取该账号所有糖果检测计划中的最新结果（最大结果 ID），显示检测时间及错误说明。数字 21 是用户指定的单题判定，不代表综合能力评测。

“自动排除降智账号”为页面顶部总开关，默认关闭。每个账号可选跟随全局、始终排除降智、不自动排除；独立选项优先于总开关。最终策略开启后，最近结果为 degraded 时调度拒绝该账号；not_degraded、inconclusive、未检测不会因该策略被排除。新的探针结果同步账号调度缓存。手动暂停、限流、到期及其他健康条件仍独立生效。

“强制恢复”复用现有恢复接口，清除本地可恢复异常和限流缓存，不重置上游配额，不打开手动暂停，不取消降智排除。操作需在页面确认，失败保留错误反馈。


## 全局策略与 Excel2API（2026-10-19）

全局值保存在 settings.reasoning_auto_exclude_global。账号 extra.reasoning_exclusion_policy 是 inherit/enabled/disabled；extra.reasoning_auto_exclude 保存最终值兼容上一版本调度。写入使用事务互斥锁，和结果快照、scheduler outbox 同事务提交。切换后缓存通过 outbox 刷新。历史 true 保留独立开启，false 迁移为跟随全局。新账号首次检测保存结果时计算当前全局值。

插件来自 https://github.com/1812095643/sub2api-excel2api-plugin ，固定 ea32f88a64ac625164c5cf9313c0b24bc4addb75 的 0.7.0 包；SHA256 bbac0a2610606bee55fafef826b5c7261ec9d1cd19e099ec2ad09b66b6a92007。仅配置官方发布者公钥并验证签名，不允许 unsigned。业务配置加密存储在共享数据库，签名信任配置须在两个槽位的 config.yaml 中同步。

插件管理 → OpenAI 账号健康 → 配置：开启 Excel2API，账号 ID 每行一个；初始只选择 18。检测账号同填 18，测试已暂停账号时关闭“只测试启用调度账号”。插件 ModelTrace 列表暂不支持 gpt-6.1-sol，使用支持的 gpt-6-sol 点击“保存并测试 Excel2API”。这类模型识别结果与糖果题判断不同。

对照 6.1 sol 糖果题：账号管理 → 降智检测总览 → 找到账号 → 立即检测。检测路径也经过已启用插件。关闭 Excel2API 再检测得到原路径对照；开启并指定同一账号再次检测得到 BPS 路径结果。保存完整结果和时间，不以请求成功等同于未降智。插件不会自动取消手动暂停或上游限流。

插件自己的定时任务默认关闭，保留现有糖果题定时任务。插件启用后所有 OpenAI OAuth 请求会经过插件，未选择的账号仍走原上游但受插件时区/语言处理；包内有 64 MiB 请求体上限，和 vm2api 的 128 MiB 是不同链路。

蓝绿期间插件绑定共享：新安装插件先导入且保持禁用，切换排空后再启用；避免旧运行进程未读取新的发布者信任配置而拒绝加载。回滚前确认旧槽位磁盘 config.yaml 已含发布者公钥；启动旧进程会重新读取。插件配置变更并不随应用镜像回滚。


### 本次验证结果

- 6.1 sol 糖果题，账号 18：原线路回答 29（不含 21）；BPS 线路返回 usage policy HTTP 403，没有答案，判为无法判断，不能认定解决降智。
- 插件自带 gpt-6-sol 检测，同一账号返回 HTTP 403。
- 未选中 BPS 的账号 47，启用插件后糖果题回答包含 21，流式转发成功。
- 蓝绿切换中 186 次健康采样无失败，原绿色流式请求完整结束。
- 全局开关保持关闭，账号默认跟随全局。账号 18 保持手动暂停。

自动排空可使用 `nohup python3 -u deploy/bluegreen.py finish > /tmp/sub2api-drain.log 2>&1 </dev/null &`，每 30 秒检查一次，最长 65 分钟；每次检查单独取得发布锁，允许中途回滚。旧连接清空后再等待探针宽限并停止旧容器。当前版本两槽位均已复制验签后的插件文件，运行时再次校验哈希，且两个槽位的配置已同步发布者公钥。
