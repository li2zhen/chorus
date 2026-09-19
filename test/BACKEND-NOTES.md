# 后端交付说明（B/C/E/G 组）

负责人：后端线。契约按 `api/CONTRACT.md` 字面实现，未改契约。

## 一、改了哪些文件

| 文件 | 一句话 |
|---|---|
| `internal/store/store.go` | 数据层骨架：单文件 JSON + `sync.RWMutex` + 原子写（临时文件 + rename）；成员 CRUD |
| `internal/store/groups.go` | 分组 CRUD；删分组时把引用它的任务/实例 `group_id` 置空 |
| `internal/store/chores.go` | 任务定义 CRUD（PATCH 语义：nil 不改）；改标题/备注/分组会同步未完成实例 |
| `internal/store/instances.go` | 实例查询（区间/成员/分组/状态）、发布、认领、放弃、完成、撤销、改期 |
| `internal/store/gen.go` | 循环生成器：启动补齐 + 每 10 分钟一次；daily/weekly/monthly（无此日取月末）；按 `(chore_id, due_date)` 幂等 |
| `internal/store/activity.go` | 活动日志追加 + 倒序查询（保留最近 2000 条） |
| `internal/store/inputs.go` | `ChoreInput` / `InstanceInput`（指针字段表达"不改"） |
| `internal/store/today.go` | `Today()`：按注入时区取"今天" |
| `internal/store/store_test.go` | 单测 9 个：daily/weekly/monthly、幂等、并发认领、完成/撤销、权限、落盘重开 |
| `internal/httpapi/api.go` | 全部 handler + 序列化视图（snake_case）+ 错误体 + Cookie 鉴权 + CORS |
| `internal/httpapi/routes.go` | 路由注册表 + `/api/` 兜底（未实现路径返回 JSON 404，不是 Go 的纯文本 404） |
| `internal/httpapi/files.go` | `/api/admin/export`（attachment 下载）、`/api/admin/seed` |
| `internal/seed/seed.go` | 演示数据：4 成员 / 2 分组 / 6 任务（每天×3、每周×2、每月×1），仅库空时执行 |
| `cmd/chorus/main.go` | 装配：时区、store、生成器协程、路由、静态资源与 SPA 回落、`/health` |
| `scripts/backend-check.mjs` | 后端自查脚本（Node 内置 fetch，27 项断言，B/C/E 组全覆盖） |
| `scripts/backend-verify.mjs` | 补充自查（G 组、未实现路径、静态资源、PATCH/DELETE 定义、单次发布） |

未碰 `web/`、未改 `api/*.md`、未加任何第三方依赖（`go.mod` 仍是 `module chores` + `go 1.24`）。

## 二、关键实现决定（契约没写死的部分）

1. **存储**：契约 DDL 是 SQLite，但硬性约束禁止第三方依赖。改用单文件 JSON + 原子写，字段与 DDL 一一对应（`members/groups/chores/chore_instances/activity/settings`）。文件里有 `version`（当前 2）。导出接口按契约名字仍叫 `chorus.db`。
2. **认领的 409 语义**：写锁内"检查 `state==open` → 改"，输的一方返回 `ErrConflict`，HTTP 层输出 `409` + `{"error":{...},"instance":{当前实例}}`（契约第 12 行）。
3. **`requires_claim` 语义**：`true` 时主按钮「认领」；`false` 时前端直接「完成」。后端的 `/claim` 对任意 `open` 实例都允许——**没有**按 `requires_claim` 拒绝认领（契约只约束前端按钮，见 CONTRACT.md 第 104 行）。`/complete` 对未认领实例也可直接完成，并把完成人同时写进 `claimed_by/claimed_at`（这样历史里"谁做的"永远有值），记一行决定在此。
4. **状态回退**：`/release` 与 `/uncomplete` 仅本人或管理员（否则 `403 FORBIDDEN`）；`/reschedule` 仅发布者或管理员。
5. **`defaultRange`**：按你补充的契约取并集 —— `min(今天-7, 本月 1 日) ~ max(今天+30, 本月最后一天)`。实测 `2026-09-18` 当天返回 `2026-09-01 ~ 2026-10-18`，周/月日历首屏够用。查询量：单次返回约 105 条实例，内存过滤，毫秒级；短期不会成为瓶颈。
6. **`bootstrap` 未登录**：返回 `me: null` + 成员/分组/实例，首屏登录页可以只调一次。另外多返回了 `today` / `is_admin` / `range`（超集，不影响前端）。
7. **时间**：全部 RFC3339 UTC；"今天"用注入时区（compose 的 `TZ=Asia/Shanghai`）。

## 三、修掉的两个真 bug（自查时抓到的）

1. **建任务不分配 id**：`CreateChore` 忘了写 `c.ID`，导致所有定义 `id=0`、幂等键 `(0, due_date)` 互相撞车 —— 只有第一条任务生成了实例。已修，单测 `TestDailyGeneratesEveryDay` 覆盖。
2. **认领死锁**：`Generate()` 取写锁后又调 `nowLocal()`（内部再取读锁），Go 直接 `fatal error: all goroutines are asleep - deadlock!`。已改为锁内直接读 `d.loc`。

