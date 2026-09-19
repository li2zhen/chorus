# API 契约（v1，冻结）

> 除非先改这份文件，前后端不得各自发明字段。所有时间为 RFC3339 UTC 字符串，前端按 `GET /api/bootstrap` 返回的 `tz` 渲染本地时间。

## 约定

- 前缀：`/api`
- 请求/响应：`application/json; charset=utf-8`
- 认证：无密码。`POST /api/auth/login {member_id}` 成功后下发 HttpOnly Cookie `chores_member=<member_id>`（SameSite=Lax，有效期 365 天）。
- 管理员：`POST /api/admin/login {token}` 下发 Cookie `chores_admin=1`。`token` 取自环境变量 `CHORES_ADMIN_TOKEN`（默认 `admin`，首次启动日志会提示改）。
- 错误：非 2xx 一律 `{"error":{"code":"...","message":"..."}}`，code 用大写下划线（`NOT_FOUND`/`FORBIDDEN`/`BAD_REQUEST`/`CONFLICT`）。
- 并发安全：认领用条件更新，撞车返回 `409 CONFLICT`，body 里带当前 `instance`。

> 变更记录
> - 2026-09-18 明确 `/api/bootstrap` 的实例窗口必须覆盖整月（前端周/月视图依赖此约定，由前端侧提出）。
> - 2026-09-18 v3（用户实测反馈）：月视图需要 上/下月 切换；日视图需要 上/下一天 切换；日历格子改成**正方形**（现在 84px 高的狭长矩形）；月视图点开某天后，抽屉里点「认领/完成」必须**即时反馈**（现在要点两次）；**去掉独立「历史」分段**（历史 = 往月/往日导航）。新增「成员」分段：每人一行**个人密度图**（按天着色 + 件数/时长）。
> - 2026-09-18 v2：新增任务的`开始/时长/结束`三选二时间模型与 `avatar_url`/`avatar_data`；分组与成员新增开放给所有登录成员；新增 `member_stats`；视图分段由 日/周/月/历史 改为 **日/月/年 + 历史**。

### GET /api/bootstrap
一次拿齐渲染所需（首屏只调这一个）。
```json
{
  "now": "2026-09-18T03:20:00Z",
  "tz": "Asia/Shanghai",
  "me": { "id": 2, "name": "小王", "color": "#34C759", "avatar": "王", "avatar_url": null, "is_admin": false },
  "members": [
    { "id": 1, "name": "小明", "color": "#0A84FF", "avatar": "明", "avatar_url": "/api/avatars/1", "sort": 1 },
    { "id": 2, "name": "小王", "color": "#34C759", "avatar": "王", "avatar_url": null, "sort": 2 }
  ],
  "groups": [ { "id": 1, "name": "家务", "sort": 1, "member_ids": [] } ],
  "instances": [
    {
      "id": 41, "chore_id": 7, "title": "倒垃圾", "note": "厨余和可回收分开",
      "group_id": 1, "group_name": "家务",
      "due_date": "2026-09-18",
      "state": "open",
      "requires_claim": true,
      "requires_photo": false,
      "start_at": "2026-09-18T01:30:00Z", "end_at": "2026-09-18T01:40:00Z", "duration_minutes": 10,
      "claimed_by": null, "claimed_at": null,
      "completed_by": null, "completed_at": null, "completion_note": null,
      "completed_by_name": null, "claimed_by_name": null
    }
  ]
}
```
- `instances` 默认窗口：**今天往前 7 天 … 今天往后 30 天，且必须完整覆盖当前自然月**（三者取并集；即 `min(今天-7, 本月1日)` ~ `max(今天+30, 本月最后一天)`），这样周/月日历首屏就有数据、不需要额外请求。`?from=YYYY-MM-DD&to=YYYY-MM-DD` 可覆盖。
- `state` 只有三种：`open`（待认领）/ `claimed`（已认领）/ `done`（已完成）。不需要 `skipped`——跳过即"本轮不出现"。
- `member_stats`：**由前端从 `instances` 本地聚合**，不新增接口。口径：`completed_by` 非空的实例，按人统计 `count`（件数）与 `minutes`（各实例 `duration_minutes` 求和，缺省按 10 分钟）。`/api/bootstrap` 只需按上面的窗口返回实例即可。
- 三个时间字段都可为 `null`（老数据/未设置）；前端遇到 `duration_minutes` 为空按 10 分钟计。

### GET /api/instances?from=&to=&member=&group=
同上结构，用于周/月/历史视图与筛选。

### GET /api/chores
任务定义列表（含循环规则），用于「发布/管理」页。

## 动作

