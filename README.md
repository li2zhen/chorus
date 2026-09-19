# Chorus · 家务认领板

> 家里的事，谁认领谁负责。一个单文件、单容器、零依赖的家庭任务板。

Chorus 只做一件事：**把家务发布到池子里，谁有空谁认领，做完留痕。**
「待认领 → 已认领 → 已完成」是三个真实状态，每一步都记录**是谁、在什么时候**做的。

- **无密码登录** —— 点头像就进，家里不需要账号体系
- **发布 → 认领 → 完成** —— 两段状态各自留时间戳；抢同一个任务只有一个赢家，另一个收到 409 并自动看到最新状态
- **日历** —— 日 / 月 / 年三档；方格日历 + 状态点；翻出已加载范围会自动补数据
- **成员密度图** —— 每个成员一条按天铺开的贡献带（GitHub 风格），一眼看出谁最近哪天干得多
- **统计** —— 每人「N 件 · M 分钟」，日 / 月 / 年三种口径
- **循环任务** —— 每天 / 每周 / 每月到期自动进池，幂等、不重复、不缺失
- **大屏模式** —— `/?tv=1` 黑底大字，挂在厨房或客厅的旧平板上
- **单容器 + 单文件数据库** —— 拷贝数据文件就是备份

---

## 快速开始

```bash
git clone https://github.com/<你的账号>/chorus.git && cd chorus
docker compose --profile prod up -d --build     # 生产模式，镜像约 24 MB
# 打开 http://localhost:2022
```

首次打开是登录页但还没有成员：点左下角**管理** → 用管理员口令进 `/admin` → 添加家庭成员 → 回首页点头像进入。

管理员口令默认 `0317`（写在 `docker-compose.yml` 的 `CHORUS_ADMIN_TOKEN`，**上线前请改掉**）。

想先看效果：进 `/admin` 点「写入演示数据」（4 个成员 / 2 个分组 / 6 个任务 / 整月实例），看完点「清空数据」（对应 `POST /api/admin/reset`，幂等、不删数据文件）。

---

## 界面

| 分段 | 内容 |
|---|---|
| **今日** | 按分组的任务行：状态点 · 标题 · 谁 + 时刻；左右滑动翻天，点标题回今天 |
| **本月** | 方格日历（7 列，今天高亮，格内状态点，超过 3 个显示 +N）；箭头翻月；点某天出抽屉，抽屉里直接认领/完成 |
| **本年** | 12 个小月历，按当天完成情况四级着色；点某月跳进该月 |
| **成员** | 每人一行：头像 + 姓名 + 本期「N 件 · M 分钟」+ 按天密度带（0 空 / 1 浅 / 2 中 / ≥3 实） |
| **大屏** | `/?tv=1`：黑底大字，只列今天未完成 + 认领人，60 秒自动刷新 |

设计原则：**层级靠字重与间距，不靠边框和颜色；动效只有淡入与 ≤8px 位移；文案不超过一行。**

---

## 开发

开发环境也跑在容器里，宿主机只要 Docker（**不需要装 Go**）：

```bash
docker compose --profile dev up -d --build
# 改 internal/** 或 web/** 下的文件，容器内约 2 秒自动重编译重启
docker compose --profile dev logs -f
```

| 场景 | 命令 | 端口 |
|---|---|---|
| 开发（热重载） | `docker compose --profile dev up -d --build` | 127.0.0.1:2022 |
| 生产镜像 | `docker compose --profile prod up -d --build` | 0.0.0.0:2022 |
| 只验证生产镜像 | `docker compose --profile prod-check up -d --build` | 127.0.0.1:2023 |
| 停 | `docker compose --profile dev down` | |
| 停并删干净 | `docker compose --profile dev down -v --rmi local` | |

> ⚠️ 容器里不要 `pkill -f dev-watch`（会连自己那条 shell 一起杀）。重启用 `docker restart chorus-dev`。

### 为什么前端没有构建步骤

前端是**原生 ES 模块 + 一份 CSS**：没有 npm、没有打包器、没有外部字体和图标库，`go:embed` 直接嵌进二进制。
项目小到不需要框架；少一层构建就少一层会坏的地方，最终产物只有一个可执行文件。

### 离线构建（国内网络友好）

构建**不依赖 Docker Hub 拉 `golang` 镜像**：基础镜像用 `alpine`，Go 工具链从构建上下文里的 `vendor/go.linux-amd64.tar.gz` 解出来。
克隆后如果缺少这个文件：

```powershell
Invoke-WebRequest -Uri "https://mirrors.aliyun.com/golang/go1.24.6.linux-amd64.tar.gz" -OutFile "vendor\go.linux-amd64.tar.gz"
```

