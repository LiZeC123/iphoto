#!/usr/bin/env bash
# iphoto 开发环境变量
#
# 背景：当前开发会话的 DSH 沙箱策略为 workspace-write，只允许写工作区
# (/Users/lucas/WorkSpace/DeepseekSpace)。Go / npm 默认缓存位于 $HOME，
# 写入会被拒绝（operation not permitted）。因此这里把全部工具链缓存
# 重定向到项目内的 .cache/，保证构建可复现且不污染系统目录。
#
# 用法： source scripts/env.sh
set -u

IPHOTO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export IPHOTO_ROOT
export IPHOTO_CACHE_DIR="${IPHOTO_CACHE_DIR:-$IPHOTO_ROOT/.cache}"

# Go：模块缓存、构建缓存、校验和数据库缓存、临时目录全部落在 .cache 内
export GOPATH="$IPHOTO_CACHE_DIR/gopath"
export GOMODCACHE="$GOPATH/pkg/mod"
export GOCACHE="$IPHOTO_CACHE_DIR/go-build"
export GOTMPDIR="$IPHOTO_CACHE_DIR/tmp"
export GOTELEMETRY=off

# npm：缓存与日志目录（默认 ~/.npm，不可写）
export npm_config_cache="$IPHOTO_CACHE_DIR/npm"
export npm_config_update_notifier=false
export npm_config_fund=false

mkdir -p "$GOPATH" "$GOCACHE" "$GOTMPDIR" "$npm_config_cache"

if [ "${IPHOTO_ENV_VERBOSE:-0}" = "1" ]; then
  echo "IPHOTO_ROOT      = $IPHOTO_ROOT"
  echo "IPHOTO_CACHE_DIR = $IPHOTO_CACHE_DIR"
  echo "GOPATH           = $GOPATH"
  echo "GOCACHE          = $GOCACHE"
  echo "npm_config_cache = $npm_config_cache"
fi
