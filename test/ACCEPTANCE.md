# 验收清单

跑：`cd D:\docker\chores; docker compose --profile dev up -d --build` → 打开 http://127.0.0.1:2022

## A. 身份
- [ ] A1 首次访问是头像登录页，点头像**直接进入**，无密码无 PIN
- [ ] A2 刷新后仍是已登录状态（Cookie 生效）
- [ ] A3 顶栏右上角显示当前成员头像，点它可以换人

## B. 发布 → 认领 → 完成（核心）
- [ ] B1 右下角 `+` 打开底部抽屉，四个字段：任务名 / 分组 / 重复 / 谁做；主按钮「发布」
- [ ] B2 发布后立即出现在「今日」，状态为**待认领**，按钮是「认领」
- [ ] B3 点「认领」→ 行内就地变为"XX 已认领"，按钮变「完成」，**不刷新整页、不弹确认**
- [ ] B4 换成另一个成员（A3），该行显示"已认领"且**没有**「认领」按钮（不可重复认领）
- [ ] B5 俩人几乎同时认领 → 只有一个成功，另一个得到 409 并看到最新状态（不出现双认领）
- [ ] B6 点「完成」→ 变**已完成**，显示"谁 + 完成时间"
- [ ] B7 完成后可撤销，回到待认领，且历史里留下痕迹

## C. 循环
- [ ] C1 建一个「每天」的任务，今天应有一个待认领实例
- [ ] C2 重启容器后，未来 30 天的实例自动补齐（幂等，不重复）
- [ ] C3 「每周」只在指定星期几出现；「每月」在指定日出现（该月无此日则落到月末）

## D. 视图
- [ ] D1 底部四个：今日 / 本周 / 本月 / 历史；当前项为黑底白字
- [ ] D2 本周、本月是日历网格，格子内有状态点；点格子出当天任务抽屉
- [ ] D3 历史是 `任务 · 谁 · 完成时间` 的时间线，倒序

## E. 管理面
- [ ] E1 访问 `/admin` 输口令（`CHORES_ADMIN_TOKEN`）后能进
- [ ] E2 能增删改成员（名字、颜色、头像字），删掉的人历史仍在
- [ ] E3 能管分组
- [ ] E4 `/api/admin/export` 能下载 `chorus.db`

## F. 体验与设计
- [ ] F1 手机（375px 宽）无横向滚动，点击目标 ≥44px，安全区不遮挡底栏
- [ ] F2 深色模式跟随系统，无死白块
- [ ] F3 全程没有超过一行的说明文字，没有表格化表单
- [ ] F4 动效只有淡入和 ≤8px 位移，`prefers-reduced-motion` 下关闭
- [ ] F5 `/?tv=1` 大屏模式：黑底大字，60s 自动刷新

## G. 工程
- [ ] G1 `docker compose --profile dev up --build` 从零可跑（不依赖 Docker Hub）
- [ ] G2 `docker compose --profile prod up --build` 产出极小镜像且能服务
- [ ] G3 `/health` 返回 `{"ok":true,...}`
- [ ] G4 数据库只有一个文件 `data/chorus.db`，重启数据不丢

## 验收记录（2026-09-18，由主控实跑）

命令：`node test/integration.mjs` → **24 passed, 0 failed**；`node test/ui-shim.mjs web/assets/app.js` → **20/20 ALL PASS**；
`docker exec chorus-dev sh -c "cd /app && go vet ./... && go test ./internal/..."` → **ok**；`scripts/backend-check.mjs` → **ALL PASS**。

