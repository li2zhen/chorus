#!/usr/bin/env bash
# 开发热重载：源码有变化就重新编译并重启进程。不依赖 air，不联网。
# 用"内容指纹"而不是时间戳，避免改完正好落在两次轮询之间被漏掉。
set -u
BIN=/tmp/chorus-dev
PID=""
cd /app

fingerprint() {
  find /app/cmd /app/internal /app/web -type f \
    \( -name '*.go' -o -name '*.html' -o -name '*.css' -o -name '*.js' -o -name '*.svg' \) \
    -exec md5sum {} + 2>/dev/null | sort | md5sum | awk '{print $1}'
}

restart() {
  if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
    kill "$PID" 2>/dev/null
    wait "$PID" 2>/dev/null
  fi
  "$BIN" &
  PID=$!
  echo "[dev] running pid=$PID"
}

LAST="$(fingerprint)"
echo "[dev] initial build…"
if go build -o "$BIN" ./cmd/chorus; then restart; else echo "[dev] initial build failed"; fi

while true; do
  sleep 2
  NOW="$(fingerprint)"
  if [ "$NOW" != "$LAST" ]; then
    LAST="$NOW"
    echo "[dev] change detected, rebuilding…"
    if go build -o "$BIN" ./cmd/chorus; then restart; else echo "[dev] build failed, keeping old binary"; fi
  fi
done
