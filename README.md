# Chorus · 家务认领板

> 家里的事，谁认领谁负责。

一个跑在 Docker 里的家庭任务板：**发布 → 认领 → 完成**，每一步都记下是谁、在什么时候做的。单容器，数据就是一个文件。

- 没有密码：点自己的头像就进去
- 任务先扔进池子，谁有空谁认领；两个人同时抢，只有一个能抢到
- 循环任务（每天 / 每周 / 每月）到期自动出现在池子里
- 日 / 月 / 年三档日历，还有按人统计的「谁干得多」
- 挂在厨房平板上当大屏：加 `/?tv=1`

---

## 跑起来

```bash
docker run -d --name chorus --restart unless-stopped \
  -p 2022:2022 \
  -v chorus-data:/data \
  -e CHORUS_ADMIN_TOKEN=自己想一个口令 \
  -e TZ=Asia/Shanghai \
  li2zhen/chorus:latest
```

打开 http://localhost:2022 ，这时还没有成员，点「管理」用刚设的口令进 `/admin` 把家里人加进去，回首页点头像进。

想先看看长什么样，就在 `/admin` 点「写入演示数据」，看完点「清空数据」。

用 compose 的话：

```yaml
services:
  chorus:
    image: li2zhen/chorus:latest
    container_name: chorus
    restart: unless-stopped
    ports:
      - "2022:2022"
    volumes:
      - ./data:/data
    environment:
      - CHORUS_ADMIN_TOKEN=自己想一个口令
      - TZ=Asia/Shanghai
```

> 挂宿主目录时注意属主：镜像里以 uid 10001 运行，宿主目录不是它的话写不进去，用命名卷最省事。

---

## 怎么用

| 页面 | 说明 |
|---|---|
| **今日** | 一行一件事：状态点 · 任务 · 谁 + 几点。左右滑动翻天，点标题回今天 |
| **本月** | 方格日历，今天高亮；点某天弹出当天的事，可以直接在那里认领或完成 |
| **本年** | 12 个小月历，颜色深浅 = 那天做完了几件；点某月进去 |
| **成员** | 每人一条按天的带子，一眼看出谁最近哪天干得多，外加「N 件 · M 分钟」 |

发布任务只有四个输入：任务名、分组、重复、谁做；时间可以只填两个（开始 / 时长 / 结束），第三个自动算出来，默认是现在 + 10 分钟。

**认领和完成是两件事。** 认领之后别人不会再来抢，做完点完成才落记录；点错了能撤销。

---

## 源码长什么样

```
app/                 前端资源（index.html + app.js + app.css + icon.svg），go:embed 进二进制
cmd/chorus/          入口：路由、静态资源、SPA 回落、/health
internal/store/      数据层：成员 / 分组 / 任务定义 / 实例 / 活动日志 / 循环生成
internal/httpapi/    REST 接口、Cookie 登录、导入导出
deploy/Dockerfile    生产镜像（多阶段构建，给从源码构建的人）
docker-compose.yml   拿现成镜像跑起来的那份
docs/                更新记录与部署手册
```

想自己构建镜像（宿主机不用装 Go）：

```bash
docker build -f deploy/Dockerfile --target runtime -t chorus:local .
```

`dev/` 是本地开发目录，**不在仓库里**：里面有热重载用的 compose、Go 工具链包、接口契约、验收脚本。项目的接口契约与验收记录也随之放在那里，不随用户下载的代码发布。
## 备份

整个数据库就是一个文件。

```bash
docker run --rm -v chorus-data:/data -v "$PWD:/out" alpine cp /data/chorus.db /out/backup.db
```

也可以在 `/admin` 里点「导出数据库」。恢复就是停掉容器、把文件放回去、再启动。

---

## 几个刻意这么做的选择

- **不用 SQLite。** 整个后端只用 Go 标准库，不引第三方依赖，所以数据层是「一个 JSON 文件 + 读写锁 + 原子写」。上万条任务没问题；真到几十万条历史再换。
- **前端没有构建步骤。** 原生 ES 模块 + 一份 CSS，没有 npm、没有打包器，也没有外部字体和图标库。改完刷新就能看到。
- **没有鉴权。** 这东西是给家里局域网的，登录只回答「你是谁」。要放到公网，前面自己加一层 Basic Auth 或 VPN。删成员、导出、清空数据仍然要管理员口令。
- **构建不依赖 Docker Hub。** 基础镜像用 alpine，Go 工具链从 `vendor/` 里的包解出来（README 下面有下载命令，也能用 `--build-arg CHORUS_GO_MIRROR` 换源）。

## 常见问题

**点认领时提示「刚被别人认领」？** 那就是有人先下手了，界面会自动显示最新状态。
**循环任务没有更早的记录？** 生成器只补到本月 1 日；想让历史更长，把任务定义的 `start_date` 往前调。
**能两台机器同时用吗？** 可以，但同一时间只让一台写这个数据文件。

## 许可

MIT。

## 致谢

这个项目是在 [DSH（DeepSeek Harness）](https://github.com/deepseek-ai) 里、由我一个编码智能体做出来的：我读代码、写前后端、跑验收、发现 bug 再修，人只负责拍板和提需求。
交互上参考了 [Donetick](https://github.com/donetick/donetick) 和 [Grocy](https://github.com/grocy/grocy) 的家务模块，成员密度带的样式来自 GitHub 的贡献图。