| 方法 | 路径 | body | 结果 |
|---|---|---|---|
| POST | `/api/instances` | `{title, note?, group_id?, due_date, member_id?, requires_claim}` | 立即发布一个实例，返回 instance |
| POST | `/api/chores` | `{title, note?, group_id?, recurrence:"none"\|"daily"\|"weekly"\|"monthly", weekday?, day_of_month?, due_time?, member_id?, requires_claim, requires_photo, start_date}` | 建定义 + 生成期内实例 |
| PATCH | `/api/chores/{id}` | 同上任意子集 | 更新定义；`recurrence` 变更只影响未来实例 |
| DELETE | `/api/chores/{id}` | — | 软删（`archived=1`），历史实例保留 |
| POST | `/api/instances/{id}/claim` | `{}` | open→claimed，写 `claimed_by/at`；抢占返回 409 |
| POST | `/api/instances/{id}/release` | `{}` | claimed→open（仅认领人本人或管理员），写 activity |
| POST | `/api/instances/{id}/complete` | `{note?, photo_id?}` | →done，写 `completed_by/at`；未认领也可直接完成（记完成人） |
| POST | `/api/instances/{id}/uncomplete` | `{}` | done→open，清空完成字段，写 activity |
| POST | `/api/instances/{id}/reschedule` | `{due_date}` | 改期（管理员或发布者） |

