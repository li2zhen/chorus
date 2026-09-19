# 部署手册

镜像：[`li2zhen/chorus`](https://hub.docker.com/r/li2zhen/chorus)　仓库：[github.com/li2zhen/chorus](https://github.com/li2zhen/chorus)

## 1. Docker Compose（推荐）

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
      - CHORUS_ADMIN_TOKEN=改成你自己的口令
      - TZ=Asia/Shanghai
```

```bash
docker compose up -d
curl -s localhost:2022/health     # {"ok":true,"version":"..."}
```

## 2. docker run

```bash
docker run -d --name chorus --restart unless-stopped \
  -p 2022:2022 \
  -v chorus-data:/data \
  -e CHORUS_ADMIN_TOKEN=你的口令 \
  -e TZ=Asia/Shanghai \
  li2zhen/chorus:latest
```

数据在命名卷 `chorus-data` 里，容器重建不丢。

## 3. 从源码构建（宿主机不需要 Go）

```bash
git clone https://github.com/li2zhen/chorus.git && cd chorus
# 若缺 vendor/go.linux-amd64.tar.gz，按 README「离线构建」一节下载
docker compose --profile prod up -d --build
```

换 Go 工具链来源：

```bash
docker build --build-arg CHORUS_GO_MIRROR=https://golang.google.cn/dl/go1.24.6.linux-amd64.tar.gz \
  --target runtime -t chorus:local .
```

## 4. 反向代理（Nginx 片段）

```nginx
location / {
    proxy_pass http://127.0.0.1:2022;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_http_version 1.1;
}
```

同源部署，不需要额外配置 CORS。

## 5. 备份与恢复

```bash
# 备份（容器在跑也行）
docker exec chorus sh -c 'wget -qO- http://127.0.0.1:2022/api/admin/export' > chorus-$(date +%F).db
# 或直接拷文件
docker run --rm -v chorus-data:/data -v "$PWD:/out" alpine cp /data/chorus.db /out/backup.db

# 恢复：停容器 → 覆盖文件 → 起容器
docker stop chorus
docker run --rm -v chorus-data:/data -v "$PWD:/out" alpine cp /out/backup.db /data/chorus.db
docker start chorus
```

## 6. 清空数据（回到刚安装）

```bash
curl -X POST -b "chores_admin=1" localhost:2022/api/admin/reset     # 幂等，不删数据文件
```

或直接进 `/admin` 点「清空数据」。

## 7. 安全

- 本项目**没有鉴权**，设计前提是「跑在家里局域网」。暴露公网前必须加一层：反代 Basic Auth、VPN、Cloudflare Access 等。
- `CHORUS_ADMIN_TOKEN` 决定谁能删成员 / 导出数据 / 清空数据，请改掉默认值。
- 数据文件里存着成员头像（base64）与全部任务历史，注意备份文件的权限。