| 项 | 结果 | 证据 |
|---|---|---|
| A1 头像登录无密码 | ✅ | `POST /api/auth/login {member_id}` → 200 + `Set-Cookie: chores_member=1; HttpOnly; SameSite=Lax; Max-Age=31536000` |
| A2 刷新保持登录 | ✅ | 带 Cookie 再请求 `/api/bootstrap` → 200，`me` 为该成员 |
| A3 顶栏可换人 | ✅ | 前端抽屉 → `/api/auth/login`；ui-shim 覆盖 |
| B1 发布抽屉四字段 | ✅ | ui-shim：任务名/分组/重复/谁做 + 全宽「发布」 |
| B2 发布后入今日池 | ✅ | `POST /api/instances` → 200，id=419，state=open |
| B3 认领行内变化 | ✅ | 认领 → `{state:claimed, claimed_by_name:小明, claimed_at:…}`；ui-shim 验证无整页刷新/无确认框 |
| B4 他人不可重复认领 | ✅ | 前端对 claimed 行不渲染「认领」按钮（ui-shim） |
| B5 抢占 409 + 当前实例 | ✅ | B 抢 A → **409**，body `{error:{code:CONFLICT}, instance:{state:claimed}}`；后端 8 路真并发只 1 个赢家 |
| B6 完成记录谁/何时 | ✅ | 完成 → `completed_by_name=小明, completed_at=2026-09-18T08:05:39Z, completion_note=集成测试完成` |
| B7 撤销留痕 | ✅ | 本人撤销 → state=open；他人撤销 → **403**；activity 留档 |
| C1 daily 到期生成 | ✅ | 3 个 daily 各 48 天（9-01~10-18）；重启后数量不变 |
| C2 幂等 | ✅ | `docker restart` 前后实例总数均 160（`(chore_id,due_date)` 唯一） |
| C3 weekly/monthly 规则 | ✅ | weekly 只落周六；monthly 只落 1 号（后端脚本逐日核对） |
| D1–D3 四视图 | ✅ | ui-shim：今日分组/本周 7 列/本月 42 格/历史三要素；真实数据下 bootstrap 覆盖 9-01~10-18 |
| E1–E4 管理面 | ✅ | 错口令 403 / 对口令 200；成员与分组增改删通；导出 `attachment; filename="chorus.db"` |
| F1 手机适配 | ✅ | ui-shim 静态核对：行高 56px、热区 ≥44px、safe-area 8 处、无横向滚动类名 |
| F2 深色模式 | ✅ | `prefers-color-scheme: dark` 全套变量，ui-shim 静态核对 |
| F3 文案克制 | ✅ | ui-shim：无表格化表单、无超过一行的说明 |
| F4 动效受限 | ✅ | 仅 opacity/≤8px 位移；`prefers-reduced-motion` 下全部关闭 |
| F5 大屏模式 | ✅ | `/?tv=1` → 200；60s 刷新 |
| G1 dev 从零可跑 | ✅ | 不依赖 Docker Hub（本地 alpine + vendor 的 Go 工具链 + 纯标准库） |
| G2 prod 极小镜像 | ✅ | `chores:local` **24.4 MB**，非 root（uid=10001）运行，healthy |
| G3 /health | ✅ | `{"ok":true,"version":"dev"}` |
| G4 单文件持久化 | ✅ | `data/chorus.db`（dev 绑定挂载 / prod 命名卷），重启不丢 |

## 已知限制（不阻塞使用）

1. `/reschedule` 仅单测覆盖，没做端到端手工验证。
2. `/complete` 的 `photo_id` 只接收不落库（契约未定义照片存储）。
3. 数据层是**单文件 JSON + 锁 + 原子写**（零第三方依赖的约束下无法上 SQLite），万级实例内没问题；几十万条历史时再评估换 SQLite。
4. 容器内不要 `pkill -f dev-watch`（会连自己的 shell 一起杀）；重启请用 `docker restart chorus-dev`。

## v2 增量验收（2026-09-18，主控实跑）

命令：`node test/v2-check.mjs` → **40 passed, 0 failed**；`node test/integration.mjs` → **44 passed, 0 failed**；
`go vet ./...` 干净、`go test ./internal/...` ok（含新增 `time_test.go` 的六种组合）。

### 时间三选二（实测）