或者换源构建：`docker build --build-arg CHORUS_GO_MIRROR=<tar.gz 地址> --target runtime .`

---

## 配置

| 环境变量 | 默认 | 说明 |
|---|---|---|
| `CHORUS_ADDR` | `:2022` | 监听地址 |
| `CHORUS_DB` | `/data/chorus.db` | 数据文件路径 |
| `CHORUS_ADMIN_TOKEN` | 镜像内 `admin`；compose 里 `0317` | `/admin` 口令（老名字 `CHORES_*` 仍兼容，优先读 `CHORUS_*`） |
| `TZ` | `Asia/Shanghai` | 循环任务按此时区判断「今天」 |

**备份**：整个数据库就是一个文件，拷走即备份；界面 `/admin → 导出数据库` 也行。

```bash
# 生产用的是命名卷 chorus_prod-data
docker run --rm -v chorus_prod-data:/data -v "$PWD:/out" alpine cp /data/chorus.db /out/chorus-backup.db
```

---

## 部署

### NAS / 家用服务器（推荐）

```yaml
services:
  chorus:
    image: <你的dockerhub账号>/chorus:latest
    container_name: chorus
    restart: unless-stopped
    ports:
      - "2022:2022"
    volumes:
      - ./data:/data
    environment:
      - CHORUS_ADMIN_TOKEN=改成你自己的口令
      - TZ=Asia/Shanghai
```

- 宿主目录属主不是 root 时，非 root 运行的生产镜像可能写不了数据文件：用命名卷（默认）或 `chmod 777 ./data`。
- 不要挂 `./config:/config`（本项目不读 config 目录，但很多同类项目的 compose 模板里有这一行，会踩坑）。

### 手机 / 局域网

生产模式默认绑 `0.0.0.0:2022`，手机连 `http://<主机IP>:2022` 即可。

### 反向代理

- 静态资源与 API 同源，无需额外配置；`/` 与任意非 `/api/*` 路径回落 `index.html`
- 健康检查：`GET /health` → `{"ok":true,"version":"..."}`
- 数据文件放独立卷，容器重建不丢

> ⚠️ **Chorus 没有鉴权**（家用取舍）。暴露到公网前请自行加一层：反代 Basic Auth / VPN / Cloudflare Access。

---

## 架构

```
cmd/chorus/main.go     入口：路由、静态资源、SPA 回落、/health
internal/store/        数据层：单文件 JSON + RWMutex + 原子写（temp+rename）
                       成员 / 分组 / 任务定义 / 实例 / 活动日志 / 循环生成器
internal/httpapi/      REST handler、Cookie 鉴权、统一错误体、导入导出
internal/seed/         演示数据
web/                   index.html + assets/{app.js,app.css,icon.svg}（go:embed）
api/CONTRACT.md        接口契约（先改文档再改代码）
api/DESIGN.md          设计 token 与页面规格
test/ACCEPTANCE.md     验收清单（每条都有实测记录）
docker-compose.yml     dev / prod / prod-check 三个 profile
```

**刻意的技术取舍**：

- **零第三方 Go 依赖**：只用标准库，所以数据层是「单文件 JSON + 锁 + 原子写」而不是 SQLite —— 万级实例以内完全够用，真要几十万条历史再换。
- **前端零构建**：原生 ES 模块，没有 npm / 打包器 / CDN / 外部字体。
- **无密码**：登录只回答「你是谁」，不回答「你能不能进」；删除成员、导出数据仍需要管理员口令。
- **认领用条件更新**：撞车返回 409 并把当前状态带回来，前端乐观更新后回滚，不会出现两个人同时拥有一件事。

---

## 常见问题

**Q：点「认领」提示「刚被别人认领」？**
A：服务端保证只有一个赢家，前端会立刻显示最新状态——这是设计，不是错误。

**Q：循环任务为什么没有更早的历史？**
A：生成器会补齐**当前自然月 1 日**到今天；更早的历史不会凭空生成。把任务定义的 `start_date` 往前调即可。

**Q：能多台机器同时用吗？**
A：可以，但同一时间只建议**一台机器写**这个数据文件（单文件 + 进程内锁，跨机器不安全）。

**Q：为什么不用 SQLite？**
A：为了零第三方依赖（纯标准库、一个二进制）。数据量大到 JSON 撑不住时会换。

---

## 许可

MIT，见 [LICENSE](LICENSE)。

## 致谢

交互与信息架构受 [Donetick](https://github.com/donetick/donetick)、[Grocy](https://github.com/grocy/grocy) 的家务模块以及 GitHub 贡献图启发；最终形态是为「发布 → 认领 → 完成 + 谁做的」这条链路重写的。