## 四、实测（命令 + 结果）

```
docker exec chorus-dev sh -c "cd /app && go vet ./... && go test ./internal/..."
ok  chores/internal/store  0.441s        # 9 个单测全过

node scripts/backend-check.mjs            # 27 项
ALL PASS
```

关键输出摘录：

```
PASS B3 认领成功  state=claimed by=小明 at=2026-09-18T08:02:51Z
PASS B5 抢占返回 409 且带最新状态  state=claimed by=小明
PASS B5b 并发抢占 #102 只有一人成功  codes=200,409 winner=小明
PASS B6 完成写入谁/何时  by=小明 at=... note=已分好类
PASS B7b 别人不能撤销我的完成 -> 403
PASS C1 daily 每天都有实例  {"倒垃圾":31,"洗碗":31,"清猫砂":31}
PASS C3 weekly 只在周六  2026-09-19,2026-09-26,2026-10-03,...
PASS C3 monthly 只在 1 号  2026-10-01
PASS E4 导出文件  attachment; filename="chorus.db" bytes=129360
PASS 未实现 /api 返回 404 + 错误体  {"error":{"code":"NOT_FOUND","message":"接口不存在"}}
PASS SPA 回落 index.html

node scripts/backend-verify.mjs           # 15 项
ALL PASS
```

G 组：
- **G3/G4**：`docker restart chorus-dev` 后 `/health` 200，`data/chorus.db` 仍在（129 KB），实例数不变（生成幂等）。
- **G2**：`go build ./cmd/chorus` 干净通过（生产镜像用同一份源码）。
- 前端资源：`GET /` 200（572 B）、`/assets/app.js` 200（24,696 B）、`/assets/app.css` 200（14,597 B）、`/?tv=1` 200。
- 前端自带 UI 测试：`node test/ui-shim.mjs web/assets/app.js` → ALL PASS（注意它需要显式传路径参数，不传会 `ERR_INVALID_ARG_TYPE`）。

## 五、没做完 / 有疑问

1. **`/api/instances/{id}/reschedule`** 未做端到端手工验证（单测未覆盖），逻辑是"仅发布者或管理员可改期，改完仍受 `(chore_id, due_date)` 唯一性约束"——手工发布的实例 `chore_id=null`，没有唯一约束风险。
2. **`photo_id`**：`/complete` 接收但不落库（契约里没有照片存储的 DDL，未引入文件存储）。
3. **`settings` 表**：数据文件里保留了 `settings` 字段，但当前没有任何接口读写它（契约也没要求）。
4. **并发尺度**：数据量在万级实例以内没问题；如果以后要几十万条历史，建议换真正的 SQLite（那时需要放开"零第三方依赖"的约束）。
5. **dev-watch 的坑**：容器里 `pkill -f dev-watch` 会连自己那条 shell 一起杀（命令行匹配），导致容器只剩 PID 1 而没有服务进程。要重启服务请用 `docker restart chorus-dev`，别在容器里 pkill。

## 六、v2 收尾（时间模型 / 头像 / 权限 / 读接口）— 2026-09-18 晚

### 这轮涉及的文件

| 文件 | 一句话 |
|---|---|
| `internal/store/time.go` | 三选二解析 `ResolveTime`（给两个算第三个、三个都给以"开始+时长"为准、只给一个报契约原话、都不给默认 10 分钟）；**本轮新增 `ParseInstant`** |
| `internal/store/template.go` | 循环定义的时间模板：生成的实例按各自 `due_date` 平移**本地时刻** |
| `internal/store/avatar.go` | 头像存进数据文件（base64，不落磁盘单文件）、256 KB 上限、png/jpeg 白名单 |
| `internal/store/inputs.go` | `ParseTimeFields` 增加 `loc` 形参；`*fieldError` 增加 `Unwrap()` |
| `internal/httpapi/api_v2.go` | v2 的 HTTP 增量；`decodeInstanceTime/decodeChoreTime` 带服务器时区；**新增两个读接口** |
| `internal/httpapi/api.go` | 新增 `badInputMessage()` 剥掉 `bad input: ` 前缀；三处 `decode*Time(..., a.loc)` |
| `internal/httpapi/routes.go` | 注册 `GET /api/groups`、`GET /api/members` 与老前缀 `GET /api/admin/groups|members` 兼容别名 |
| `internal/store/compat_test.go` | 新增 3 个单测（见下"老数据兼容"） |
| `scripts/backend-check.mjs` | 口令默认值改 `0317`；E2 不再因登录失败抛 TypeError；**新增 v2 断言块 22 条** |
| `scripts/backend-v2-check.mjs` | 由 `test/v2-check.mjs` 改名（正式名字，与 backend-check/verify 并列，独立可跑） |

### 独立验收发现并修掉的两处契约偏离