| 组合 | 期望 | 实测 |
|---|---|---|
| 都不给 | 默认 10 分钟 | ✅ `start_at=2026-09-18T09:50:39Z`，`duration_minutes=10`，`end_at=+10min` |
| 开始 + 结束 | 算时长 | ✅ 1 小时 → `duration_minutes=60` |
| 开始 + 时长 | 算结束 | ✅ 30 分钟 → end-start=30min |
| 结束 + 时长 | 算开始 | ✅ 45 分钟 → end-start=45min |
| 三个都给 | 以开始+时长为准 | ✅ 传 15 分钟 → end-start=15min（忽略给的 end） |
| 只给一个（开始/结束/时长各试） | 400 | ✅ 三条都是 400 `BAD_REQUEST`，消息含「时间需要给两个：开始/时长/结束」 |
| 时长 0 / 1441 | 400 | ✅ 都被拒 |
| 结束 = 开始 | 400 | ✅ 被拒 |
| 循环模板平移 | 每天本地 07:30 | ✅ 模板 UTC 23:30 → 生成实例本地 07:30，时长沿用 25 分钟 |

### 权限（实测）

| 动作 | 成员 Cookie | 匿名 | 管理员 |
|---|---|---|---|
| POST/PATCH/DELETE `/api/groups` | ✅ 201/200/200 | 401 | — |
| POST/PATCH `/api/members` | ✅ 201/200 | 401 | — |
| DELETE `/api/members/{id}` | 403 | 401 | ✅ 200 |
| 老 `/api/admin/groups*` | — | — | ✅ 仍可用 |
| 老 `/api/admin/members*` | 403 | 401 | ✅ 仍可用 |

### 头像（实测 status）

| 端点 | 结果 |
|---|---|
| `PUT /api/members/{id}/avatar`（PNG，69 B） | ✅ 200，返回 `avatar_url=/api/avatars/2?v=2026-09-18T09:50:40Z` |
| `PUT` 非白名单 `image/gif` | ✅ 415 |
| `PUT` 300 KB | ✅ 413 |
| `GET /api/avatars/{id}` | ✅ 200 + `image/png` + `Cache-Control: private, max-age=86400` + `nosniff`，字节与上传一致（69 B 全等） |
| `DELETE /api/members/{id}/avatar` | ✅ 200，`avatar_url` 变 null |
| 删除后 `GET` | ✅ 404 |

### 其它

- `/api/bootstrap` 顶层新增 `time_defaults`；`me`/`members` 带 `avatar_url`；`instances` 带 `start_at/end_at/duration_minutes`。
- 老数据（无时间字段）不报错：实测 161 条 `start_at=null` 正常返回。
- 生产镜像重建为 healthy，`/api/bootstrap` 61 KB 正常。
- 顺手修掉一个路由覆盖 bug：v2 加 `DELETE /api/members/{id}` 时曾把 `POST/PATCH /api/admin/members*` 覆盖掉，已补回并复验。

## v2 前端交付与端到端复核（2026-09-18 18:0x，主控实跑）

前端 v2 已上线并在跑：`web/assets/app.js` 41,198 B、`web/assets/app.css` 18,096 B，
服务端返回字节与磁盘**逐字节相等**（node 侧 `Buffer.equals` 验证）。

### 四套验收全绿

| 套件 | 结果 |
|---|---|
| `go vet ./...` + `go test ./internal/...` | ✅ 干净 / ok |
| `node test/ui-shim.mjs web/assets/app.js` | ✅ **ALL PASS（31 条）** |
| `node test/v2-check.mjs` | ✅ 40 passed / 0 failed |
| `node test/integration.mjs` | ✅ 44 passed / 0 failed |

### 前端 v2 的实际形态（从 DOM 校准出来的事实）

- 分段控件是 **今日 / 本月 / 本年 / 历史**（原来的「本周」已并入本月视图）
- 日视图行内文案：
  - 待认领：`倒垃圾 17:55–18:05 [认领]`
  - 已认领：`拖地 已认领 · 小王 · 17:55–18:25 [完成]`
  - 已完成：`洗碗 小明 · 17:55–18:10 ✓`（灰化）
