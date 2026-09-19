# 生产镜像：多阶段构建，宿主机不需要装 Go。
# 基础镜像只用 alpine；Go 工具链来自构建上下文里的工具链包
# （默认 dev/vendor/go.linux-amd64.tar.gz，见 README 的「离线构建」）。
FROM alpine:3.20 AS build
# 换源/换版本：docker build --build-arg CHORUS_GO_MIRROR=https://golang.google.cn/dl/go1.24.6.linux-amd64.tar.gz .
ARG CHORUS_GO_MIRROR=""
RUN apk add --no-cache bash curl ca-certificates
COPY dev/vendor/ /tmp/vendor/
RUN set -eu; \
    mirror="${CHORUS_GO_MIRROR:-https://mirrors.aliyun.com/golang/go1.24.6.linux-amd64.tar.gz}"; \
    if [ -s /tmp/vendor/go.linux-amd64.tar.gz ]; then cp /tmp/vendor/go.linux-amd64.tar.gz /tmp/go.tgz; \
    else curl -fsSL "$mirror" -o /tmp/go.tgz; fi; \
    tar -C /usr/local -xzf /tmp/go.tgz && rm -rf /tmp/go.tgz /tmp/vendor
ENV PATH=/usr/local/go/bin:$PATH CGO_ENABLED=0 GOFLAGS=-mod=mod GOTOOLCHAIN=local
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY app ./app
# 交叉编译：buildx 按目标平台注入这两个参数，这样 arm64 不需要在 QEMU 里跑 Go 工具链。
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/chorus ./cmd/chorus

FROM alpine:3.20 AS runtime
RUN apk add --no-cache tzdata ca-certificates \
 && addgroup -g 10001 -S app \
 && adduser -u 10001 -S -G app -h /app -s /sbin/nologin app
WORKDIR /app
COPY --from=build /out/chorus /app/chorus
# /data 的属主会被"命名卷首次创建"继承，这样非 root 用户也能写。
RUN mkdir -p /data && chown -R app:app /data
USER app
EXPOSE 2022
VOLUME ["/data"]
ENV CHORUS_ADDR=":2022" CHORUS_DB="/data/chorus.db"
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s CMD wget -qO- http://127.0.0.1:2022/health || exit 1
ENTRYPOINT ["/app/chorus"]