1. **`400` 的 message 带了内部前缀。** 契约 v2 规定只给一个时间字段时消息必须是
   `时间需要给两个：开始/时长/结束`；实际返回 `bad input: 时间需要给两个：开始/时长/结束`
   （`store` 用 `%w` 包了 `ErrBadInput`，HTTP 层直接 `err.Error()`）。
   修法：`writeDomainError` 改走 `badInputMessage()` 剥前缀（所有 400 文案同时变干净），
   并给 `*fieldError` 加 `Unwrap() error { return ErrBadInput }`。
   顺带修掉一个潜在 500：`{"start_at": 123}`（非字符串）以前会落到 `default` 分支返回 500，现在是 400 + 干净文案。
   证据：临时探针（放在 `%TEMP%`，未入库、已删）修前 3 条 FAIL、修后 36/36；已固化成 `backend-check.mjs` 的断言。

2. **`/api/bootstrap` 的预填格式无法回传。** `time_defaults.start_at_local` 是 `"2026-09-18T17:59"`（无时区），
   前端把它原样 POST 回来会 400（`ParseRFC3339` 只认带时区的）。
   修法：新增 `store.ParseInstant(raw, loc)` —— 带时区按原样、不带时区按服务器时区解释；
   `ParseTimeFields` 增加 `loc` 形参，HTTP 层传 `a.loc`。
   证据：修前 400 `时间需为 RFC3339（如 …）`，修后 201；已固化成 `TestParseInstantAcceptsLocalPrefillFormat`。

### 主控点名的 3 个缺口

1. `scripts/backend-check.mjs` 写死旧口令 → 改为 `process.env.CHORES_ADMIN_TOKEN ?? '0317'`；
   `E2` 的 `r.json.member.id` 改成 `r.json?.member?.id` 并加 `typeof mid === 'number'`，
   失败时不再抛 TypeError 把后面的断言全吞掉。
2. `GET /api/groups` / `GET /api/members` 404 → 已补：登录成员可读（匿名 401），返回 `{"groups":[…]}` / `{"members":[…]}`
   （成员视图只输出契约字段，含 `avatar_url`，不暴露头像字节）；老前缀同名 GET 也补了管理员兼容别名。
   `test/integration.mjs` 因此从 46 passed / 2 failed 变 **48 passed / 0 failed**。
3. 库里的探针/测试数据 → 已重置（见下）。

### 老数据兼容（主控第 3 条）

重置前实测 `GET /api/bootstrap` → **200**，无时间的老实例三个字段都是 `null`：

```json
{"id":9,"chore_id":7,"title":"倒垃圾","note":"厨余和可回收分开","group_id":5,"group_name":"家务","due_date":"2026-09-01","state":"open","requires_claim":true,"requires_photo":false,"claimed_by":null,"claimed_at":null,"completed_by":null,"completed_at":null,"completion_note":null,"completed_by_name":null,"claimed_by_name":null,"start_at":null,"end_at":null,"duration_minutes":null}
```

- 落盘文件里 `"start_at": ""` 出现 **0** 次、`"duration_minutes": 0` 出现 **0** 次（`omitempty` + 指针，零值不落盘）。
- 已固化为单测 `TestLegacyInstanceWithoutTimesStaysNull`（读老 JSON → 零值；再 marshal → 三个键都不出现）。

### dev 库已重置为干净演示数据

做法（可复用）：`docker compose --profile dev stop` → 删 `data/chorus.db` → `start` →
`POST /api/admin/seed` body `{}`（口令 `0317`）。
结果：4 成员 / 2 分组 / 6 任务 / 160 条实例（`2026-09-01 ~ 2026-10-18`），
探针成员、`主控核对-*`、`v2自检*`、`集成验收-*` 等测试实例全部消失；
只读冒烟全 200：`/health`、`/api/bootstrap`、`GET /api/groups|members|instances|chores`。

**顺序提醒**：四个脚本都会往库里写数据（建成员/分组/实例）。要"库里干净"就先跑脚本、最后重置；
反过来会在库里留下测试数据。

### 现在怎么跑（全绿）

```
docker exec chorus-dev sh -c "cd /app && go vet ./... && go test -count=1 ./internal/..."   # ok（含 12 个单测）
node scripts/backend-check.mjs        # ALL PASS（含新增 v2 断言块）
node scripts/backend-verify.mjs       # ALL PASS
node scripts/backend-v2-check.mjs     # 40 passed, 0 failed
node test/integration.mjs             # 48 passed, 0 failed（主控的验收）
```

`gofmt -l ./cmd ./internal` 已清空（本轮统一格式化过一次）。

### 遗留

1. `prod-check`（127.0.0.1:2023）跑的还是 **v2 之前的镜像**；要用 v2 得
   `docker compose --profile prod-check up -d --build`（数据在命名卷里，不受影响）。
2. `photo_id` 仍只接收不落库；`settings` 仍无接口（契约未要求）。
3. 本仓库没有 git。我移走的 `test/integration-v2.mjs`（与 `integration.mjs`+`backend-v2-check.mjs` 高度重叠的一次性草稿）
   已备份到 `%TEMP%\chores-archive\integration-v2.mjs`，需要可直接取回。