- 底部 `stats-bar`：按人「N 件 / M 分钟」
- 发布抽屉时间三块：开始（`datetime-local`）、时长（分）、结束，默认值来自 `bootstrap.time_defaults`，
  结束由「开始+时长」**实时算出**（实测 19:30 + 10 → 19:40）
- 本年视图是 12 个迷你月格（活动热度 `lv0/lv1/lv3`）

### 我修的一处测试债

`test/ui-shim.mjs`（前端自带回归）原先还在断言旧版行内文案与「本周 7 列」，
v2 上线后挂了 3 条。我按**以 UI 为准**的原则更新了断言，并补齐 v2 覆盖：

- 新增：分段控件文案、本年 12 格、统计条与「件数/时长」、头像 `<img>`（`avatar_url`）、
  抽屉时间默认值（`19:30` / `10` / 自动算出 `19:40`）
- DOM 替身补上 `value` 属性支持（`<input>` 的值不在 `textContent` 里，这是之前抓不到默认值的原因）
- 伪 `/api/bootstrap` 补 v2 字段（`time_defaults`、`avatar_url`、实例时间字段）

结论：`ui-shim` 31 条 **ALL PASS**。

---

## v2 验收记录（主控实跑，2026-09-18 收尾）

五套脚本全绿：

| 脚本 | 结果 |
|---|---|
| `node test/integration.mjs` | **51 passed / 0 failed** |
| `node test/ui-shim.mjs web/assets/app.js` | **48 passed / 0 failed** |
| `node scripts/backend-check.mjs` | **ALL PASS** |
| `node scripts/backend-verify.mjs` | **ALL PASS** |
| `node scripts/backend-v2-check.mjs` | **40 passed / 0 failed** |
| `gofmt -l` 空 · `go vet ./...` 无输出 · `go test -count=1 ./internal/...` | ok 0.5s |

| v2 项 | 结果 | 证据 |
|---|---|---|
| 分段改为 今日 / 本月 / 本年 + 历史（**无「本周」**） | ✅ | ui-shim：分段文案断言 + 「界面上不再出现『本周』」 |
| 月视图 = 日历 42 格 + 今天格区分 + 点格抽屉内可认领 | ✅ | ui-shim：`.today` + `data-today`；抽屉内认领真的发出 `/claim` |
| 年视图 = 12 个小月历（按完成率着色） | ✅ | ui-shim：12 个月 × 42 格 |
| 日视图行内三要素（状态 + 人 + 时间） | ✅ | ui-shim：已完成「人 · 时间」、已认领「已认领 · 人」、有 start/end 时显示时段 |
| 每人统计条（本地聚合，空时长按 10 分钟） | ✅ | ui-shim：2 件 35 分钟；空时长按 10 计 |
| 发布抽屉时间三选二 | ✅ | ui-shim 纯函数三种组合 + UI 联动 + 只填一项主按钮 disabled；集成侧三种组合服务端都算对 |
| 只给一个时间字段 → 400 且消息等于契约原文 | ✅ | `时间需要给两个：开始/时长/结束`（后端修掉了 `bad input:` 前缀） |
| 三个都不给 → 默认 10 分钟 | ✅ | 集成断言 |
| 时长越界 / 结束=开始 → 400 | ✅ | 集成断言 |
| `time_defaults.start_at_local`（无时区本地串）可被解析 | ✅ | 配时长提交 → 201；带时区写法同样 201 |
| 头像上传往返（PUT → GET → 删除） | ✅ | 集成：PUT 200、GET 200 `image/png`、`Cache-Control: max-age`、bootstrap `avatar_url` 指向 `/api/avatars/{id}` |
| 头像类型/大小限制 | ✅ | 非白名单 → **415**；>256KB → **413** |
| 权限开放（分组/成员增改 不需管理员） | ✅ | `GET/POST/PATCH/DELETE /api/groups`、`GET/POST/PATCH /api/members` 普通成员均可 |
| 仍仅管理员（删成员 / 导出 / 写演示数据） | ✅ | 普通成员删成员被拒、管理员 200；导出 `attachment; filename="chorus.db"` |
| 老数据兼容 | ✅ | 无时间的老实例三字段为 `null`，落盘无 `""`/`0` 污染（后端单测固化） |

