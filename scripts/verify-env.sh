#!/usr/bin/env bash
# 一键验证开发环境：构建后端与前端、启动服务、检查健康接口与目录初始化。
# 用法： make verify   或   bash scripts/verify-env.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/env.sh
source scripts/env.sh

PORT=18765
BASE_URL="http://127.0.0.1:${PORT}"
FAILED=0

step() { printf '\n\033[1;36m==> %s\033[0m\n' "$1"; }
ok()   { printf '  \033[32mPASS\033[0m %s\n' "$1"; }
bad()  { printf '  \033[31mFAIL\033[0m %s\n' "$1"; FAILED=1; }

step "工具链版本"
go version || bad "go 不可用"
node --version || bad "node 不可用"
npm --version || bad "npm 不可用"

step "Go 构建（缓存：${GOCACHE}）"
if go build -trimpath -o bin/iphoto ./cmd/app; then ok "go build"; else bad "go build"; fi

step "前端依赖与构建（缓存：${npm_config_cache}）"
if [ ! -d web/node_modules ]; then
  (cd web && npm install --no-audit --no-fund) || bad "npm install"
fi
if (cd web && npm run build); then ok "vite build"; else bad "vite build"; fi
# Vite 的 emptyOutDir 会删掉占位文件，重建以保证 go:embed 在空 dist 下也可编译。
touch web/dist/.gitkeep

step "重新构建二进制以嵌入前端产物"
if go build -trimpath -o bin/iphoto ./cmd/app; then ok "go build (embed)"; else bad "go build (embed)"; fi

if [ "$FAILED" -ne 0 ]; then
  printf '\n\033[31m环境验证失败，见上方输出。\033[0m\n'
  exit 1
fi

step "启动服务并检查 /api/health"
IPHOTO_BASE_DIR="$ROOT" ./bin/iphoto -no-browser >"$IPHOTO_CACHE_DIR/verify-server.log" 2>&1 &
SERVER_PID=$!
cleanup() {
  kill "$SERVER_PID" 2>/dev/null
  wait "$SERVER_PID" 2>/dev/null
}
trap cleanup EXIT

HEALTH=""
for _ in $(seq 1 50); do
  if HEALTH="$(curl -fsS --max-time 1 "$BASE_URL/api/health" 2>/dev/null)"; then
    break
  fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    break
  fi
  sleep 0.2
done

if [ -n "$HEALTH" ]; then
  ok "GET /api/health -> $HEALTH"
else
  bad "GET /api/health 无响应（端口 $PORT 可能被占用）"
  sed -n '1,40p' "$IPHOTO_CACHE_DIR/verify-server.log"
fi

step "目录初始化检查"
for d in library library/blobs runtime runtime/thumbs runtime/previews runtime/trash runtime/logs runtime/exports; do
  if [ -d "$ROOT/$d" ]; then ok "存在 $d/"; else bad "缺失 $d/"; fi
done
if [ -f "$ROOT/library/library.sqlite" ]; then ok "存在 library/library.sqlite"; else bad "缺失 library/library.sqlite"; fi

step "API 边界检查"
code="$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/api/not-implemented" || true)"
[ "$code" = "404" ] && ok "未知 /api 路径返回 404" || bad "未知 /api 路径返回 ${code}（期望 404）"

code="$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/settings" || true)"
[ "$code" = "200" ] && ok "SPA fallback /settings 返回 200" || bad "SPA fallback 返回 ${code}（期望 200）"

cleanup
trap - EXIT

if [ "$FAILED" -ne 0 ]; then
  printf '\n\033[31m环境验证失败。\033[0m\n'
  exit 1
fi
printf '\n\033[32m环境验证通过：后端 + 前端 + 数据库 + 静态资源均正常。\033[0m\n'