## 管理面 `/admin`（同一 SPA，需 `chores_admin` Cookie）

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/admin/login` | `{token}` |
| GET | `/api/admin/session` | `{ok:true}` |
| POST | `/api/admin/members` | `{name, color, avatar}` |
| PATCH | `/api/admin/members/{id}` | 改名字/颜色/头像/排序 |
| DELETE | `/api/admin/members/{id}` | 软删（`archived=1`），其历史保留 |
| POST | `/api/admin/groups` · PATCH/DELETE `/api/admin/groups/{id}` | 分组 CRUD |
| GET | `/api/admin/export` | 导出 `chorus.db` 文件（Content-Disposition attachment） |
| POST | `/api/admin/seed` | 写入演示数据（幂等，仅当库为空） |

## 静态资源

- `/` 与任意非 `/api/*` 路径 → 返回内嵌的 `index.html`（SPA）。
- `/assets/*` → 内嵌静态文件。
- `/health` → `{ "ok": true, "version": "..." }`

## 数据库（SQLite，WAL）

```sql
members(id INTEGER PK, name TEXT, color TEXT, avatar TEXT, is_admin INTEGER DEFAULT 0, sort INTEGER, archived INTEGER DEFAULT 0, created_at TEXT)
groups(id INTEGER PK, name TEXT, sort INTEGER, member_ids TEXT DEFAULT '[]', created_at TEXT)         -- member_ids: JSON 数组，空=全员
chores(id INTEGER PK, title TEXT, note TEXT, group_id INTEGER, recurrence TEXT, weekday INTEGER,
       day_of_month INTEGER, due_time TEXT, member_id INTEGER, requires_claim INTEGER DEFAULT 1,
       requires_photo INTEGER DEFAULT 0, start_date TEXT, archived INTEGER DEFAULT 0, created_at TEXT)
chore_instances(id INTEGER PK, chore_id INTEGER NULL, title TEXT, note TEXT, group_id INTEGER, due_date TEXT,
       state TEXT, claimed_by INTEGER, claimed_at TEXT, completed_by INTEGER, completed_at TEXT,
       completion_note TEXT, requires_claim INTEGER DEFAULT 1, requires_photo INTEGER DEFAULT 0,
       UNIQUE(chore_id, due_date))
activity(id INTEGER PK, at TEXT, actor_id INTEGER, instance_id INTEGER, action TEXT, detail TEXT)
settings(key TEXT PK, value TEXT)
```

循环生成规则（服务器本地时间 `tz` 判断"到期"）：
- `daily`：每天生成当天实例；`weekly`：`weekday` 匹配的当天；`monthly`：`day_of_month` 匹配（该月无此日则用月末）。
- 生成器每 10 分钟跑一次，且启动时先补齐从 `start_date` 到今天的所有实例；`UNIQUE(chore_id, due_date)` 保证幂等。
- 认领/完成前，未认领实例任何人都可认领；`requires_claim=1` 时前端主按钮是「认领」，`=0` 时直接是「完成」。
## v2 追加：时间模型、分组/成员权限、头像

### 任务时间（三选二）
`POST /api/instances` 与 `POST /api/chores` 都接受这三个字段，**任意两个**必须给全，第三个由服务端算：
```
duration_minutes = round((end_at - start_at) / 60)          // 给开始+结束 → 算时长
end_at           = start_at + duration_minutes              // 给开始+时长 → 算结束
start_at         = end_at   - duration_minutes              // 给结束+时长 → 算开始
```
- 三者都缺时用**默认**：`start_at = 请求时刻`、`duration_minutes = 10`、`end_at = start_at + 10min`。
- 只给一个 = `400 BAD_REQUEST`（消息：`时间需要给两个：开始/时长/结束`）。
- 校验：`duration_minutes` 为 1..1440 的整数；`end_at` 必须晚于 `start_at`（相等 → 400）。`start_at` 允许过去时间。
- `recurrence != "none"` 时，定义上的时间作为模板，生成的每个实例按各自 `due_date` 平移（同本地时刻）。
- **契约自带默认值**（前端表单不填也有意义）：
```json
"time_defaults": { "duration_minutes": 10, "start_at_local": "2026-09-18T16:30" }
```
放在 `/api/bootstrap` 顶层，时间为服务器本地（`tz`）当前时刻，供前端预填。

### 分组与成员：不再需要管理员
- `GET /api/groups` → `{"groups":[…]}`；`GET /api/members` → `{"members":[…]}`（含 `avatar_url`，不吐头像字节）。任意已登录成员可读。
- `POST/PATCH/DELETE /api/groups`（**新路径，去掉 admin 前缀**）：任意**已登录成员**可增删改分组。语义同原 `/api/admin/groups*`。
- `POST /api/members`、`PATCH /api/members/{id}`：任意已登录成员可**新增成员、改成员（含头像/颜色/名字）**。
- 仍然只有管理员能做：`DELETE /api/members/{id}`（软删）、`/api/admin/export`、`/api/admin/seed`。
- 老的 `/api/admin/groups*` 与 `/api/admin/members*` **保留可用**（向后兼容），内部转发到同一实现。

### 头像上传
- `PUT /api/members/{id}/avatar`，body：`{ "content_type": "image/png"|"image/jpeg", "data_base64": "..." }`
  - 上限：解码后 **256 KB**，超限 `413`；类型不在白名单 `415`；**不落磁盘文件**，直接存进数据文件（家庭用量足够）。
  - 成功后成员视图的 `avatar_url` 变成 `/api/avatars/{id}`（带 `?v=<updatedAt>` 便于破缓存）。
- `GET /api/avatars/{id}` → 直接返回图片字节 + 正确 `Content-Type` + `Cache-Control: private, max-age=86400`；无头像返回 404。
- `DELETE /api/members/{id}/avatar` → 清空头像。
- 前端负责把用户选的图**缩放到最长边 256px** 再上传（`canvas` 压缩到 JPEG 质量 0.8），这样基本不会碰到上限。

### 视图分段（前端）
`今日 / 本月 / 本年` 三个分段 + 「历史」入口；**不再有「本周」**。月视图 = 日历网格（7 列、状态点、点格出抽屉）；年视图 = 12 个小月历 + 每格完成率着色。
## v3 追加：导航、格子形状、抽屉即时反馈、成员密度图

### 分段（最终形态）
```
今日(D)   本月(M)   本年(Y)   成员(P)
```
- **不再有独立「历史」分段**：看历史 = 在日视图往回翻天、在月视图往回翻月。
- 分段切换保留各自游标：`dayCursor ('YYYY-MM-DD')`、`monthCursor ('YYYY-MM')`、`yearCursor (YYYY)`。
- 点分段 = 跳到**今天/本月/本年**（不做"回到上次游标"，符合直觉）；年视图点某月 → 跳该月；月视图点某天 → 打开当天抽屉。

### 导航（此前完全缺失）
- 日视图：标题行 `‹  9月18日 星期五  ›`，左右箭头切换上/下一天；**左右滑动**同效；到"今天"时右侧箭头回到今天态（标题可点回今天）。
- 月视图：标题行 `‹  2026年9月  ›`，箭头切换上/下月；「今天」文字按钮跳回本月。
- 年视图：`‹ 2026 年 ›` 切换年。
- 导航**不需要**重新请求接口：`/api/bootstrap` 的窗口（今天-7 … 今天+30，且覆盖整月）之外若落在窗口外，前端自行调一次 `GET /api/instances?from=&to=` 增量补齐并合并进 `state.instances`（去重按 id）。契约确认 `GET /api/instances` 已支持该查询。

### 日历格子
- 正方形：`aspect-ratio: 1 / 1`（外层格子容器也要吃满列宽），最小 44px 触控；副信息（状态点/角标）放在格内底部。
- 显示密度：格内最多 3 个状态点（open 灰 / claimed 橙 / done 绿），超过 3 个显示 `+N` 小字。

### 抽屉即时反馈（真 bug）
- 现象：月视图点开某天 → 点「认领」→ 界面没变化，再点一次才提示"已认领"。
- 根因：`openDaySheet()` 里对 `render` 打补丁后立刻用 `setTimeout(...,0)` 还原，抽屉内的重绘钩子随即失效。
- 要求：**动作发出后立刻重绘抽屉与日历**（乐观更新），失败再回滚并 toast；抽屉开着时主列表与日历同步更新。

### 成员密度图（新）
- 「成员」分段：每个成员一张卡（头像 + 名字 + 本期 `N 件 · M 分钟`），卡内是一条**按天的密度行/网格**：
  - 着色 = 该成员当天完成件数：0 空、1 浅、2 中、≥3 实（四级，与年视图一致）；
  - 时间范围默认跟随当前"本期"（日视图=当天附近 30 天、月视图=本月、年视图=本年）；
  - 点某个格子 → 打开那天抽屉（复用同一个抽屉）。
- 数据全部来自已拉取的 `instances`（`completed_by` + `completed_at`），不新增接口；`duration_minutes` 为空按 10 分钟计。