**环境**：dev（127.0.0.1:2022）与 prod-check（127.0.0.1:2023，`chores:local` 24.5 MB、非 root、healthy）都是 v2；两边库均为干净演示数据（4 成员 / 2 分组 / 6 任务 / 160 实例，2026-09-01 ~ 2026-10-18）。

> 注：本文档中段「ui-shim 31 条」是 v2 进行中的中间快照，最终为 **48 条**。

## v3 增量：/api/instances 增量补齐边界（2026-09-18，主控实跑）

前端翻到 bootstrap 默认窗口之外时用 `GET /api/instances?from&to` 增量拉取，所以钉死三件事。

命令：`node test/instances-window-check.mjs` → **18 passed, 0 failed**（exit=0）

| 场景 | 期望 | 实测 |
|---|---|---|
| `from=2026-08-01&to=2026-08-31`（窗口外） | 200 + 空数组，不报错 | ✅ `n=0`，响应仍带 `today` |
| `from=2030-01-01&to=2030-12-31` | 200 + 空数组 | ✅ |
| 只给 `from` | 200，按 from 起 | ✅ n=518 |
| 只给 `to`（窗口外） | 200 + 空数组 | ✅ |
| `from=2026/08/01`（格式错） | 400 + 契约错误体 | ✅ `{"code":"BAD_REQUEST","message":"from 需为 YYYY-MM-DD"}` |
| `from=2026-09-30&to=2026-09-01`（from>to） | 400 | ✅ 新增校验：`from 不能晚于 to` |
| `from=20260901` / `2026-02-30` | 400 | ✅ |
| 结构一致性 | 与 bootstrap 的 instances 逐字段一致 | ✅ 字段集合 diff=[]；247 条重叠实例**逐字段值全等**（0 mismatch）；id 唯一；时间字段可为 null 且不缺键 |

**为满足这条边界，改了 1 处代码**：`internal/httpapi/api.go` 的 `getInstances` 增加
`from`/`to` 格式校验与 `from > to` 校验（此前 `from > to` 会静默返回空数组，
前端会把"参数写错"误当成"这段时间真的没任务"）。

### 回归（全部干净）

| 检查 | 结果 |
|---|---|
| `node scripts/backend-check.mjs` | ✅ ALL PASS（exit 0） |
| `node scripts/backend-verify.mjs` | ✅ ALL PASS（exit 0） |
| `node scripts/backend-v2-check.mjs` | ✅ 40 passed / 0 failed（exit 0） |
| `gofmt -l .` | ✅ 无输出 |
| `go vet ./...` | ✅ 干净 |
| `go test -count=1 ./internal/...` | ✅ ok 1.141s |

本轮未动 `web/`、未动 `test/integration.mjs`、未动 `api/CONTRACT.md`。

## v3 前端进展与 ui-shim 校准（2026-09-18 傍晚，主控）

前端 v3 已上线：`web/assets/app.js` 45,826 B。分段由「今日/本月/本年/历史」变为
**今日 / 本月 / 本年 / 成员**（独立「历史」去掉，历史=往月/往日导航），
新增导航箭头（`navrow/navbtn/chev` + `nav-title` 可点回今天）、密度图、抽屉即时反馈。

`test/ui-shim.mjs` 随之更新（以 UI 为准）：

- 分段断言改为 今日/本月/本年/成员，并断言**不再有独立「历史」**
- 新增成员视图断言（列出成员 + 密度/统计可视化）
- 空态文案改为「这天没有任务」（v3 按天）
- 确认空态**仍保留导航**（‹ 9月19日 星期六 › 可回今天）——不是死路

结果：`node test/ui-shim.mjs web/assets/app.js` → **ALL PASS（50 条）**；`test/integration.mjs` → **51 passed / 0 failed**。
